package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
)

// Screen represents a TUI screen type
type Screen int

const (
	ScreenOnboarding Screen = iota
	ScreenDashboard
	ScreenSyncWizard
	ScreenSyncProgress
	ScreenClean
	ScreenSettings
	ScreenConfirmDelete
	// Legacy screens (kept for compatibility during transition)
	ScreenRepos
)

// ScreenModel is the interface that all screens must implement
type ScreenModel interface {
	tea.Model
	// Title returns the screen title for the header
	Title() string
	// ShortHelp returns key bindings for the footer help
	ShortHelp() []key.Binding
}

// ScreenFactory creates a screen with dependencies
type ScreenFactory func(ctx context.Context, app *App) ScreenModel

// App is the main TUI application model
type App struct {
	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Current screen
	currentScreen Screen
	screenStack   []Screen

	// Screen models (lazy initialized)
	screens         map[Screen]ScreenModel
	screenFactories map[Screen]ScreenFactory

	// Dependencies
	ghClient github.Client
	storage  *state.Storage

	// Auth state
	isAuthenticated bool
	username        string
	token           string

	// Selected profile(s) for sync
	selectedProfile  *state.SyncProfile
	profilesToSync   []*state.SyncProfile

	// Profile deletion state
	deleteProfileID   string
	deleteProfileName string

	// Terminal dimensions
	width  int
	height int

	// UI components
	styles *Styles
	keys   KeyMap

	// State
	err      error
	quitting bool

	// Version info
	version string
}

// AppOption configures the App
type AppOption func(*App)

// WithContext sets the context for the App
func WithContext(ctx context.Context) AppOption {
	return func(a *App) {
		a.ctx, a.cancel = context.WithCancel(ctx)
	}
}

// WithAuth sets the authentication state
func WithAuth(authenticated bool, username, token string) AppOption {
	return func(a *App) {
		a.isAuthenticated = authenticated
		a.username = username
		a.token = token
	}
}

// WithGitHubClient sets the GitHub client
func WithGitHubClient(client github.Client) AppOption {
	return func(a *App) {
		a.ghClient = client
	}
}

// WithStorage sets the state storage
func WithStorage(storage *state.Storage) AppOption {
	return func(a *App) {
		a.storage = storage
	}
}

// WithVersion sets the app version for display
func WithVersion(version string) AppOption {
	return func(a *App) {
		a.version = version
	}
}

// NewApp creates a new TUI application
func NewApp(opts ...AppOption) *App {
	ctx, cancel := context.WithCancel(context.Background())

	app := &App{
		ctx:             ctx,
		cancel:          cancel,
		currentScreen:   ScreenOnboarding,
		screenStack:     make([]Screen, 0),
		screens:         make(map[Screen]ScreenModel),
		screenFactories: make(map[Screen]ScreenFactory),
		styles:          DefaultStyles(),
		keys:            DefaultKeyMap(),
		width:           80,
		height:          24,
	}

	for _, opt := range opts {
		opt(app)
	}

	return app
}

// RegisterScreenFactory registers a factory function for lazy screen creation
func (a *App) RegisterScreenFactory(screen Screen, factory ScreenFactory) {
	a.screenFactories[screen] = factory
}

// RegisterScreen registers a pre-created screen model
func (a *App) RegisterScreen(screen Screen, model ScreenModel) {
	a.screens[screen] = model
}

// getOrCreateScreen returns the screen model, creating it if necessary
func (a *App) getOrCreateScreen(screen Screen) ScreenModel {
	if model, ok := a.screens[screen]; ok {
		return model
	}

	if factory, ok := a.screenFactories[screen]; ok {
		model := factory(a.ctx, a)
		a.screens[screen] = model
		return model
	}

	return nil
}

// SetScreen changes the current screen
func (a *App) SetScreen(screen Screen) tea.Cmd {
	a.currentScreen = screen
	if model := a.getOrCreateScreen(screen); model != nil {
		return model.Init()
	}
	return nil
}

// PushScreen pushes current screen to stack and sets new screen
func (a *App) PushScreen(screen Screen) tea.Cmd {
	a.screenStack = append(a.screenStack, a.currentScreen)
	return a.SetScreen(screen)
}

// PopScreen returns to the previous screen
func (a *App) PopScreen() tea.Cmd {
	if len(a.screenStack) == 0 {
		return nil
	}
	prevScreen := a.screenStack[len(a.screenStack)-1]
	a.screenStack = a.screenStack[:len(a.screenStack)-1]
	return a.SetScreen(prevScreen)
}

// ResetToScreen clears the stack and sets a new screen
func (a *App) ResetToScreen(screen Screen) tea.Cmd {
	a.screenStack = make([]Screen, 0)
	return a.SetScreen(screen)
}

// Context returns the app context
func (a *App) Context() context.Context {
	return a.ctx
}

// Styles returns the app styles
func (a *App) Styles() *Styles {
	return a.styles
}

// GitHubClient returns the GitHub client
func (a *App) GitHubClient() github.Client {
	return a.ghClient
}

// Storage returns the state storage
func (a *App) Storage() *state.Storage {
	return a.storage
}

// Username returns the authenticated username
func (a *App) Username() string {
	return a.username
}

// Token returns the authentication token
func (a *App) Token() string {
	return a.token
}

// IsAuthenticated returns whether the user is authenticated
func (a *App) IsAuthenticated() bool {
	return a.isAuthenticated
}

// SelectedProfile returns the currently selected profile for quick sync
func (a *App) SelectedProfile() *state.SyncProfile {
	return a.selectedProfile
}

// SetSelectedProfile sets the selected profile for quick sync
func (a *App) SetSelectedProfile(profile *state.SyncProfile) {
	a.selectedProfile = profile
}

// ProfilesToSync returns the list of profiles to sync (for batch sync)
func (a *App) ProfilesToSync() []*state.SyncProfile {
	return a.profilesToSync
}

// SetProfilesToSync sets the list of profiles for batch sync
func (a *App) SetProfilesToSync(profiles []*state.SyncProfile) {
	a.profilesToSync = profiles
}

// DeleteProfileInfo returns the profile ID and name for deletion confirmation
func (a *App) DeleteProfileInfo() (string, string) {
	return a.deleteProfileID, a.deleteProfileName
}

// Width returns the terminal width
func (a *App) Width() int {
	return a.width
}

// Height returns the terminal height
func (a *App) Height() int {
	return a.height
}

// Init initializes the app
func (a *App) Init() tea.Cmd {
	// Initialize the current screen
	if model := a.getOrCreateScreen(a.currentScreen); model != nil {
		return model.Init()
	}
	return nil
}

// Update handles messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global key handling - but let screens handle 'q' first
		// Only quit on 'q' if not in a form or editor
		// (screens should pass through QuitMsg when appropriate)

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case ScreenChangeMsg:
		return a, a.SetScreen(msg.Screen)

	case ScreenPushMsg:
		return a, a.PushScreen(msg.Screen)

	case ScreenPopMsg:
		return a, a.PopScreen()

	case QuitMsg:
		a.quitting = true
		return a, tea.Quit

	case ErrorMsg:
		a.err = msg.Err

	case AuthCompleteMsg:
		// Handle authentication completion from onboarding
		if msg.Error != nil {
			a.err = msg.Error
		} else {
			a.isAuthenticated = true
			a.username = msg.Username
			a.token = msg.Token
			// Create GitHub client with the new token
			if msg.Token != "" {
				a.ghClient = github.NewClient(msg.Token)
			}
			// Mark onboarding as complete
			if a.storage != nil {
				_ = a.storage.SetOnboardingComplete(true)
			}
			// Transition to dashboard, clearing the screen stack
			return a, a.ResetToScreen(ScreenDashboard)
		}

	case RefreshDashboardMsg:
		// If we're on dashboard, trigger a refresh
		if a.currentScreen == ScreenDashboard {
			if model := a.getOrCreateScreen(ScreenDashboard); model != nil {
				updatedModel, cmd := model.Update(msg)
				if screenModel, ok := updatedModel.(ScreenModel); ok {
					a.screens[ScreenDashboard] = screenModel
				}
				cmds = append(cmds, cmd)
			}
		}

	case NewSyncRequestedMsg:
		// Clear any cached wizard so we get a fresh one
		delete(a.screens, ScreenSyncWizard)
		// Push sync wizard onto the stack
		return a, a.PushScreen(ScreenSyncWizard)

	case ProfileSelectedMsg:
		// Store the selected profile for the sync progress screen
		a.selectedProfile = msg.Profile
		a.profilesToSync = nil // Clear batch list
		// Clear any cached sync progress screen so we get a fresh one
		delete(a.screens, ScreenSyncProgress)
		// Push sync progress screen
		return a, a.PushScreen(ScreenSyncProgress)

	case SyncAllProfilesMsg:
		// Set all profiles for batch sync
		if a.storage != nil {
			a.profilesToSync = a.storage.GetProfiles()
			a.selectedProfile = nil // Clear single profile
			delete(a.screens, ScreenSyncProgress)
			return a, a.PushScreen(ScreenSyncProgress)
		}

	case SyncPendingProfilesMsg:
		// Set pending profiles (not synced in 24h) for batch sync
		if a.storage != nil {
			allProfiles := a.storage.GetProfiles()
			var pending []*state.SyncProfile
			for _, p := range allProfiles {
				if time.Since(p.LastSyncAt) > 24*time.Hour {
					pending = append(pending, p)
				}
			}
			if len(pending) > 0 {
				a.profilesToSync = pending
				a.selectedProfile = nil
				delete(a.screens, ScreenSyncProgress)
				return a, a.PushScreen(ScreenSyncProgress)
			}
		}

	case DeleteProfileRequestMsg:
		// Show confirmation screen for profile deletion
		delete(a.screens, ScreenConfirmDelete)
		// Store profile info for the confirmation screen
		a.deleteProfileID = msg.ProfileID
		a.deleteProfileName = msg.ProfileName
		return a, a.PushScreen(ScreenConfirmDelete)

	case DeleteProfileConfirmedMsg:
		// Actually delete the profile
		if a.storage != nil {
			if err := a.storage.DeleteProfile(msg.ProfileID); err != nil {
				a.err = err
			}
		}
		// Pop back to dashboard and refresh
		cmds = append(cmds, a.PopScreen())
		cmds = append(cmds, RefreshDashboardCmd())
		return a, tea.Batch(cmds...)

	case DeleteProfileCancelledMsg:
		// Just pop back to dashboard
		return a, a.PopScreen()
	}

	// Update current screen
	if model := a.getOrCreateScreen(a.currentScreen); model != nil {
		updatedModel, cmd := model.Update(msg)
		if screenModel, ok := updatedModel.(ScreenModel); ok {
			a.screens[a.currentScreen] = screenModel
		}
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

// View renders the app
func (a *App) View() string {
	if a.quitting {
		return ""
	}

	// Get current screen view
	var content string
	if model := a.getOrCreateScreen(a.currentScreen); model != nil {
		content = model.View()
	} else {
		content = "Loading..."
	}

	// Render with header and footer
	return a.renderWithChrome(content)
}

// renderWithChrome adds header and footer to content
func (a *App) renderWithChrome(content string) string {
	// Header
	header := a.renderHeader()

	// Footer
	footer := a.renderFooter()

	// Calculate available height for content
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)
	contentHeight := a.height - headerHeight - footerHeight - 4 // padding

	// Render content with height constraint
	contentStyle := lipgloss.NewStyle().
		Height(contentHeight).
		MaxHeight(contentHeight)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		contentStyle.Render(content),
		footer,
	)
}

// renderHeader renders the app header
func (a *App) renderHeader() string {
	title := a.styles.HeaderTitle.Render("GitHubby")

	// Status section
	var status string
	if a.isAuthenticated {
		status = a.styles.Success.Render("●") + " " + a.styles.HeaderStatus.Render(a.username)
	} else {
		status = a.styles.Error.Render("●") + " " + a.styles.HeaderStatus.Render("Not authenticated")
	}

	// Screen title
	var screenTitle string
	if model := a.getOrCreateScreen(a.currentScreen); model != nil {
		screenTitle = a.styles.Muted.Render(" | " + model.Title())
	}

	left := title + screenTitle
	right := status

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right) - 6
	if gap < 1 {
		gap = 1
	}

	headerContent := left + lipgloss.NewStyle().Width(gap).Render("") + right

	return a.styles.Header.Width(a.width - 4).Render(headerContent)
}

// renderFooter renders the app footer with help and version
func (a *App) renderFooter() string {
	var helpItems []string

	// Get screen-specific help
	if model := a.getOrCreateScreen(a.currentScreen); model != nil {
		for _, binding := range model.ShortHelp() {
			if binding.Enabled() {
				help := a.styles.HelpKey.Render(binding.Help().Key) + " " +
					a.styles.HelpValue.Render(binding.Help().Desc)
				helpItems = append(helpItems, help)
			}
		}
	}

	// Add quit help
	quitHelp := a.styles.HelpKey.Render("q") + " " + a.styles.HelpValue.Render("quit")
	helpItems = append(helpItems, quitHelp)

	helpText := joinWithSeparator(helpItems, "  ")

	// Version on the right
	versionText := ""
	if a.version != "" {
		versionText = a.styles.Muted.Render("v" + a.version)
	}

	// Calculate gap between help and version
	footerWidth := a.width - 4
	helpWidth := lipgloss.Width(helpText)
	versionWidth := lipgloss.Width(versionText)
	gap := footerWidth - helpWidth - versionWidth - 2
	if gap < 1 {
		gap = 1
	}

	footerContent := helpText + lipgloss.NewStyle().Width(gap).Render("") + versionText

	return a.styles.Footer.Width(footerWidth).Render(footerContent)
}

// joinWithSeparator joins strings with a separator
func joinWithSeparator(items []string, sep string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += sep
		}
		result += item
	}
	return result
}

// Message types and command helpers are defined in messages.go

// RunApp starts the unified TUI application (creates new App instance)
func RunApp(ctx context.Context, opts ...AppOption) error {
	app := NewApp(opts...)

	// Determine initial screen based on auth state AND config completeness
	// Config is "complete" if at least one profile exists
	startScreen := ScreenOnboarding
	if app.isAuthenticated {
		hasProfiles := false
		if app.storage != nil {
			hasProfiles = len(app.storage.GetProfiles()) > 0
		}
		if hasProfiles {
			startScreen = ScreenDashboard
		}
		// If authenticated but no profiles, stay on onboarding
	}
	app.currentScreen = startScreen

	// Run the program
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

// RunAppInstance runs an existing App instance
func RunAppInstance(ctx context.Context, app *App) error {
	// Determine initial screen based on auth state AND config completeness
	// Config is "complete" if at least one profile exists
	startScreen := ScreenOnboarding
	if app.isAuthenticated {
		hasProfiles := false
		if app.storage != nil {
			hasProfiles = len(app.storage.GetProfiles()) > 0
		}
		if hasProfiles {
			startScreen = ScreenDashboard
		}
		// If authenticated but no profiles, stay on onboarding
	}
	app.currentScreen = startScreen

	// Run the program
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

// Legacy functions for backward compatibility

// Run starts the TUI application (legacy)
func Run(ctx context.Context, startScreen Screen, isAuthenticated bool, username string) error {
	return RunApp(ctx, WithContext(ctx), WithAuth(isAuthenticated, username, ""))
}

// RunOnboarding starts the TUI in onboarding mode (legacy)
func RunOnboarding(ctx context.Context) error {
	return Run(ctx, ScreenOnboarding, false, "")
}

// RunDashboard starts the TUI in dashboard mode (legacy)
func RunDashboard(ctx context.Context, username string) error {
	return Run(ctx, ScreenDashboard, true, username)
}

// PrintNonInteractiveHelp prints help when not in an interactive terminal
func PrintNonInteractiveHelp() {
	fmt.Println("GitHubby - GitHub CLI Utility")
	fmt.Println()
	fmt.Println("Interactive mode is not available in this environment.")
	fmt.Println("Use one of the following commands:")
	fmt.Println()
	fmt.Println("  githubby login               - Authenticate with GitHub")
	fmt.Println("  githubby sync --help         - Sync repositories")
	fmt.Println("  githubby clean --help        - Clean releases")
	fmt.Println("  githubby --help              - Show all commands")
	fmt.Println()
	fmt.Println("For interactive mode, run githubby in a terminal.")
}
