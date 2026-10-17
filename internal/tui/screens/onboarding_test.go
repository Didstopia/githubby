package screens

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/cli/oauth/device"
	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/Didstopia/githubby/internal/auth"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
)

type ob_roundTrip func(*http.Request) (*http.Response, error)

func (f ob_roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// ob_isolate swaps the network, keychain and home directory for fakes.
func ob_isolate(t *testing.T, fn ob_roundTrip) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	prev := homedir.DisableCache
	homedir.DisableCache = true
	keyring.MockInit()
	orig := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() {
		http.DefaultTransport = orig
		homedir.DisableCache = prev
		keyring.MockInit()
	})
	return home
}

func ob_resp(status int, ctype, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{ctype}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// ob_github fakes the device flow endpoints and the user lookup.
func ob_github(r *http.Request) (*http.Response, error) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/device/code"):
		return ob_resp(200, "application/x-www-form-urlencoded", "device_code=DEV&user_code=ABCD-1234&verification_uri=https%3A%2F%2Fexample.com%2Fdev&interval=0&expires_in=60"), nil
	case strings.HasSuffix(r.URL.Path, "/access_token"):
		return ob_resp(200, "application/x-www-form-urlencoded", "access_token=gho_tok&token_type=bearer"), nil
	case r.URL.Path == "/user":
		return ob_resp(200, "application/json", `{"login":"octo"}`), nil
	}
	return ob_resp(404, "application/json", `{}`), nil
}

// ob_fakeDesktopTools puts do-nothing clipboard and browser commands first in PATH.
func ob_fakeDesktopTools(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"pbcopy", "open", "xclip", "xsel", "xdg-open"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\ncat >/dev/null 2>&1\nexit 0\n"), 0o755)) //nolint:gosec // test script
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func ob_key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func ob_new(opts ...OnboardingOption) *Onboarding {
	return NewOnboarding(context.Background(), opts...)
}

func TestOB_BasicsAndOptions(t *testing.T) {
	o := ob_new()
	assert.Equal(t, "Setup Wizard", o.Title())
	assert.Len(t, o.ShortHelp(), 2)
	assert.Equal(t, StepAuthMethod, o.step)
	assert.False(t, o.embeddedMode)
	assert.NotNil(t, o.Init())

	st := state.NewStorageWithPath(filepath.Join(t.TempDir(), "s.yaml"))
	o = ob_new(WithEmbeddedMode(), WithStorage(st), WithSkipToSyncConfig("alice"))
	assert.True(t, o.embeddedMode)
	assert.Same(t, st, o.storage)
	assert.Equal(t, StepSyncConfig, o.step)
	assert.Equal(t, "alice", o.username)

	assert.NotNil(t, o.Init())
	assert.Equal(t, "alice", o.syncUser, "username becomes the default sync user")
	cwd, _ := os.Getwd()
	assert.Equal(t, cwd, o.syncTarget)
}

func TestOB_WindowSize(t *testing.T) {
	o := ob_new()
	o.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	assert.Equal(t, 100, o.width)
	assert.Equal(t, 50, o.height)
}

func TestOB_ExitConfirmation(t *testing.T) {
	t.Run("ctrl+c twice quits", func(t *testing.T) {
		o := ob_new()
		_, cmd := o.Update(ob_key("ctrl+c"))
		assert.NotNil(t, cmd)
		assert.True(t, o.exitPending)
		assert.Contains(t, o.View(), "Press Ctrl+C again to quit")
		_, cmd = o.Update(ob_key("ctrl+c"))
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})
	t.Run("esc twice quits on the first step", func(t *testing.T) {
		o := ob_new()
		o.Update(ob_key("esc"))
		assert.Contains(t, o.View(), "Press Escape again to quit")
		_, cmd := o.Update(ob_key("esc"))
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})
	t.Run("q twice quits", func(t *testing.T) {
		o := ob_new(WithSkipToSyncConfig("a"))
		o.step = StepComplete
		o.Update(ob_key("q"))
		assert.Contains(t, o.View(), "Press q again to quit")
		_, cmd := o.Update(ob_key("q"))
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})
	t.Run("another key or a timeout cancels", func(t *testing.T) {
		o := ob_new()
		o.Update(ob_key("ctrl+c"))
		o.Update(ob_key("x"))
		assert.False(t, o.exitPending)

		o.Update(ob_key("ctrl+c"))
		o.Update(exitTimeoutMsg{})
		assert.False(t, o.exitPending)
	})
	t.Run("esc on a later step goes back", func(t *testing.T) {
		o := ob_new()
		o.step = StepTokenInput
		o.exitPending = true
		_, cmd := o.Update(ob_key("esc"))
		assert.Equal(t, StepOAuthFlow, o.step)
		assert.False(t, o.exitPending)
		assert.Nil(t, cmd, "nothing to init for the OAuth step")

		o.step = StepSyncConfig
		_, cmd = o.Update(ob_key("esc"))
		assert.Equal(t, StepTokenInput, o.step)
		assert.NotNil(t, cmd)
	})
}

func TestOB_InitCurrentForm(t *testing.T) {
	o := ob_new()
	for _, step := range []OnboardingStep{StepAuthMethod, StepTokenInput, StepSyncConfig} {
		o.step = step
		assert.NotNil(t, o.initCurrentForm(), step)
	}
	o.step = StepOAuthFlow
	assert.Nil(t, o.initCurrentForm())
}

func TestOB_SpinnerTickOnlyDuringOAuth(t *testing.T) {
	o := ob_new()
	_, cmd := o.Update(spinner.TickMsg{})
	assert.Nil(t, cmd)
	o.step = StepOAuthFlow
	_, cmd = o.Update(o.oauthSpinner.Tick())
	assert.NotNil(t, cmd)
}

func TestOB_AuthFormChoosesMethod(t *testing.T) {
	t.Run("token", func(t *testing.T) {
		o := ob_new()
		o.authMethod = "token"
		o.authForm.State = huh.StateCompleted
		_, cmd := o.Update(struct{}{})
		assert.Equal(t, StepTokenInput, o.step)
		assert.NotNil(t, cmd)
	})
	t.Run("oauth requests a device code", func(t *testing.T) {
		ob_isolate(t, ob_github)
		o := ob_new()
		o.authMethod = "oauth"
		o.authForm.State = huh.StateCompleted
		_, cmd := o.Update(struct{}{})
		assert.Equal(t, StepOAuthFlow, o.step)
		assert.NotNil(t, cmd)

		msg, ok := o.waitForOAuthCode()().(oauthCodeMsg)
		require.True(t, ok)
		assert.Equal(t, "ABCD-1234", msg.code.UserCode)
	})
	t.Run("device code failure becomes an oauth error", func(t *testing.T) {
		ob_isolate(t, func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })
		o := ob_new()
		o.step = StepOAuthFlow
		o.startOAuthFlow()
		msg, ok := o.waitForOAuthCode()().(oauthCompleteMsg)
		require.True(t, ok)
		require.Error(t, msg.err)
		assert.Contains(t, msg.err.Error(), "failed to get device code")

		o.Update(msg)
		assert.Equal(t, "Authentication failed", o.oauthStatus)
		v := o.View()
		assert.Contains(t, v, "Error:")
		assert.Contains(t, v, "Esc to go back")
	})
}

func TestOB_OAuthHappyPath(t *testing.T) {
	ob_isolate(t, ob_github)
	ob_fakeDesktopTools(t)

	o := ob_new(WithEmbeddedMode())
	o.step = StepOAuthFlow
	assert.Contains(t, o.View(), "Starting authentication")

	o.startOAuthFlow()
	codeMsg := o.waitForOAuthCode()()
	_, cmd := o.Update(codeMsg)
	require.NotNil(t, cmd)
	assert.Equal(t, "Waiting for browser authorization...", o.oauthStatus)
	assert.Contains(t, o.View(), "ABCD-1234")
	assert.Contains(t, o.View(), "https://example.com/dev")

	done, ok := o.waitForOAuthResult()().(oauthCompleteMsg)
	require.True(t, ok)
	require.NoError(t, done.err)
	assert.Equal(t, "gho_tok", done.token)
	assert.Equal(t, "octo", done.username)

	_, cmd = o.Update(done)
	assert.Equal(t, StepSyncConfig, o.step)
	assert.Equal(t, "octo", o.username)
	assert.Equal(t, "gho_tok", o.token)
	assert.Equal(t, "octo", o.syncUser)
	assert.NotNil(t, cmd)

	stored, _, err := auth.NewStorage().GetToken("")
	require.NoError(t, err)
	assert.Equal(t, "gho_tok", stored)
}

func TestOB_OAuthCodeViewVariants(t *testing.T) {
	o := ob_new()
	o.step = StepOAuthFlow
	o.oauthCode = &device.CodeResponse{UserCode: "WXYZ-9999"}
	o.codeCopied = true
	v := o.View()
	assert.Contains(t, v, "(copied to clipboard)")
	assert.Contains(t, v, "https://github.com/login/device", "falls back to the default URL")
}

func TestOB_OAuthCodeMsgWithNilCode(t *testing.T) {
	o := ob_new()
	o.step = StepOAuthFlow
	_, cmd := o.Update(oauthCodeMsg{})
	assert.Nil(t, cmd)
}

func TestOB_OAuthFlowFailures(t *testing.T) {
	t.Run("polling fails", func(t *testing.T) {
		ob_isolate(t, func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/access_token") {
				return ob_resp(200, "application/x-www-form-urlencoded", "error=access_denied"), nil
			}
			return ob_github(r)
		})
		o := ob_new()
		o.oauthCode = &device.CodeResponse{DeviceCode: "D", ExpiresIn: 60}
		o.waitForOAuthComplete()
		msg := o.waitForOAuthResult()().(oauthCompleteMsg)
		require.Error(t, msg.err)
		o.Update(msg)
		assert.Equal(t, "Authentication failed", o.oauthStatus)
	})
	t.Run("token validation fails", func(t *testing.T) {
		ob_isolate(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/user" {
				return ob_resp(401, "application/json", `{"message":"bad"}`), nil
			}
			return ob_github(r)
		})
		o := ob_new()
		o.oauthCode = &device.CodeResponse{DeviceCode: "D", ExpiresIn: 60}
		o.waitForOAuthComplete()
		msg := o.waitForOAuthResult()().(oauthCompleteMsg)
		require.Error(t, msg.err)
		assert.Contains(t, msg.err.Error(), "token validation failed")
	})
	t.Run("storing the token fails", func(t *testing.T) {
		home := ob_isolate(t, ob_github)
		keyring.MockInitWithError(errors.New("locked"))
		// Without a keychain the token goes to ~/.githubby.yaml; make that path unusable.
		require.NoError(t, os.Mkdir(filepath.Join(home, ".githubby.yaml"), 0o700))

		o := ob_new()
		o.Update(oauthCompleteMsg{token: "tok", username: "octo"})
		assert.Equal(t, "Failed to store token", o.oauthStatus)
		require.Error(t, o.oauthError)
		assert.Equal(t, StepAuthMethod, o.step)
	})
}

func TestOB_TokenFlow(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		ob_isolate(t, ob_github)
		o := ob_new()
		o.step = StepTokenInput
		o.tokenInput = "ghp_valid_token_value"
		o.tokenForm.State = huh.StateCompleted

		_, cmd := o.Update(struct{}{})
		assert.True(t, o.loading)
		assert.NotNil(t, cmd)
		assert.Contains(t, o.View(), "Validating token")

		msg, ok := o.waitForTokenValidation()().(tokenValidateMsg)
		require.True(t, ok)
		require.NoError(t, msg.err)
		assert.Equal(t, "octo", msg.username)

		o.Update(msg)
		assert.Equal(t, StepSyncConfig, o.step)
		assert.Equal(t, "ghp_valid_token_value", o.token)
		assert.Equal(t, "octo", o.username)
		stored, _, err := auth.NewStorage().GetToken("")
		require.NoError(t, err)
		assert.Equal(t, "ghp_valid_token_value", stored)
	})
	t.Run("invalid token shows the error", func(t *testing.T) {
		ob_isolate(t, func(*http.Request) (*http.Response, error) {
			return ob_resp(401, "application/json", `{"message":"Bad credentials"}`), nil
		})
		o := ob_new()
		o.step = StepTokenInput
		o.tokenInput = "ghp_invalid_token"
		o.validateToken()
		msg := o.waitForTokenValidation()().(tokenValidateMsg)
		require.Error(t, msg.err)

		o.loading = true
		o.Update(msg)
		assert.False(t, o.loading)
		assert.Equal(t, StepTokenInput, o.step)
		v := o.View()
		assert.Contains(t, v, "Error:")
		assert.Contains(t, v, "Personal Access Token")
	})
	t.Run("storage failure is reported", func(t *testing.T) {
		home := ob_isolate(t, ob_github)
		keyring.MockInitWithError(errors.New("locked"))
		require.NoError(t, os.Mkdir(filepath.Join(home, ".githubby.yaml"), 0o700))

		o := ob_new()
		o.step = StepTokenInput
		o.tokenInput = "ghp_valid_token_value"
		o.Update(tokenValidateMsg{username: "octo"})
		require.Error(t, o.err)
		assert.Equal(t, StepTokenInput, o.step)
	})
}

func TestOB_TokenFormValidation(t *testing.T) {
	// The form rejects short tokens: submitting an empty value must not complete the form.
	o := ob_new()
	o.step = StepTokenInput
	o.Init()
	o.Update(ob_key("enter"))
	assert.NotEqual(t, huh.StateCompleted, o.tokenForm.State)
	assert.Equal(t, StepTokenInput, o.step)
}

func TestOB_SyncConfigAndComplete(t *testing.T) {
	st := state.NewStorageWithPath(filepath.Join(t.TempDir(), "state.yaml"))
	o := ob_new(WithEmbeddedMode(), WithStorage(st), WithSkipToSyncConfig("alice"))
	o.Init()
	assert.Contains(t, o.View(), "Welcome, alice!")
	o.syncTarget = "/srv/repos"

	o.syncConfigForm.State = huh.StateCompleted
	_, cmd := o.Update(struct{}{})
	assert.Nil(t, cmd)
	assert.Equal(t, StepComplete, o.step)
	assert.Equal(t, "/srv/repos", st.GetDefaultTargetDir())
	assert.Equal(t, "alice", st.GetDefaultUsername())

	v := o.View()
	assert.Contains(t, v, "Setup Complete!")
	assert.Contains(t, v, "You're all set, alice!")
	assert.Contains(t, v, "githubby sync --user <username>")

	// Other keys do nothing; Enter in embedded mode hands the auth result to the app.
	_, cmd = o.Update(ob_key("x"))
	assert.Nil(t, cmd)
	assert.False(t, o.launchDashboard)

	o.token = "tok"
	_, cmd = o.Update(ob_key("enter"))
	assert.True(t, o.launchDashboard)
	assert.Equal(t, tui.AuthCompleteMsg{Token: "tok", Username: "alice"}, cmd())
}

func TestOB_CompleteStandaloneAndAnonymous(t *testing.T) {
	o := ob_new()
	o.step = StepComplete
	assert.Contains(t, o.View(), "You're all set!")
	_, cmd := o.Update(ob_key("enter"))
	assert.True(t, o.launchDashboard)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestOB_SaveSyncConfigWithoutStorage(t *testing.T) {
	o := ob_new()
	assert.NotPanics(t, o.saveSyncConfig)
}

func TestOB_ViewsPerStep(t *testing.T) {
	o := ob_new()
	assert.Contains(t, o.View(), "Welcome to GitHubby!")
	o.step = StepTokenInput
	assert.Contains(t, o.View(), "Personal Access Token")
	o.step = StepSyncConfig
	assert.Contains(t, o.View(), "Sync Configuration")
	assert.NotContains(t, o.View(), "Welcome,")
}

func TestOB_SetSyncConfigDefaults_Cwd(t *testing.T) {
	o := ob_new()
	o.setSyncConfigDefaults()
	cwd, _ := os.Getwd()
	assert.Equal(t, cwd, o.syncTarget)
	assert.Empty(t, o.syncUser)
}

func TestOB_SetSyncConfigDefaults_NoCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	orig, err := os.Getwd()
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Remove(dir))

	o := ob_new()
	o.setSyncConfigDefaults()
	if _, err := os.Getwd(); err != nil {
		assert.Equal(t, ".", o.syncTarget)
	}
}

func TestOB_RunOnboardingProgram(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	extraProgramOptions = []tea.ProgramOption{tea.WithInput(nil)} // never wait on a console read
	t.Cleanup(func() { extraProgramOptions = nil })
	assert.NotPanics(t, func() { _, _ = RunOnboardingProgram(ctx) })
}
