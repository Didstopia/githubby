package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

func releasesJSON(n int) string {
	var parts []string
	for i := 1; i <= n; i++ {
		created := time.Now().Add(-time.Duration(i) * 48 * time.Hour).UTC().Format(time.RFC3339)
		parts = append(parts, fmt.Sprintf(`{"id":%d,"tag_name":"v%d","created_at":%q}`, i, i, created))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func cleanRoutes(n int, deleteStatus int) map[string]fakeRoute {
	routes := map[string]fakeRoute{
		"GET /repos/octo/cat/releases": route(200, releasesJSON(n)),
	}
	for i := 1; i <= n; i++ {
		routes[fmt.Sprintf("DELETE /repos/octo/cat/releases/%d", i)] = func(*http.Request) *http.Response {
			return jsonResponse(deleteStatus, `{"message":"x"}`)
		}
		routes[fmt.Sprintf("DELETE /repos/octo/cat/git/refs/tags/v%d", i)] = route(204, ``)
	}
	return routes
}

func TestClean_RequiresAFilter(t *testing.T) {
	testEnv(t)
	res := runCLI(t, "", "clean", "--repository", "octo/cat", "--token", "tok")
	require.ErrorIs(t, res.err, gherrors.ErrMissingFilter)
}

func TestClean_RequiresAuthentication(t *testing.T) {
	testEnv(t)
	res := runCLI(t, "", "clean", "--repository", "octo/cat", "--filter-count", "1")
	var authErr *gherrors.AuthError
	require.ErrorAs(t, res.err, &authErr)
}

func TestClean_RejectsBadRepository(t *testing.T) {
	testEnv(t)
	res := runCLI(t, "", "clean", "--repository", "not-a-repo", "--filter-count", "1", "--token", "tok")
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "repository")
}

func TestClean_DeletesReleasesBeyondTheKeepCount(t *testing.T) {
	testEnv(t)
	seen := fakeGitHub(t, cleanRoutes(3, 204))

	res := runCLI(t, "", "clean", "--repository", "octo/cat", "--filter-count", "1", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Found 3 release(s) total")
	assert.Contains(t, res.stdout, "Found 2 release(s) matching the filters, starting cleanup")
	assert.Contains(t, res.stdout, "Successfully cleaned up 2 release(s)!")

	assert.Contains(t, *seen, "DELETE /repos/octo/cat/releases/2")
	assert.Contains(t, *seen, "DELETE /repos/octo/cat/releases/3")
	assert.NotContains(t, *seen, "DELETE /repos/octo/cat/releases/1", "the newest release is kept")
	assert.Contains(t, *seen, "DELETE /repos/octo/cat/git/refs/tags/v2")
}

func TestClean_VerboseByDays(t *testing.T) {
	testEnv(t)
	seen := fakeGitHub(t, cleanRoutes(3, 204))

	// Releases are 2, 4 and 6 days old; older than 3 days removes two of them.
	res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-days", "3", "--verbose", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Validation succeeded for repository octo/cat")
	assert.Contains(t, res.stdout, "Found 3 releases total")
	assert.Contains(t, res.stdout, "Found 2 releases that match cleanup filters")
	assert.Contains(t, res.stdout, "falls outside of day filter")
	assert.Contains(t, res.stdout, "Successfully deleted release")
	assert.Contains(t, *seen, "DELETE /repos/octo/cat/releases/3")
}

func TestClean_DryRunDeletesNothing(t *testing.T) {
	testEnv(t)
	seen := fakeGitHub(t, map[string]fakeRoute{"GET /repos/octo/cat/releases": route(200, releasesJSON(2))})

	res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--dry-run", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "starting simulated cleanup")
	assert.Equal(t, []string{"GET /repos/octo/cat/releases"}, *seen)
}

func TestClean_VerboseDryRun(t *testing.T) {
	testEnv(t)
	fakeGitHub(t, map[string]fakeRoute{"GET /repos/octo/cat/releases": route(200, releasesJSON(2))})

	res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--dry-run", "--verbose", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Dry run detected, simulating cleanup")
	assert.Contains(t, res.stdout, "Dry run enabled, simulating cleanup")
}

func TestClean_NothingMatches(t *testing.T) {
	testEnv(t)
	fakeGitHub(t, map[string]fakeRoute{"GET /repos/octo/cat/releases": route(200, releasesJSON(1))})

	res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "5", "--token", "tok")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Found 0 release(s) matching the filters")
	assert.NotContains(t, res.stdout, "Successfully cleaned up")
}

func TestClean_APIErrors(t *testing.T) {
	t.Run("expired token while listing", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, map[string]fakeRoute{"GET /repos/octo/cat/releases": route(401, `{"message":"Bad credentials"}`)})
		res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--token", "tok")
		var authErr *gherrors.AuthError
		require.ErrorAs(t, res.err, &authErr)
		assert.Contains(t, authErr.Error(), "command line flag")
	})
	t.Run("other error while listing", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, map[string]fakeRoute{"GET /repos/octo/cat/releases": route(500, `{"message":"boom"}`)})
		res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--token", "tok")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "failed to fetch releases")
	})
	t.Run("expired token while deleting stops the run", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, cleanRoutes(2, 401))
		res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--token", "tok")
		var authErr *gherrors.AuthError
		require.ErrorAs(t, res.err, &authErr)
	})
	t.Run("a failed delete is reported and the run continues", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, cleanRoutes(3, 500))
		res := runCLI(t, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--token", "tok")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "Error deleting release")
		assert.Contains(t, res.stdout, "Successfully cleaned up 0 release(s)!")
	})
}

func TestClean_Cancelled(t *testing.T) {
	testEnv(t)
	fakeGitHub(t, map[string]fakeRoute{"GET /repos/octo/cat/releases": route(200, releasesJSON(2))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := runCLIContext(t, ctx, "", "clean", "-r", "octo/cat", "--filter-count", "1", "--token", "tok")
	require.Error(t, res.err)
}
