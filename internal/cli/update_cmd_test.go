package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := Version
	Version = v
	t.Cleanup(func() { Version = orig })
}

// releaseRoutes serves a single release v<version> whose asset download fails,
// so no test can ever replace the running binary.
func releaseRoutes(version string) map[string]fakeRoute {
	asset := fmt.Sprintf("githubby_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	body := fmt.Sprintf(`[{"id":1,"tag_name":"v%s","html_url":"https://example.com/release","assets":[`+
		`{"id":11,"name":%q,"size":1,"browser_download_url":"https://example.com/a"},`+
		`{"id":12,"name":"checksums.txt","size":1,"browser_download_url":"https://example.com/c"}]}]`, version, asset)
	return map[string]fakeRoute{
		"GET /repos/Didstopia/githubby/releases":           route(200, body),
		"GET /repos/Didstopia/githubby/releases/assets/11": route(404, `{"message":"gone"}`),
	}
}

func TestUpdateCommand_DevBuild(t *testing.T) {
	testEnv(t)
	withVersion(t, "dev")
	res := runCLI(t, "", "update")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "development build")
}

func TestUpdateCommand_UpToDate(t *testing.T) {
	testEnv(t)
	withVersion(t, "1.0.0")
	fakeGitHub(t, releaseRoutes("1.0.0"))
	res := runCLI(t, "", "update")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "You're running the latest version (v1.0.0)")
}

func TestUpdateCommand_CheckOnly(t *testing.T) {
	testEnv(t)
	withVersion(t, "1.0.0")
	seen := fakeGitHub(t, releaseRoutes("2.0.0"))
	res := runCLI(t, "", "update", "--check")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "New version available: v1.0.0 -> v2.0.0")
	assert.Contains(t, res.stdout, "Release: https://example.com/release")
	assert.Contains(t, res.stdout, "Run 'githubby update' to install the update.")
	assert.NotContains(t, *seen, "GET /repos/Didstopia/githubby/releases/assets/11", "--check must not download")
}

func TestUpdateCommand_InstallFailure(t *testing.T) {
	testEnv(t)
	withVersion(t, "1.0.0")
	fakeGitHub(t, releaseRoutes("2.0.0"))
	res := runCLI(t, "", "update")
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "failed to update")
	assert.Contains(t, res.stdout, "Downloading and installing update...")
}

func TestUpdateCommand_CheckFailure(t *testing.T) {
	testEnv(t)
	withVersion(t, "1.0.0")
	fakeGitHub(t, map[string]fakeRoute{
		"GET /repos/Didstopia/githubby/releases": func(*http.Request) *http.Response { return jsonResponse(500, `{"message":"boom"}`) },
	})
	res := runCLI(t, "", "update")
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "failed to check for updates")
}

func TestShouldAutoUpdate(t *testing.T) {
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })

	tests := []struct {
		name    string
		version string
		args    []string
		want    bool
	}{
		{"dev build", "dev", []string{"githubby", "sync"}, false},
		{"empty version", "", []string{"githubby", "sync"}, false},
		{"release build runs sync", "1.0.0", []string{"githubby", "sync"}, true},
		{"release build without args", "1.0.0", []string{"githubby"}, true},
		{"update command", "1.0.0", []string{"githubby", "update"}, false},
		{"version command", "1.0.0", []string{"githubby", "version"}, false},
		{"help", "1.0.0", []string{"githubby", "help"}, false},
		{"--help", "1.0.0", []string{"githubby", "--help"}, false},
		{"-h", "1.0.0", []string{"githubby", "-h"}, false},
		{"--version", "1.0.0", []string{"githubby", "--version"}, false},
		{"-v", "1.0.0", []string{"githubby", "-v"}, false},
		{"interactive", "1.0.0", []string{"githubby", "interactive"}, false},
		{"tui", "1.0.0", []string{"githubby", "tui"}, false},
		{"ui", "1.0.0", []string{"githubby", "ui"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withVersion(t, tt.version)
			os.Args = tt.args
			assert.Equal(t, tt.want, shouldAutoUpdate())
		})
	}
}

func TestCheckAndAutoUpdate(t *testing.T) {
	t.Run("check failure continues silently", func(t *testing.T) {
		withVersion(t, "1.0.0")
		fakeGitHub(t, map[string]fakeRoute{
			"GET /repos/Didstopia/githubby/releases": route(500, `{"message":"boom"}`),
		})
		var restarted bool
		out, _ := captureOutput(t, func() { restarted = checkAndAutoUpdate(context.Background()) })
		assert.False(t, restarted)
		assert.Contains(t, out, "Checking for updates...")
	})
	t.Run("already up to date", func(t *testing.T) {
		withVersion(t, "1.0.0")
		fakeGitHub(t, releaseRoutes("1.0.0"))
		var restarted bool
		out, _ := captureOutput(t, func() { restarted = checkAndAutoUpdate(context.Background()) })
		assert.False(t, restarted)
		assert.NotContains(t, out, "Updating to")
	})
	t.Run("failed install carries on with the current version", func(t *testing.T) {
		withVersion(t, "1.0.0")
		fakeGitHub(t, releaseRoutes("2.0.0"))
		var restarted bool
		out, errOut := captureOutput(t, func() { restarted = checkAndAutoUpdate(context.Background()) })
		assert.False(t, restarted)
		assert.Contains(t, out, "Updating to v2.0.0...")
		assert.Contains(t, out, "Continuing with current version...")
		assert.Contains(t, errOut, "Update failed")
	})
}
