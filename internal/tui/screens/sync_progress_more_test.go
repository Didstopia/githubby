package screens

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gherrors "github.com/Didstopia/githubby/internal/errors"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
)

// sp_isolateGit keeps git away from the real global/system config.
func sp_isolateGit(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(cfg, []byte("[user]\n\tname = Test\n\temail = t@example.com\n[init]\n\tdefaultBranch = main\n"), 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// sp_makeRemote creates a local repo with one commit and returns its path.
func sp_makeRemote(t *testing.T) string {
	t.Helper()
	sp_isolateGit(t)
	dir := filepath.Join(t.TempDir(), "remote")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o600))
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func sp_repo(owner, name, cloneURL string) *gh.Repository {
	return &gh.Repository{
		Name:          gh.Ptr(name),
		FullName:      gh.Ptr(owner + "/" + name),
		DefaultBranch: gh.Ptr("main"),
		CloneURL:      gh.Ptr(cloneURL),
		Owner:         &gh.User{Login: gh.Ptr(owner)},
	}
}

// sp_quietMock errors on GetRepository (the default mock returns nil, nil, which the screen does not expect).
func sp_quietMock() *github.MockClient {
	m := github.NewMockClient()
	m.GetRepositoryFunc = func(context.Context, string, string) (*gh.Repository, error) {
		return nil, errors.New("lookup failed")
	}
	return m
}

func sp_newApp(t *testing.T, client github.Client, profiles ...*state.SyncProfile) (*tui.App, *state.Storage) {
	t.Helper()
	st := state.NewStorageWithPath(filepath.Join(t.TempDir(), "state.yaml"))
	opts := []tui.AppOption{tui.WithStorage(st), tui.WithAuth(true, "alice", "tok")}
	if client != nil {
		opts = append(opts, tui.WithGitHubClient(client))
	}
	app := tui.NewApp(opts...)
	for _, p := range profiles {
		require.NoError(t, st.AddProfile(p))
	}
	switch len(profiles) {
	case 0:
	case 1:
		app.SetSelectedProfile(profiles[0])
	default:
		app.SetProfilesToSync(profiles)
	}
	return app, st
}

// sp_drain runs Init and feeds every channel message back into Update until completion.
func sp_drain(t *testing.T, s *SyncProgressScreen) []profileSyncProgressUpdate {
	t.Helper()
	s.Init()
	var seen []profileSyncProgressUpdate
	for i := 0; i < 500 && !s.complete; i++ {
		msg := s.waitForSyncProgress()()
		pm, ok := msg.(profileSyncProgressMsg)
		require.True(t, ok)
		seen = append(seen, pm.update)
		s.Update(msg)
	}
	require.True(t, s.complete, "sync never completed")
	return seen
}

func sp_key(s string) tea.KeyMsg {
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

func TestSP_TitleAndHelp(t *testing.T) {
	app, _ := sp_newApp(t, nil)
	s := NewSyncProgress(context.Background(), app)
	assert.Equal(t, "Syncing", s.Title())
	assert.Empty(t, s.ShortHelp())

	s.profiles = []*state.SyncProfile{{Name: "a"}}
	assert.Equal(t, "Syncing a", s.Title())
	s.profile = &state.SyncProfile{Name: "single"}
	assert.Equal(t, "Syncing single", s.Title())
	s.profiles = []*state.SyncProfile{{Name: "a"}, {Name: "b"}}
	assert.Equal(t, "Syncing 2 Profiles", s.Title())

	s.complete = true
	assert.Len(t, s.ShortHelp(), 2)
}

func TestSP_InitNoProfile(t *testing.T) {
	app, _ := sp_newApp(t, nil)
	s := NewSyncProgress(context.Background(), app)
	assert.Contains(t, s.View(), "Loading profile")

	assert.Nil(t, s.Init())
	assert.True(t, s.complete)
	assert.False(t, s.loading)
	v := s.View()
	assert.Contains(t, v, "no profile selected")
	assert.Contains(t, v, "Esc")
}

func TestSP_InitItems(t *testing.T) {
	t.Run("specific repos give a known total", func(t *testing.T) {
		p := state.NewProfile("p", "user", "alice", t.TempDir(), false)
		p.SelectedRepos = []string{"alice/a", "alice/b"}
		app, _ := sp_newApp(t, sp_quietMock(), p)
		s := NewSyncProgress(context.Background(), app)
		s.Init()
		assert.Len(t, s.items, 2)
		assert.Equal(t, 2, s.totalRepos)
		assert.True(t, s.syncing)
		assert.Same(t, p, s.profile)
	})
	t.Run("all-repos profiles have an unknown total", func(t *testing.T) {
		p1 := state.NewProfile("p1", "user", "alice", t.TempDir(), false)
		p1.SyncAllRepos = true
		p2 := state.NewProfile("p2", "org", "acme", t.TempDir(), false)
		p2.SelectedRepos = []string{"acme/x"}
		app, _ := sp_newApp(t, sp_quietMock(), p1, p2)
		s := NewSyncProgress(context.Background(), app)
		s.Init()
		require.Len(t, s.items, 2)
		assert.Contains(t, s.items[0].name, "user/alice (all repos)")
		assert.Equal(t, 0, s.totalRepos)
		assert.Len(t, s.profiles, 2)
	})
}

func TestSP_KeyHandling(t *testing.T) {
	newScreen := func() *SyncProgressScreen {
		app, _ := sp_newApp(t, nil)
		return NewSyncProgress(context.Background(), app)
	}

	t.Run("ctrl+c needs two presses", func(t *testing.T) {
		s := newScreen()
		_, cmd := s.Update(sp_key("ctrl+c"))
		assert.True(t, s.exitPending)
		assert.NotNil(t, cmd)
		s.loading = false
		s.syncing = true
		assert.Contains(t, s.View(), "Press Ctrl+C again to cancel")

		_, cmd = s.Update(sp_key("ctrl+c"))
		assert.IsType(t, tui.ScreenPopMsg{}, cmd())
	})
	t.Run("timeout clears the pending exit", func(t *testing.T) {
		s := newScreen()
		s.exitPending = true
		s.Update(tui.ExitTimeoutMsg{})
		assert.False(t, s.exitPending)
	})
	t.Run("esc and enter do nothing until complete", func(t *testing.T) {
		s := newScreen()
		_, cmd := s.Update(sp_key("esc"))
		assert.Nil(t, cmd)
		_, cmd = s.Update(sp_key("enter"))
		assert.Nil(t, cmd)
	})
	t.Run("esc and enter leave a finished screen", func(t *testing.T) {
		for _, k := range []string{"esc", "enter"} {
			s := newScreen()
			s.complete = true
			_, cmd := s.Update(sp_key(k))
			require.NotNil(t, cmd)
			batch, ok := cmd().(tea.BatchMsg)
			require.True(t, ok)
			assert.Len(t, batch, 2)
		}
	})
	t.Run("q needs two presses once complete", func(t *testing.T) {
		s := newScreen()
		_, cmd := s.Update(sp_key("q"))
		assert.Nil(t, cmd, "ignored while syncing")

		s.complete = true
		_, cmd = s.Update(sp_key("q"))
		assert.NotNil(t, cmd)
		assert.True(t, s.exitPending)
		s.loading, s.complete = false, true
		assert.Contains(t, s.View(), "Press q again to quit")

		_, cmd = s.Update(sp_key("q"))
		assert.IsType(t, tui.QuitMsg{}, cmd())
	})
	t.Run("other keys reset the pending exit", func(t *testing.T) {
		s := newScreen()
		s.exitPending = true
		s.Update(sp_key("x"))
		assert.False(t, s.exitPending)
	})
	t.Run("window size, spinner and progress frames", func(t *testing.T) {
		s := newScreen()
		s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		assert.Equal(t, 100, s.width)
		assert.Equal(t, 80, s.progress.Width)
		_, cmd := s.Update(s.spinner.Tick())
		assert.NotNil(t, cmd, "spinner keeps ticking while loading")
		s.loading, s.syncing = false, false
		_, cmd = s.Update(spinner.TickMsg{})
		assert.Nil(t, cmd)
		assert.NotPanics(t, func() { s.Update(progress.FrameMsg{}) })
	})
}

func TestSP_ProgressMessages(t *testing.T) {
	newScreen := func() *SyncProgressScreen {
		app, _ := sp_newApp(t, nil)
		s := NewSyncProgress(context.Background(), app)
		s.startTime = time.Now().Add(-time.Minute)
		s.syncProgressChan = make(chan profileSyncProgressUpdate, 1)
		return s
	}
	send := func(s *SyncProgressScreen, u profileSyncProgressUpdate) tea.Cmd {
		_, cmd := s.Update(profileSyncProgressMsg{update: u})
		return cmd
	}

	s := newScreen()
	send(s, profileSyncProgressUpdate{status: "collecting", current: 7})
	assert.True(t, s.collecting)
	assert.Equal(t, 7, s.collectingCount)

	send(s, profileSyncProgressUpdate{status: "syncing", repoName: "a/b", total: 10})
	assert.False(t, s.collecting)
	assert.Equal(t, 10, s.totalRepos)
	assert.Equal(t, "a/b", s.currentRepo)

	for _, st := range []string{"cloned", "updated", "up-to-date", "skipped", "archived"} {
		send(s, profileSyncProgressUpdate{status: st, repoName: "a/" + st})
	}
	assert.Equal(t, 1, s.cloned)
	assert.Equal(t, 1, s.updated)
	assert.Equal(t, 1, s.upToDate)
	assert.Equal(t, 1, s.skipped)
	assert.Equal(t, 1, s.archived)
	assert.Equal(t, 5, s.reposCompleted)
	assert.Greater(t, s.etaUpdateCount, 0, "ETA is computed after repos finish")

	cmd := send(s, profileSyncProgressUpdate{status: "failed", repoName: "a/bad", err: errors.New("boom")})
	assert.Equal(t, 1, s.failed)
	assert.Equal(t, "boom", s.failedRepos["a/bad"])
	assert.NotNil(t, cmd, "keeps listening")

	send(s, profileSyncProgressUpdate{status: "failed", err: errors.New("no name")})
	assert.Len(t, s.failedRepos, 1, "failures without a repo name are not recorded per repo")

	s.syncing = true
	send(s, profileSyncProgressUpdate{status: "complete", err: errors.New("overall")})
	assert.True(t, s.complete)
	assert.False(t, s.syncing)
	assert.EqualError(t, s.err, "overall")
}

func TestSP_CompleteWithoutTotal(t *testing.T) {
	app, _ := sp_newApp(t, nil)
	s := NewSyncProgress(context.Background(), app)
	_, cmd := s.Update(profileSyncProgressMsg{update: profileSyncProgressUpdate{status: "complete"}})
	assert.True(t, s.complete)
	assert.Nil(t, cmd)
}

func TestSP_ETAIncrementalWarmup(t *testing.T) {
	app, _ := sp_newApp(t, nil)
	s := NewSyncProgress(context.Background(), app)
	s.startTime = time.Now().Add(-10 * time.Minute)
	s.totalRepos = 20
	s.syncProgressChan = make(chan profileSyncProgressUpdate, 1)

	for i := 0; i < 2; i++ {
		s.Update(profileSyncProgressMsg{update: profileSyncProgressUpdate{status: "up-to-date"}})
	}
	assert.Equal(t, 0, s.etaUpdateCount, "needs a few samples when nothing was cloned")

	s.Update(profileSyncProgressMsg{update: profileSyncProgressUpdate{status: "up-to-date"}})
	assert.Equal(t, 1, s.etaUpdateCount)
	first := s.lastDisplayedETA
	assert.Greater(t, first, time.Duration(0))

	// A later estimate is smoothed (not a straight replacement).
	s.startTime = time.Now().Add(-time.Hour)
	s.Update(profileSyncProgressMsg{update: profileSyncProgressUpdate{status: "up-to-date"}})
	assert.Equal(t, 2, s.etaUpdateCount)
}

func TestSP_ViewStates(t *testing.T) {
	app, _ := sp_newApp(t, nil)
	newScreen := func() *SyncProgressScreen {
		s := NewSyncProgress(context.Background(), app)
		s.loading = false
		s.syncing = true
		return s
	}

	t.Run("titles", func(t *testing.T) {
		s := newScreen()
		assert.Contains(t, s.View(), "Syncing")
		s.profile = &state.SyncProfile{Name: "mine", TargetDir: "/t"}
		assert.Contains(t, s.View(), "Syncing: mine")
		s.profiles = []*state.SyncProfile{{}, {}}
		assert.Contains(t, s.View(), "Syncing 2 Profiles")
	})
	t.Run("collecting", func(t *testing.T) {
		s := newScreen()
		s.collecting = true
		assert.Contains(t, s.View(), "Collecting repositories from GitHub")
		s.collectingCount = 12
		assert.Contains(t, s.View(), "(12 found)")
	})
	t.Run("current repo with running totals and ETA", func(t *testing.T) {
		s := newScreen()
		s.totalRepos = 10
		s.cloned, s.updated, s.upToDate = 1, 2, 3
		s.currentRepo = "a/b"
		s.lastDisplayedETA = 90 * time.Second
		v := s.View()
		assert.Contains(t, v, "6/10 repos")
		assert.Contains(t, v, "[7/10] Syncing a/b")
		assert.Contains(t, v, "cloned: 1, updated: 2, up-to-date: 3")
		assert.Contains(t, v, "~1 minute remaining")
	})
	t.Run("waiting messages", func(t *testing.T) {
		s := newScreen()
		s.profiles = []*state.SyncProfile{{}, {}}
		s.totalRepos = 4
		assert.Contains(t, s.View(), "Syncing 4 repositories across 2 profiles")
		s.totalRepos = 0
		assert.Contains(t, s.View(), "Syncing all repositories across 2 profiles")

		s = newScreen()
		s.profile = &state.SyncProfile{TargetDir: "/t", Type: "user", Source: "alice"}
		s.totalRepos = 3
		assert.Contains(t, s.View(), "Syncing 3 repositories to /t")
		s.totalRepos = 0
		assert.Contains(t, s.View(), "Syncing repositories to /t")
		s.profile.SyncAllRepos = true
		assert.Contains(t, s.View(), "Syncing all user/alice repositories to /t")

		s = newScreen()
		s.totalRepos = 0
		assert.Contains(t, s.View(), "Syncing 0 repositories...")
	})
	t.Run("complete success with no changes", func(t *testing.T) {
		s := newScreen()
		s.syncing, s.complete = false, true
		v := s.View()
		assert.Contains(t, v, "Successfully synced 0 repositories")
		assert.Contains(t, v, "No changes")
		assert.Contains(t, v, "Completed in")
	})
	t.Run("complete with every counter", func(t *testing.T) {
		s := newScreen()
		s.syncing, s.complete = false, true
		s.totalRepos = 8
		s.cloned, s.updated, s.upToDate, s.skipped, s.archived, s.failed = 1, 1, 1, 1, 1, 3
		s.failedRepos = map[string]string{"z/z": "zed bad", "a/a": "alpha bad"}
		v := s.View()
		for _, want := range []string{"Synced 8 repositories with 3 failures", "Cloned: 1", "Updated: 1", "Up-to-date: 1", "Skipped: 1", "Failed: 3", "Archived: 1", "Failed repositories:", "alpha bad", "zed bad"} {
			assert.Contains(t, v, want)
		}
		assert.Less(t, strings.Index(v, "a/a"), strings.Index(v, "z/z"), "failed repos are sorted")
	})
	t.Run("complete with errors", func(t *testing.T) {
		s := newScreen()
		s.syncing, s.complete = false, true
		s.items = []syncProgressItem{{name: "x", status: "pending"}}
		s.err = errors.New("kaput")
		assert.Contains(t, s.View(), "Sync completed with errors: kaput")

		s.err = gherrors.NewAPIError(401, "bad creds", nil)
		v := s.View()
		assert.Contains(t, v, "Authentication failed")
		assert.Contains(t, v, "githubby logout && githubby login")

		s.err = gherrors.NewAPIError(403, "forbidden", nil)
		assert.Contains(t, s.View(), "Authentication failed")
	})
}

func TestSP_RunSync_NotAuthenticated(t *testing.T) {
	p := state.NewProfile("p", "user", "alice", t.TempDir(), false)
	app, _ := sp_newApp(t, nil, p)
	s := NewSyncProgress(context.Background(), app)
	seen := sp_drain(t, s)
	last := seen[len(seen)-1]
	assert.Equal(t, "complete", last.status)
	assert.EqualError(t, s.err, "not authenticated")
}

func TestSP_RunSync_ListErrors(t *testing.T) {
	newProfile := func(typ string) *state.SyncProfile {
		p := state.NewProfile("p", typ, "src", t.TempDir(), false)
		p.SyncAllRepos = true
		return p
	}

	t.Run("auth error aborts", func(t *testing.T) {
		mock := github.NewMockClient()
		mock.ListUserReposFunc = func(context.Context, string, *github.ListOptions) ([]*gh.Repository, error) {
			return nil, gherrors.NewAPIError(401, "nope", nil)
		}
		app, _ := sp_newApp(t, mock, newProfile("user"))
		s := NewSyncProgress(context.Background(), app)
		sp_drain(t, s)
		require.Error(t, s.err)
		assert.Contains(t, s.err.Error(), "authentication failed")
		assert.True(t, gherrors.IsUnauthorized(s.err))
	})
	t.Run("other error is recorded as a failed source", func(t *testing.T) {
		mock := github.NewMockClient()
		mock.ListOrgReposFunc = func(context.Context, string, *github.ListOptions) ([]*gh.Repository, error) {
			return nil, errors.New("boom")
		}
		app, _ := sp_newApp(t, mock, newProfile("org"))
		s := NewSyncProgress(context.Background(), app)
		seen := sp_drain(t, s)
		var failed bool
		for _, u := range seen {
			if u.status == "failed" && u.repoName == "org/src" {
				failed = true
			}
		}
		assert.True(t, failed)
		assert.Equal(t, 1, s.failed)
		assert.NoError(t, s.err)
	})
	t.Run("cancelled context stops the sync", func(t *testing.T) {
		mock := github.NewMockClient()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		app, _ := sp_newApp(t, mock, newProfile("user"))
		s := NewSyncProgress(ctx, app)
		sp_drain(t, s)
		assert.ErrorIs(t, s.err, context.Canceled)
	})
	t.Run("list error with a cancelled context reports the cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		mock := github.NewMockClient()
		mock.ListUserReposFunc = func(context.Context, string, *github.ListOptions) ([]*gh.Repository, error) {
			cancel()
			return nil, errors.New("interrupted")
		}
		app, _ := sp_newApp(t, mock, newProfile("user"))
		s := NewSyncProgress(ctx, app)
		sp_drain(t, s)
		assert.ErrorIs(t, s.err, context.Canceled)
	})
}

func TestSP_RunSync_SelectedRepos(t *testing.T) {
	t.Run("auth error from GetRepository aborts", func(t *testing.T) {
		mock := github.NewMockClient()
		mock.GetRepositoryFunc = func(context.Context, string, string) (*gh.Repository, error) {
			return nil, gherrors.NewAPIError(403, "denied", nil)
		}
		p := state.NewProfile("p", "user", "alice", t.TempDir(), false)
		p.SelectedRepos = []string{"alice/a"}
		app, _ := sp_newApp(t, mock, p)
		s := NewSyncProgress(context.Background(), app)
		sp_drain(t, s)
		assert.True(t, gherrors.IsForbidden(s.err))
	})
	t.Run("cancelled context while collecting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		p := state.NewProfile("p", "user", "alice", t.TempDir(), false)
		p.SelectedRepos = []string{"alice/a"}
		app, _ := sp_newApp(t, sp_quietMock(), p)
		s := NewSyncProgress(ctx, app)
		sp_drain(t, s)
		assert.ErrorIs(t, s.err, context.Canceled)
	})
	t.Run("missing repo data falls back, bad names are ignored", func(t *testing.T) {
		sp_isolateGit(t)
		mock := github.NewMockClient()
		mock.GetRepositoryFunc = func(context.Context, string, string) (*gh.Repository, error) {
			return nil, errors.New("lookup failed")
		}
		p := state.NewProfile("p", "user", "alice", t.TempDir(), false)
		p.SelectedRepos = []string{"alice/a", "not-a-repo-name"}
		app, _ := sp_newApp(t, mock, p)
		s := NewSyncProgress(context.Background(), app)
		seen := sp_drain(t, s)
		var total int
		for _, u := range seen {
			if u.total > total {
				total = u.total
			}
		}
		assert.Equal(t, 1, total, "only the well-formed name is synced")
		assert.Equal(t, 1, s.failed, "the clone has no URL and fails")
		assert.NoError(t, s.err)
	})
}

func TestSP_RunSync_ClonesAndUpdatesLocalRepo(t *testing.T) {
	remote := sp_makeRemote(t)
	target := t.TempDir()

	mock := github.NewMockClient()
	mock.GetRepositoryFunc = func(_ context.Context, owner, repo string) (*gh.Repository, error) {
		return sp_repo(owner, repo, remote), nil
	}
	mock.GetBranchRefFunc = func(context.Context, string, string, string) (string, error) {
		return "", errors.New("no ref") // forces the full pull path
	}
	mock.ListUserReposFunc = func(context.Context, string, *github.ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{sp_repo("alice", "hello", remote)}, nil
	}

	p := state.NewProfile("p", "user", "alice", target, false)
	p.SyncAllRepos = true
	app, st := sp_newApp(t, mock, p)

	s := NewSyncProgress(context.Background(), app)
	seen := sp_drain(t, s)
	assert.Equal(t, 1, s.cloned, "statuses: %v", seen)
	assert.FileExists(t, filepath.Join(target, "alice", "hello", "a.txt"))
	assert.False(t, st.GetProfile(p.ID).LastSyncAt.IsZero(), "last sync time is saved")
	v := s.View()
	assert.Contains(t, v, "Successfully synced 1 repositories")
	assert.Contains(t, v, "Cloned: 1")

	// Second run: the repo exists, so it is pulled instead of cloned.
	s2 := NewSyncProgress(context.Background(), app)
	sp_drain(t, s2)
	assert.Equal(t, 0, s2.cloned)
	assert.Equal(t, 1, s2.updated+s2.upToDate+s2.failed)
}

func TestSP_WaitForSyncProgress_ClosedChannel(t *testing.T) {
	app, _ := sp_newApp(t, nil)
	s := NewSyncProgress(context.Background(), app)
	s.syncProgressChan = make(chan profileSyncProgressUpdate)
	close(s.syncProgressChan)
	msg := s.waitForSyncProgress()().(profileSyncProgressMsg)
	assert.Equal(t, "complete", msg.update.status)
}

func TestSP_RunSync_NoProfilesGuard(t *testing.T) {
	app, _ := sp_newApp(t, github.NewMockClient())
	s := NewSyncProgress(context.Background(), app)
	s.syncProgressChan = make(chan profileSyncProgressUpdate, 4)
	s.runSyncInBackground()
	u := <-s.syncProgressChan
	assert.Equal(t, "complete", u.status)
	assert.EqualError(t, u.err, "no profiles")
}

func TestSP_HumanizeDurationEdges(t *testing.T) {
	tests := map[time.Duration]string{
		500 * time.Millisecond:       "less than a second",
		time.Second:                  "1 second",
		30 * time.Second:             "30 seconds",
		time.Minute:                  "1 minute",
		5 * time.Minute:              "5 minutes",
		time.Hour:                    "1 hour",
		time.Hour + 5*time.Minute:    "1 hour 5 min",
		3 * time.Hour:                "3 hours",
		3*time.Hour + 20*time.Minute: "3 hours 20 min",
	}
	for d, want := range tests {
		assert.Equal(t, want, humanizeDuration(d))
	}
}
