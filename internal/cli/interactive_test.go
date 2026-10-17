package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
)

// fakeTUI makes RunTUI think it has a terminal and captures the app instead of running it.
func fakeTUI(t *testing.T, runErr error) **tui.App {
	t.Helper()
	var captured *tui.App
	origInteractive, origRun := isInteractive, runAppInstance
	isInteractive = func() bool { return true }
	runAppInstance = func(ctx context.Context, app *tui.App) error {
		captured = app
		return runErr
	}
	t.Cleanup(func() { isInteractive, runAppInstance = origInteractive, origRun })
	return &captured
}

func TestRunTUI_NonInteractivePrintsHelp(t *testing.T) {
	testEnv(t)
	orig := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = orig })

	out, _ := captureOutput(t, func() { require.NoError(t, RunTUI()) })
	assert.Contains(t, out, "Interactive mode is not available")
}

func TestRunTUI_NotLoggedInStartsOnboarding(t *testing.T) {
	testEnv(t)
	app := fakeTUI(t, nil)

	require.NoError(t, RunTUI())
	require.NotNil(t, *app)
	assert.False(t, (*app).IsAuthenticated())
	assert.Nil(t, (*app).GitHubClient())

	(*app).SetScreen(tui.ScreenOnboarding)
	assert.Contains(t, (*app).View(), "Setup Wizard")
}

func TestRunTUI_RejectedTokenIsTreatedAsLoggedOut(t *testing.T) {
	testEnv(t)
	t.Setenv("GITHUB_TOKEN", "ghp_rejected")
	fakeGitHub(t, map[string]fakeRoute{"GET /user": route(401, `{"message":"Bad credentials"}`)})
	app := fakeTUI(t, nil)

	require.NoError(t, RunTUI())
	assert.False(t, (*app).IsAuthenticated())
}

func TestRunTUI_LoggedInWithoutProfilesResumesOnboarding(t *testing.T) {
	testEnv(t)
	t.Setenv("GITHUB_TOKEN", "ghp_valid")
	fakeGitHub(t, map[string]fakeRoute{"GET /user": route(200, userJSON)})
	app := fakeTUI(t, nil)

	require.NoError(t, RunTUI())
	a := *app
	assert.True(t, a.IsAuthenticated())
	assert.Equal(t, "octo", a.Username())
	assert.Equal(t, "ghp_valid", a.Token())
	assert.NotNil(t, a.GitHubClient())
	a.SetScreen(tui.ScreenOnboarding)
	assert.Contains(t, a.View(), "Setup Wizard")
}

func TestRunTUI_LoggedInWithProfilesShowsDashboard(t *testing.T) {
	testEnv(t)
	t.Setenv("GITHUB_TOKEN", "ghp_valid")
	fakeGitHub(t, map[string]fakeRoute{"GET /user": route(200, userJSON)})
	st, err := state.NewStorage()
	require.NoError(t, err)
	require.NoError(t, st.AddProfile(state.NewProfile("p", "user", "octo", filepath.Join(t.TempDir(), "x"), false)))
	app := fakeTUI(t, nil)

	require.NoError(t, RunTUI())
	(*app).SetScreen(tui.ScreenDashboard)
	assert.Contains(t, (*app).View(), "Dashboard")
}

func TestRunTUI_PassesOnRunnerError(t *testing.T) {
	testEnv(t)
	fakeTUI(t, errors.New("terminal exploded"))
	assert.EqualError(t, RunTUI(), "terminal exploded")
}

func TestRunTUI_BrokenStateFileStartsFresh(t *testing.T) {
	home := testEnv(t)
	require.NoError(t, mkStateFile(home, "profiles: [unclosed"))
	app := fakeTUI(t, nil)
	require.NoError(t, RunTUI())
	assert.NotNil(t, *app)
}

func TestRunTUI_NoHomeDirectory(t *testing.T) {
	testEnv(t)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // Windows reads the home directory from here
	fakeTUI(t, nil)

	err := RunTUI()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize state storage")
}

func TestRegisterScreenFactories_BuildEveryScreen(t *testing.T) {
	testEnv(t)
	app := tui.NewApp(tui.WithGitHubClient(nil))
	registerScreenFactories(app)

	titles := map[tui.Screen]string{
		tui.ScreenOnboarding:    "Setup Wizard",
		tui.ScreenDashboard:     "Dashboard",
		tui.ScreenSyncWizard:    "",
		tui.ScreenClean:         "Clean Releases",
		tui.ScreenSyncProgress:  "",
		tui.ScreenConfirmDelete: "Confirm Delete",
	}
	for screen, title := range titles {
		app.SetScreen(screen)
		view := app.View()
		assert.NotEqual(t, "Loading...", view, "screen %d has no factory", screen)
		if title != "" {
			assert.Contains(t, view, title)
		}
	}
}
