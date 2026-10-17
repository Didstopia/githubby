package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

const api = "https://api.github.com/repos/owner/repo"

// newTestExporter returns an exporter on mocked HTTP and the list of waits it did
func newTestExporter(t *testing.T) (*Exporter, string, *[]time.Duration) {
	t.Helper()
	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)

	target := t.TempDir()
	e := New(gh.NewClient(nil), nil, target)
	var waits []time.Duration
	e.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	return e, target, &waits
}

func json200(v any) httpmock.Responder { return httpmock.NewJsonResponderOrPanic(200, v) }

// registerRepo mocks every REST endpoint a repo export reads, all fine
func registerRepo() {
	httpmock.RegisterResponder("GET", api, json200(map[string]any{"name": "repo", "full_name": "owner/repo"}))

	httpmock.RegisterResponder("GET", api+"/issues", func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("state") != "all" {
			return httpmock.NewStringResponse(400, "want state=all"), nil
		}
		if req.URL.Query().Get("page") == "2" {
			return httpmock.NewJsonResponse(200, []map[string]any{
				{"number": 3, "title": "a pull", "pull_request": map[string]any{"url": "x"}},
			})
		}
		resp, _ := httpmock.NewJsonResponse(200, []map[string]any{
			{"number": 1, "title": "first"}, {"number": 2, "title": "second"},
		})
		resp.Header.Set("Link", `<`+api+`/issues?page=2&state=all>; rel="next"`)
		return resp, nil
	})
	httpmock.RegisterResponder("GET", api+"/issues/comments", json200([]map[string]any{
		{"id": 10, "body": "on issue 1", "issue_url": api + "/issues/1"},
		{"id": 11, "body": "on pull 3", "issue_url": api + "/issues/3"},
	}))
	httpmock.RegisterResponder("GET", api+"/pulls", json200([]map[string]any{{"number": 3, "title": "a pull"}}))
	httpmock.RegisterResponder("GET", api+"/pulls/comments", json200([]map[string]any{
		{"id": 20, "body": "review note", "pull_request_url": api + "/pulls/3"},
	}))
	httpmock.RegisterResponder("GET", api+"/pulls/3/reviews", json200([]map[string]any{{"id": 30, "state": "APPROVED"}}))

	httpmock.RegisterResponder("GET", api+"/releases", json200([]map[string]any{
		{"id": 1, "tag_name": "v1/beta", "assets": []map[string]any{{"id": 5, "name": "app.bin", "size": 3}}},
	}))
	httpmock.RegisterResponder("GET", api+"/releases/assets/5", func(req *http.Request) (*http.Response, error) {
		resp := httpmock.NewStringResponse(302, "")
		resp.Header.Set("Location", "https://objects.example.com/app.bin")
		return resp, nil
	})
	httpmock.RegisterResponder("GET", "https://objects.example.com/app.bin", func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "" {
			return httpmock.NewStringResponse(400, "no auth here"), nil
		}
		return httpmock.NewStringResponse(200, "abc"), nil
	})

	httpmock.RegisterResponder("GET", api+"/labels", json200([]map[string]any{{"name": "bug"}}))
	httpmock.RegisterResponder("GET", api+"/milestones", json200([]map[string]any{{"number": 1, "title": "v1"}}))
	httpmock.RegisterResponder("GET", api+"/topics", json200(map[string]any{"names": []string{"go", "cli"}}))
	httpmock.RegisterResponder("GET", api+"/branches", json200([]map[string]any{{"name": "main", "protected": true}}))
	httpmock.RegisterResponder("GET", api+"/branches/main/protection", json200(map[string]any{
		"required_linear_history": map[string]any{"enabled": true},
	}))
	httpmock.RegisterResponder("GET", api+"/hooks", json200([]map[string]any{
		{"id": 1, "config": map[string]any{"url": "https://hook.example.com", "secret": "********"}},
	}))
	httpmock.RegisterResponder("GET", api+"/collaborators", httpmock.NewJsonResponderOrPanic(403,
		map[string]any{"message": "Must have push access to view repository collaborators."}))
	httpmock.RegisterResponder("GET", api+"/keys", json200([]map[string]any{}))
	httpmock.RegisterResponder("GET", api+"/actions/variables", json200(map[string]any{
		"total_count": 1, "variables": []map[string]any{{"name": "REGION", "value": "eu"}},
	}))
	httpmock.RegisterResponder("GET", api+"/actions/secrets", json200(map[string]any{
		"total_count": 1, "secrets": []map[string]any{{"name": "DEPLOY_KEY"}},
	}))
	httpmock.RegisterResponder("GET", api+"/rulesets", func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("includes_parents") != "false" {
			return httpmock.NewStringResponse(400, "org rulesets belong to the org export"), nil
		}
		return httpmock.NewJsonResponse(200, []map[string]any{{"id": 42, "name": "main", "enforcement": "active"}})
	})
	httpmock.RegisterResponder("GET", api+"/rulesets/42", json200(map[string]any{
		"id": 42, "name": "main", "enforcement": "active", "rules": []map[string]any{{"type": "deletion"}},
	}))
	httpmock.RegisterResponder("GET", api+"/environments", json200(map[string]any{
		"total_count": 1, "environments": []map[string]any{{"name": "prod"}},
	}))
}

func testRepo(discussions bool) *gh.Repository {
	return &gh.Repository{
		Name: gh.Ptr("repo"), FullName: gh.Ptr("owner/repo"),
		Owner: &gh.User{Login: gh.Ptr("owner")}, HasDiscussions: gh.Ptr(discussions),
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, v))
}

func TestExportRepo(t *testing.T) {
	e, target, _ := newTestExporter(t)
	registerRepo()

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(false), rep)

	assert.Empty(t, rep.Failed)
	assert.Equal(t, []string{"owner/repo"}, rep.Exported)
	require.Len(t, rep.Warnings, 1)
	assert.Contains(t, rep.Warnings[0], "collaborators.json skipped, needs repo and push access")

	dir := RepoDir(target, "owner", "repo")

	var issues []IssueExport
	readJSON(t, filepath.Join(dir, IssuesFile), &issues)
	require.Len(t, issues, 2, "the pull request is left out of issues")
	assert.Equal(t, 1, issues[0].Issue.GetNumber())
	require.Len(t, issues[0].Comments, 1)
	assert.Equal(t, "on issue 1", issues[0].Comments[0].GetBody())
	assert.Empty(t, issues[1].Comments)

	var pulls []PullExport
	readJSON(t, filepath.Join(dir, PullsFile), &pulls)
	require.Len(t, pulls, 1)
	require.Len(t, pulls[0].Comments, 1)
	assert.Equal(t, "on pull 3", pulls[0].Comments[0].GetBody())
	require.Len(t, pulls[0].ReviewComments, 1)
	require.Len(t, pulls[0].Reviews, 1)
	assert.Equal(t, "APPROVED", pulls[0].Reviews[0].GetState())

	asset, err := os.ReadFile(filepath.Join(dir, "releases", "v1_beta", "app.bin"))
	require.NoError(t, err)
	assert.Equal(t, "abc", string(asset))

	var hooks []map[string]any
	readJSON(t, filepath.Join(dir, "hooks.json"), &hooks)
	require.Len(t, hooks, 1)
	assert.NotContains(t, hooks[0]["config"], "secret")

	var protection map[string]any
	readJSON(t, filepath.Join(dir, "branch-protection.json"), &protection)
	assert.Contains(t, protection, "main")

	var topics []string
	readJSON(t, filepath.Join(dir, "topics.json"), &topics)
	assert.Equal(t, []string{"go", "cli"}, topics)

	var secrets []map[string]any
	readJSON(t, filepath.Join(dir, "actions-secrets.json"), &secrets)
	assert.Equal(t, "DEPLOY_KEY", secrets[0]["name"])

	var variables []map[string]any
	readJSON(t, filepath.Join(dir, "actions-variables.json"), &variables)
	assert.Equal(t, "eu", variables[0]["value"])

	assert.Equal(t, [][]string{{"deletion"}}, rulesetRules(t, filepath.Join(dir, RulesetsFile)), "full detail per ruleset")

	for _, f := range []string{"repo.json", "releases.json", "labels.json", "milestones.json", "deploy-keys.json", "environments.json"} {
		assert.FileExists(t, filepath.Join(dir, f))
	}
	assert.NoFileExists(t, filepath.Join(dir, "collaborators.json"))
	assert.NoFileExists(t, filepath.Join(dir, "discussions.json"))

	// Second run keeps the asset of the same size and does not download it again
	before := httpmock.GetCallCountInfo()["GET https://objects.example.com/app.bin"]
	e.ExportRepo(context.Background(), testRepo(false), NewReport())
	assert.Equal(t, before, httpmock.GetCallCountInfo()["GET https://objects.example.com/app.bin"])
}

func TestExportRepo_FailuresAndDisabledIssues(t *testing.T) {
	e, target, _ := newTestExporter(t)
	registerRepo()
	httpmock.RegisterResponder("GET", api+"/issues", httpmock.NewJsonResponderOrPanic(410, map[string]any{"message": "Issues are disabled"}))
	httpmock.RegisterResponder("GET", api+"/milestones", httpmock.NewJsonResponderOrPanic(500, map[string]any{"message": "oops"}))

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(false), rep)

	require.Contains(t, rep.Failed, "owner/repo")
	assert.Contains(t, rep.Failed["owner/repo"].Error(), "milestones.json")
	assert.Empty(t, rep.Exported)

	var issues []IssueExport
	readJSON(t, filepath.Join(RepoDir(target, "owner", "repo"), IssuesFile), &issues)
	assert.Empty(t, issues)
	// The other parts still got written
	assert.FileExists(t, filepath.Join(RepoDir(target, "owner", "repo"), "labels.json"))
}

func TestExportRepo_AssetSizeMismatchFails(t *testing.T) {
	e, _, _ := newTestExporter(t)
	registerRepo()
	httpmock.RegisterResponder("GET", "https://objects.example.com/app.bin", httpmock.NewStringResponder(200, "abcdef"))

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(false), rep)
	require.Contains(t, rep.Failed, "owner/repo")
	assert.Contains(t, rep.Failed["owner/repo"].Error(), "got 6 bytes, want 3")
}

func TestExportRepo_WaitsOutRateLimit(t *testing.T) {
	e, _, waits := newTestExporter(t)
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
		return httpmock.NewJsonResponse(200, []map[string]any{{"name": "bug"}})
	})

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(false), rep)
	assert.Empty(t, rep.Failed)
	assert.Equal(t, 2, calls)
	assert.Len(t, *waits, 1)
}

func TestExportRepo_CancelledContext(t *testing.T) {
	e, _, _ := newTestExporter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep := NewReport()
	e.ExportRepo(ctx, testRepo(false), rep)
	assert.ErrorIs(t, rep.Failed["owner/repo"], context.Canceled)
}

// graphqlResponder answers each query by a keyword in it
func graphqlResponder(t *testing.T, answers map[string][]string) httpmock.Responder {
	seen := map[string]int{}
	return func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &q))
		for key, replies := range answers {
			if strings.Contains(q.Query, key) {
				i := seen[key]
				seen[key]++
				if i >= len(replies) {
					i = len(replies) - 1
				}
				resp := httpmock.NewStringResponse(200, replies[i])
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			}
		}
		t.Errorf("unexpected query: %s", q.Query)
		return httpmock.NewStringResponse(500, "{}"), nil
	}
}

func TestExportRepo_Discussions(t *testing.T) {
	e, target, _ := newTestExporter(t)
	registerRepo()

	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"discussions(first": {
			`{"data":{"repository":{"discussions":{"totalCount":2,"pageInfo":{"hasNextPage":true,"endCursor":"d1"},"nodes":[
			  {"id":"D1","number":1,"title":"Hello","comments":{"totalCount":2,"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[
			    {"id":"C1","body":"first","replies":{"totalCount":0,"pageInfo":{"hasNextPage":false},"nodes":[]}}]}}]}}}}`,
			`{"data":{"repository":{"discussions":{"totalCount":2,"pageInfo":{"hasNextPage":false},"nodes":[
			  {"id":"D2","number":2,"title":"Second","comments":{"totalCount":0,"pageInfo":{"hasNextPage":false},"nodes":[]}}]}}}}`,
		},
		"... on Discussion {": {
			`{"data":{"node":{"comments":{"totalCount":2,"pageInfo":{"hasNextPage":false},"nodes":[
			  {"id":"C2","body":"second","replies":{"totalCount":2,"pageInfo":{"hasNextPage":true,"endCursor":"r1"},"nodes":[{"id":"R1"}]}}]}}}}`,
		},
		"... on DiscussionComment {": {
			`{"data":{"node":{"replies":{"totalCount":2,"pageInfo":{"hasNextPage":false},"nodes":[{"id":"R2"}]}}}}`,
		},
	}))

	rep := NewReport()
	e.ExportRepo(context.Background(), testRepo(true), rep)
	require.Empty(t, rep.Failed)

	var discussions []Discussion
	readJSON(t, filepath.Join(RepoDir(target, "owner", "repo"), "discussions.json"), &discussions)
	require.Len(t, discussions, 2)
	require.Len(t, discussions[0].Comments.Nodes, 2)
	assert.Nil(t, discussions[0].Comments.PageInfo)
	assert.Equal(t, "second", discussions[0].Comments.Nodes[1].Body)
	assert.Len(t, discussions[0].Comments.Nodes[1].Replies.Nodes, 2)
}

func registerOrg(org string) {
	base := "https://api.github.com/orgs/" + org
	httpmock.RegisterResponder("GET", base, json200(map[string]any{"login": org}))
	httpmock.RegisterResponder("GET", base+"/actions/variables", httpmock.NewJsonResponderOrPanic(403, map[string]any{"message": "Resource not accessible"}))
	httpmock.RegisterResponder("GET", base+"/actions/secrets", json200(map[string]any{"secrets": []map[string]any{{"name": "NPM_TOKEN"}}}))
	httpmock.RegisterResponder("GET", base+"/members", json200([]map[string]any{{"login": "dids"}}))
	httpmock.RegisterResponder("GET", base+"/teams", json200([]map[string]any{{"slug": "core"}}))
	httpmock.RegisterResponder("GET", base+"/rulesets", json200([]map[string]any{{"id": 7, "name": "all", "enforcement": "active"}}))
	httpmock.RegisterResponder("GET", base+"/rulesets/7", json200(map[string]any{
		"id": 7, "name": "all", "enforcement": "active", "rules": []map[string]any{{"type": "non_fast_forward"}},
	}))
}

// rulesetRules reads a rulesets.json and returns the rule types of each ruleset
func rulesetRules(t *testing.T, path string) [][]string {
	t.Helper()
	var sets []struct {
		Rules []struct {
			Type string `json:"type"`
		} `json:"rules"`
	}
	readJSON(t, path, &sets)
	out := [][]string{}
	for _, s := range sets {
		types := []string{}
		for _, r := range s.Rules {
			types = append(types, r.Type)
		}
		out = append(out, types)
	}
	return out
}

func TestExportOrg(t *testing.T) {
	e, target, _ := newTestExporter(t)
	registerOrg("Didstopia")

	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"organization(login": {
			`{"data":{"organization":{"projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[
			  {"id":"P1","number":1,"title":"Roadmap","fields":{"totalCount":1,"nodes":[{"id":"F1","name":"Status"}]}}]}}}}`,
		},
		"... on ProjectV2 {": {
			`{"data":{"node":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":"i1"},"nodes":[{"id":"I1"}]}}}}`,
			`{"data":{"node":{"items":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"I2","content":null}]}}},
			  "errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`,
		},
	}))

	rep := NewReport()
	e.ExportOrg(context.Background(), "Didstopia", rep)
	assert.Empty(t, rep.Failed)
	assert.Equal(t, []string{"org Didstopia"}, rep.Exported)
	require.Len(t, rep.Warnings, 2)
	assert.Contains(t, strings.Join(rep.Warnings, "\n"), "actions-variables.json skipped, needs admin:org")
	assert.Contains(t, strings.Join(rep.Warnings, "\n"), "projects-v2.json is missing some entries")

	dir := OwnerDir(target, "Didstopia")
	var projects []Project
	readJSON(t, filepath.Join(dir, "projects-v2.json"), &projects)
	require.Len(t, projects, 1)
	assert.Len(t, projects[0].Items, 2)
	assert.Len(t, projects[0].Fields.Nodes, 1)
	for _, f := range []string{"org.json", "actions-secrets.json", "members.json", "teams.json"} {
		assert.FileExists(t, filepath.Join(dir, f))
	}
	assert.Equal(t, [][]string{{"non_fast_forward"}}, rulesetRules(t, filepath.Join(dir, RulesetsFile)))
}

func TestExportUser_MissingScopeIsWarning(t *testing.T) {
	e, target, _ := newTestExporter(t)
	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"user(login": {`{"data":{"user":null},"errors":[{"type":"INSUFFICIENT_SCOPES","message":"Your token has not been granted the required scopes"}]}`},
	}))

	rep := NewReport()
	e.ExportUser(context.Background(), "Dids", rep)
	assert.Empty(t, rep.Failed)
	require.Len(t, rep.Warnings, 1)
	assert.Contains(t, rep.Warnings[0], "needs read:project")
	assert.NoFileExists(t, filepath.Join(OwnerDir(target, "Dids"), "projects-v2.json"))
}

func TestExportUser_MissingScopeWarningIsShort(t *testing.T) {
	e, _, _ := newTestExporter(t)
	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"user(login": {`{"data":{"user":null},"errors":[` +
			`{"type":"INSUFFICIENT_SCOPES","message":"The 'id' field requires read:project"},` +
			`{"type":"INSUFFICIENT_SCOPES","message":"The 'title' field requires read:project"}]}`},
	}))

	rep := NewReport()
	e.ExportUser(context.Background(), "Dids", rep)
	require.Len(t, rep.Warnings, 1)
	assert.True(t, strings.HasSuffix(rep.Warnings[0], "needs read:project (INSUFFICIENT_SCOPES)"), rep.Warnings[0])
}

func TestExportUser_GraphQLRateLimitWaits(t *testing.T) {
	e, target, waits := newTestExporter(t)
	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"user(login": {
			`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`,
			`{"data":{"user":{"projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}`,
		},
	}))

	rep := NewReport()
	e.ExportUser(context.Background(), "Dids", rep)
	assert.Empty(t, rep.Failed)
	assert.Empty(t, rep.Warnings)
	assert.Len(t, *waits, 1)
	assert.FileExists(t, filepath.Join(OwnerDir(target, "Dids"), "projects-v2.json"))
}

func TestExportUser_MissingRootFails(t *testing.T) {
	e, _, _ := newTestExporter(t)
	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", graphqlResponder(t, map[string][]string{
		"user(login": {`{"data":{"user":null}}`},
	}))
	rep := NewReport()
	e.ExportUser(context.Background(), "Dids", rep)
	assert.Contains(t, rep.Failed, "user Dids")
}

func TestRateLimitWait(t *testing.T) {
	_, ok := rateLimitWait(nil)
	assert.False(t, ok)
	_, ok = rateLimitWait(errors.New("x"))
	assert.False(t, ok)

	retry := 5 * time.Second
	wait, ok := rateLimitWait(&gh.AbuseRateLimitError{RetryAfter: &retry})
	assert.True(t, ok)
	assert.Equal(t, retry, wait)

	wait, ok = rateLimitWait(&gh.AbuseRateLimitError{})
	assert.True(t, ok)
	assert.Equal(t, time.Minute, wait)

	wait, ok = rateLimitWait(&gh.RateLimitError{Rate: gh.Rate{Reset: gh.Timestamp{Time: time.Now().Add(time.Hour)}}})
	assert.True(t, ok)
	assert.Greater(t, wait, 59*time.Minute)

	_, ok = rateLimitWait(&github.GraphQLError{Errors: []github.GraphQLErrorItem{{Type: "NOT_FOUND"}}})
	assert.False(t, ok)
}

func TestCallGivesUpAfterMaxWaits(t *testing.T) {
	e := New(gh.NewClient(nil), nil, t.TempDir())
	n := 0
	e.sleep = func(ctx context.Context, d time.Duration) error { n++; return nil }
	err := e.call(context.Background(), func() error { return &gh.AbuseRateLimitError{} })
	assert.Error(t, err)
	assert.Equal(t, maxRateLimitWaits, n)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, sleepCtx(ctx, time.Hour), context.Canceled)
	assert.NoError(t, sleepCtx(context.Background(), time.Millisecond))
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, 12, numberFromURL("https://api.github.com/repos/o/r/issues/12"))
	assert.Equal(t, 0, numberFromURL("https://api.github.com/repos/o/r/issues/x"))
	assert.Equal(t, "_..", safeName(".."))
	assert.Equal(t, "a_b", safeName("a/b"))

	release := &gh.RepositoryRelease{ID: gh.Ptr(int64(7))}
	asset := &gh.ReleaseAsset{Name: gh.Ptr("x.zip")}
	assert.Equal(t, filepath.Join("m", "releases", "release-7", "x.zip"), AssetPath("m", release, asset))

	rep := NewReport()
	rep.fail("a", errors.New("one"))
	rep.fail("a", errors.New("two"))
	assert.ErrorContains(t, rep.Failed["a"], "one")
	assert.ErrorContains(t, rep.Failed["a"], "two")
}

func TestOwnerFilesMatchExports(t *testing.T) {
	e := New(gh.NewClient(nil), nil, t.TempDir())
	names := func(parts []part) []string {
		out := []string{}
		for _, p := range parts {
			out = append(out, p.file)
		}
		return out
	}
	assert.Equal(t, OrgFiles, names(e.orgParts("o")))
	assert.Equal(t, UserFiles, names(e.userParts("u")))
}
