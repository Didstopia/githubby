package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/Didstopia/githubby/internal/auth"
)

func TestLogin_WithToken(t *testing.T) {
	testEnv(t)
	fakeGitHub(t, map[string]fakeRoute{"GET /user": route(200, userJSON)})

	res := runCLI(t, "ghp_secret\n", "login", "--with-token")
	require.NoError(t, res.err)
	assert.Contains(t, res.stdout, "Logged in as octo")
	assert.Contains(t, res.stdout, "Token stored in")

	stored, err := keyring.Get(auth.KeyringServiceName, auth.DefaultHostname)
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", stored)
}

func TestLogin_WithToken_Errors(t *testing.T) {
	t.Run("no input", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "", "login", "--with-token")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "failed to read token from stdin")
	})
	t.Run("blank line", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "  \n", "login", "--with-token")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "no token provided")
	})
	t.Run("rejected token", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, map[string]fakeRoute{"GET /user": route(401, `{"message":"Bad credentials"}`)})
		res := runCLI(t, "bad\n", "login", "--with-token")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "token validation failed")
		_, err := keyring.Get(auth.KeyringServiceName, auth.DefaultHostname)
		assert.ErrorIs(t, err, keyring.ErrNotFound, "a bad token must not be stored")
	})
}

func loginRoutes(userStatus int) map[string]fakeRoute {
	return map[string]fakeRoute{
		"POST /login/device/code": func(*http.Request) *http.Response {
			return formResponse(200, "device_code=DEV&user_code=ABCD-1234&verification_uri=https%3A%2F%2Fexample.com%2Fdevice&interval=0&expires_in=60")
		},
		"POST /login/oauth/access_token": func(*http.Request) *http.Response {
			return formResponse(200, "access_token=gho_flow&token_type=bearer")
		},
		"GET /user": route(userStatus, userJSON),
	}
}

func TestLogin_DeviceFlow(t *testing.T) {
	t.Run("opener missing prints the manual hint", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, loginRoutes(200))
		t.Setenv("PATH", t.TempDir())

		res := runCLI(t, "\n", "login")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "ABCD-1234")
		assert.Contains(t, res.stdout, "Please open https://example.com/device manually")
		assert.Contains(t, res.stdout, "Authentication complete.")
		assert.Contains(t, res.stdout, "Logged in as octo")

		stored, err := keyring.Get(auth.KeyringServiceName, auth.DefaultHostname)
		require.NoError(t, err)
		assert.Equal(t, "gho_flow", stored)
	})

	t.Run("no-prompt opens the browser without waiting for Enter", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, loginRoutes(200))
		opener := map[string]string{"darwin": "open", "linux": "xdg-open"}[runtime.GOOS]
		if opener == "" {
			t.Skip("no fake opener for " + runtime.GOOS)
		}
		dir := t.TempDir()
		marker := filepath.Join(dir, "opened")
		require.NoError(t, os.WriteFile(filepath.Join(dir, opener), []byte("#!/bin/sh\necho \"$1\" > "+marker+"\n"), 0o755)) //nolint:gosec // test script
		t.Setenv("PATH", dir)

		res := runCLI(t, "", "login", "--no-prompt", "--hostname", "github.com")
		require.NoError(t, res.err)
		assert.NotContains(t, res.stdout, "manually")
		assert.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 5e9, 10e6)
	})

	t.Run("invalid token after the flow", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, loginRoutes(401))
		t.Setenv("PATH", t.TempDir())
		res := runCLI(t, "", "login", "--no-prompt")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "failed to validate token")
	})

	t.Run("device flow failure", func(t *testing.T) {
		testEnv(t)
		fakeGitHub(t, map[string]fakeRoute{"POST /login/device/code": route(404, `{}`)})
		res := runCLI(t, "", "login", "--no-prompt")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "authentication failed")
	})
}

func TestLogout(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "", "logout")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "Not logged in to github.com")
	})
	t.Run("removes the stored token", func(t *testing.T) {
		testEnv(t)
		require.NoError(t, keyring.Set(auth.KeyringServiceName, "github.com", "tok"))
		res := runCLI(t, "", "logout")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "Logged out of github.com")
		_, err := keyring.Get(auth.KeyringServiceName, "github.com")
		assert.ErrorIs(t, err, keyring.ErrNotFound)
	})
	t.Run("enterprise host", func(t *testing.T) {
		testEnv(t)
		require.NoError(t, keyring.Set(auth.KeyringServiceName, "ghe.example.com", "tok"))
		res := runCLI(t, "", "logout", "--hostname", "ghe.example.com")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "Logged out of ghe.example.com")
	})
}

func TestAuthStatus(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		testEnv(t)
		res := runCLI(t, "", "auth", "status")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "Not logged in to github.com")
		assert.Contains(t, res.stdout, "githubby login")
	})
	t.Run("logged in", func(t *testing.T) {
		testEnv(t)
		require.NoError(t, keyring.Set(auth.KeyringServiceName, "github.com", "ghp_abcdefghijkl"))
		fakeGitHub(t, map[string]fakeRoute{"GET /user": route(200, userJSON)})
		res := runCLI(t, "", "auth", "status")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "Logged in to github.com as octo")
		assert.Contains(t, res.stdout, "ghp_****ijkl")
		assert.Contains(t, res.stdout, "Token source: keychain")
		assert.Contains(t, res.stdout, "Name: Octo Cat")
		assert.Contains(t, res.stdout, "Email: octo@example.com")
		assert.NotContains(t, res.stdout, "abcdefghijkl", "the token must stay masked")
	})
	t.Run("logged in without name and email", func(t *testing.T) {
		testEnv(t)
		t.Setenv("GITHUB_TOKEN", "ghp_fromenvironment")
		fakeGitHub(t, map[string]fakeRoute{"GET /user": route(200, `{"login":"plain"}`)})
		res := runCLI(t, "", "auth", "status")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "as plain")
		assert.Contains(t, res.stdout, "environment variable")
		assert.NotContains(t, res.stdout, "Name:")
		assert.NotContains(t, res.stdout, "Email:")
	})
	t.Run("expired token", func(t *testing.T) {
		testEnv(t)
		t.Setenv("GITHUB_TOKEN", "ghp_expired_token")
		fakeGitHub(t, map[string]fakeRoute{"GET /user": route(401, `{"message":"Bad credentials"}`)})
		res := runCLI(t, "", "auth", "status")
		require.NoError(t, res.err)
		assert.Contains(t, res.stdout, "token is invalid or expired")
		assert.Contains(t, res.stdout, "To re-authenticate")
	})
}
