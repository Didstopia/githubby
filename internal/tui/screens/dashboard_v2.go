package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
)

// DashboardAction represents available dashboard actions
type DashboardAction int

const (
	ActionNewSync DashboardAction = iota
	ActionSyncAll
	ActionSyncPending
	ActionCleanReleases
	ActionViewHistory
)

// DashboardItem represents a menu item in the dashboard
type DashboardItem struct {
	title       string
	description string
	action      DashboardAction
	profileID   string // For profile items
	disabled    bool
}

func (i DashboardItem) Title() string       { return i.title }
func (i DashboardItem) Description() string { return i.description }
func (i DashboardItem) FilterValue() string { return i.title }

// DashboardV2 is the redesigned main dashboard screen
type DashboardV2 struct {
	ctx    context.Context
	app    *tui.App
	styles *tui.Styles
	keys   tui.KeyMap

	// List component for actions
	actionList list.Model

	// Dashboard state
	profiles    []*state.SyncProfile
	stats       state.SyncStats
	lastSync    time.Time
	pendingSync int

	// Dimensions
	width  int
	height int

	// State
	message string
	err     error

	// Exit confirmation state
	exitPending bool
	exitKey     string
}

// NewDashboardV2 creates a new dashboard screen
func NewDashboardV2(ctx context.Context, app *tui.App) *DashboardV2 {
	d := &DashboardV2{
		ctx:    ctx,
		app:    app,
		styles: tui.GetStyles(),
		keys:   tui.GetKeyMap(),
		width:  80,
		height: 24,
	}

	d.loadData()
	d.initList()
	return d
}

// loadData loads data from storage
func (d *DashboardV2) loadData() {
	if d.app.Storage() == nil {
		return
	}

	d.profiles = d.app.Storage().GetProfiles()
	d.stats = d.app.Storage().GetSyncStats()
	d.lastSync = d.stats.LastSync

	// Calculate pending syncs (profiles that haven't synced in 24h)
	d.pendingSync = 0
	for _, p := range d.profiles {
		if time.Since(p.LastSyncAt) > 24*time.Hour {
			d.pendingSync++
		}
	}
}

// initList initializes the action list
func (d *DashboardV2) initList() {
	items := d.buildMenuItems()

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = d.styles.MenuItemSelected
	delegate.Styles.SelectedDesc = d.styles.Muted
	delegate.Styles.NormalTitle = d.styles.MenuItem
	delegate.Styles.NormalDesc = d.styles.Muted
	delegate.SetHeight(3)

	l := list.New(items, delegate, 60, 12)
	l.Title = ""
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowTitle(false)

	d.actionList = l
}

// buildMenuItems creates the menu items based on current state
func (d *DashboardV2) buildMenuItems() []list.Item {
	items := []list.Item{}

	// Quick actions
	items = append(items, DashboardItem{
		title:       "New Sync Profile",
		description: "Set up a new repository sync configuration",
		action:      ActionNewSync,
	})

	if len(d.profiles) > 0 {
		items = append(items, DashboardItem{
			title:       "Sync All Profiles",
			description: fmt.Sprintf("Sync all %d configured profiles", len(d.profiles)),
			action:      ActionSyncAll,
		})
	}

	if d.pendingSync > 0 {
		items = append(items, DashboardItem{
			title:       fmt.Sprintf("Sync Pending (%d)", d.pendingSync),
			description: "Sync profiles that haven't synced recently",
			action:      ActionSyncPending,
		})
	}

	// Profiles section
	for _, p := range d.profiles {
		lastSync := "never"
		if !p.LastSyncAt.IsZero() {
			lastSync = formatTimeAgo(p.LastSyncAt)
		}
		items = append(items, DashboardItem{
			title:       fmt.Sprintf("  %s", p.Name),
			description: fmt.Sprintf("%s/%s - Last sync: %s", p.Type, p.Source, lastSync),
			profileID:   p.ID,
		})
	}

	// Other actions
	items = append(items, DashboardItem{
		title:       "Clean Releases",
		description: "Remove old releases from a repository",
		action:      ActionCleanReleases,
	})

	return items
}

// Title returns the screen title
func (d *DashboardV2) Title() string {
	return "Dashboard"
}

// ShortHelp returns key bindings for the footer
func (d *DashboardV2) ShortHelp() []key.Binding {
	return []key.Binding{
		d.keys.Up,
		d.keys.Down,
		d.keys.Select,
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new profile")),
	}
}

// Init initializes the dashboard screen
func (d *DashboardV2) Init() tea.Cmd {
	d.loadData()
	d.initList()
	return nil
}

// Update handles messages
func (d *DashboardV2) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		d.actionList.SetWidth(msg.Width - 8)
		d.actionList.SetHeight(msg.Height - 16)

	case tea.KeyMsg:
		// Handle Ctrl+C - require double press
		if msg.Type == tea.KeyCtrlC {
			if d.exitPending && d.exitKey == "ctrl+c" {
				return d, tea.Quit
			}
			d.exitPending = true
			d.exitKey = "ctrl+c"
			return d, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)
		}
		switch {
		case key.Matches(msg, d.keys.Select):
			d.exitPending = false
			return d, d.handleSelection()

		case msg.String() == "n":
			d.exitPending = false
			return d, func() tea.Msg {
				return tui.NewSyncRequestedMsg{}
			}

		case key.Matches(msg, d.keys.Back):
			if d.exitPending && d.exitKey == "escape" {
				return d, tea.Quit
			}
			d.exitPending = true
			d.exitKey = "escape"
			return d, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)

		case key.Matches(msg, d.keys.Quit):
			if d.exitPending && d.exitKey == "q" {
				return d, tea.Quit
			}
			d.exitPending = true
			d.exitKey = "q"
			return d, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)
		}
		if d.exitPending {
			d.exitPending = false
		}

	case tui.ExitTimeoutMsg:
		d.exitPending = false

	case tui.ClearMessageMsg:
		d.message = ""

	case tui.RefreshDashboardMsg:
		d.loadData()
		d.initList()
	}

	// Update list
	var cmd tea.Cmd
	d.actionList, cmd = d.actionList.Update(msg)
	cmds = append(cmds, cmd)

	return d, tea.Batch(cmds...)
}

// handleSelection handles menu item selection
func (d *DashboardV2) handleSelection() tea.Cmd {
	item, ok := d.actionList.SelectedItem().(DashboardItem)
	if !ok {
		return nil
	}

	if item.disabled {
		d.message = "This feature is not yet available."
		return tui.ClearMessageCmd(tui.MessageDisplayDuration)
	}

	// If it's a profile item
	if item.profileID != "" {
		profile := d.app.Storage().GetProfile(item.profileID)
		if profile != nil {
			return func() tea.Msg {
				return tui.ProfileSelectedMsg{Profile: profile}
			}
		}
		return nil
	}

	// Handle action
	switch item.action {
	case ActionNewSync:
		return func() tea.Msg {
			return tui.NewSyncRequestedMsg{}
		}
	case ActionSyncAll:
		d.message = "Sync All Profiles: Coming soon! For now, select individual profiles to sync."
		return tui.ClearMessageCmd(tui.MessageDisplayDuration * 2)
	case ActionSyncPending:
		d.message = "Sync Pending: Coming soon! For now, select individual profiles to sync."
		return tui.ClearMessageCmd(tui.MessageDisplayDuration * 2)
	case ActionCleanReleases:
		return tui.PushScreenCmd(tui.ScreenClean)
	case ActionViewHistory:
		d.message = "History view coming soon!"
		return tui.ClearMessageCmd(tui.MessageDisplayDuration)
	}

	return nil
}

// View renders the dashboard screen
func (d *DashboardV2) View() string {
	var content strings.Builder

	// Sync status summary
	content.WriteString(d.renderSyncStatus())
	content.WriteString("\n\n")

	// Quick actions section
	content.WriteString(d.styles.FormTitle.Render("QUICK ACTIONS"))
	content.WriteString("\n\n")
	content.WriteString(d.actionList.View())

	// Show message if any
	if d.message != "" {
		content.WriteString("\n\n")
		content.WriteString(d.styles.Info.Render(d.message))
	}

	// Add exit confirmation message if pending
	if d.exitPending {
		var msg string
		switch d.exitKey {
		case "ctrl+c":
			msg = "Press Ctrl+C again to quit"
		case "escape":
			msg = "Press Escape again to quit"
		case "q":
			msg = "Press q again to quit"
		}
		content.WriteString("\n\n")
		content.WriteString(d.styles.Warning.Render(msg))
	}

	return lipgloss.NewStyle().
		Padding(1, 2).
		Render(content.String())
}

// renderSyncStatus renders the sync status summary box
func (d *DashboardV2) renderSyncStatus() string {
	var status strings.Builder

	// Last sync info
	lastSyncStr := "Never"
	if !d.lastSync.IsZero() {
		lastSyncStr = formatTimeAgo(d.lastSync)
	}
	status.WriteString(d.styles.Muted.Render("Last Sync: "))
	status.WriteString(lastSyncStr)
	status.WriteString("\n")

	// Stats
	if d.stats.TotalSyncs > 0 {
		status.WriteString(d.styles.Success.Render("●"))
		status.WriteString(fmt.Sprintf(" %d repos synced   ", d.stats.TotalReposSynced()))

		if d.pendingSync > 0 {
			status.WriteString(d.styles.Warning.Render("●"))
			status.WriteString(fmt.Sprintf(" %d pending   ", d.pendingSync))
		}

		if d.stats.TotalFailed > 0 {
			status.WriteString(d.styles.Error.Render("○"))
			status.WriteString(fmt.Sprintf(" %d failed", d.stats.TotalFailed))
		}
	} else {
		status.WriteString(d.styles.Muted.Render("No sync history yet. Create a sync profile to get started!"))
	}

	boxStyle := d.styles.Box.Width(d.width - 12)
	return boxStyle.Render(status.String())
}

// formatTimeAgo formats a time as a human-readable "time ago" string
func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case duration < 7*24*time.Hour:
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 2, 2006")
	}
}
