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
	gh "github.com/google/go-github/v68/github"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/sync"
	"github.com/Didstopia/githubby/internal/tui"
	tuiutil "github.com/Didstopia/githubby/internal/tui/util"
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
	items    []syncProgressItem
	progress progress.Model
	spinner    spinner.Model

	// Statistics
	cloned     int
	updated    int
	upToDate   int
	skipped    int
	failed     int
	archived   int
	startTime  time.Time
	totalRepos int

	// ETA tracking
	lastUpdateTime   time.Time
	reposCompleted   int
	lastDisplayedETA time.Duration
	etaUpdateCount   int
	lastETADoneCount int // tracks done count when ETA was last calculated

	// Dimensions
	width  int
	height int

	// State
	loading    bool
	collecting bool // True while collecting repos from API
	syncing    bool
	complete   bool
	err        error

	// Collecting phase tracking
	collectingCount int // Number of repos collected so far

	// Exit confirmation
	exitPending bool
	exitKey     string

	// Channel for async sync progress (completion is sent as status="complete")
	syncProgressChan chan profileSyncProgressUpdate

	// Current repo being synced
	currentRepo string
}

type syncProgressItem struct {
	name   string
	status string // "pending", "syncing", "cloned", "updated", "skipped", "failed"
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
	// For "all repos" profiles (SyncAllRepos=true or empty SelectedRepos),
	// we don't have a count upfront - it will be fetched from the API
	s.items = []syncProgressItem{}
	hasAllReposProfile := false
	for _, profile := range s.profiles {
		if profile.SyncAllRepos || len(profile.SelectedRepos) == 0 {
			hasAllReposProfile = true
			// Add a placeholder item for "all repos" profiles
			s.items = append(s.items, syncProgressItem{
				name:   fmt.Sprintf("%s/%s (all repos)", profile.Type, profile.Source),
				status: "pending",
			})
		} else {
			for _, repoName := range profile.SelectedRepos {
				s.items = append(s.items, syncProgressItem{
					name:   repoName,
					status: "pending",
				})
			}
		}
	}
	// If any profile syncs all repos, we can't know the total upfront
	if hasAllReposProfile {
		s.totalRepos = 0 // Will be determined during sync
	} else {
		s.totalRepos = len(s.items)
	}

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

	case profileSyncProgressMsg:
		s.currentRepo = msg.update.repoName
		if msg.update.total > 0 {
			s.totalRepos = msg.update.total
		}
		s.lastUpdateTime = time.Now()

		// Update statistics based on status (only count completed statuses, not "syncing" or "collecting")
		switch msg.update.status {
		case "collecting":
			// Update collecting count for display, transition from collecting to syncing phase
			s.collecting = true
			s.collectingCount = msg.update.current
		case "syncing":
			// Transition out of collecting phase when syncing starts
			s.collecting = false
		case "cloned":
			s.collecting = false
			s.cloned++
			s.reposCompleted++
		case "updated":
			s.collecting = false
			s.updated++
			s.reposCompleted++
		case "up-to-date":
			s.collecting = false
			s.upToDate++
			s.reposCompleted++
		case "skipped":
			s.collecting = false
			s.skipped++
			s.reposCompleted++
		case "failed":
			s.collecting = false
			s.failed++
			s.reposCompleted++
		case "archived":
			s.collecting = false
			s.archived++
			s.reposCompleted++
		case "complete":
			// Sync finished - set progress to 100% and mark complete
			s.syncing = false
			s.complete = true
			s.collecting = false
			if msg.update.err != nil {
				s.err = msg.update.err
			}
			// Set progress bar to 100%
			if s.totalRepos > 0 {
				cmds = append(cmds, s.progress.SetPercent(1.0))
			}
			// Ring terminal bell to notify user
			tuiutil.Bell()
			return s, tea.Batch(cmds...)
		}

		// Update progress bar and ETA (skip for complete status - handled above)
		if s.totalRepos > 0 {
			done := s.cloned + s.updated + s.upToDate + s.skipped + s.failed + s.archived
			cmds = append(cmds, s.progress.SetPercent(float64(done)/float64(s.totalRepos)))

			// Recalculate ETA only when done count actually changes (repo completed)
			// This prevents ETA from going up while waiting for batch to complete
			if done > s.lastETADoneCount && msg.update.status != "syncing" {
				s.lastETADoneCount = done

				// Only calculate ETA after warmup period
				// For initial sync (mostly clones), we can show ETA sooner since each operation takes longer
				// For incremental sync (mostly updates), wait for more samples for accuracy
				minSamples := 1 // Start with 1 for initial sync
				if s.cloned == 0 && done > 0 {
					// Incremental sync (no clones yet) - use more conservative warmup
					minSamples = min(10, s.totalRepos/10)
					if minSamples < 3 {
						minSamples = 3
					}
				}

				if done >= minSamples {
					elapsed := time.Since(s.startTime)
					avgPerRepo := elapsed / time.Duration(done)
					remaining := s.totalRepos - done
					newETA := avgPerRepo * time.Duration(remaining)

					// Smooth the ETA: blend 70% old, 30% new (exponential moving average)
					if s.lastDisplayedETA > 0 {
						newETA = time.Duration(float64(s.lastDisplayedETA)*0.7 + float64(newETA)*0.3)
					}

					// Only decrease ETA, never increase (user expectation)
					// Allow small increases (< 10%) for accuracy, but cap large jumps
					if s.lastDisplayedETA == 0 || newETA <= s.lastDisplayedETA || float64(newETA) < float64(s.lastDisplayedETA)*1.1 {
						s.lastDisplayedETA = newETA
					}
					s.etaUpdateCount++
				}
			}
		}

		// Continue listening for more progress updates
		cmds = append(cmds, s.waitForSyncProgress())
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
	done := s.cloned + s.updated + s.upToDate + s.skipped + s.failed + s.archived
	if total > 0 {
		pct := float64(done) / float64(total)
		content.WriteString(s.progress.ViewAs(pct))
		content.WriteString(fmt.Sprintf(" %d/%d repos", done, total))

		// Show pre-calculated ETA (calculated in Update when repos complete)
		// This prevents ETA from going up while waiting for batch completions
		if s.syncing && s.lastDisplayedETA > 5*time.Second {
			content.WriteString(fmt.Sprintf(" • ~%s remaining", humanizeDuration(s.lastDisplayedETA)))
		}
		content.WriteString("\n\n")
	}

	// Current operation
	if s.syncing {
		content.WriteString(s.spinner.View())
		if s.collecting {
			// Show collecting phase with count
			if s.collectingCount > 0 {
				content.WriteString(fmt.Sprintf(" Collecting repositories... (%d found)", s.collectingCount))
			} else {
				content.WriteString(" Collecting repositories from GitHub...")
			}
		} else if s.currentRepo != "" {
			// Show current repo being synced with running totals
			content.WriteString(fmt.Sprintf(" [%d/%d] Syncing %s...", done+1, total, s.currentRepo))
			// Show running totals if any work has been done
			if s.cloned > 0 || s.updated > 0 || s.upToDate > 0 {
				content.WriteString(fmt.Sprintf(" (cloned: %d, updated: %d, up-to-date: %d)", s.cloned, s.updated, s.upToDate))
			}
		} else if len(s.profiles) > 1 {
			if total > 0 {
				content.WriteString(fmt.Sprintf(" Syncing %d repositories across %d profiles...", total, len(s.profiles)))
			} else {
				content.WriteString(fmt.Sprintf(" Syncing all repositories across %d profiles...", len(s.profiles)))
			}
		} else if s.profile != nil {
			if total > 0 {
				content.WriteString(fmt.Sprintf(" Syncing %d repositories to %s...", total, s.profile.TargetDir))
			} else if s.profile.SyncAllRepos {
				content.WriteString(fmt.Sprintf(" Syncing all %s/%s repositories to %s...", s.profile.Type, s.profile.Source, s.profile.TargetDir))
			} else {
				content.WriteString(fmt.Sprintf(" Syncing repositories to %s...", s.profile.TargetDir))
			}
		} else {
			content.WriteString(fmt.Sprintf(" Syncing %d repositories...", total))
		}
		content.WriteString("\n\n")
	}

	// Results (when complete)
	if s.complete {
		// Use actual done count if total was unknown (0)
		actualTotal := total
		if actualTotal == 0 {
			actualTotal = done
		}
		if s.err != nil {
			content.WriteString(s.styles.Error.Render("Sync completed with errors: " + s.err.Error()))
		} else if s.failed > 0 {
			content.WriteString(s.styles.Warning.Render(fmt.Sprintf("Synced %d repositories with %d failures", actualTotal, s.failed)))
		} else {
			content.WriteString(s.styles.Success.Render(fmt.Sprintf("Successfully synced %d repositories!", actualTotal)))
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
		if s.upToDate > 0 {
			content.WriteString(fmt.Sprintf("  %s Up-to-date: %d\n", s.styles.Success.Render("●"), s.upToDate))
		}
		if s.skipped > 0 {
			content.WriteString(fmt.Sprintf("  %s Skipped: %d\n", s.styles.Warning.Render("●"), s.skipped))
		}
		if s.failed > 0 {
			content.WriteString(fmt.Sprintf("  %s Failed: %d\n", s.styles.Error.Render("●"), s.failed))
		}
		if s.archived > 0 {
			content.WriteString(fmt.Sprintf("  %s Archived: %d (preserved locally, no longer on remote)\n", s.styles.Info.Render("●"), s.archived))
		}
		if s.cloned == 0 && s.updated == 0 && s.upToDate == 0 && s.skipped == 0 && s.failed == 0 && s.archived == 0 {
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

// startSync starts the sync operation in a background goroutine
func (s *SyncProgressScreen) startSync() tea.Cmd {
	// Initialize channel with larger buffer to prevent worker blocking
	// Buffer size accounts for 4 concurrent workers + main loop sending completions
	s.syncProgressChan = make(chan profileSyncProgressUpdate, 16)

	// Start sync in background goroutine
	go s.runSyncInBackground()

	// Return command to listen for first progress update
	return tea.Batch(
		s.spinner.Tick,
		s.waitForSyncProgress(),
	)
}

// runSyncInBackground performs the actual sync operation
func (s *SyncProgressScreen) runSyncInBackground() {
	defer close(s.syncProgressChan)

	if len(s.profiles) == 0 {
		s.syncProgressChan <- profileSyncProgressUpdate{status: "complete", err: fmt.Errorf("no profiles")}
		return
	}

	client := s.app.GitHubClient()
	if client == nil {
		s.syncProgressChan <- profileSyncProgressUpdate{status: "complete", err: fmt.Errorf("not authenticated")}
		return
	}

	// Use quiet git mode with authentication token
	gitOps, err := git.NewQuietWithToken(s.app.Token())
	if err != nil {
		s.syncProgressChan <- profileSyncProgressUpdate{status: "complete", err: fmt.Errorf("git not available: %w", err)}
		return
	}

	var cloned, updated, skipped, failed, archived int

	// First, collect all repos to sync to get accurate total
	type repoToSync struct {
		owner         string
		repo          string
		defaultBranch string
		cloneURL      string
		isPrivate     bool
		pushedAt      *gh.Timestamp
		profile       *state.SyncProfile
	}
	var allRepos []repoToSync

	// Send initial collecting status
	s.syncProgressChan <- profileSyncProgressUpdate{
		status:  "collecting",
		current: 0,
		total:   0,
	}

	for _, profile := range s.profiles {
		// Check for context cancellation before processing each profile
		select {
		case <-s.ctx.Done():
			s.syncProgressChan <- profileSyncProgressUpdate{status: "complete", err: s.ctx.Err()}
			return
		default:
		}

		// Check if this is an "all repos" profile
		if profile.SyncAllRepos || len(profile.SelectedRepos) == 0 {
			// Fetch repos from API first
			listOpts := &github.ListOptions{
				IncludePrivate: profile.IncludePrivate,
			}

			var repos []*gh.Repository
			var err error

			if profile.Type == "org" {
				repos, err = client.ListOrgRepos(s.ctx, profile.Source, listOpts)
			} else {
				repos, err = client.ListUserRepos(s.ctx, profile.Source, listOpts)
			}

			if err != nil {
				// Check if it's a context cancellation
				if s.ctx.Err() != nil {
					s.syncProgressChan <- profileSyncProgressUpdate{status: "complete", err: s.ctx.Err()}
					return
				}
				// Send error as a failed repo
				s.syncProgressChan <- profileSyncProgressUpdate{
					repoName: fmt.Sprintf("%s/%s", profile.Type, profile.Source),
					status:   "failed",
					current:  len(allRepos),
					total:    0,
				}
				failed++
				continue
			}
			for _, r := range repos {
				allRepos = append(allRepos, repoToSync{
					owner:         r.GetOwner().GetLogin(),
					repo:          r.GetName(),
					defaultBranch: r.GetDefaultBranch(),
					cloneURL:      r.GetCloneURL(),
					isPrivate:     r.GetPrivate(),
					pushedAt:      r.PushedAt,
					profile:       profile,
				})
			}
			// Send collecting progress update after each profile's repos are fetched
			s.syncProgressChan <- profileSyncProgressUpdate{
				status:  "collecting",
				current: len(allRepos),
				total:   0,
			}
		} else {
			// Use specific repos from profile - fetch repo data upfront
			for _, repoFullName := range profile.SelectedRepos {
				// Check for context cancellation periodically
				select {
				case <-s.ctx.Done():
					s.syncProgressChan <- profileSyncProgressUpdate{status: "complete", err: s.ctx.Err()}
					return
				default:
				}

				parts := strings.SplitN(repoFullName, "/", 2)
				if len(parts) == 2 {
					owner, repoName := parts[0], parts[1]
					// Fetch repo data to get defaultBranch, cloneURL, etc.
					repoData, err := client.GetRepository(s.ctx, owner, repoName)
					if err != nil {
						// If we can't fetch repo data, still add it but without the optimization data
						// The sync will fall back to fetching it again
						allRepos = append(allRepos, repoToSync{
							owner:   owner,
							repo:    repoName,
							profile: profile,
						})
					} else {
						allRepos = append(allRepos, repoToSync{
							owner:         owner,
							repo:          repoName,
							defaultBranch: repoData.GetDefaultBranch(),
							cloneURL:      repoData.GetCloneURL(),
							isPrivate:     repoData.GetPrivate(),
							pushedAt:      repoData.PushedAt,
							profile:       profile,
						})
					}
					// Send collecting progress update periodically (every 10 repos)
					if len(allRepos)%10 == 0 {
						s.syncProgressChan <- profileSyncProgressUpdate{
							status:  "collecting",
							current: len(allRepos),
							total:   0,
						}
					}
				}
			}
		}
	}

	total := len(allRepos)

	// Send final collecting update with total count before starting sync
	if total > 0 {
		s.syncProgressChan <- profileSyncProgressUpdate{
			status:  "collecting",
			current: total,
			total:   total,
		}
	}

	// Parallel sync with worker pool (4 concurrent workers)
	const numWorkers = 4

	// Create job and result channels
	jobs := make(chan int, len(allRepos))
	results := make(chan struct {
		status string
		idx    int
	}, len(allRepos))

	// Track completed count for progress
	var completedCount int32

	// Start workers
	for w := 0; w < numWorkers; w++ {
		go func() {
			for idx := range jobs {
				r := allRepos[idx]
				repoName := fmt.Sprintf("%s/%s", r.owner, r.repo)

				// Send progress update before starting this repo
				s.syncProgressChan <- profileSyncProgressUpdate{
					repoName: repoName,
					status:   "syncing",
					current:  int(completedCount) + 1,
					total:    total,
				}

				opts := &sync.Options{
					Target:               r.profile.TargetDir,
					IncludePrivate:       r.profile.IncludePrivate,
					Include:              r.profile.IncludeFilter,
					Exclude:              r.profile.ExcludeFilter,
					SkipArchiveDetection: true, // TUI syncs per-repo; archive detection would walk entire dir per repo
				}

				syncer := sync.New(client, gitOps, opts)

				// Construct gh.Repository from our cached data to avoid redundant API call
				// This enables fast sync optimization (comparing local/remote HEAD SHA)
				repo := &gh.Repository{
					Name:          gh.Ptr(r.repo),
					DefaultBranch: gh.Ptr(r.defaultBranch),
					Owner:         &gh.User{Login: gh.Ptr(r.owner)},
					CloneURL:      gh.Ptr(r.cloneURL),
					Private:       gh.Ptr(r.isPrivate),
					PushedAt:      r.pushedAt,
				}
				result, err := syncer.SyncRepoWithData(s.ctx, repo)

				status := "skipped"
				if err != nil {
					status = "failed"
				} else if result != nil {
					if len(result.Failed) > 0 {
						status = "failed"
					} else if len(result.Cloned) > 0 {
						status = "cloned"
					} else if len(result.Updated) > 0 {
						status = "updated"
					} else if len(result.UpToDate) > 0 {
						status = "up-to-date"
					} else if len(result.Archived) > 0 {
						status = "archived"
					} else if len(result.Skipped) > 0 {
						status = "skipped"
					} else {
						status = "skipped"
					}
				}

				results <- struct {
					status string
					idx    int
				}{status: status, idx: idx}
			}
		}()
	}

	// Send all jobs
	for i := range allRepos {
		jobs <- i
	}
	close(jobs)

	// Collect results
	for i := 0; i < len(allRepos); i++ {
		res := <-results
		completedCount++
		r := allRepos[res.idx]
		repoName := fmt.Sprintf("%s/%s", r.owner, r.repo)

		switch res.status {
		case "cloned":
			cloned++
		case "updated":
			updated++
		case "up-to-date":
			// up-to-date repos don't need a separate counter in the final msg
			// they're counted as part of the overall "done" repos
		case "skipped":
			skipped++
		case "failed":
			failed++
		case "archived":
			archived++
		}

		// Send completion update
		s.syncProgressChan <- profileSyncProgressUpdate{
			repoName: repoName,
			status:   res.status,
			current:  int(completedCount),
			total:    total,
		}
	}

	// Update profile last sync times
	for _, profile := range s.profiles {
		if s.app.Storage() != nil {
			profile.LastSyncAt = time.Now()
			s.app.Storage().UpdateProfile(profile)
		}
	}

	// Save storage once at the end
	if s.app.Storage() != nil {
		s.app.Storage().Save()
	}

	// Send completion through the same channel to preserve message ordering
	// (avoids race condition where done message could be received before all progress messages)
	s.syncProgressChan <- profileSyncProgressUpdate{status: "complete"}
}

// waitForSyncProgress returns a command that waits for the next progress update
func (s *SyncProgressScreen) waitForSyncProgress() tea.Cmd {
	return func() tea.Msg {
		update, ok := <-s.syncProgressChan
		if !ok {
			// Channel closed unexpectedly
			return profileSyncProgressMsg{update: profileSyncProgressUpdate{status: "complete"}}
		}
		return profileSyncProgressMsg{update: update}
	}
}

// humanizeDuration converts a duration to a human-friendly string
func humanizeDuration(d time.Duration) string {
	if d < time.Second {
		return "less than a second"
	}
	if d < time.Minute {
		secs := int(d.Seconds())
		if secs == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", secs)
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours == 1 {
		if mins == 0 {
			return "1 hour"
		}
		return fmt.Sprintf("1 hour %d min", mins)
	}
	if mins == 0 {
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%d hours %d min", hours, mins)
}

// Message types
type profileSyncProgressUpdate struct {
	repoName string
	status   string // "collecting", "syncing", "cloned", "updated", "up-to-date", "skipped", "failed", "complete"
	current  int
	total    int
	err      error // Only set when status="complete" and there was an error
}

type profileSyncProgressMsg struct {
	update profileSyncProgressUpdate
}
