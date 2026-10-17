package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
)

// fakeScreen is a minimal ScreenModel that records what it receives.
type fakeScreen struct {
	title string
	inits int
	msgs  []tea.Msg
	cmd   tea.Cmd
	help  []key.Binding
}

func (f *fakeScreen) Init() tea.Cmd { f.inits++; return nil }
func (f *fakeScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	f.msgs = append(f.msgs, msg)
	return f, f.cmd
}
func (f *fakeScreen) View() string             { return "view of " + f.title }
func (f *fakeScreen) Title() string            { return f.title }
func (f *fakeScreen) ShortHelp() []key.Binding { return f.help }

// otherModel is a tea.Model that is not a ScreenModel, to cover the type-assertion miss.
type otherModel struct{ *fakeScreen }

func (o otherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return struct{ tea.Model }{o}, nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeNetwork(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func failingNetwork(t *testing.T) {
	fakeNetwork(t, func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })
}

// releaseNetwork serves one release list; the version is the only thing tests care about.
func releaseNetwork(t *testing.T, version string) {
	asset := fmt.Sprintf("githubby_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	body := fmt.Sprintf(`[{"id":1,"tag_name":"v%s","html_url":"https://example.com/r","assets":[{"id":11,"name":%q,"size":1,"browser_download_url":"https://example.com/a"},{"id":12,"name":"checksums.txt","size":1,"browser_download_url":"https://example.com/c"}]}]`, version, asset)
	fakeNetwork(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
}

func newTestApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	return NewApp(opts...)
}

func newStorage(t *testing.T) *state.Storage {
	t.Helper()
	return state.NewStorageWithPath(filepath.Join(t.TempDir(), "state.yaml"))
}

func TestNewApp_Defaults(t *testing.T) {
	a := NewApp()
	assert.Equal(t, ScreenOnboarding, a.currentScreen)
	assert.Equal(t, 80, a.Width())
	assert.Equal(t, 24, a.Height())
	assert.False(t, a.IsAuthenticated())
	assert.Empty(t, a.Username())
	assert.Empty(t, a.Token())
	assert.NotNil(t, a.Context())
	assert.NotNil(t, a.Styles())
	assert.Nil(t, a.GitHubClient())
	assert.Nil(t, a.Storage())
}

func TestNewApp_Options(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st := newStorage(t)
	gh := github.NewMockClient()

	a := NewApp(
		WithContext(ctx),
		WithAuth(true, "alice", "tok"),
		WithGitHubClient(gh),
		WithStorage(st),
		WithVersion("1.2.3", "abcdef123", "2026-01-02T00:00:00Z"),
	)
	assert.True(t, a.IsAuthenticated())
	assert.Equal(t, "alice", a.Username())
	assert.Equal(t, "tok", a.Token())
	assert.Same(t, gh, a.GitHubClient())
	assert.Same(t, st, a.Storage())
	assert.Equal(t, "1.2.3", a.version)

	cancel()
	<-a.Context().Done() // the app context derives from the one passed in
}

func TestAppSettersAndGetters(t *testing.T) {
	a := NewApp()
	p := state.NewProfile("p", "user", "alice", "/t", false)
	a.SetSelectedProfile(p)
	assert.Same(t, p, a.SelectedProfile())
	a.SetProfilesToSync([]*state.SyncProfile{p})
	assert.Len(t, a.ProfilesToSync(), 1)
	a.deleteProfileID, a.deleteProfileName = "id", "name"
	id, name := a.DeleteProfileInfo()
	assert.Equal(t, "id", id)
	assert.Equal(t, "name", name)
}

func TestScreenManagement(t *testing.T) {
	a := NewApp()
	created := 0
	a.RegisterScreenFactory(ScreenDashboard, func(ctx context.Context, app *App) ScreenModel {
		created++
		return &fakeScreen{title: "dash"}
	})
	reg := &fakeScreen{title: "onb"}
	a.RegisterScreen(ScreenOnboarding, reg)

	assert.Nil(t, a.getOrCreateScreen(ScreenClean), "unknown screen has no model")
	assert.Nil(t, a.SetScreen(ScreenClean), "no model means no init command")

	a.SetScreen(ScreenDashboard)
	a.SetScreen(ScreenDashboard)
	assert.Equal(t, 1, created, "factory result is cached")
	assert.Equal(t, ScreenDashboard, a.currentScreen)

	a.currentScreen = ScreenOnboarding
	a.PushScreen(ScreenDashboard)
	assert.Equal(t, []Screen{ScreenOnboarding}, a.screenStack)
	assert.Equal(t, ScreenDashboard, a.currentScreen)

	a.PopScreen()
	assert.Equal(t, ScreenOnboarding, a.currentScreen)
	assert.Empty(t, a.screenStack)
	assert.Nil(t, a.PopScreen(), "pop on an empty stack does nothing")
	assert.Equal(t, ScreenOnboarding, a.currentScreen)

	a.PushScreen(ScreenDashboard)
	a.ResetToScreen(ScreenOnboarding)
	assert.Empty(t, a.screenStack)
	assert.Equal(t, ScreenOnboarding, a.currentScreen)
	assert.Equal(t, 2, reg.inits, "returning to a screen runs its Init again (pop, then reset)")
}

func TestInit_DevBuildInitialisesCurrentScreen(t *testing.T) {
	a := NewApp(WithVersion("dev", "", ""))
	s := &fakeScreen{title: "onb"}
	a.RegisterScreen(ScreenOnboarding, s)

	a.Init()
	assert.Equal(t, 1, s.inits)
	assert.Equal(t, UpdatePhaseNone, a.startupUpdatePhase)
}

func TestInit_ReleaseBuildChecksForUpdates(t *testing.T) {
	t.Run("no update", func(t *testing.T) {
		failingNetwork(t)
		a := NewApp(WithVersion("1.0.0", "", ""))
		cmd := a.Init()
		assert.Equal(t, UpdatePhaseChecking, a.startupUpdatePhase)
		require.NotNil(t, cmd)
		assert.IsType(t, StartupUpdateNotNeededMsg{}, cmd())
	})
	t.Run("update available", func(t *testing.T) {
		releaseNetwork(t, "2.0.0")
		a := NewApp(WithVersion("1.0.0", "", ""))
		msg := a.Init()()
		avail, ok := msg.(StartupUpdateAvailableMsg)
		require.True(t, ok, "got %T", msg)
		assert.Equal(t, "1.0.0", avail.CurrentVersion)
		assert.Equal(t, "2.0.0", avail.LatestVersion)
	})
	t.Run("already latest", func(t *testing.T) {
		releaseNetwork(t, "1.0.0")
		a := NewApp(WithVersion("1.0.0", "", ""))
		assert.IsType(t, StartupUpdateNotNeededMsg{}, a.checkForStartupUpdateCmd()())
	})
}

func TestBackgroundUpdateCheck(t *testing.T) {
	t.Run("dev builds skip it", func(t *testing.T) {
		assert.Nil(t, NewApp(WithVersion("dev", "", "")).checkForUpdateCmd())
	})
	t.Run("error is swallowed", func(t *testing.T) {
		failingNetwork(t)
		assert.Nil(t, NewApp(WithVersion("1.0.0", "", "")).checkForUpdateCmd()())
	})
	t.Run("no newer version gives no message", func(t *testing.T) {
		releaseNetwork(t, "1.0.0")
		assert.Nil(t, NewApp(WithVersion("1.0.0", "", "")).checkForUpdateCmd()())
	})
	t.Run("newer version", func(t *testing.T) {
		releaseNetwork(t, "1.5.0")
		msg := NewApp(WithVersion("1.0.0", "", "")).checkForUpdateCmd()()
		assert.Equal(t, UpdateAvailableMsg{CurrentVersion: "1.0.0", LatestVersion: "1.5.0"}, msg)
	})
}

func TestPerformUpdateCmds_ReportErrors(t *testing.T) {
	failingNetwork(t)
	a := NewApp(WithVersion("1.0.0", "", ""))

	startup, ok := a.performStartupUpdateCmd()().(StartupUpdateErrorMsg)
	require.True(t, ok)
	assert.Error(t, startup.Error)

	runtimeMsg, ok := a.performUpdateCmd()().(UpdateCompleteMsg)
	require.True(t, ok)
	assert.Error(t, runtimeMsg.Error)
}

func TestPerformUpdateCmds_NothingToInstall(t *testing.T) {
	// An empty release list means "no update": no error and nothing is installed.
	fakeNetwork(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
	})
	a := NewApp(WithVersion("1.0.0", "", ""))
	assert.IsType(t, StartupUpdateCompleteMsg{}, a.performStartupUpdateCmd()())
	assert.Equal(t, UpdateCompleteMsg{}, a.performUpdateCmd()())
}

func TestDelayedRestartCmd(t *testing.T) {
	a := NewApp()
	assert.IsType(t, StartupRestartMsg{}, a.delayedRestartCmd(3*time.Second)())
	assert.NotNil(t, a.delayedRestartCmd(0), "short elapsed time waits for the minimum display time")
}

func TestUpdate_StartupUpdateFlow(t *testing.T) {
	a := NewApp(WithVersion("dev", "", ""))
	a.startupUpdatePhase = UpdatePhaseChecking
	s := &fakeScreen{title: "onb"}
	a.RegisterScreen(ScreenOnboarding, s)

	// Keys are swallowed while the startup update runs.
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.Nil(t, cmd)
	assert.Empty(t, s.msgs)

	_, _ = a.Update(StartupUpdateAvailableMsg{CurrentVersion: "1", LatestVersion: "2"})
	assert.Equal(t, UpdatePhaseDownloading, a.startupUpdatePhase)
	assert.Equal(t, "2", a.startupUpdateVersion)
	assert.False(t, a.updateStartTime.IsZero())

	_, cmd = a.Update(StartupUpdateProgressMsg{Status: "x"})
	assert.Nil(t, cmd)

	a.updateStartTime = time.Now().Add(-time.Minute)
	_, cmd = a.Update(StartupUpdateCompleteMsg{NewVersion: "2"})
	assert.Equal(t, UpdatePhaseComplete, a.startupUpdatePhase)
	assert.IsType(t, StartupRestartMsg{}, cmd())

	_, cmd = a.Update(StartupRestartMsg{})
	assert.True(t, a.restartAfterQuit)
	assert.True(t, a.quitting)
	assert.IsType(t, tea.QuitMsg{}, cmd())

	a.quitting = false
	boom := errors.New("boom")
	_, cmd = a.Update(StartupUpdateErrorMsg{Error: boom})
	assert.Equal(t, UpdatePhaseError, a.startupUpdatePhase)
	assert.Equal(t, boom, a.startupUpdateError)
	assert.NotNil(t, cmd)

	_, _ = a.Update(StartupUpdateNotNeededMsg{})
	assert.Equal(t, UpdatePhaseNone, a.startupUpdatePhase)
	assert.Equal(t, 1, s.inits, "normal init runs once the update phase ends")
}

func TestUpdate_RuntimeUpdateKeys(t *testing.T) {
	failingNetwork(t)
	press := func(s string) tea.KeyMsg {
		if s == "esc" {
			return tea.KeyMsg{Type: tea.KeyEsc}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}

	t.Run("u starts confirmation only when an update exists", func(t *testing.T) {
		a := NewApp(WithVersion("1.0.0", "", ""))
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		a.Update(press("u"))
		assert.False(t, a.updateConfirming)

		a.Update(UpdateAvailableMsg{CurrentVersion: "1.0.0", LatestVersion: "1.1.0"})
		assert.True(t, a.updateAvailable)
		assert.Equal(t, "1.1.0", a.latestVersion)
		a.Update(press("u"))
		assert.True(t, a.updateConfirming)
	})
	t.Run("confirm with y", func(t *testing.T) {
		a := NewApp(WithVersion("1.0.0", "", ""))
		a.updateConfirming = true
		_, cmd := a.Update(press("y"))
		assert.False(t, a.updateConfirming)
		assert.True(t, a.updateInProgress)
		require.NotNil(t, cmd)
		assert.IsType(t, UpdateCompleteMsg{}, cmd())
	})
	t.Run("cancel with n and esc, ignore other keys", func(t *testing.T) {
		for _, k := range []string{"n", "N", "esc"} {
			a := NewApp()
			a.updateConfirming = true
			a.Update(press(k))
			assert.False(t, a.updateConfirming, k)
			assert.False(t, a.updateInProgress, k)
		}
		a := NewApp()
		a.updateConfirming = true
		_, cmd := a.Update(press("x"))
		assert.True(t, a.updateConfirming)
		assert.Nil(t, cmd)
	})
	t.Run("any key after a finished update quits", func(t *testing.T) {
		a := NewApp()
		a.updateComplete = true
		_, cmd := a.Update(press("x"))
		assert.True(t, a.quitting)
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})
	t.Run("update result messages", func(t *testing.T) {
		a := NewApp()
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		a.updateInProgress = true
		a.Update(UpdateCompleteMsg{NewVersion: "9.9.9"})
		assert.False(t, a.updateInProgress)
		assert.True(t, a.updateComplete)
		assert.Equal(t, "9.9.9", a.version)

		b := NewApp()
		b.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		b.updateInProgress = true
		b.Update(UpdateCompleteMsg{Error: errors.New("bad")})
		assert.False(t, b.updateComplete)
		assert.EqualError(t, b.updateError, "bad")

		c := NewApp()
		c.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		c.updateInProgress = true
		c.Update(UpdateErrorMsg{Error: errors.New("nope")})
		assert.False(t, c.updateInProgress)
		assert.EqualError(t, c.updateError, "nope")
	})
}

func TestUpdate_NavigationMessages(t *testing.T) {
	a := NewApp()
	onb := &fakeScreen{title: "onb"}
	dash := &fakeScreen{title: "dash"}
	a.RegisterScreen(ScreenOnboarding, onb)
	a.RegisterScreen(ScreenDashboard, dash)

	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, a.Width())
	assert.Equal(t, 40, a.Height())
	assert.Len(t, onb.msgs, 1, "window size also reaches the current screen")

	a.Update(ScreenChangeMsg{Screen: ScreenDashboard})
	assert.Equal(t, ScreenDashboard, a.currentScreen)

	a.Update(ScreenPushMsg{Screen: ScreenOnboarding})
	assert.Equal(t, ScreenOnboarding, a.currentScreen)
	assert.Equal(t, []Screen{ScreenDashboard}, a.screenStack)

	a.Update(ScreenPopMsg{})
	assert.Equal(t, ScreenDashboard, a.currentScreen)

	_, cmd := a.Update(QuitMsg{})
	assert.True(t, a.quitting)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestUpdate_ErrorMsgIsStoredAndForwarded(t *testing.T) {
	a := NewApp()
	s := &fakeScreen{}
	a.RegisterScreen(ScreenOnboarding, s)
	a.Update(ErrorMsg{Err: errors.New("x")})
	assert.EqualError(t, a.err, "x")
	assert.Len(t, s.msgs, 1)
}

func TestUpdate_AuthComplete(t *testing.T) {
	t.Run("error is kept and screen stays", func(t *testing.T) {
		a := NewApp()
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		a.Update(AuthCompleteMsg{Error: errors.New("denied")})
		assert.EqualError(t, a.err, "denied")
		assert.False(t, a.IsAuthenticated())
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
	})
	t.Run("success authenticates and goes to the dashboard", func(t *testing.T) {
		st := newStorage(t)
		a := NewApp(WithStorage(st))
		a.RegisterScreen(ScreenDashboard, &fakeScreen{title: "dash"})
		a.screenStack = []Screen{ScreenSettings}

		a.Update(AuthCompleteMsg{Token: "tok", Username: "alice"})
		assert.True(t, a.IsAuthenticated())
		assert.Equal(t, "alice", a.Username())
		assert.Equal(t, "tok", a.Token())
		assert.NotNil(t, a.GitHubClient())
		assert.True(t, st.IsOnboardingComplete())
		assert.Equal(t, ScreenDashboard, a.currentScreen)
		assert.Empty(t, a.screenStack)
	})
	t.Run("empty token creates no client and works without storage", func(t *testing.T) {
		a := NewApp()
		a.RegisterScreen(ScreenDashboard, &fakeScreen{})
		a.Update(AuthCompleteMsg{Username: "bob"})
		assert.Nil(t, a.GitHubClient())
		assert.Equal(t, ScreenDashboard, a.currentScreen)
	})
}

func TestUpdate_RefreshDashboard(t *testing.T) {
	a := NewApp()
	dash := &fakeScreen{title: "dash"}
	onb := &fakeScreen{title: "onb"}
	a.RegisterScreen(ScreenDashboard, dash)
	a.RegisterScreen(ScreenOnboarding, onb)

	a.Update(RefreshDashboardMsg{})
	assert.Empty(t, dash.msgs, "ignored while another screen is current")
	assert.Len(t, onb.msgs, 1)

	a.currentScreen = ScreenDashboard
	a.Update(RefreshDashboardMsg{})
	assert.NotEmpty(t, dash.msgs)
}

func TestUpdate_SyncRequests(t *testing.T) {
	st := newStorage(t)
	old := state.NewProfile("old", "user", "alice", "/a", false)
	old.LastSyncAt = time.Now().Add(-48 * time.Hour)
	fresh := state.NewProfile("fresh", "user", "bob", "/b", false)
	fresh.LastSyncAt = time.Now()
	require.NoError(t, st.AddProfile(old))
	require.NoError(t, st.AddProfile(fresh))

	newApp := func() *App {
		a := NewApp(WithStorage(st))
		for _, sc := range []Screen{ScreenOnboarding, ScreenSyncWizard, ScreenSyncProgress, ScreenConfirmDelete, ScreenDashboard} {
			sc := sc
			a.RegisterScreenFactory(sc, func(context.Context, *App) ScreenModel { return &fakeScreen{title: fmt.Sprint(sc)} })
		}
		return a
	}

	t.Run("new sync pushes a fresh wizard", func(t *testing.T) {
		a := newApp()
		stale := &fakeScreen{title: "stale"}
		a.RegisterScreen(ScreenSyncWizard, stale)
		a.Update(NewSyncRequestedMsg{})
		assert.Equal(t, ScreenSyncWizard, a.currentScreen)
		assert.NotSame(t, stale, a.screens[ScreenSyncWizard])
	})
	t.Run("profile selected", func(t *testing.T) {
		a := newApp()
		a.profilesToSync = []*state.SyncProfile{fresh}
		a.Update(ProfileSelectedMsg{Profile: old})
		assert.Same(t, old, a.SelectedProfile())
		assert.Nil(t, a.ProfilesToSync())
		assert.Equal(t, ScreenSyncProgress, a.currentScreen)
	})
	t.Run("sync all", func(t *testing.T) {
		a := newApp()
		a.selectedProfile = old
		a.Update(SyncAllProfilesMsg{})
		assert.Len(t, a.ProfilesToSync(), 2)
		assert.Nil(t, a.SelectedProfile())
		assert.Equal(t, ScreenSyncProgress, a.currentScreen)
	})
	t.Run("sync all without storage does nothing", func(t *testing.T) {
		a := NewApp()
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		a.Update(SyncAllProfilesMsg{})
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
	})
	t.Run("sync pending only takes profiles older than a day", func(t *testing.T) {
		a := newApp()
		a.Update(SyncPendingProfilesMsg{})
		require.Len(t, a.ProfilesToSync(), 1)
		assert.Equal(t, "old", a.ProfilesToSync()[0].Name)
		assert.Equal(t, ScreenSyncProgress, a.currentScreen)
	})
	t.Run("sync pending with nothing pending stays put", func(t *testing.T) {
		st2 := newStorage(t)
		require.NoError(t, st2.AddProfile(fresh))
		a := NewApp(WithStorage(st2))
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		a.Update(SyncPendingProfilesMsg{})
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
		assert.Nil(t, a.ProfilesToSync())
	})
	t.Run("sync pending without storage does nothing", func(t *testing.T) {
		a := NewApp()
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{})
		a.Update(SyncPendingProfilesMsg{})
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
	})
}

func TestUpdate_DeleteProfileFlow(t *testing.T) {
	st := newStorage(t)
	p := state.NewProfile("doomed", "user", "alice", "/a", false)
	require.NoError(t, st.AddProfile(p))

	a := NewApp(WithStorage(st))
	a.RegisterScreen(ScreenDashboard, &fakeScreen{title: "dash"})
	a.RegisterScreenFactory(ScreenConfirmDelete, func(context.Context, *App) ScreenModel { return &fakeScreen{title: "confirm"} })
	a.currentScreen = ScreenDashboard

	a.Update(DeleteProfileRequestMsg{ProfileID: p.ID, ProfileName: p.Name})
	assert.Equal(t, ScreenConfirmDelete, a.currentScreen)
	id, name := a.DeleteProfileInfo()
	assert.Equal(t, p.ID, id)
	assert.Equal(t, "doomed", name)

	a.Update(DeleteProfileCancelledMsg{})
	assert.Equal(t, ScreenDashboard, a.currentScreen)
	assert.Len(t, st.GetProfiles(), 1, "cancel keeps the profile")

	a.Update(DeleteProfileRequestMsg{ProfileID: p.ID, ProfileName: p.Name})
	_, cmd := a.Update(DeleteProfileConfirmedMsg{ProfileID: p.ID, ProfileName: p.Name})
	assert.Empty(t, st.GetProfiles())
	assert.Equal(t, ScreenDashboard, a.currentScreen)
	assert.NotNil(t, cmd)
	assert.NoError(t, a.err)

	// Deleting an unknown profile records the error.
	a.Update(DeleteProfileConfirmedMsg{ProfileID: "missing"})
	assert.Error(t, a.err)
}

func TestUpdate_ScreenReturningNonScreenModelKeepsOldScreen(t *testing.T) {
	a := NewApp()
	s := &fakeScreen{}
	a.RegisterScreen(ScreenOnboarding, otherModel{s})
	a.Update(ErrorMsg{})
	_, isOther := a.screens[ScreenOnboarding].(otherModel)
	assert.True(t, isOther)
}

func TestUpdate_ForwardsCommandFromScreen(t *testing.T) {
	a := NewApp()
	a.RegisterScreen(ScreenOnboarding, &fakeScreen{cmd: func() tea.Msg { return ErrorMsg{Err: errors.New("from screen")} }})
	_, cmd := a.Update(ErrorMsg{})
	require.NotNil(t, cmd)
	assert.Equal(t, "from screen", cmd().(ErrorMsg).Err.Error())
}

func TestView(t *testing.T) {
	a := NewApp()
	assert.Equal(t, "Loading...", stripChrome(a.View()), "no screen registered")

	a.RegisterScreen(ScreenOnboarding, &fakeScreen{title: "Welcome"})
	v := a.View()
	assert.Contains(t, v, "GitHubby")
	assert.Contains(t, v, "Welcome")
	assert.Contains(t, v, "view of Welcome")
	assert.Contains(t, v, "Not authenticated")

	a.isAuthenticated, a.username = true, "alice"
	assert.Contains(t, a.View(), "alice")

	a.quitting = true
	assert.Equal(t, "", a.View())
}

func stripChrome(v string) string {
	if strings.Contains(v, "Loading...") {
		return "Loading..."
	}
	return v
}

func TestView_StartupModal(t *testing.T) {
	tests := []struct {
		phase UpdatePhase
		want  string
	}{
		{UpdatePhaseChecking, "Checking for updates"},
		{UpdatePhaseAvailable, "Update available: v2.0.0"},
		{UpdatePhaseDownloading, "Updating to v2.0.0"},
		{UpdatePhaseComplete, "Restarting"},
		{UpdatePhaseError, "Continuing with current version"},
		{UpdatePhase(99), "Updating..."},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			a := NewApp()
			a.startupUpdatePhase = tt.phase
			a.startupUpdateVersion = "2.0.0"
			assert.Contains(t, a.View(), tt.want)
		})
	}

	t.Run("error shows the message", func(t *testing.T) {
		a := NewApp()
		a.startupUpdatePhase = UpdatePhaseError
		a.startupUpdateError = errors.New("disk full")
		assert.Contains(t, a.View(), "disk full")
	})
	t.Run("tiny terminal does not panic", func(t *testing.T) {
		a := NewApp()
		a.startupUpdatePhase = UpdatePhaseChecking
		a.width, a.height = 1, 1
		assert.NotPanics(t, func() { _ = a.View() })
	})
}

func TestFooter(t *testing.T) {
	newApp := func() *App {
		a := NewApp()
		a.RegisterScreen(ScreenOnboarding, &fakeScreen{
			title: "x",
			help: []key.Binding{
				key.NewBinding(key.WithKeys("e"), key.WithHelp("enter", "go on")),
				func() key.Binding {
					b := key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "secret"))
					b.SetEnabled(false)
					return b
				}(),
			},
		})
		return a
	}

	t.Run("screen help and quit", func(t *testing.T) {
		f := newApp().renderFooter()
		assert.Contains(t, f, "go on")
		assert.NotContains(t, f, "secret")
		assert.Contains(t, f, "quit")
	})
	t.Run("update states", func(t *testing.T) {
		a := newApp()
		a.latestVersion = "3.0.0"
		a.updateConfirming = true
		assert.Contains(t, a.renderFooter(), "Update to v3.0.0?")

		a.updateConfirming, a.updateInProgress = false, true
		assert.Contains(t, a.renderFooter(), "Downloading v3.0.0")

		a.updateInProgress, a.updateComplete, a.version = false, true, "3.0.0"
		assert.Contains(t, a.renderFooter(), "Updated to v3.0.0")

		a.updateComplete, a.updateError = false, errors.New("nope")
		assert.Contains(t, a.renderFooter(), "Update failed: nope")
	})
	t.Run("update hint", func(t *testing.T) {
		a := newApp()
		a.updateAvailable = true
		assert.Contains(t, a.renderFooter(), "update")
	})
	t.Run("version text", func(t *testing.T) {
		platform := runtime.GOOS + "/" + runtime.GOARCH

		a := newApp()
		a.version, a.commit, a.buildDate = "dev", "abcdef1234567", "2026-02-03T10:00:00Z"
		f := a.renderFooter()
		assert.Contains(t, f, "dev-abcdef1")
		assert.NotContains(t, f, "abcdef12")
		assert.Contains(t, f, "(2026-02-03)")
		assert.Contains(t, f, platform)

		a.commit, a.buildDate = "unknown", "unknown"
		f = a.renderFooter()
		assert.Contains(t, f, "dev "+platform)

		a.commit, a.buildDate = "abc", "short"
		f = a.renderFooter()
		assert.Contains(t, f, "dev-abc (short)")

		a.version = "1.2.3"
		assert.Contains(t, a.renderFooter(), "v1.2.3 "+platform)

		a.updateAvailable, a.latestVersion = true, "1.3.0"
		f = a.renderFooter()
		assert.Contains(t, f, "-> v1.3.0 available")
	})
	t.Run("narrow width keeps a gap", func(t *testing.T) {
		a := newApp()
		a.version = "1.0.0"
		a.width = 10
		assert.NotPanics(t, func() { _ = a.renderFooter() })
	})
}

func TestRenderHeader_NarrowWidth(t *testing.T) {
	a := NewApp()
	a.RegisterScreen(ScreenOnboarding, &fakeScreen{title: "t"})
	a.width = 5
	assert.NotPanics(t, func() { _ = a.renderHeader() })
}

func TestJoinWithSeparator(t *testing.T) {
	assert.Equal(t, "", joinWithSeparator(nil, ", "))
	assert.Equal(t, "a", joinWithSeparator([]string{"a"}, ", "))
	assert.Equal(t, "a, b, c", joinWithSeparator([]string{"a", "b", "c"}, ", "))
}

func TestPrintNonInteractiveHelp(t *testing.T) {
	out := captureStdout(t, PrintNonInteractiveHelp)
	for _, want := range []string{"GitHubby", "githubby login", "githubby sync --help", "githubby clean --help"} {
		assert.Contains(t, out, want)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	require.NoError(t, w.Close())
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}

func TestRunAppInstance_PicksStartScreenAndReportsRestartFailure(t *testing.T) {
	// A cancelled context makes the program exit straight away, so no terminal is needed.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("unauthenticated starts on onboarding", func(t *testing.T) {
		a := NewApp()
		a.currentScreen = ScreenDashboard
		_ = RunAppInstance(ctx, a)
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
	})
	t.Run("authenticated with profiles starts on the dashboard", func(t *testing.T) {
		st := newStorage(t)
		require.NoError(t, st.AddProfile(state.NewProfile("p", "user", "a", "/a", false)))
		a := NewApp(WithAuth(true, "a", "t"), WithStorage(st))
		_ = RunAppInstance(ctx, a)
		assert.Equal(t, ScreenDashboard, a.currentScreen)
	})
	t.Run("authenticated without profiles stays on onboarding", func(t *testing.T) {
		a := NewApp(WithAuth(true, "a", "t"), WithStorage(newStorage(t)))
		_ = RunAppInstance(ctx, a)
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
	})
	t.Run("authenticated without storage stays on onboarding", func(t *testing.T) {
		a := NewApp(WithAuth(true, "a", "t"))
		_ = RunAppInstance(ctx, a)
		assert.Equal(t, ScreenOnboarding, a.currentScreen)
	})
	t.Run("failed restart is reported", func(t *testing.T) {
		orig := os.Args
		os.Args = []string{filepath.Join(t.TempDir(), "missing-binary")}
		t.Cleanup(func() { os.Args = orig })

		a := NewApp()
		a.restartAfterQuit = true
		err := RunAppInstance(ctx, a)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "restart failed")
	})
}

func TestRunApp_AndLegacyWrappers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.NotPanics(t, func() {
		_ = RunApp(ctx, WithContext(ctx))
		_ = Run(ctx, ScreenOnboarding, false, "")
		_ = RunOnboarding(ctx)
		_ = RunDashboard(ctx, "alice")
	})

}
