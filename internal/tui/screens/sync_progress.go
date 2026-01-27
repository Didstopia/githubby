package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	gh "github.com/google/go-github/v68/github"

	"github.com/Didstopia/githubby/internal/tui"
	"github.com/Didstopia/githubby/internal/tui/components"
)

// SyncStatus represents the status of a repository sync
type SyncStatus int

const (
	SyncStatusPending SyncStatus = iota
	SyncStatusInProgress
	SyncStatusCloned
	SyncStatusUpdated
	SyncStatusSkipped
	SyncStatusFailed
)

// SyncItem represents a repository being synced
type SyncItem struct {
	Name    string
	Status  SyncStatus
	Message string
}

// SyncProgressScreen shows sync operation progress
type SyncProgressScreen struct {
	ctx    context.Context
	cancel context.CancelFunc
	styles *tui.Styles
	keys   tui.KeyMap

	// Repositories to sync
	repos     []*gh.Repository
	targetDir string

	// Progress tracking
	items       []SyncItem
	currentIdx  int
	progress    progress.Model
	spinner     spinner.Model

	// Statistics
	cloned   int
	updated  int
	skipped  int
	failed   int
	startTime time.Time

	// Dimensions
	width  int
	height int

	// State
	syncing  bool
	complete bool
	err      error
}

// NewSyncProgressScreen creates a new sync progress screen
func NewSyncProgressScreen(ctx context.Context, repos []*gh.Repository, targetDir string) *SyncProgressScreen {
	ctx, cancel := context.WithCancel(ctx)

	// Create progress bar
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	// Create spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = tui.GetStyles().Spinner

	// Initialize items
	items := make([]SyncItem, len(repos))
	for i, repo := range repos {
		items[i] = SyncItem{
			Name:   repo.GetFullName(),
			Status: SyncStatusPending,
		}
	}

	return &SyncProgressScreen{
		ctx:       ctx,
		cancel:    cancel,
		styles:    tui.GetStyles(),
		keys:      tui.GetKeyMap(),
		repos:     repos,
		targetDir: targetDir,
		items:     items,
		progress:  p,
		spinner:   s,
		width:     80,
		height:    24,
	}
}

// Title returns the screen title
func (s *SyncProgressScreen) Title() string {
	return "Syncing Repositories"
}

// ShortHelp returns key bindings for the footer
func (s *SyncProgressScreen) ShortHelp() []key.Binding {
	if s.complete {
		return []key.Binding{
			s.keys.Back,
		}
	}
	return []key.Binding{
		s.keys.Cancel,
	}
}

// Init initializes the sync progress screen
func (s *SyncProgressScreen) Init() tea.Cmd {
	s.startTime = time.Now()
	s.syncing = true
	return tea.Batch(
		s.spinner.Tick,
		s.startSync(),
	)
}

// Update handles messages
func (s *SyncProgressScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.progress.Width = msg.Width - 20

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.keys.Cancel):
			if s.syncing {
				s.cancel()
				s.syncing = false
				s.complete = true
			}
		case key.Matches(msg, s.keys.Back):
			if s.complete {
				return s, tui.PopScreenCmd()
			}
		case key.Matches(msg, s.keys.Quit):
			if s.complete {
				return s, tui.QuitCmd()
			}
		}

	case spinner.TickMsg:
		if s.syncing {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case progress.FrameMsg:
		var cmd tea.Cmd
		progressModel, cmd := s.progress.Update(msg)
		s.progress = progressModel.(progress.Model)
		cmds = append(cmds, cmd)

	case syncProgressMsg:
		s.updateItem(msg.index, msg.status, msg.message)
		if !s.complete && s.syncing {
			cmds = append(cmds, s.syncNext())
		}

	case syncCompleteMsg:
		s.syncing = false
		s.complete = true
	}

	return s, tea.Batch(cmds...)
}

// View renders the sync progress screen
func (s *SyncProgressScreen) View() string {
	var content strings.Builder

	// Progress header
	total := len(s.items)
	done := s.cloned + s.updated + s.skipped + s.failed
	pct := float64(done) / float64(total)

	content.WriteString(s.styles.FormTitle.Render("Syncing Repositories"))
	content.WriteString("\n\n")

	// Progress bar
	content.WriteString(s.progress.ViewAs(pct))
	content.WriteString(fmt.Sprintf(" %d/%d", done, total))
	content.WriteString("\n\n")

	// Statistics
	stats := []string{}
	if s.cloned > 0 {
		stats = append(stats, s.styles.Success.Render(fmt.Sprintf("%d cloned", s.cloned)))
	}
	if s.updated > 0 {
		stats = append(stats, s.styles.Info.Render(fmt.Sprintf("%d updated", s.updated)))
	}
	if s.skipped > 0 {
		stats = append(stats, s.styles.Warning.Render(fmt.Sprintf("%d skipped", s.skipped)))
	}
	if s.failed > 0 {
		stats = append(stats, s.styles.Error.Render(fmt.Sprintf("%d failed", s.failed)))
	}
	if len(stats) > 0 {
		content.WriteString(strings.Join(stats, "  "))
		content.WriteString("\n\n")
	}

	// Current operation
	if s.syncing && s.currentIdx < len(s.items) {
		content.WriteString(s.spinner.View())
		content.WriteString(" " + s.items[s.currentIdx].Name)
		content.WriteString("\n\n")
	}

	// Recent items (last 10)
	content.WriteString("Recent:\n")
	start := 0
	if done > 10 {
		start = done - 10
	}

	for i := start; i < len(s.items) && i < done+1; i++ {
		item := s.items[i]
		var statusIcon string
		var nameStyle lipgloss.Style

		switch item.Status {
		case SyncStatusPending:
			statusIcon = s.styles.Muted.Render("○")
			nameStyle = s.styles.Muted
		case SyncStatusInProgress:
			statusIcon = s.styles.Info.Render("◐")
			nameStyle = s.styles.Info
		case SyncStatusCloned:
			statusIcon = s.styles.Success.Render("●")
			nameStyle = s.styles.Success
		case SyncStatusUpdated:
			statusIcon = s.styles.Info.Render("●")
			nameStyle = s.styles.Info
		case SyncStatusSkipped:
			statusIcon = s.styles.Warning.Render("○")
			nameStyle = s.styles.Warning
		case SyncStatusFailed:
			statusIcon = s.styles.Error.Render("●")
			nameStyle = s.styles.Error
		}

		line := fmt.Sprintf("  %s %s", statusIcon, nameStyle.Render(item.Name))
		if item.Message != "" {
			line += s.styles.Muted.Render(" - " + item.Message)
		}
		content.WriteString(line + "\n")
	}

	// Completion message
	if s.complete {
		content.WriteString("\n")
		elapsed := time.Since(s.startTime).Round(time.Second)
		content.WriteString(s.styles.Success.Render(fmt.Sprintf("Sync complete in %s", elapsed)))
		content.WriteString("\n\n")
		content.WriteString("Press " + s.styles.HelpKey.Render("Esc") + " to go back")
	}

	return lipgloss.NewStyle().
		Padding(1, 2).
		Render(content.String())
}

// updateItem updates a sync item's status
func (s *SyncProgressScreen) updateItem(index int, status SyncStatus, message string) {
	if index >= 0 && index < len(s.items) {
		s.items[index].Status = status
		s.items[index].Message = message

		switch status {
		case SyncStatusCloned:
			s.cloned++
		case SyncStatusUpdated:
			s.updated++
		case SyncStatusSkipped:
			s.skipped++
		case SyncStatusFailed:
			s.failed++
		}
	}
}

// startSync starts the sync operation
func (s *SyncProgressScreen) startSync() tea.Cmd {
	return s.syncNext()
}

// syncNext syncs the next repository
func (s *SyncProgressScreen) syncNext() tea.Cmd {
	// Find next pending item
	nextIdx := -1
	for i, item := range s.items {
		if item.Status == SyncStatusPending {
			nextIdx = i
			break
		}
	}

	if nextIdx < 0 {
		// All done
		return func() tea.Msg {
			return syncCompleteMsg{}
		}
	}

	s.currentIdx = nextIdx
	s.items[nextIdx].Status = SyncStatusInProgress

	return func() tea.Msg {
		// Simulate sync operation
		// In real implementation, this would call the sync package
		select {
		case <-s.ctx.Done():
			return syncProgressMsg{
				index:   nextIdx,
				status:  SyncStatusSkipped,
				message: "cancelled",
			}
		case <-time.After(100 * time.Millisecond):
			// Simulate success/failure
			// In real implementation, this would be the actual result
			return syncProgressMsg{
				index:   nextIdx,
				status:  SyncStatusCloned,
				message: "cloned",
			}
		}
	}
}

// Message types
type syncProgressMsg struct {
	index   int
	status  SyncStatus
	message string
}

type syncCompleteMsg struct{}

// ProgressCallback is called to report sync progress
type ProgressCallback func(repo string, status components.ProgressStatus, message string)

// CreateProgressCallback creates a callback that sends messages to the TUI
func CreateProgressCallback(p *tea.Program) ProgressCallback {
	return func(repo string, status components.ProgressStatus, message string) {
		// Convert component status to sync status
		var syncStatus SyncStatus
		switch status {
		case components.StatusPending:
			syncStatus = SyncStatusPending
		case components.StatusInProgress:
			syncStatus = SyncStatusInProgress
		case components.StatusSuccess:
			syncStatus = SyncStatusCloned
		case components.StatusWarning:
			syncStatus = SyncStatusSkipped
		case components.StatusError:
			syncStatus = SyncStatusFailed
		case components.StatusSkipped:
			syncStatus = SyncStatusSkipped
		}

		p.Send(syncProgressMsg{
			index:   -1, // Will be resolved by repo name
			status:  syncStatus,
			message: message,
		})
	}
}
