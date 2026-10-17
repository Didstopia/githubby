package metadata

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v86/github"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/github"
)

func serverError() httpmock.Responder {
	return httpmock.NewJsonResponderOrPanic(500, map[string]any{"message": "oops"})
}

// A server error on any endpoint must fail the export and name the file, never
// leave a quiet gap that verify would take for a complete export.
func TestExportRepo_EveryEndpointFailureIsReported(t *testing.T) {
	cases := []struct{ path, file string }{
		{"", "repo.json"},
		{"/issues", "issues.json"},
		{"/issues/comments", "issues.json"},
		{"/pulls", "pulls.json"},
		{"/pulls/comments", "pulls.json"},
		{"/pulls/3/reviews", "reviews of #3"},
		{"/releases", "releases.json"},
		{"/releases/assets/5", "app.bin"},
		{"/labels", "labels.json"},
		{"/milestones", "milestones.json"},
		{"/topics", "topics.json"},
		{"/branches", "branch-protection.json"},
		{"/branches/main/protection", "branch main"},
		{"/hooks", "hooks.json"},
		{"/collaborators", "collaborators.json"},
		{"/keys", "deploy-keys.json"},
		{"/actions/variables", "actions-variables.json"},
		{"/actions/secrets", "actions-secrets.json"},
		{"/rulesets", "rulesets.json"},
		{"/rulesets/42", "ruleset main"},
		{"/environments", "environments.json"},
	}
	for _, c := range cases {
		t.Run(c.file+c.path, func(t *testing.T) {
			e, _, _ := newTestExporter(t)
			registerRepo()
			httpmock.RegisterResponder("GET", api+c.path, serverError())

			rep := NewReport()
			e.ExportRepo(context.Background(), testRepo(false), rep)
			require.Contains(t, rep.Failed, "owner/repo")
			assert.Contains(t, rep.Failed["owner/repo"].Error(), c.file)
			assert.Empty(t, rep.Exported, "a failed repo is not listed as exported")
		})
	}
}

func TestExportOrg_EveryEndpointFailureIsReported(t *testing.T) {
	cases := []struct{ path, file string }{
		{"", "org.json"},
		{"/actions/variables", "actions-variables.json"},
		{"/actions/secrets", "actions-secrets.json"},
		{"/members", "members.json"},
		{"/teams", "teams.json"},
		{"/rulesets", "rulesets.json"},
		{"/rulesets/7", "ruleset all"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			e, _, _ := newTestExporter(t)
			registerOrg("o")
			httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
				"organization(login": {`{"data":{"organization":{"projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}`},
			}))
			httpmock.RegisterResponder("GET", "https://api.github.com/orgs/o"+c.path, serverError())

			rep := NewReport()
			e.ExportOrg(context.Background(), "o", rep)
			require.Contains(t, rep.Failed, "org o")
			assert.Contains(t, rep.Failed["org o"].Error(), c.file)
		})
	}
}

func TestExportRepo_VerbosePrintsFilesAndWaits(t *testing.T) {
	e, _, waits := newTestExporter(t)
	e.Verbose = true
	registerRepo()
	calls := 0
	httpmock.RegisterResponder("GET", api+"/labels", func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := httpmock.NewStringResponse(403, `{"message":"API rate limit exceeded"}`)
			resp.Header.Set("X-RateLimit-Limit", "5000")
			resp.Header.Set("X-RateLimit-Remaining", "0")
			resp.Header.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10))
			return resp, nil
		}
		return httpmock.NewJsonResponse(200, []map[string]any{})
	})

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(false), rep)
	assert.Empty(t, rep.Failed)
	assert.Len(t, *waits, 1)
}

func TestCall_VerboseWaitAndSleepFailure(t *testing.T) {
	e := New(gh.NewClient(nil), nil, t.TempDir())
	e.Verbose = true
	e.sleep = func(ctx context.Context, d time.Duration) error { return errors.New("woke up cancelled") }

	err := e.call(context.Background(), func() error { return &gh.AbuseRateLimitError{} })
	assert.ErrorContains(t, err, "woke up cancelled")
}

func TestAccessDetail(t *testing.T) {
	assert.Equal(t, "NOT_FOUND", accessDetail(&github.GraphQLError{Errors: []github.GraphQLErrorItem{{Type: "NOT_FOUND", Message: "m"}}}))
	assert.Equal(t, "just a message", accessDetail(&github.GraphQLError{Errors: []github.GraphQLErrorItem{{Message: "just a message"}}}))
	assert.Equal(t, "plain", accessDetail(errors.New("plain")))
}

func TestDownloadAsset_FileSystemFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}

	t.Run("folder cannot be created", func(t *testing.T) {
		e, target, _ := newTestExporter(t)
		registerRepo()
		dir := RepoDir(target, "owner", "repo")
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "releases"), nil, 0644)) // a file where the folder goes

		rep := NewReport()
		e.ExportRepo(context.Background(), testRepo(false), rep)
		require.Contains(t, rep.Failed, "owner/repo")
		assert.Contains(t, rep.Failed["owner/repo"].Error(), "app.bin")
	})

	t.Run("temp file cannot be created", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows ignores read-only bits on directories")
		}
		e, target, _ := newTestExporter(t)
		registerRepo()
		assets := filepath.Join(RepoDir(target, "owner", "repo"), "releases", "v1_beta")
		require.NoError(t, os.MkdirAll(assets, 0755))
		require.NoError(t, os.Chmod(assets, 0500))       //nolint:gosec // read-only on purpose
		t.Cleanup(func() { _ = os.Chmod(assets, 0755) }) //nolint:gosec // restore for cleanup

		rep := NewReport()
		e.ExportRepo(context.Background(), testRepo(false), rep)
		require.Contains(t, rep.Failed, "owner/repo")
		assert.Contains(t, rep.Failed["owner/repo"].Error(), "app.bin")
	})
}

func TestExportRepo_ReportFileCannotBeWritten(t *testing.T) {
	e, target, _ := newTestExporter(t)
	registerRepo()
	dir := RepoDir(target, "owner", "repo")
	// A folder where issues.json goes: the write fails, the other parts still land
	require.NoError(t, os.MkdirAll(filepath.Join(dir, IssuesFile, "x"), 0755))

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(false), rep)
	require.Contains(t, rep.Failed, "owner/repo")
	assert.Contains(t, rep.Failed["owner/repo"].Error(), IssuesFile)
	assert.FileExists(t, filepath.Join(dir, "labels.json"))
}

func TestDiscussions_ErrorsFailTheExport(t *testing.T) {
	const (
		firstPage      = `{"data":{"repository":{"discussions":{"pageInfo":{"hasNextPage":false},"nodes":[%s]}}}}`
		moreComments   = `{"id":"D1","number":1,"comments":{"pageInfo":{"hasNextPage":true,"endCursor":"c"},"nodes":[]}}`
		withReplies    = `{"id":"D1","number":1,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"C1","replies":{"pageInfo":{"hasNextPage":true,"endCursor":"r"},"nodes":[]}}]}}`
		graphqlFailure = `{"errors":[{"type":"INTERNAL","message":"boom"}]}`
	)
	cases := []struct {
		name    string
		answers map[string][]string
		want    string
	}{
		{"discussions query fails", map[string][]string{"discussions(first": {graphqlFailure}}, "boom"},
		{"repository missing", map[string][]string{"discussions(first": {`{"data":{"repository":null}}`}}, "owner/repo not returned"},
		{"more comments fail", map[string][]string{"discussions(first": {strings.Replace(firstPage, "%s", moreComments, 1)}, "... on Discussion {": {graphqlFailure}}, "boom"},
		{"more comments node missing", map[string][]string{"discussions(first": {strings.Replace(firstPage, "%s", moreComments, 1)}, "... on Discussion {": {`{"data":{"node":null}}`}}, "discussion 1 not returned"},
		{"more replies fail", map[string][]string{"discussions(first": {strings.Replace(firstPage, "%s", withReplies, 1)}, "... on DiscussionComment {": {graphqlFailure}}, "boom"},
		{"more replies node missing", map[string][]string{"discussions(first": {strings.Replace(firstPage, "%s", withReplies, 1)}, "... on DiscussionComment {": {`{"data":{"node":null}}`}}, "comment C1 not returned"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _, _ := newTestExporter(t)
			registerRepo()
			httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, c.answers))

			rep := NewReport()
			e.ExportRepo(context.Background(), testRepo(true), rep)
			require.Contains(t, rep.Failed, "owner/repo")
			assert.Contains(t, rep.Failed["owner/repo"].Error(), "discussions.json")
			assert.Contains(t, rep.Failed["owner/repo"].Error(), c.want)
		})
	}
}

func TestProjects_ErrorsFailTheExport(t *testing.T) {
	const (
		boom      = `{"errors":[{"type":"INTERNAL","message":"boom"}]}`
		oneBoard  = `{"data":{"organization":{"projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"P1","number":4,"title":"T"}]}}}}`
		twoPages  = `{"data":{"organization":{"projectsV2":{"pageInfo":{"hasNextPage":true,"endCursor":"x"},"nodes":[]}}}}`
		lastPage  = `{"data":{"organization":{"projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"P2","number":5,"title":"U"}]}}}}`
		itemsPage = `{"data":{"node":{"items":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}`
	)

	run := func(t *testing.T, answers map[string][]string) *Report {
		e, _, _ := newTestExporter(t)
		registerOrg("o")
		httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, answers))
		rep := NewReport()
		e.ExportOrg(context.Background(), "o", rep)
		return rep
	}

	t.Run("projects query fails", func(t *testing.T) {
		rep := run(t, map[string][]string{"organization(login": {boom}})
		assert.ErrorContains(t, rep.Failed["org o"], "boom")
	})
	t.Run("items query fails", func(t *testing.T) {
		rep := run(t, map[string][]string{"organization(login": {oneBoard}, "... on ProjectV2 {": {boom}})
		assert.ErrorContains(t, rep.Failed["org o"], "project 4")
	})
	t.Run("items node missing", func(t *testing.T) {
		rep := run(t, map[string][]string{"organization(login": {oneBoard}, "... on ProjectV2 {": {`{"data":{"node":null}}`}})
		assert.ErrorContains(t, rep.Failed["org o"], "project P1 not returned")
	})
	t.Run("organization missing", func(t *testing.T) {
		rep := run(t, map[string][]string{"organization(login": {`{"data":{"organization":null}}`}})
		assert.ErrorContains(t, rep.Failed["org o"], "organization o not returned")
	})
	t.Run("several pages of projects are all kept", func(t *testing.T) {
		rep := run(t, map[string][]string{"organization(login": {twoPages, lastPage}, "... on ProjectV2 {": {itemsPage}})
		assert.Empty(t, rep.Failed)
	})
}

func TestExportOrg_ActionsVariablesAreSaved(t *testing.T) {
	e, target, _ := newTestExporter(t)
	registerOrg("o")
	httpmock.RegisterResponder("GET", "https://api.github.com/orgs/o/actions/variables",
		json200(map[string]any{"total_count": 1, "variables": []map[string]any{{"name": "REGION", "value": "eu"}}}))
	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"organization(login": {`{"data":{"organization":{"projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}`},
	}))

	rep := NewReport()
	e.ExportOrg(context.Background(), "o", rep)
	assert.Empty(t, rep.Failed)

	var vars []map[string]any
	readJSON(t, filepath.Join(OwnerDir(target, "o"), "actions-variables.json"), &vars)
	require.Len(t, vars, 1)
	assert.Equal(t, "REGION", vars[0]["name"])
}
