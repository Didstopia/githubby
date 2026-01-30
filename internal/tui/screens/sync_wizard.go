package screens

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	gh "github.com/google/go-github/v68/github"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/sync"
	"github.com/Didstopia/githubby/internal/tui"
)

// SyncWizardStep represents the current step in the sync wizard
type SyncWizardStep int

const (
	WizardStepSelectSource SyncWizardStep = iota
	WizardStepLoadingOrgs
	WizardStepSelectOrg
	WizardStepFetchRepos
	WizardStepRepoMode     // Choose: sync all or select specific
	WizardStepSelectRepos
	WizardStepSetTarget
	WizardStepConfirm
	WizardStepExecute
	WizardStepComplete
)

// WizardRepoItem represents a repository in the selection list
type WizardRepoItem struct {
	repo     *gh.Repository
	selected bool
}

func (r WizardRepoItem) Title() string {
	name := r.repo.GetFullName()
	if r.selected {
		return "[x] " + name
	}
	return "[ ] " + name
}

func (r WizardRepoItem) Description() string {
	parts := []string{}
	if r.repo.GetPrivate() {
		parts = append(parts, "private")
	}
	if lang := r.repo.GetLanguage(); lang != "" {
		parts = append(parts, lang)
	}
	if stars := r.repo.GetStargazersCount(); stars > 0 {
		parts = append(parts, fmt.Sprintf("%d stars", stars))
	}
	if len(parts) == 0 {
		return "public"
	}
	return strings.Join(parts, " | ")
}

func (r WizardRepoItem) FilterValue() string {
	return r.repo.GetFullName()
}

// SyncWizard is the multi-step sync configuration wizard
type SyncWizard struct {
	ctx    context.Context
	app    *tui.App
	styles *tui.Styles
	keys   tui.KeyMap

	// Current step
	step SyncWizardStep

	// Source selection
	sourceType     string // "user" or "org"
	sourceName     string
	includePrivate bool
	sourceForm     *huh.Form
	orgSelectForm  *huh.Form
	privateForm    *huh.Form

	// Organizations (for org selection)
	userOrgs    []*gh.Organization
	selectedOrg string

	// Repo selection mode
	selectAllRepos  bool   // true = sync all, false = select specific
	repoModeForm    *huh.Form

	// Repositories
	allRepos      []*gh.Repository
	repoItems     []WizardRepoItem
	repoList      list.Model
	selectedRepos []*gh.Repository

	// Target directory
	targetInput textinput.Model
	targetDir   string

	// Profile options
	profileName   string
	saveAsProfile bool
	confirmForm   *huh.Form

	// Execution state
	syncProgress  progress.Model
	syncSpinner   spinner.Model
	syncStatus    string
	syncCurrent   int
	syncTotal     int
	syncResults   []*state.RepoSyncResult
	syncRecord    *state.SyncRecord
	syncResult    *sync.Result
	syncError     error

	// Channel for async sync progress
	syncProgressChan chan syncProgressUpdate
	syncDoneChan     chan syncDoneUpdate

	// Channels for async loading operations
	orgsChan  chan orgsResult
	reposChan chan reposResult

	// Dimensions
	width  int
	height int

	// State
	loading bool
	err     error

	// Exit confirmation
	exitPending bool
	exitKey     string
}

// NewSyncWizard creates a new sync wizard screen
func NewSyncWizard(ctx context.Context, app *tui.App) *SyncWizard {
	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Moon
	s.Style = tui.GetStyles().Spinner

	// Initialize progress bar
	p := progress.New(progress.WithDefaultGradient())

	// Initialize target input
	ti := textinput.New()
	ti.Placeholder = "~/repos"
	ti.Width = 50

	// Set default target from storage or fall back to home/repos
	defaultTarget := ""
	if app.Storage() != nil {
		defaultTarget = app.Storage().GetDefaultTargetDir()
	}
	if defaultTarget == "" {
		homeDir, _ := os.UserHomeDir()
		defaultTarget = filepath.Join(homeDir, "repos")
	}
	ti.SetValue(defaultTarget)

	w := &SyncWizard{
		ctx:          ctx,
		app:          app,
		styles:       tui.GetStyles(),
		keys:         tui.GetKeyMap(),
		step:         WizardStepSelectSource,
		syncSpinner:  s,
		syncProgress: p,
		targetInput:  ti,
		repoItems:    make([]WizardRepoItem, 0),
		syncResults:  make([]*state.RepoSyncResult, 0),
		width:        80,
		height:       24,
	}

	w.initSourceForm()
	return w
}

// initSourceForm initializes the source type selection form
func (w *SyncWizard) initSourceForm() {
	w.sourceForm = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What would you like to sync?").
				Options(
					huh.NewOption("My Repositories", "user"),
					huh.NewOption("Organization Repositories", "org"),
				).
				Value(&w.sourceType),
		),
	).WithTheme(huh.ThemeCharm())
}

// initOrgSelectForm initializes the organization selection form
func (w *SyncWizard) initOrgSelectForm() {
	options := make([]huh.Option[string], len(w.userOrgs))
	for i, org := range w.userOrgs {
		options[i] = huh.NewOption(org.GetLogin(), org.GetLogin())
	}

	w.orgSelectForm = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an organization").
				Options(options...).
				Value(&w.selectedOrg),
		),
	).WithTheme(huh.ThemeCharm())
}

// initPrivateForm initializes the private repos toggle form
func (w *SyncWizard) initPrivateForm() {
	// Default to including private repos
	w.includePrivate = true

	w.privateForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Include private repositories?").
				Affirmative("Yes").
				Negative("No").
				Value(&w.includePrivate),
		),
	).WithTheme(huh.ThemeCharm())
}

// initRepoModeForm initializes the repo selection mode form
func (w *SyncWizard) initRepoModeForm() {
	// Default to syncing all repos
	w.selectAllRepos = true

	w.repoModeForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Sync all %d repositories?", len(w.allRepos))).
				Description("Choose 'No' to select specific repositories").
				Affirmative("Yes, sync all").
				Negative("No, let me choose").
				Value(&w.selectAllRepos),
		),
	).WithTheme(huh.ThemeCharm())
}

// initConfirmForm initializes the confirmation form
func (w *SyncWizard) initConfirmForm() {
	// Generate default profile name
	if w.profileName == "" {
		w.profileName = fmt.Sprintf("%s %s", cases.Title(language.English).String(w.sourceType), w.sourceName)
	}

	// Default to saving the profile
	w.saveAsProfile = true

	w.confirmForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Save as a sync profile?").
				Description("Save this configuration for quick syncing later").
				Affirmative("Yes (Recommended)").
				Negative("No").
				Value(&w.saveAsProfile),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Profile name").
				Value(&w.profileName),
		).WithHideFunc(func() bool {
			return !w.saveAsProfile
		}),
	).WithTheme(huh.ThemeCharm())
}

// initRepoList initializes the repository selection list
func (w *SyncWizard) initRepoList() {
	items := make([]list.Item, len(w.repoItems))
	for i, item := range w.repoItems {
		items[i] = item
	}

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = w.styles.MenuItemSelected
	delegate.Styles.SelectedDesc = w.styles.Muted
	delegate.Styles.NormalTitle = w.styles.MenuItem
	delegate.Styles.NormalDesc = w.styles.Muted

	l := list.New(items, delegate, 60, 15)
	l.Title = "Select repositories to sync"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.Styles.Title = w.styles.ListTitle

	w.repoList = l
}

// Title returns the screen title
func (w *SyncWizard) Title() string {
	switch w.step {
	case WizardStepSelectSource:
		return "New Sync - Source"
	case WizardStepLoadingOrgs:
		return "New Sync - Loading Orgs"
	case WizardStepSelectOrg:
		return "New Sync - Select Org"
	case WizardStepFetchRepos:
		return "New Sync - Loading Repos"
	case WizardStepRepoMode:
		return "New Sync - Repos"
	case WizardStepSelectRepos:
		return "New Sync - Select Repos"
	case WizardStepSetTarget:
		return "New Sync - Target"
	case WizardStepConfirm:
		return "New Sync - Confirm"
	case WizardStepExecute:
		return "Syncing..."
	case WizardStepComplete:
		return "Sync Complete"
	}
	return "New Sync"
}

// ShortHelp returns key bindings for the footer
func (w *SyncWizard) ShortHelp() []key.Binding {
	switch w.step {
	case WizardStepSelectRepos:
		return []key.Binding{
			key.NewBinding(key.WithKeys("space", "x"), key.WithHelp("space/x", "toggle")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
			key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "select none")),
			w.keys.Select,
			w.keys.Back,
		}
	case WizardStepExecute:
		return []key.Binding{}
	default:
		return []key.Binding{
			w.keys.Select,
			w.keys.Back,
		}
	}
}

// Init initializes the sync wizard
func (w *SyncWizard) Init() tea.Cmd {
	return tea.Batch(
		w.sourceForm.Init(),
		w.syncSpinner.Tick,
	)
}

// Update handles messages
func (w *SyncWizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width = msg.Width
		w.height = msg.Height
		w.syncProgress.Width = msg.Width - 20
		if w.step == WizardStepSelectRepos {
			w.repoList.SetWidth(msg.Width - 8)
			w.repoList.SetHeight(msg.Height - 12)
		}

	case tea.KeyMsg:
		// Handle Ctrl+C
		if msg.Type == tea.KeyCtrlC {
			if w.exitPending && w.exitKey == "ctrl+c" {
				return w, tui.PopScreenCmd()
			}
			w.exitPending = true
			w.exitKey = "ctrl+c"
			return w, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)
		}

		switch {
		case key.Matches(msg, w.keys.Back):
			w.exitPending = false
			if w.step > WizardStepSelectSource && w.step < WizardStepExecute {
				w.step--
				return w, w.initCurrentStep()
			}
			// Return to dashboard
			return w, tui.PopScreenCmd()

		case key.Matches(msg, w.keys.Quit):
			if w.step == WizardStepExecute {
				// Don't allow quit during execution
				return w, nil
			}
			if w.exitPending && w.exitKey == "q" {
				return w, tui.PopScreenCmd()
			}
			w.exitPending = true
			w.exitKey = "q"
			return w, tui.ExitTimeoutCmd(tui.ExitConfirmTimeout)
		}

		if w.exitPending && msg.Type != tea.KeyCtrlC {
			w.exitPending = false
		}

		// Step-specific key handling
		switch w.step {
		case WizardStepSelectRepos:
			switch {
			case msg.String() == " " || msg.String() == "x":
				// Toggle selection
				if idx := w.repoList.Index(); idx >= 0 && idx < len(w.repoItems) {
					w.repoItems[idx].selected = !w.repoItems[idx].selected
					w.updateRepoList()
				}
			case msg.String() == "a":
				// Select all
				for i := range w.repoItems {
					w.repoItems[i].selected = true
				}
				w.updateRepoList()
			case msg.String() == "n":
				// Select none
				for i := range w.repoItems {
					w.repoItems[i].selected = false
				}
				w.updateRepoList()
			case key.Matches(msg, w.keys.Select):
				// Confirm selection
				w.selectedRepos = w.getSelectedRepos()
				if len(w.selectedRepos) == 0 {
					w.err = fmt.Errorf("please select at least one repository")
					return w, nil
				}
				w.step = WizardStepSetTarget
				w.targetInput.Focus()
				return w, textinput.Blink
			}

		case WizardStepSetTarget:
			if key.Matches(msg, w.keys.Select) {
				w.targetDir = w.targetInput.Value()
				if w.targetDir == "" {
					w.err = fmt.Errorf("please enter a target directory")
					return w, nil
				}
				// Expand ~ to home directory
				if strings.HasPrefix(w.targetDir, "~") {
					homeDir, _ := os.UserHomeDir()
					w.targetDir = filepath.Join(homeDir, w.targetDir[1:])
				}
				w.step = WizardStepConfirm
				w.initConfirmForm()
				return w, w.confirmForm.Init()
			}

		case WizardStepComplete:
			if key.Matches(msg, w.keys.Select) {
				// Return to dashboard and refresh
				return w, tea.Batch(
					tui.PopScreenCmd(),
					tui.RefreshDashboardCmd(),
				)
			}
		}

	case tui.ExitTimeoutMsg:
		w.exitPending = false

	case spinner.TickMsg:
		if w.step == WizardStepLoadingOrgs || w.step == WizardStepFetchRepos || w.step == WizardStepExecute {
			var cmd tea.Cmd
			w.syncSpinner, cmd = w.syncSpinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case progress.FrameMsg:
		if w.step == WizardStepExecute {
			model, cmd := w.syncProgress.Update(msg)
			w.syncProgress = model.(progress.Model)
			cmds = append(cmds, cmd)
		}

	case tui.OrgsLoadedMsg:
		w.loading = false
		if msg.Error != nil {
			w.err = msg.Error
			w.step = WizardStepSelectSource
			return w, w.sourceForm.Init()
		}
		w.userOrgs = msg.Orgs
		if len(w.userOrgs) == 0 {
			w.err = fmt.Errorf("you don't belong to any organizations")
			w.step = WizardStepSelectSource
			return w, w.sourceForm.Init()
		}
		w.step = WizardStepSelectOrg
		w.initOrgSelectForm()
		return w, w.orgSelectForm.Init()

	case tui.ReposLoadedMsg:
		if msg.Error != nil {
			w.err = msg.Error
			w.loading = false
			w.step = WizardStepSelectSource
			return w, nil
		}
		w.allRepos = msg.Repos
		w.repoItems = make([]WizardRepoItem, len(msg.Repos))
		for i, repo := range msg.Repos {
			w.repoItems[i] = WizardRepoItem{repo: repo, selected: true} // Default select all
		}
		w.loading = false
		// Go to repo mode selection (all vs specific)
		w.step = WizardStepRepoMode
		w.initRepoModeForm()
		return w, w.repoModeForm.Init()

	case syncProgressMsg:
		w.syncCurrent = msg.update.current
		w.syncTotal = msg.update.total
		w.syncStatus = fmt.Sprintf("[%d/%d] %s: %s", msg.update.current, msg.update.total, msg.update.repoName, msg.update.status)

		// Update progress bar
		if msg.update.total > 0 {
			cmds = append(cmds, w.syncProgress.SetPercent(float64(msg.update.current)/float64(msg.update.total)))
		}

		// Continue listening for more progress updates
		cmds = append(cmds, w.waitForSyncProgress())

	case syncDoneMsg:
		w.syncResult = msg.update.result
		w.syncRecord = msg.update.record
		w.syncError = msg.update.err
		w.step = WizardStepComplete

		// Copy results from record for display
		if w.syncRecord != nil {
			w.syncResults = w.syncRecord.Results
		}

		// Save sync record if we have storage
		if w.app.Storage() != nil && w.syncRecord != nil {
			_ = w.app.Storage().AddSyncRecord(w.syncRecord)

			// Save profile if requested
			if w.saveAsProfile {
				profile := state.NewProfile(
					w.profileName,
					w.sourceType,
					w.sourceName,
					w.targetDir,
					w.includePrivate,
				)
				// Set sync mode based on user's choice
				profile.SyncAllRepos = w.selectAllRepos
				if !w.selectAllRepos {
					// Only store specific repos when not syncing all
					repoNames := make([]string, len(w.selectedRepos))
					for i, r := range w.selectedRepos {
						repoNames[i] = r.GetFullName()
					}
					profile.SelectedRepos = repoNames
				}
				if !w.syncRecord.CompletedAt.IsZero() {
					profile.LastSyncAt = w.syncRecord.CompletedAt
				}
				_ = w.app.Storage().AddProfile(profile)
			}
		}
	}

	// Update step-specific components
	switch w.step {
	case WizardStepSelectSource:
		// If private form is active (source already selected), handle it first
		if w.privateForm != nil {
			pform, pcmd := w.privateForm.Update(msg)
			if pf, ok := pform.(*huh.Form); ok {
				w.privateForm = pf
				cmds = append(cmds, pcmd)
				if pf.State == huh.StateCompleted {
					w.step = WizardStepFetchRepos
					w.loading = true
					return w, w.fetchRepos()
				}
			}
			return w, tea.Batch(cmds...)
		}

		// Handle source type form
		form, cmd := w.sourceForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			w.sourceForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				if w.sourceType == "user" {
					// For user repos, use authenticated username directly
					w.sourceName = w.app.Username()
					// Go straight to private repos question
					w.initPrivateForm()
					return w, w.privateForm.Init()
				} else {
					// For org repos, need to fetch and select org
					w.step = WizardStepLoadingOrgs
					w.loading = true
					return w, w.fetchOrgs()
				}
			}
		}

	case WizardStepSelectOrg:
		// If private form is active (org already selected), handle it first
		if w.privateForm != nil {
			pform, pcmd := w.privateForm.Update(msg)
			if pf, ok := pform.(*huh.Form); ok {
				w.privateForm = pf
				cmds = append(cmds, pcmd)
				if pf.State == huh.StateCompleted {
					w.step = WizardStepFetchRepos
					w.loading = true
					return w, w.fetchRepos()
				}
			}
			return w, tea.Batch(cmds...)
		}

		// Handle org select form
		form, cmd := w.orgSelectForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			w.orgSelectForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				w.sourceName = w.selectedOrg
				// Now ask about private repos
				w.initPrivateForm()
				return w, w.privateForm.Init()
			}
		}

	case WizardStepRepoMode:
		form, cmd := w.repoModeForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			w.repoModeForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				if w.selectAllRepos {
					// Select all repos and skip to target
					w.selectedRepos = w.allRepos
					w.step = WizardStepSetTarget
					w.targetInput.Focus()
					return w, textinput.Blink
				} else {
					// Show repo selection list
					w.initRepoList()
					w.step = WizardStepSelectRepos
					return w, nil
				}
			}
		}

	case WizardStepSelectRepos:
		var cmd tea.Cmd
		w.repoList, cmd = w.repoList.Update(msg)
		cmds = append(cmds, cmd)

	case WizardStepSetTarget:
		var cmd tea.Cmd
		w.targetInput, cmd = w.targetInput.Update(msg)
		cmds = append(cmds, cmd)

	case WizardStepConfirm:
		form, cmd := w.confirmForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			w.confirmForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				w.step = WizardStepExecute
				return w, w.startSync()
			}
		}
	}

	return w, tea.Batch(cmds...)
}

// initCurrentStep initializes the current step
func (w *SyncWizard) initCurrentStep() tea.Cmd {
	switch w.step {
	case WizardStepSelectSource:
		w.privateForm = nil // Reset private form
		w.initSourceForm()
		return w.sourceForm.Init()
	case WizardStepSelectOrg:
		w.initOrgSelectForm()
		return w.orgSelectForm.Init()
	case WizardStepSelectRepos:
		w.initRepoList()
		return nil
	case WizardStepSetTarget:
		w.targetInput.Focus()
		return textinput.Blink
	case WizardStepConfirm:
		w.initConfirmForm()
		return w.confirmForm.Init()
	}
	return nil
}

// fetchOrgs fetches the user's organizations from GitHub asynchronously
func (w *SyncWizard) fetchOrgs() tea.Cmd {
	w.orgsChan = make(chan orgsResult, 1)

	// Start fetch in background goroutine
	go func() {
		defer close(w.orgsChan)

		client := w.app.GitHubClient()
		if client == nil {
			w.orgsChan <- orgsResult{err: fmt.Errorf("not authenticated")}
			return
		}

		orgs, err := client.ListUserOrgs(w.ctx)
		w.orgsChan <- orgsResult{orgs: orgs, err: err}
	}()

	// Return batch with spinner tick to keep UI responsive
	return tea.Batch(
		w.syncSpinner.Tick,
		w.waitForOrgs(),
	)
}

// waitForOrgs waits for organizations to be fetched
func (w *SyncWizard) waitForOrgs() tea.Cmd {
	return func() tea.Msg {
		result := <-w.orgsChan
		if result.err != nil {
			return tui.OrgsLoadedMsg{Error: result.err}
		}
		return tui.OrgsLoadedMsg{Orgs: result.orgs}
	}
}

// fetchRepos fetches repositories from GitHub asynchronously
func (w *SyncWizard) fetchRepos() tea.Cmd {
	w.reposChan = make(chan reposResult, 1)

	// Start fetch in background goroutine
	go func() {
		defer close(w.reposChan)

		client := w.app.GitHubClient()
		if client == nil {
			w.reposChan <- reposResult{err: fmt.Errorf("not authenticated")}
			return
		}

		opts := &github.ListOptions{
			IncludePrivate: w.includePrivate,
		}

		var repos []*gh.Repository
		var err error

		if w.sourceType == "org" {
			repos, err = client.ListOrgRepos(w.ctx, w.sourceName, opts)
		} else {
			repos, err = client.ListUserRepos(w.ctx, w.sourceName, opts)
		}

		w.reposChan <- reposResult{repos: repos, err: err}
	}()

	// Return batch with spinner tick to keep UI responsive
	return tea.Batch(
		w.syncSpinner.Tick,
		w.waitForRepos(),
	)
}

// waitForRepos waits for repositories to be fetched
func (w *SyncWizard) waitForRepos() tea.Cmd {
	return func() tea.Msg {
		result := <-w.reposChan
		if result.err != nil {
			return tui.ReposLoadedMsg{Error: result.err}
		}
		return tui.ReposLoadedMsg{Repos: result.repos}
	}
}

// startSync starts the sync operation in a background goroutine
func (w *SyncWizard) startSync() tea.Cmd {
	// Initialize channels
	w.syncProgressChan = make(chan syncProgressUpdate, 1)
	w.syncDoneChan = make(chan syncDoneUpdate, 1)

	// Start sync in background goroutine
	go w.runSyncInBackground()

	// Return command to listen for first progress update
	return tea.Batch(
		w.syncSpinner.Tick,
		w.waitForSyncProgress(),
	)
}

// runSyncInBackground performs the actual sync operation
func (w *SyncWizard) runSyncInBackground() {
	defer close(w.syncProgressChan)
	defer close(w.syncDoneChan)

	client := w.app.GitHubClient()
	if client == nil {
		w.syncDoneChan <- syncDoneUpdate{err: fmt.Errorf("not authenticated")}
		return
	}

	// Create sync record
	record := state.NewSyncRecord("", w.profileName)

	// Use quiet git mode with authentication token
	gitOps, err := git.NewQuietWithToken(w.app.Token())
	if err != nil {
		w.syncDoneChan <- syncDoneUpdate{err: fmt.Errorf("git not available: %w", err)}
		return
	}

	opts := &sync.Options{
		Target:         w.targetDir,
		IncludePrivate: w.includePrivate,
	}

	syncer := sync.New(client, gitOps, opts)
	total := len(w.selectedRepos)
	results := make([]*state.RepoSyncResult, 0, total)
	finalResult := sync.NewResult()

	for i, repo := range w.selectedRepos {
		repoName := repo.GetFullName()

		// Send progress update before starting this repo
		w.syncProgressChan <- syncProgressUpdate{
			current:  i + 1,
			total:    total,
			repoName: repoName,
			status:   "syncing",
		}

		result, err := syncer.SyncRepo(w.ctx, repo.GetOwner().GetLogin(), repo.GetName())

		repoResult := &state.RepoSyncResult{
			FullName: repoName,
			SyncedAt: time.Now(),
		}

		status := "skipped"
		errMsg := ""

		if err != nil {
			repoResult.Status = "failed"
			repoResult.Error = err.Error()
			finalResult.Failed[repoName] = err
			status = "failed"
			errMsg = err.Error()
		} else if result != nil {
			if len(result.Failed) > 0 {
				for name, repoErr := range result.Failed {
					repoResult.Status = "failed"
					repoResult.Error = repoErr.Error()
					finalResult.Failed[name] = repoErr
					status = "failed"
					errMsg = repoErr.Error()
				}
			} else if len(result.Cloned) > 0 {
				repoResult.Status = "cloned"
				finalResult.Cloned = append(finalResult.Cloned, result.Cloned...)
				status = "cloned"
			} else if len(result.Updated) > 0 {
				repoResult.Status = "updated"
				finalResult.Updated = append(finalResult.Updated, result.Updated...)
				status = "updated"
			} else if len(result.Skipped) > 0 {
				repoResult.Status = "skipped"
				finalResult.Skipped = append(finalResult.Skipped, result.Skipped...)
				status = "skipped"
			}
		}

		results = append(results, repoResult)

		// Send completion update for this repo
		w.syncProgressChan <- syncProgressUpdate{
			current:  i + 1,
			total:    total,
			repoName: repoName,
			status:   status,
			err:      errMsg,
		}
	}

	// Store results for display
	record.Results = results
	record.TotalRepos = total
	record.Complete()

	w.syncDoneChan <- syncDoneUpdate{
		result: finalResult,
		record: record,
	}
}

// waitForSyncProgress returns a command that waits for the next progress update
func (w *SyncWizard) waitForSyncProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case update, ok := <-w.syncProgressChan:
			if !ok {
				// Channel closed, check done channel
				if done, ok := <-w.syncDoneChan; ok {
					return syncDoneMsg{update: done}
				}
				return nil
			}
			return syncProgressMsg{update: update}
		case done, ok := <-w.syncDoneChan:
			if ok {
				return syncDoneMsg{update: done}
			}
			return nil
		}
	}
}

// updateRepoList updates the repo list with current selection state
func (w *SyncWizard) updateRepoList() {
	items := make([]list.Item, len(w.repoItems))
	for i, item := range w.repoItems {
		items[i] = item
	}
	w.repoList.SetItems(items)
}

// getSelectedRepos returns the selected repositories
func (w *SyncWizard) getSelectedRepos() []*gh.Repository {
	selected := make([]*gh.Repository, 0)
	for _, item := range w.repoItems {
		if item.selected {
			selected = append(selected, item.repo)
		}
	}
	return selected
}

// View renders the sync wizard screen
func (w *SyncWizard) View() string {
	var content strings.Builder

	switch w.step {
	case WizardStepSelectSource:
		content.WriteString(w.viewSelectSource())
	case WizardStepLoadingOrgs:
		content.WriteString(w.viewLoadingOrgs())
	case WizardStepSelectOrg:
		content.WriteString(w.viewSelectOrg())
	case WizardStepFetchRepos:
		content.WriteString(w.viewFetchRepos())
	case WizardStepRepoMode:
		content.WriteString(w.viewRepoMode())
	case WizardStepSelectRepos:
		content.WriteString(w.viewSelectRepos())
	case WizardStepSetTarget:
		content.WriteString(w.viewSetTarget())
	case WizardStepConfirm:
		content.WriteString(w.viewConfirm())
	case WizardStepExecute:
		content.WriteString(w.viewExecute())
	case WizardStepComplete:
		content.WriteString(w.viewComplete())
	}

	// Show error if any
	if w.err != nil {
		content.WriteString("\n\n")
		content.WriteString(w.styles.Error.Render("Error: " + w.err.Error()))
	}

	// Exit confirmation
	if w.exitPending {
		var msg string
		switch w.exitKey {
		case "ctrl+c":
			msg = "Press Ctrl+C again to cancel"
		case "q":
			msg = "Press q again to cancel"
		}
		content.WriteString("\n\n")
		content.WriteString(w.styles.Warning.Render(msg))
	}

	return w.styles.Content.Render(content.String())
}

func (w *SyncWizard) viewSelectSource() string {
	title := w.styles.FormTitle.Render("Step 1: Select Source")

	// If we've selected source type and now showing private form
	if w.privateForm != nil && w.sourceForm.State == huh.StateCompleted {
		sourceInfo := w.styles.Success.Render(fmt.Sprintf("Syncing: %s (%s)", w.sourceName, w.sourceType))
		return lipgloss.JoinVertical(lipgloss.Left, title, "", sourceInfo, "", w.privateForm.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, "", w.sourceForm.View())
}

func (w *SyncWizard) viewLoadingOrgs() string {
	title := w.styles.FormTitle.Render("Step 1: Select Source")
	loading := w.syncSpinner.View() + " Loading your organizations..."
	return lipgloss.JoinVertical(lipgloss.Left, title, "", loading)
}

func (w *SyncWizard) viewSelectOrg() string {
	title := w.styles.FormTitle.Render("Step 1: Select Source")

	// If we've selected org and now showing private form
	if w.privateForm != nil && w.orgSelectForm != nil && w.orgSelectForm.State == huh.StateCompleted {
		sourceInfo := w.styles.Success.Render(fmt.Sprintf("Syncing: %s (organization)", w.sourceName))
		return lipgloss.JoinVertical(lipgloss.Left, title, "", sourceInfo, "", w.privateForm.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, "", w.orgSelectForm.View())
}

func (w *SyncWizard) viewFetchRepos() string {
	title := w.styles.FormTitle.Render("Step 2: Loading Repositories")
	loading := w.syncSpinner.View() + " Fetching repositories from " + w.sourceName + "..."
	return lipgloss.JoinVertical(lipgloss.Left, title, "", loading)
}

func (w *SyncWizard) viewRepoMode() string {
	title := w.styles.FormTitle.Render("Step 2: Repository Selection")
	sourceInfo := w.styles.Success.Render(fmt.Sprintf("Found %d repositories in %s", len(w.allRepos), w.sourceName))
	return lipgloss.JoinVertical(lipgloss.Left, title, "", sourceInfo, "", w.repoModeForm.View())
}

func (w *SyncWizard) viewSelectRepos() string {
	title := w.styles.FormTitle.Render("Step 3: Select Repositories")
	count := fmt.Sprintf("%d of %d selected", len(w.getSelectedRepos()), len(w.repoItems))
	return lipgloss.JoinVertical(lipgloss.Left, title, w.styles.Muted.Render(count), "", w.repoList.View())
}

func (w *SyncWizard) viewSetTarget() string {
	title := w.styles.FormTitle.Render("Step 4: Set Target Directory")
	prompt := "Where should repositories be cloned?"
	input := w.targetInput.View()
	preview := w.styles.Muted.Render(fmt.Sprintf("Repos will be cloned to: %s/<owner>/<repo>", w.targetInput.Value()))
	return lipgloss.JoinVertical(lipgloss.Left, title, "", prompt, "", input, "", preview)
}

func (w *SyncWizard) viewConfirm() string {
	title := w.styles.FormTitle.Render("Step 5: Confirm")

	var summary strings.Builder
	summary.WriteString(w.styles.Info.Render("Summary:"))
	summary.WriteString("\n")
	summary.WriteString(fmt.Sprintf("  Source: %s/%s\n", w.sourceType, w.sourceName))
	if w.selectAllRepos {
		summary.WriteString(fmt.Sprintf("  Repositories: All (%d repos, auto-updates with new repos)\n", len(w.selectedRepos)))
	} else {
		summary.WriteString(fmt.Sprintf("  Repositories: %d selected\n", len(w.selectedRepos)))
	}
	summary.WriteString(fmt.Sprintf("  Target: %s\n", w.targetDir))
	summary.WriteString(fmt.Sprintf("  Private repos: %v\n", w.includePrivate))

	return lipgloss.JoinVertical(lipgloss.Left, title, "", summary.String(), "", w.confirmForm.View())
}

func (w *SyncWizard) viewExecute() string {
	title := w.styles.FormTitle.Render("Syncing...")

	var content strings.Builder

	// Progress bar
	if w.syncTotal > 0 {
		content.WriteString(w.syncProgress.View())
		content.WriteString("\n\n")
	}

	// Current status with spinner
	content.WriteString(w.syncSpinner.View())
	if w.syncStatus != "" {
		content.WriteString(" ")
		content.WriteString(w.syncStatus)
	} else {
		content.WriteString(" Starting sync...")
	}
	content.WriteString("\n\n")

	// Overall progress
	if w.syncTotal > 0 {
		content.WriteString(w.styles.Info.Render(fmt.Sprintf("Progress: %d / %d repositories", w.syncCurrent, w.syncTotal)))
		content.WriteString("\n")
	}

	content.WriteString(w.styles.Muted.Render(fmt.Sprintf("Target: %s", w.targetDir)))

	return lipgloss.JoinVertical(lipgloss.Left, title, "", content.String())
}

func (w *SyncWizard) viewComplete() string {
	title := w.styles.FormTitle.Render("Sync Complete!")

	var content strings.Builder

	// Get counts from sync result
	cloned, updated, skipped, failed, archived := 0, 0, 0, 0, 0
	if w.syncResult != nil {
		cloned = len(w.syncResult.Cloned)
		updated = len(w.syncResult.Updated)
		skipped = len(w.syncResult.Skipped)
		failed = len(w.syncResult.Failed)
		archived = len(w.syncResult.Archived)
	}

	total := cloned + updated + skipped + failed
	if w.syncError != nil {
		content.WriteString(w.styles.Error.Render("Sync completed with errors: " + w.syncError.Error()))
	} else if failed > 0 {
		content.WriteString(w.styles.Warning.Render(fmt.Sprintf("Synced %d repositories with %d failures", total, failed)))
	} else {
		content.WriteString(w.styles.Success.Render(fmt.Sprintf("Successfully synced %d repositories!", total)))
	}

	content.WriteString("\n\n")
	content.WriteString(w.styles.Info.Render("Results:"))
	content.WriteString("\n")

	if cloned > 0 {
		content.WriteString(fmt.Sprintf("  %s Cloned: %d\n", w.styles.Success.Render("●"), cloned))
	}
	if updated > 0 {
		content.WriteString(fmt.Sprintf("  %s Updated: %d\n", w.styles.Success.Render("●"), updated))
	}
	if skipped > 0 {
		content.WriteString(fmt.Sprintf("  %s Skipped: %d\n", w.styles.Warning.Render("●"), skipped))
	}
	if failed > 0 {
		content.WriteString(fmt.Sprintf("  %s Failed: %d\n", w.styles.Error.Render("●"), failed))
	}
	if archived > 0 {
		content.WriteString(fmt.Sprintf("  %s Archived: %d (preserved locally, no longer on remote)\n", w.styles.Info.Render("●"), archived))
	}

	if cloned == 0 && updated == 0 && skipped == 0 && failed == 0 && archived == 0 {
		content.WriteString(w.styles.Muted.Render("  No changes - all repositories up to date\n"))
	}

	if w.saveAsProfile {
		content.WriteString("\n")
		content.WriteString(w.styles.Success.Render(fmt.Sprintf("Profile '%s' saved!", w.profileName)))
	}

	content.WriteString("\n\n")
	content.WriteString("Press " + w.styles.HelpKey.Render("Enter") + " to return to dashboard")

	return lipgloss.JoinVertical(lipgloss.Left, title, "", content.String())
}

// syncProgressUpdate represents a progress update from the sync goroutine
type syncProgressUpdate struct {
	current  int
	total    int
	repoName string
	status   string
	err      string
}

// syncDoneUpdate represents completion of the sync operation
type syncDoneUpdate struct {
	result *sync.Result
	record *state.SyncRecord
	err    error
}

// syncProgressMsg is sent when we receive a progress update
type syncProgressMsg struct {
	update syncProgressUpdate
}

// syncDoneMsg is sent when sync is complete
type syncDoneMsg struct {
	update syncDoneUpdate
}

// orgsResult represents the result of fetching organizations
type orgsResult struct {
	orgs []*gh.Organization
	err  error
}

// reposResult represents the result of fetching repositories
type reposResult struct {
	repos []*gh.Repository
	err   error
}
