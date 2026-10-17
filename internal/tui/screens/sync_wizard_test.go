package screens

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/sync"
	"github.com/Didstopia/githubby/internal/tui"
)

func sw_key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func sw_new(t *testing.T, client github.Client) (*SyncWizard, *state.Storage) {
	t.Helper()
	st := state.NewStorageWithPath(filepath.Join(t.TempDir(), "state.yaml"))
	opts := []tui.AppOption{tui.WithStorage(st), tui.WithAuth(true, "alice", "tok")}
	if client != nil {
		opts = append(opts, tui.WithGitHubClient(client))
	}
	app := tui.NewApp(opts...)
	return NewSyncWizard(context.Background(), app), st
}

func sw_repos(names ...string) []*gh.Repository {
	var out []*gh.Repository
	for _, n := range names {
		out = append(out, &gh.Repository{
			Name:     gh.Ptr(n),
			FullName: gh.Ptr("alice/" + n),
			Owner:    &gh.User{Login: gh.Ptr("alice")},
		})
	}
	return out
}

func sw_msgOf(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	require.NotNil(t, cmd)
	return cmd()
}

func TestSW_RepoItem(t *testing.T) {
	r := &gh.Repository{
		FullName:        gh.Ptr("a/b"),
		Private:         gh.Ptr(true),
		Language:        gh.Ptr("Go"),
		StargazersCount: gh.Ptr(5),
	}
	item := WizardRepoItem{repo: r}
	assert.Equal(t, "[ ] a/b", item.Title())
	assert.Equal(t, "private | Go | 5 stars", item.Description())
	assert.Equal(t, "a/b", item.FilterValue())

	item.selected = true
	assert.Equal(t, "[x] a/b", item.Title())
	assert.Equal(t, "public", WizardRepoItem{repo: &gh.Repository{}}.Description())
}

func TestSW_DefaultTarget(t *testing.T) {
	w, st := sw_new(t, nil)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "repos"), w.targetInput.Value())

	require.NoError(t, st.SetDefaults("/data/repos", "alice"))
	app := tui.NewApp(tui.WithStorage(st))
	assert.Equal(t, "/data/repos", NewSyncWizard(context.Background(), app).targetInput.Value())

	assert.NotPanics(t, func() { NewSyncWizard(context.Background(), tui.NewApp()) })
}

func TestSW_TitleAndHelp(t *testing.T) {
	w, _ := sw_new(t, nil)
	titles := map[SyncWizardStep]string{
		WizardStepSelectSource: "New Sync - Source",
		WizardStepLoadingOrgs:  "New Sync - Loading Orgs",
		WizardStepSelectOrg:    "New Sync - Select Org",
		WizardStepFetchRepos:   "New Sync - Loading Repos",
		WizardStepRepoMode:     "New Sync - Repos",
		WizardStepSelectRepos:  "New Sync - Select Repos",
		WizardStepSetTarget:    "New Sync - Target",
		WizardStepConfirm:      "New Sync - Confirm",
		WizardStepExecute:      "Syncing...",
		WizardStepComplete:     "Sync Complete",
		SyncWizardStep(99):     "New Sync",
	}
	for step, want := range titles {
		w.step = step
		assert.Equal(t, want, w.Title())
	}

	w.step = WizardStepSelectRepos
	assert.Len(t, w.ShortHelp(), 5)
	w.step = WizardStepExecute
	assert.Empty(t, w.ShortHelp())
	w.step = WizardStepSetTarget
	assert.Len(t, w.ShortHelp(), 2)

	assert.NotNil(t, w.Init())
}

func TestSW_ExitKeys(t *testing.T) {
	t.Run("ctrl+c twice pops", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		_, cmd := w.Update(sw_key("ctrl+c"))
		assert.NotNil(t, cmd)
		assert.Contains(t, w.View(), "Press Ctrl+C again to cancel")
		_, cmd = w.Update(sw_key("ctrl+c"))
		assert.IsType(t, tui.ScreenPopMsg{}, cmd())
	})
	t.Run("q twice pops, but not while syncing", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		w.Update(sw_key("q"))
		assert.Contains(t, w.View(), "Press q again to cancel")
		_, cmd := w.Update(sw_key("q"))
		assert.IsType(t, tui.ScreenPopMsg{}, cmd())

		w.step = WizardStepExecute
		_, cmd = w.Update(sw_key("q"))
		assert.Nil(t, cmd)
	})
	t.Run("timeout and other keys clear the pending exit", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		w.Update(sw_key("q"))
		w.Update(tui.ExitTimeoutMsg{})
		assert.False(t, w.exitPending)

		w.Update(sw_key("q"))
		w.Update(sw_key("z"))
		assert.False(t, w.exitPending)
	})
	t.Run("esc at the first step or after leaves the wizard", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		_, cmd := w.Update(sw_key("esc"))
		assert.IsType(t, tui.ScreenPopMsg{}, cmd())

		w.step = WizardStepExecute
		_, cmd = w.Update(sw_key("esc"))
		assert.IsType(t, tui.ScreenPopMsg{}, cmd())
	})
}

func TestSW_EscGoesBackAndReinitialisesSteps(t *testing.T) {
	tests := []struct {
		from, to SyncWizardStep
		hasCmd   bool
	}{
		{WizardStepLoadingOrgs, WizardStepSelectSource, true},
		{WizardStepSelectOrg + 1, WizardStepSelectOrg, true},
		{WizardStepSelectRepos + 1, WizardStepSelectRepos, false},
		{WizardStepConfirm, WizardStepSetTarget, true},
		{WizardStepExecute - 1, WizardStepConfirm - 1, true},
		{WizardStepRepoMode, WizardStepFetchRepos, false},
	}
	for _, tt := range tests {
		w, _ := sw_new(t, nil)
		w.userOrgs = []*gh.Organization{{Login: gh.Ptr("acme")}}
		w.repoItems = []WizardRepoItem{{repo: sw_repos("x")[0]}}
		w.initPrivateForm()
		w.sourceType, w.sourceName = "user", "alice"
		w.step = tt.from
		_, cmd := w.Update(sw_key("esc"))
		assert.Equal(t, tt.to, w.step)
		if tt.hasCmd {
			assert.NotNil(t, cmd, "step %d", tt.to)
		}
		if tt.to == WizardStepSelectSource {
			assert.Nil(t, w.privateForm, "going back to the start resets the private form")
		}
	}
}

func TestSW_WindowSize(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.repoItems = []WizardRepoItem{{repo: sw_repos("x")[0]}}
	w.initRepoList()
	w.step = WizardStepSelectRepos
	w.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, w.width)
	assert.Equal(t, 112, w.repoList.Width())
	assert.Equal(t, 28, w.repoList.Height())
}

func TestSW_SourceUserPath(t *testing.T) {
	mock := github.NewMockClient()
	mock.ListUserReposFunc = func(_ context.Context, user string, opts *github.ListOptions) ([]*gh.Repository, error) {
		assert.Equal(t, "alice", user)
		assert.True(t, opts.IncludePrivate)
		return sw_repos("one", "two"), nil
	}
	w, _ := sw_new(t, mock)
	w.Init()

	w.sourceType = "user"
	w.sourceForm.State = huh.StateCompleted
	_, cmd := w.Update(struct{}{})
	assert.Equal(t, "alice", w.sourceName)
	assert.NotNil(t, cmd)
	require.NotNil(t, w.privateForm)
	assert.True(t, w.includePrivate)
	assert.Contains(t, w.View(), "Syncing: alice (user)")

	// Answering the private question starts the repo fetch.
	w.privateForm.State = huh.StateCompleted
	_, _ = w.Update(struct{}{})
	assert.Equal(t, WizardStepFetchRepos, w.step)
	assert.True(t, w.loading)
	assert.Contains(t, w.View(), "Fetching repositories from alice")

	loaded := w.waitForRepos()()
	_, cmd = w.Update(loaded)
	assert.NotNil(t, cmd)
	assert.Equal(t, WizardStepRepoMode, w.step)
	assert.Len(t, w.allRepos, 2)
	assert.Len(t, w.repoItems, 2)
	assert.Contains(t, w.View(), "Found 2 repositories in alice")

	// "Sync all" goes straight to the target.
	w.selectAllRepos = true
	w.repoModeForm.State = huh.StateCompleted
	w.Update(struct{}{})
	assert.Equal(t, WizardStepSetTarget, w.step)
	assert.Len(t, w.selectedRepos, 2)
	assert.Contains(t, w.View(), "Repos will be cloned to:")
}

func TestSW_SourceOrgPath(t *testing.T) {
	mock := github.NewMockClient()
	mock.ListUserOrgsFunc = func(context.Context) ([]*gh.Organization, error) {
		return []*gh.Organization{{Login: gh.Ptr("acme")}, {Login: gh.Ptr("globex")}}, nil
	}
	var orgSeen string
	mock.ListOrgReposFunc = func(_ context.Context, org string, _ *github.ListOptions) ([]*gh.Repository, error) {
		orgSeen = org
		return sw_repos("r"), nil
	}
	w, _ := sw_new(t, mock)

	w.sourceType = "org"
	w.sourceForm.State = huh.StateCompleted
	_, cmd := w.Update(struct{}{})
	assert.Equal(t, WizardStepLoadingOrgs, w.step)
	assert.NotNil(t, cmd)
	assert.Contains(t, w.View(), "Loading your organizations")

	loaded := w.waitForOrgs()()
	_, cmd = w.Update(loaded)
	assert.Equal(t, WizardStepSelectOrg, w.step)
	assert.NotNil(t, cmd)
	assert.False(t, w.loading)
	assert.Contains(t, w.View(), "Select an organization")

	w.selectedOrg = "acme"
	w.orgSelectForm.State = huh.StateCompleted
	w.Update(struct{}{})
	assert.Equal(t, "acme", w.sourceName)
	require.NotNil(t, w.privateForm)
	assert.Contains(t, w.View(), "Syncing: acme (organization)")

	w.privateForm.State = huh.StateCompleted
	w.Update(struct{}{})
	assert.Equal(t, WizardStepFetchRepos, w.step)
	w.Update(w.waitForRepos()())
	assert.Equal(t, "acme", orgSeen)
	assert.Equal(t, WizardStepRepoMode, w.step)
}

func TestSW_OrgLoadFailures(t *testing.T) {
	t.Run("API error returns to the source step", func(t *testing.T) {
		mock := github.NewMockClient()
		mock.ListUserOrgsFunc = func(context.Context) ([]*gh.Organization, error) { return nil, errors.New("boom") }
		w, _ := sw_new(t, mock)
		w.sourceType = "org"
		w.fetchOrgs()
		w.Update(w.waitForOrgs()())
		assert.Equal(t, WizardStepSelectSource, w.step)
		assert.Contains(t, w.View(), "Error: boom")
	})
	t.Run("no organizations", func(t *testing.T) {
		w, _ := sw_new(t, github.NewMockClient())
		w.fetchOrgs()
		w.Update(w.waitForOrgs()())
		assert.Equal(t, WizardStepSelectSource, w.step)
		assert.Contains(t, w.View(), "you don't belong to any organizations")
	})
	t.Run("not authenticated", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		w.fetchOrgs()
		msg := w.waitForOrgs()().(tui.OrgsLoadedMsg)
		assert.EqualError(t, msg.Error, "not authenticated")
	})
}

func TestSW_RepoLoadFailures(t *testing.T) {
	t.Run("API error", func(t *testing.T) {
		mock := github.NewMockClient()
		mock.ListUserReposFunc = func(context.Context, string, *github.ListOptions) ([]*gh.Repository, error) {
			return nil, errors.New("rate limited")
		}
		w, _ := sw_new(t, mock)
		w.sourceType, w.sourceName = "user", "alice"
		w.step = WizardStepFetchRepos
		w.fetchRepos()
		w.Update(w.waitForRepos()())
		assert.Equal(t, WizardStepSelectSource, w.step)
		assert.False(t, w.loading)
		assert.Contains(t, w.View(), "rate limited")
	})
	t.Run("not authenticated", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		w.fetchRepos()
		msg := w.waitForRepos()().(tui.ReposLoadedMsg)
		assert.EqualError(t, msg.Error, "not authenticated")
	})
}

func TestSW_RepoModeChooseSpecific(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.Update(tui.ReposLoadedMsg{Repos: sw_repos("a", "b", "c")})
	require.Equal(t, WizardStepRepoMode, w.step)

	w.selectAllRepos = false
	w.repoModeForm.State = huh.StateCompleted
	w.Update(struct{}{})
	assert.Equal(t, WizardStepSelectRepos, w.step)
	v := w.View()
	assert.Contains(t, v, "3 of 3 selected")
	assert.Contains(t, v, "Select repositories to sync")
}

func TestSW_SelectReposKeys(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.Update(tui.ReposLoadedMsg{Repos: sw_repos("a", "b", "c")})
	w.selectAllRepos = false
	w.repoModeForm.State = huh.StateCompleted
	w.Update(struct{}{})
	require.Equal(t, WizardStepSelectRepos, w.step)

	w.Update(sw_key("n"))
	assert.Empty(t, w.getSelectedRepos())

	// Enter with nothing selected shows an error and stays.
	w.Update(sw_key("enter"))
	assert.Equal(t, WizardStepSelectRepos, w.step)
	assert.Contains(t, w.View(), "please select at least one repository")

	w.Update(sw_key("x"))
	assert.Len(t, w.getSelectedRepos(), 1)
	w.Update(sw_key("space"))
	assert.Empty(t, w.getSelectedRepos(), "space toggles the same row back off")

	w.Update(sw_key("a"))
	assert.Len(t, w.getSelectedRepos(), 3)

	_, cmd := w.Update(sw_key("enter"))
	assert.NotNil(t, cmd)
	assert.Equal(t, WizardStepSetTarget, w.step)
	assert.Len(t, w.selectedRepos, 3)
}

func TestSW_TargetStep(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.sourceType, w.sourceName = "user", "alice"
	w.step = WizardStepSetTarget

	t.Run("empty target is rejected", func(t *testing.T) {
		w.targetInput.SetValue("")
		_, cmd := w.Update(sw_key("enter"))
		assert.Nil(t, cmd)
		assert.Contains(t, w.View(), "please enter a target directory")
		assert.Equal(t, WizardStepSetTarget, w.step)
	})
	t.Run("tilde expands to the home directory", func(t *testing.T) {
		w.err = nil
		w.targetInput.SetValue("~/code")
		_, cmd := w.Update(sw_key("enter"))
		assert.NotNil(t, cmd)
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, "code"), w.targetDir)
		assert.Equal(t, WizardStepConfirm, w.step)
		assert.Equal(t, "User alice", w.profileName)
		v := w.View()
		assert.Contains(t, v, "Step 5: Confirm")
		assert.Contains(t, v, "Source: user/alice")
	})
}

func TestSW_ConfirmView(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.sourceType, w.sourceName = "org", "acme"
	w.targetDir = "/t"
	w.selectedRepos = sw_repos("a", "b")
	w.initConfirmForm()
	w.step = WizardStepConfirm

	w.selectAllRepos = true
	assert.Contains(t, w.View(), "All (2 repos")
	w.selectAllRepos = false
	assert.Contains(t, w.View(), "2 selected")
}

func TestSW_TypingIntoTargetInput(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.step = WizardStepSetTarget
	w.targetInput.SetValue("")
	w.targetInput.Focus()
	w.Update(sw_key("x"))
	assert.Equal(t, "x", w.targetInput.Value())
}

func TestSW_SpinnerAndProgressFrames(t *testing.T) {
	w, _ := sw_new(t, nil)
	_, cmd := w.Update(spinner.TickMsg{})
	assert.Nil(t, cmd, "spinner ignored on the first step")

	w.step = WizardStepLoadingOrgs
	_, cmd = w.Update(w.syncSpinner.Tick())
	assert.NotNil(t, cmd)

	w.step = WizardStepExecute
	assert.NotPanics(t, func() { w.Update(progress.FrameMsg{}) })
	w.step = WizardStepSelectSource
	assert.NotPanics(t, func() { w.Update(progress.FrameMsg{}) })
}

func TestSW_ExecuteViewAndProgressMessages(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.step = WizardStepExecute
	w.targetDir = "/t"
	w.syncProgressChan = make(chan syncProgressUpdate, 1)
	w.syncDoneChan = make(chan syncDoneUpdate, 1)
	assert.Contains(t, w.View(), "Starting sync...")

	_, cmd := w.Update(syncProgressMsg{update: syncProgressUpdate{current: 1, total: 4, repoName: "a/b", status: "syncing"}})
	assert.NotNil(t, cmd)
	assert.Equal(t, "[1/4] a/b: syncing", w.syncStatus)
	v := w.View()
	assert.Contains(t, v, "Progress: 1 / 4 repositories")
	assert.Contains(t, v, "Target: /t")

	// A message with an unknown total only updates the status text.
	w.Update(syncProgressMsg{update: syncProgressUpdate{current: 0, total: 0, repoName: "x", status: "cloned"}})
	assert.Contains(t, w.syncStatus, "x: cloned")

	// The listener forwards progress, then completion once the channels close.
	w.syncProgressChan <- syncProgressUpdate{current: 2}
	assert.IsType(t, syncProgressMsg{}, w.waitForSyncProgress()())
	close(w.syncProgressChan)
	w.syncDoneChan <- syncDoneUpdate{err: errors.New("x")}
	assert.IsType(t, syncDoneMsg{}, w.waitForSyncProgress()())
	close(w.syncDoneChan)
	assert.Nil(t, w.waitForSyncProgress()())
}

func TestSW_WaitForSyncProgress_DoneFirst(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.syncProgressChan = make(chan syncProgressUpdate)
	w.syncDoneChan = make(chan syncDoneUpdate, 1)
	w.syncDoneChan <- syncDoneUpdate{}
	assert.IsType(t, syncDoneMsg{}, w.waitForSyncProgress()())

	// Both closed: nothing to report.
	close(w.syncDoneChan)
	assert.Nil(t, w.waitForSyncProgress()())

	w.syncProgressChan = make(chan syncProgressUpdate)
	close(w.syncProgressChan)
	w.syncDoneChan = make(chan syncDoneUpdate)
	close(w.syncDoneChan)
	assert.Nil(t, w.waitForSyncProgress()())
}

func TestSW_SyncDone_SavesRecordAndProfile(t *testing.T) {
	t.Run("sync all", func(t *testing.T) {
		w, st := sw_new(t, nil)
		w.sourceType, w.sourceName, w.targetDir = "user", "alice", "/t"
		w.profileName = "My profile"
		w.saveAsProfile = true
		w.selectAllRepos = true
		record := state.NewSyncRecord("", "My profile")
		record.AddResult("alice/a", "cloned", "")
		record.Complete()
		res := sync.NewResult()
		res.Cloned = []string{"alice/a"}

		w.Update(syncDoneMsg{update: syncDoneUpdate{result: res, record: record}})
		assert.Equal(t, WizardStepComplete, w.step)
		require.Len(t, st.GetProfiles(), 1)
		p := st.GetProfiles()[0]
		assert.Equal(t, "My profile", p.Name)
		assert.True(t, p.SyncAllRepos)
		assert.Empty(t, p.SelectedRepos)
		assert.False(t, p.LastSyncAt.IsZero())
		assert.Len(t, st.GetSyncHistory(), 1)

		v := w.View()
		assert.Contains(t, v, "Successfully synced 1 repositories!")
		assert.Contains(t, v, "Cloned: 1")
		assert.Contains(t, v, "Profile 'My profile' saved!")
	})
	t.Run("specific repos", func(t *testing.T) {
		w, st := sw_new(t, nil)
		w.sourceType, w.sourceName, w.profileName = "user", "alice", "P"
		w.saveAsProfile = true
		w.selectAllRepos = false
		w.selectedRepos = sw_repos("a", "b")
		w.Update(syncDoneMsg{update: syncDoneUpdate{record: state.NewSyncRecord("", "P")}})
		require.Len(t, st.GetProfiles(), 1)
		assert.Equal(t, []string{"alice/a", "alice/b"}, st.GetProfiles()[0].SelectedRepos)
	})
	t.Run("no profile requested", func(t *testing.T) {
		w, st := sw_new(t, nil)
		w.saveAsProfile = false
		w.Update(syncDoneMsg{update: syncDoneUpdate{record: state.NewSyncRecord("", "P")}})
		assert.Empty(t, st.GetProfiles())
		assert.Len(t, st.GetSyncHistory(), 1)
	})
	t.Run("without a record nothing is stored", func(t *testing.T) {
		w, st := sw_new(t, nil)
		w.saveAsProfile = true
		w.Update(syncDoneMsg{update: syncDoneUpdate{err: errors.New("boom")}})
		assert.Empty(t, st.GetProfiles())
		assert.Contains(t, w.View(), "Sync completed with errors: boom")
	})
}

func TestSW_CompleteView(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.step = WizardStepComplete
	assert.Contains(t, w.View(), "No changes - all repositories up to date")

	res := sync.NewResult()
	res.Updated = []string{"a"}
	res.Skipped = []string{"b"}
	res.Archived = []string{"c"}
	res.Failed["d"] = errors.New("x")
	w.syncResult = res
	v := w.View()
	for _, want := range []string{"Synced 3 repositories with 1 failures", "Updated: 1", "Skipped: 1", "Failed: 1", "Archived: 1"} {
		assert.Contains(t, v, want)
	}
}

func TestSW_CompleteEnterReturnsToDashboard(t *testing.T) {
	w, _ := sw_new(t, nil)
	w.step = WizardStepComplete
	_, cmd := w.Update(sw_key("enter"))
	batch, ok := sw_msgOf(t, cmd).(tea.BatchMsg)
	require.True(t, ok)
	assert.Len(t, batch, 2)
}

func TestSW_FullSyncRunsAgainstLocalRemote(t *testing.T) {
	remote := sp_makeRemote(t)
	mock := github.NewMockClient()
	mock.GetRepositoryFunc = func(_ context.Context, owner, repo string) (*gh.Repository, error) {
		return sp_repo(owner, repo, remote), nil
	}
	mock.GetBranchRefFunc = func(context.Context, string, string, string) (string, error) {
		return "", errors.New("no ref")
	}

	w, st := sw_new(t, mock)
	w.sourceType, w.sourceName = "user", "alice"
	w.targetDir = t.TempDir()
	w.profileName = "Local"
	w.saveAsProfile = true
	w.selectAllRepos = true
	w.selectedRepos = []*gh.Repository{sp_repo("alice", "hello", remote), sp_repo("alice", "other", remote)}

	w.step = WizardStepConfirm
	w.initConfirmForm()
	w.confirmForm.State = huh.StateCompleted
	_, cmd := w.Update(struct{}{})
	require.NotNil(t, cmd)
	assert.Equal(t, WizardStepExecute, w.step)

	for i := 0; i < 50 && w.step != WizardStepComplete; i++ {
		w.Update(w.waitForSyncProgress()())
	}
	require.Equal(t, WizardStepComplete, w.step)
	require.NotNil(t, w.syncResult)
	assert.Len(t, w.syncResult.Cloned, 2)
	assert.FileExists(t, filepath.Join(w.targetDir, "alice", "hello", "a.txt"))
	assert.Len(t, st.GetProfiles(), 1)
	assert.Len(t, w.syncResults, 2)
}

func TestSW_SyncReportsFailuresAndMissingDependencies(t *testing.T) {
	t.Run("not authenticated", func(t *testing.T) {
		w, _ := sw_new(t, nil)
		w.startSync()
		w.Update(w.waitForSyncProgress()())
		assert.Equal(t, WizardStepComplete, w.step)
		assert.EqualError(t, w.syncError, "not authenticated")
	})
	t.Run("git not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		w, _ := sw_new(t, github.NewMockClient())
		w.startSync()
		w.Update(w.waitForSyncProgress()())
		require.Error(t, w.syncError)
		assert.Contains(t, w.syncError.Error(), "git not available")
	})
	t.Run("repo errors become failed results", func(t *testing.T) {
		sp_isolateGit(t)
		mock := github.NewMockClient()
		mock.GetRepositoryFunc = func(context.Context, string, string) (*gh.Repository, error) {
			return nil, errors.New("gone")
		}
		w, _ := sw_new(t, mock)
		w.targetDir = t.TempDir()
		w.selectedRepos = sw_repos("a")
		w.startSync()
		for i := 0; i < 20 && w.step != WizardStepComplete; i++ {
			w.Update(w.waitForSyncProgress()())
		}
		require.Equal(t, WizardStepComplete, w.step)
		assert.Len(t, w.syncResult.Failed, 1)
		require.Len(t, w.syncResults, 1)
		assert.Equal(t, "failed", w.syncResults[0].Status)
		assert.Contains(t, w.View(), "Failed: 1")
	})
}
