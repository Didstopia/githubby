package cli

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gherrors "github.com/Didstopia/githubby/internal/errors"
	"github.com/Didstopia/githubby/internal/state"
)

const oneRepoJSON = `[{"name":"hello","full_name":"octo/hello","owner":{"login":"octo"},"private":false,"default_branch":"main","clone_url":"https://github.com/octo/hello.git"}]`

func syncStorage(t *testing.T) *state.Storage {
	t.Helper()
	s, err := state.NewStorage()
	require.NoError(t, err)
	require.NoError(t, s.Load())
	return s
}

func TestSync_FlagValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad schedule", []string{"sync", "-u", "octo", "-T", "/x", "--schedule", "nope"}, "schedule"},
		{"target missing", []string{"sync", "-u", "octo"}, "--target is required"},
		{"user and org missing", []string{"sync", "-T", "/x"}, "either --user, --org or --all-orgs"},
		{"user and org together", []string{"sync", "-u", "a", "-o", "b", "-T", "/x"}, "only one of --user or --org"},
		{"profile and user together", []string{"sync", "--profile", "p", "-u", "a"}, "none of the others can be"},
		{"profile and all-profiles together", []string{"sync", "--profile", "p", "--all-profiles"}, "none of the others can be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEnv(t)
			res := runCLI(t, "", tt.args...)
			require.Error(t, res.err)
			assert.Contains(t, res.err.Error(), tt.want)
		})
	}
}

func TestSync_DryRunUser(t *testing.T) {
	testEnv(t)
	seen := fakeGitHub(t, map[string]fakeRoute{"GET /users/octo/repos": route(200, oneRepoJSON)})
	target := t.TempDir()

	res := runCLI(t, "", "sync", "--user", "octo", "--target", target, "--dry-run", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Syncing repositories for user: octo")
	assert.Contains(t, res.stdout, "Cloned (1):")
	assert.Contains(t, res.stdout, "octo/hello")
	assert.Contains(t, res.stdout, "Total: 1 cloned, 0 updated, 0 skipped, 0 failed")
	assert.Equal(t, []string{"GET /users/octo/repos"}, *seen)
	assert.NoDirExists(t, filepath.Join(target, "octo"), "a dry run must not create anything")
}

func TestSync_DryRunOrg(t *testing.T) {
	testEnv(t)
	fakeGitHub(t, map[string]fakeRoute{"GET /orgs/acme/repos": route(200, oneRepoJSON)})

	res := runCLI(t, "", "sync", "--org", "acme", "--target", t.TempDir(), "--dry-run", "--token", "tok", "--verbose")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Syncing repositories for organization: acme")
	assert.Contains(t, res.stdout, "Would clone")
}

func TestSync_ExcludeFilterSkipsRepos(t *testing.T) {
	testEnv(t)
	fakeGitHub(t, map[string]fakeRoute{"GET /users/octo/repos": route(200, oneRepoJSON)})

	res := runCLI(t, "", "sync", "-u", "octo", "-T", t.TempDir(), "--dry-run", "--token", "tok", "--exclude", "hel*")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Skipped (1):")
	assert.Contains(t, res.stdout, "Total: 0 cloned, 0 updated, 1 skipped, 0 failed")
}

func TestSync_AuthAndAPIFailures(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "", "sync", "-u", "octo", "-T", t.TempDir())
		var authErr *gherrors.AuthError
		require.ErrorAs(t, res.err, &authErr)
	})
	t.Run("expired token", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, map[string]fakeRoute{"GET /users/octo/repos": route(401, `{"message":"Bad credentials"}`)})
		res := runCLI(t, "", "sync", "-u", "octo", "-T", t.TempDir(), "--token", "tok")
		var authErr *gherrors.AuthError
		require.ErrorAs(t, res.err, &authErr)
		assert.Contains(t, authErr.Error(), "command line flag")
	})
	t.Run("other API failure is passed on", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, map[string]fakeRoute{"GET /users/octo/repos": route(500, `{"message":"boom"}`)})
		res := runCLI(t, "", "sync", "-u", "octo", "-T", t.TempDir(), "--token", "tok")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "failed to list repos")
	})
	t.Run("git missing", func(t *testing.T) {
		testEnv(t)
		t.Setenv("PATH", t.TempDir())
		res := runCLI(t, "", "sync", "-u", "octo", "-T", t.TempDir(), "--token", "tok")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "git initialization failed")
	})
}

func TestSync_Profiles(t *testing.T) {
	t.Run("all-profiles without any", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "", "sync", "--all-profiles")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "no sync profiles found")
	})
	t.Run("unknown profile and none exist", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "", "sync", "--profile", "ghost")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), `profile "ghost" not found; no profiles exist yet`)
	})
	t.Run("unknown profile lists the available ones", func(t *testing.T) {
		testEnv(t)
		st := syncStorage(t)
		require.NoError(t, st.AddProfile(state.NewProfile("alpha", "user", "octo", t.TempDir(), false)))
		require.NoError(t, st.AddProfile(state.NewProfile("beta", "org", "acme", t.TempDir(), false)))
		res := runCLI(t, "", "sync", "--profile", "ghost")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "available profiles: alpha, beta")
	})
	t.Run("broken state file", func(t *testing.T) {
		home := testEnv(t)
		require.NoError(t, mkStateFile(home, "profiles: [unclosed"))
		res := runCLI(t, "", "sync", "--all-profiles")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "failed to load state")
	})
}

func TestSync_ProfileRunUpdatesLastSync(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	user := state.NewProfile("alpha", "user", "octo", t.TempDir(), false)
	org := state.NewProfile("beta", "org", "acme", t.TempDir(), false)
	require.NoError(t, st.AddProfile(user))
	require.NoError(t, st.AddProfile(org))
	fakeGitHub(t, map[string]fakeRoute{
		"GET /users/octo/repos": route(200, oneRepoJSON),
		"GET /orgs/acme/repos":  route(200, `[]`),
	})

	before := time.Now()
	res := runCLI(t, "", "sync", "--all-profiles", "--dry-run", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Syncing 2 profile(s)")
	assert.Contains(t, res.stdout, `Syncing profile "alpha"`)
	assert.Contains(t, res.stdout, "Syncing repositories for organization: acme")

	reloaded := syncStorage(t)
	for _, name := range []string{"alpha", "beta"} {
		p := reloaded.GetProfileByName(name)
		require.NotNil(t, p)
		assert.False(t, p.LastSyncAt.Before(before.Add(-time.Second)), "%s must record a sync time", name)
	}
}

func TestSync_SingleProfile(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	require.NoError(t, st.AddProfile(state.NewProfile("alpha", "user", "octo", t.TempDir(), false)))
	fakeGitHub(t, map[string]fakeRoute{"GET /users/octo/repos": route(200, oneRepoJSON)})

	res := runCLI(t, "", "sync", "--profile", "alpha", "--dry-run", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, `Syncing profile "alpha"`)
	assert.NotContains(t, res.stdout, "Syncing 1 profile(s)")
}

func TestSync_ProfileFailuresDoNotStopTheOthers(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	bad := state.NewProfile("bad", "user", "broken", t.TempDir(), false)
	odd := state.NewProfile("odd", "team", "x", t.TempDir(), false)
	good := state.NewProfile("good", "user", "octo", t.TempDir(), false)
	for _, p := range []*state.SyncProfile{bad, odd, good} {
		require.NoError(t, st.AddProfile(p))
	}
	fakeGitHub(t, map[string]fakeRoute{
		"GET /users/broken/repos": route(401, `{"message":"Bad credentials"}`),
		"GET /users/octo/repos":   route(200, oneRepoJSON),
	})

	res := runCLI(t, "", "sync", "--all-profiles", "--dry-run", "--token", "tok")
	require.Error(t, res.err, "the last failure is returned")
	assert.Contains(t, res.stdout, `Syncing profile "good"`)

	reloaded := syncStorage(t)
	assert.False(t, reloaded.GetProfileByName("good").LastSyncAt.IsZero())
	assert.True(t, reloaded.GetProfileByName("bad").LastSyncAt.IsZero())
	assert.True(t, reloaded.GetProfileByName("odd").LastSyncAt.IsZero())
}

func TestSync_ProfileWithoutAuth(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	require.NoError(t, st.AddProfile(state.NewProfile("alpha", "user", "octo", t.TempDir(), false)))
	res := runCLI(t, "", "sync", "--profile", "alpha")
	var authErr *gherrors.AuthError
	require.ErrorAs(t, res.err, &authErr)
}

func TestSync_ProfileGitMissing(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	require.NoError(t, st.AddProfile(state.NewProfile("alpha", "user", "octo", t.TempDir(), false)))
	t.Setenv("PATH", t.TempDir())
	res := runCLI(t, "", "sync", "--profile", "alpha", "--token", "tok")
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "git initialization failed")
}

func TestSync_ProfileCancelled(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	require.NoError(t, st.AddProfile(state.NewProfile("alpha", "user", "octo", t.TempDir(), false)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := runCLIContext(t, ctx, "", "sync", "--all-profiles", "--token", "tok")
	require.ErrorIs(t, res.err, context.Canceled)
}

func TestSync_ScheduledRunsOnceThenStopsOnCancel(t *testing.T) {
	testEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeGitHub(t, map[string]fakeRoute{
		"GET /users/octo/repos": func(*http.Request) *http.Response {
			cancel() // stop the scheduler once the immediate sync has run
			return jsonResponse(200, oneRepoJSON)
		},
	})

	res := runCLIContext(t, ctx, "", "sync", "-u", "octo", "-T", t.TempDir(), "--dry-run", "--token", "tok", "--schedule", "@every 1h")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Starting scheduled sync with schedule: @every 1h")
	assert.Contains(t, res.stdout, "Syncing repositories for user: octo", "the first sync runs immediately")
}

func TestSync_ScheduledProfiles(t *testing.T) {
	testEnv(t)
	st := syncStorage(t)
	require.NoError(t, st.AddProfile(state.NewProfile("alpha", "user", "octo", t.TempDir(), false)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeGitHub(t, map[string]fakeRoute{
		"GET /users/octo/repos": func(*http.Request) *http.Response {
			cancel()
			return jsonResponse(200, `[]`)
		},
	})

	res := runCLIContext(t, ctx, "", "sync", "--all-profiles", "--dry-run", "--token", "tok", "--schedule", "@every 1h")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Starting scheduled sync")
}

func TestPrintSyncSummary_Output(t *testing.T) {
	out, _ := captureOutput(t, func() {
		printSyncSummary(sampleResult())
	})
	assert.Contains(t, out, "Cloned (2):")
	assert.Contains(t, out, "Updated (1):")
	assert.Contains(t, out, "Skipped (1):")
	assert.Contains(t, out, "Failed (1):")
	assert.Contains(t, out, "octo/bad: boom")
	assert.Contains(t, out, "Archived (1) - preserved locally")
	assert.Contains(t, out, "Total: 2 cloned, 1 updated, 1 skipped, 1 failed, 1 archived")
}
