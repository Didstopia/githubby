package screens

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
)

func db_storage(t *testing.T) *state.Storage {
	t.Helper()
	return state.NewStorageWithPath(filepath.Join(t.TempDir(), "state.yaml"))
}

func db_profile(t *testing.T, st *state.Storage, name string, lastSync time.Time) *state.SyncProfile {
	t.Helper()
	p := state.NewProfile(name, "user", name+"-src", "/tmp/"+name, false)
	p.LastSyncAt = lastSync
	require.NoError(t, st.AddProfile(p))
	return p
}

func db_dashboard(t *testing.T, st *state.Storage) *DashboardV2 {
	t.Helper()
	opts := []tui.AppOption{}
	if st != nil {
		opts = append(opts, tui.WithStorage(st))
	}
	return NewDashboardV2(context.Background(), tui.NewApp(opts...))
}

func db_press(d *DashboardV2, k string) tea.Cmd {
	_, cmd := d.Update(cl_press(k))
	return cmd
}

func db_titles(d *DashboardV2) []string {
	var out []string
	for _, it := range d.actionList.Items() {
		out = append(out, it.(DashboardItem).title)
	}
	return out
}

func TestDashboardV2_EmptyState(t *testing.T) {
	d := db_dashboard(t, db_storage(t))
	assert.Equal(t, "Dashboard", d.Title())
	assert.Equal(t, []string{"New Sync Profile", "Clean Releases"}, db_titles(d))
	assert.Contains(t, d.View(), "No profiles configured")
	assert.Contains(t, d.View(), "QUICK ACTIONS")
}

func TestDashboardV2_WithoutStorage(t *testing.T) {
	d := db_dashboard(t, nil)
	assert.Empty(t, d.profiles)
	assert.Equal(t, []string{"New Sync Profile", "Clean Releases"}, db_titles(d))
}

func TestDashboardV2_MenuWithProfiles(t *testing.T) {
	st := db_storage(t)
	fresh := db_profile(t, st, "fresh", time.Now().Add(-time.Hour))
	db_profile(t, st, "stale", time.Now().Add(-72*time.Hour))
	never := db_profile(t, st, "never", time.Time{})
	never.SyncAllRepos = false
	never.SelectedRepos = []string{"a/b", "c/d"}
	fresh.SyncAllRepos = true

	rec := state.NewSyncRecord(fresh.ID, fresh.Name)
	rec.Archived = 3
	rec.Complete()
	require.NoError(t, st.AddSyncRecord(rec))

	d := db_dashboard(t, st)
	assert.Equal(t, []string{"Sync All Profiles", "Sync Pending (2)", "fresh", "stale", "never", "Clean Releases"}, db_titles(d))
	assert.Equal(t, 2, d.pendingSync)
	assert.Equal(t, 3, d.archivedCount)

	items := d.actionList.Items()
	assert.Contains(t, items[0].(DashboardItem).description, "Sync all 3 configured profiles")
	assert.Contains(t, items[2].(DashboardItem).description, "user/fresh-src (all repos) - Last sync: 1 hour ago")
	assert.Contains(t, items[4].(DashboardItem).description, "(2 repos) - Last sync: never")

	v := d.View()
	assert.Contains(t, v, "3 profiles")
	assert.Contains(t, v, "2 pending")
	assert.Contains(t, v, "3 archived")
	assert.Contains(t, v, "Last Sync: 1 hour ago")
}

func TestDashboardV2_SingleProfileAndNeverSynced(t *testing.T) {
	st := db_storage(t)
	db_profile(t, st, "only", time.Time{})
	v := db_dashboard(t, st).View()
	assert.Contains(t, v, "1 profile")
	assert.NotContains(t, v, "1 profiles")
	assert.Contains(t, v, "Last Sync: Never")
}

func TestDashboardV2_ShortHelpShowsDeleteOnProfiles(t *testing.T) {
	st := db_storage(t)
	db_profile(t, st, "p", time.Now())
	d := db_dashboard(t, st)
	assert.Len(t, d.ShortHelp(), 4, "sync-all row selected: no delete hint")

	db_press(d, "down") // Sync All -> profile (fresh, so no pending row)
	assert.Len(t, d.ShortHelp(), 5)
	assert.Equal(t, "d", d.ShortHelp()[4].Help().Key)
}

func TestDashboardV2_Selection(t *testing.T) {
	st := db_storage(t)
	p := db_profile(t, st, "p", time.Now().Add(-48*time.Hour))
	d := db_dashboard(t, st)
	// Rows: Sync All, Sync Pending (1), p, Clean Releases

	assert.Equal(t, tui.SyncAllProfilesMsg{}, db_press(d, "enter")())

	db_press(d, "down")
	assert.Equal(t, tui.SyncPendingProfilesMsg{}, db_press(d, "enter")())

	db_press(d, "down")
	sel, ok := db_press(d, "enter")().(tui.ProfileSelectedMsg)
	require.True(t, ok)
	assert.Equal(t, p.ID, sel.Profile.ID)

	db_press(d, "down")
	assert.Equal(t, tui.ScreenPushMsg{Screen: tui.ScreenClean}, db_press(d, "enter")())
}

func TestDashboardV2_SelectNewSyncWhenEmpty(t *testing.T) {
	d := db_dashboard(t, db_storage(t))
	assert.Equal(t, tui.NewSyncRequestedMsg{}, db_press(d, "enter")())
}

func TestDashboardV2_ShortcutKeys(t *testing.T) {
	st := db_storage(t)
	p := db_profile(t, st, "p", time.Now())
	d := db_dashboard(t, st)

	assert.Equal(t, tui.NewSyncRequestedMsg{}, db_press(d, "n")())

	assert.Nil(t, db_press(d, "d"), "d on a non-profile row does nothing")

	db_press(d, "down")
	del, ok := db_press(d, "d")().(tui.DeleteProfileRequestMsg)
	require.True(t, ok)
	assert.Equal(t, p.ID, del.ProfileID)
	assert.Equal(t, "p", del.ProfileName)

	// A profile removed behind the dashboard's back falls back to the row title.
	require.NoError(t, st.DeleteProfile(p.ID))
	del = db_press(d, "d")().(tui.DeleteProfileRequestMsg)
	assert.Equal(t, "p", del.ProfileName)

	// Selecting a vanished profile does nothing.
	assert.Nil(t, db_press(d, "enter"))
}

func TestDashboardV2_ActionsThatAreNotWiredYet(t *testing.T) {
	d := db_dashboard(t, db_storage(t))

	d.actionList.SetItems(append(d.actionList.Items(), DashboardItem{title: "Soon", disabled: true}))
	d.actionList.Select(2)
	cmd := db_press(d, "enter")
	assert.NotNil(t, cmd)
	assert.Contains(t, d.View(), "This feature is not yet available.")

	d.Update(tui.ClearMessageMsg{})
	assert.NotContains(t, d.View(), "not yet available")

	d.actionList.SetItems([]list.Item{DashboardItem{title: "History", action: ActionViewHistory}})
	d.actionList.Select(0)
	assert.NotNil(t, db_press(d, "enter"))
	assert.Contains(t, d.View(), "History view coming soon!")

	d.actionList.SetItems(nil)
	assert.Nil(t, d.handleSelection(), "empty list selects nothing")

	d.actionList.SetItems([]list.Item{DashboardItem{title: "Odd", action: DashboardAction(99)}})
	d.actionList.Select(0)
	assert.Nil(t, d.handleSelection())
}

func TestDashboardV2_ExitConfirmation(t *testing.T) {
	tests := []struct {
		name string
		key  string
		hint string
	}{
		{"ctrl+c", "ctrl+c", "Press Ctrl+C again to quit"},
		{"escape", "esc", "Press Escape again to quit"},
		{"q", "q", "Press q again to quit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := db_dashboard(t, db_storage(t))

			first := db_press(d, tt.key)
			require.NotNil(t, first)
			assert.True(t, d.exitPending)
			assert.Contains(t, d.View(), tt.hint)

			second := db_press(d, tt.key)
			require.NotNil(t, second)
			assert.IsType(t, tea.QuitMsg{}, second())
		})
	}

	t.Run("timeout clears the pending state", func(t *testing.T) {
		d := db_dashboard(t, db_storage(t))
		db_press(d, "q")
		d.Update(tui.ExitTimeoutMsg{})
		assert.False(t, d.exitPending)
	})
	t.Run("another key cancels it", func(t *testing.T) {
		d := db_dashboard(t, db_storage(t))
		db_press(d, "q")
		db_press(d, "down")
		assert.False(t, d.exitPending)
	})
	t.Run("pressing a different quit key restarts the confirmation", func(t *testing.T) {
		d := db_dashboard(t, db_storage(t))
		db_press(d, "q")
		db_press(d, "ctrl+c")
		assert.Equal(t, "ctrl+c", d.exitKey)
	})
	t.Run("selecting something clears it", func(t *testing.T) {
		d := db_dashboard(t, db_storage(t))
		db_press(d, "q")
		db_press(d, "n")
		assert.False(t, d.exitPending)
	})
}

func TestDashboardV2_ResizeRefreshAndErrors(t *testing.T) {
	st := db_storage(t)
	d := db_dashboard(t, st)

	d.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	assert.Equal(t, 120, d.width)
	assert.Equal(t, 50, d.height)
	d.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	assert.Equal(t, 15, d.actionList.Height(), "list keeps a minimum height")

	db_profile(t, st, "new", time.Now())
	assert.Equal(t, []string{"New Sync Profile", "Clean Releases"}, db_titles(d))
	d.Update(tui.RefreshDashboardMsg{})
	assert.Contains(t, db_titles(d), "new")
	d.Init()
	assert.Contains(t, db_titles(d), "new")

	_, cmd := d.Update(tui.ErrorMsg{Err: errors.New("kaboom")})
	assert.NotNil(t, cmd)
	assert.Contains(t, d.View(), "Error: kaboom")
}

func TestDashboard_formatTimeAgo(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"seconds", now.Add(-10 * time.Second), "just now"},
		{"one minute", now.Add(-61 * time.Second), "1 minute ago"},
		{"minutes", now.Add(-5*time.Minute - time.Second), "5 minutes ago"},
		{"one hour", now.Add(-61 * time.Minute), "1 hour ago"},
		{"hours", now.Add(-5*time.Hour - time.Minute), "5 hours ago"},
		{"one day", now.Add(-25 * time.Hour), "1 day ago"},
		{"days", now.Add(-3*24*time.Hour - time.Hour), "3 days ago"},
		{"old date", time.Date(2020, 3, 4, 0, 0, 0, 0, time.UTC), "Mar 4, 2020"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatTimeAgo(tt.t))
		})
	}
}

func TestDashboardItem(t *testing.T) {
	it := DashboardItem{title: "T", description: "D"}
	assert.Equal(t, "T", it.Title())
	assert.Equal(t, "D", it.Description())
	assert.Equal(t, "T", it.FilterValue())
}
