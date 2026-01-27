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

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/sync"
	"github.com/Didstopia/githubby/internal/tui"
)

// SyncProgressScreen shows sync operation progress for a profile or multiple profiles
type SyncProgressScreen struct {
	ctx    context.Context
	app    *tui.App
	styles *tui.Styles
	keys   tui.KeyMap

	// Profile(s) being synced
	profile  *state.SyncProfile   // Single profile
	profiles []*state.SyncProfile // Multiple profiles (batch sync)

	// Progress tracking
	items      []syncProgressItem
	currentIdx int
	progress   progress.Model
	spinner    spinner.Model

	// Statistics
	cloned    int
	updated   int
	skipped   int
	failed    int
	startTime time.Time
	totalRepos int

	// Dimensions
	width  int
	height int

	// State
	loading  bool
	syncing  bool
	complete bool
	err      error

	// Exit confirmation
	exitPending bool
	exitKey     string
}

type syncProgressItem struct {
	name    string
	status  string // "pending", "syncing", "cloned", "updated", "skipped", "failed"
	message string
}

// NewSyncProgress creates a new sync progress screen
func NewSyncProgress(ctx context.Context, app *tui.App) *SyncProgressScreen {
	// Create progress bar
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	// Create spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = tui.GetStyles().Spinner

	return &SyncProgressScreen{
		ctx:      ctx,
		app:      app,
		styles:   tui.GetStyles(),
		keys:     tui.GetKeyMap(),
		progress: p,
		spinner:  s,
		width:    80,
		height:   24,
		loading:  true,
	}
}

// Title returns the screen title
func (s *SyncProgressScreen) Title() string {
	if len(s.profiles) > 1 {
		return fmt.Sprintf("Syncing %d Profiles", len(s.profiles))
	}
	if s.profile != nil {
		return fmt.Sprintf("Syncing %s", s.profile.Name)
	}
	if len(s.profiles) == 1 {
		return fmt.Sprintf("Syncing %s", s.profiles[0].Name)
	}
	return "Syncing"
}

// ShortHelp returns key bindings for the footer
func (s *SyncProgressScreen) ShortHelp() []key.Binding {
	if s.complete {
		return []key.Binding{
			s.keys.Select,
			s.keys.Back,
		}
	}
	return []key.Binding{}
}

// Init initializes the sync progress screen
func (s *SyncProgressScreen) Init() tea.Cmd {
	// Check for batch sync first
	s.profiles = s.app.ProfilesToSync()
	if len(s.profiles) == 0 {
		// Fall back to single profile
		s.profile = s.app.SelectedProfile()
		if s.profile != nil {
			s.profiles = []*state.SyncProfile{s.profile}
		}
	}

	if len(s.profiles) == 0 {
		s.err = fmt.Errorf("no profile selected")
		s.loading = false
		s.complete = true
		return nil
	}

	// For single profile, also set the profile field for compatibility
	if len(s.profiles) == 1 {
		s.profile = s.profiles[0]
	}

	s.startTime = time.Now()

	// Initialize items from all profiles' selected repos
	s.items = []syncProgressItem{}
	for _, profile := range s.profiles {
		for _, repoName := range profile.SelectedRepos {
			s.items = append(s.items, syncProgressItem{
				name:   repoName,
				status: "pending",
			})
		}
	}
	s.totalRepos = len(s.items)

	s.loading = false
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
		// Handle Ctrl+C
		if msg.Type == tea.KeyCtrlC {
			if s.exitPending && s.exitKey == "ctrl+c" {
				return s, tui.PopScreenCmd()
			}
			s.exitPending = true
			s.exitKey = "ctrl+c"
			return s, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)
		}

		switch {
		case key.Matches(msg, s.keys.Back), key.Matches(msg, s.keys.Select):
			s.exitPending = false
			if s.complete {
				return s, tea.Batch(
					tui.PopScreenCmd(),
					tui.RefreshDashboardCmd(),
				)
			}
		case key.Matches(msg, s.keys.Quit):
			if s.complete {
				if s.exitPending && s.exitKey == "q" {
					return s, tui.QuitCmd()
				}
				s.exitPending = true
				s.exitKey = "q"
				return s, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)
			}
		}

		if s.exitPending {
			s.exitPending = false
		}

	case tui.ExitTimeoutMsg:
		s.exitPending = false

	case spinner.TickMsg:
		if s.syncing || s.loading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case progress.FrameMsg:
		var cmd tea.Cmd
		progressModel, cmd := s.progress.Update(msg)
		s.progress = progressModel.(progress.Model)
		cmds = append(cmds, cmd)

	case profileSyncCompleteMsg:
		s.syncing = false
		s.complete = true
		s.cloned = msg.cloned
		s.updated = msg.updated
		s.skipped = msg.skipped
		s.failed = msg.failed
		s.err = msg.err
		// Profile updates are done in startSync
	}

	return s, tea.Batch(cmds...)
}

// View renders the sync progress screen
func (s *SyncProgressScreen) View() string {
	var content strings.Builder

	if s.loading {
		content.WriteString(s.spinner.View())
		content.WriteString(" Loading profile...")
		return s.styles.Content.Render(content.String())
	}

	if s.err != nil && !s.syncing && len(s.items) == 0 {
		content.WriteString(s.styles.Error.Render("Error: " + s.err.Error()))
		content.WriteString("\n\n")
		content.WriteString("Press " + s.styles.HelpKey.Render("Esc") + " to go back")
		return s.styles.Content.Render(content.String())
	}

	// Title
	if len(s.profiles) > 1 {
		content.WriteString(s.styles.FormTitle.Render(fmt.Sprintf("Syncing %d Profiles", len(s.profiles))))
	} else if s.profile != nil {
		content.WriteString(s.styles.FormTitle.Render(fmt.Sprintf("Syncing: %s", s.profile.Name)))
	} else {
		content.WriteString(s.styles.FormTitle.Render("Syncing"))
	}
	content.WriteString("\n\n")

	// Progress
	total := s.totalRepos
	done := s.cloned + s.updated + s.skipped + s.failed
	if total > 0 {
		pct := float64(done) / float64(total)
		content.WriteString(s.progress.ViewAs(pct))
		content.WriteString(fmt.Sprintf(" %d/%d repos", done, total))
		content.WriteString("\n\n")
	}

	// Current operation
	if s.syncing {
		content.WriteString(s.spinner.View())
		if len(s.profiles) > 1 {
			content.WriteString(fmt.Sprintf(" Syncing %d repositories across %d profiles...", total, len(s.profiles)))
		} else if s.profile != nil {
			content.WriteString(fmt.Sprintf(" Syncing %d repositories to %s...", total, s.profile.TargetDir))
		} else {
			content.WriteString(fmt.Sprintf(" Syncing %d repositories...", total))
		}
		content.WriteString("\n")
		content.WriteString(s.styles.Muted.Render("This may take a while for large repositories."))
		content.WriteString("\n\n")
	}

	// Results (when complete)
	if s.complete {
		if s.err != nil {
			content.WriteString(s.styles.Error.Render("Sync completed with errors: " + s.err.Error()))
		} else if s.failed > 0 {
			content.WriteString(s.styles.Warning.Render(fmt.Sprintf("Synced %d repositories with %d failures", total, s.failed)))
		} else {
			content.WriteString(s.styles.Success.Render(fmt.Sprintf("Successfully synced %d repositories!", total)))
		}
		content.WriteString("\n\n")

		// Statistics
		content.WriteString(s.styles.Info.Render("Results:"))
		content.WriteString("\n")
		if s.cloned > 0 {
			content.WriteString(fmt.Sprintf("  %s Cloned: %d\n", s.styles.Success.Render("●"), s.cloned))
		}
		if s.updated > 0 {
			content.WriteString(fmt.Sprintf("  %s Updated: %d\n", s.styles.Success.Render("●"), s.updated))
		}
		if s.skipped > 0 {
			content.WriteString(fmt.Sprintf("  %s Skipped: %d\n", s.styles.Warning.Render("●"), s.skipped))
		}
		if s.failed > 0 {
			content.WriteString(fmt.Sprintf("  %s Failed: %d\n", s.styles.Error.Render("●"), s.failed))
		}
		if s.cloned == 0 && s.updated == 0 && s.skipped == 0 && s.failed == 0 {
			content.WriteString(s.styles.Muted.Render("  No changes - all repositories up to date\n"))
		}

		elapsed := time.Since(s.startTime).Round(time.Second)
		content.WriteString("\n")
		content.WriteString(s.styles.Muted.Render(fmt.Sprintf("Completed in %s", elapsed)))
		content.WriteString("\n\n")
		content.WriteString("Press " + s.styles.HelpKey.Render("Enter") + " to return to dashboard")
	}

	// Exit confirmation
	if s.exitPending {
		var msg string
		switch s.exitKey {
		case "ctrl+c":
			msg = "Press Ctrl+C again to cancel"
		case "q":
			msg = "Press q again to quit"
		}
		content.WriteString("\n\n")
		content.WriteString(s.styles.Warning.Render(msg))
	}

	return s.styles.Content.Render(content.String())
}

// startSync starts the sync operation
func (s *SyncProgressScreen) startSync() tea.Cmd {
	return func() tea.Msg {
		if len(s.profiles) == 0 {
			return profileSyncCompleteMsg{err: fmt.Errorf("no profiles")}
		}

		client := s.app.GitHubClient()
		if client == nil {
			return profileSyncCompleteMsg{err: fmt.Errorf("not authenticated")}
		}

		// Use quiet git mode
		gitOps, err := git.NewQuiet()
		if err != nil {
			return profileSyncCompleteMsg{err: fmt.Errorf("git not available: %w", err)}
		}

		var cloned, updated, skipped, failed int

		// Sync each profile
		for _, profile := range s.profiles {
			opts := &sync.Options{
				Target:         profile.TargetDir,
				IncludePrivate: profile.IncludePrivate,
			}

			syncer := sync.New(client, gitOps, opts)

			// Sync each repo in this profile
			for _, repoFullName := range profile.SelectedRepos {
				parts := strings.SplitN(repoFullName, "/", 2)
				if len(parts) != 2 {
					failed++
					continue
				}
				owner, repo := parts[0], parts[1]

				result, err := syncer.SyncRepo(s.ctx, owner, repo)
				if err != nil {
					failed++
					continue
				}

				if result != nil {
					cloned += len(result.Cloned)
					updated += len(result.Updated)
					skipped += len(result.Skipped)
					failed += len(result.Failed)
				}
			}

			// Update this profile's last sync time
			if s.app.Storage() != nil {
				profile.LastSyncAt = time.Now()
				s.app.Storage().UpdateProfile(profile)
			}
		}

		// Save storage once at the end
		if s.app.Storage() != nil {
			s.app.Storage().Save()
		}

		return profileSyncCompleteMsg{
			cloned:  cloned,
			updated: updated,
			skipped: skipped,
			failed:  failed,
		}
	}
}

// Message types
type profileSyncCompleteMsg struct {
	cloned  int
	updated int
	skipped int
	failed  int
	err     error
}
