// Package screens provides TUI screen implementations
package screens

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/cli/oauth/device"

	"github.com/Didstopia/githubby/internal/auth"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
)

// OnboardingStep represents the current step in onboarding
type OnboardingStep int

const (
	StepAuthMethod OnboardingStep = iota
	StepOAuthFlow
	StepTokenInput
	StepSyncConfig
	StepComplete
)

// Onboarding is the first-run onboarding wizard screen
type Onboarding struct {
	ctx     context.Context
	storage *state.Storage
	styles  *tui.Styles
	keys    tui.KeyMap

	// Current step
	step OnboardingStep

	// Auth method selection
	authMethod string

	// OAuth flow state
	oauthCode    *device.CodeResponse
	oauthSpinner spinner.Model
	oauthStatus  string
	oauthError   error
	codeCopied   bool

	// Token input state
	tokenInput string

	// Forms
	authForm       *huh.Form
	tokenForm      *huh.Form
	syncConfigForm *huh.Form

	// Sync config values
	syncTarget string
	syncUser   string

	// User info after auth
	username string
	token    string // stored token for AuthCompleteMsg

	// Channels for async operations
	oauthCodeChan     chan oauthCodeResult
	oauthCompleteChan chan oauthCompleteResult
	tokenValidateChan chan tokenValidateResult

	// Completion state
	launchDashboard bool

	// Embedded mode (within unified app vs standalone)
	embeddedMode bool

	// Dimensions
	width  int
	height int

	// State
	loading bool
	err     error

	// Exit confirmation state
	exitPending bool
	exitKey     string // "escape" or "ctrl+c"
}

// OnboardingOption configures the Onboarding screen
type OnboardingOption func(*Onboarding)

// WithEmbeddedMode sets the onboarding to run in embedded mode (within unified app)
func WithEmbeddedMode() OnboardingOption {
	return func(o *Onboarding) {
		o.embeddedMode = true
	}
}

// WithSkipToSyncConfig skips auth steps and goes directly to sync config
// Used when auth is already complete but onboarding wasn't finished
func WithSkipToSyncConfig(username string) OnboardingOption {
	return func(o *Onboarding) {
		o.step = StepSyncConfig
		o.username = username
	}
}

// WithStorage sets the storage instance for persisting settings
func WithStorage(storage *state.Storage) OnboardingOption {
	return func(o *Onboarding) {
		o.storage = storage
	}
}

// NewOnboarding creates a new onboarding screen
func NewOnboarding(ctx context.Context, opts ...OnboardingOption) *Onboarding {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = tui.GetStyles().Spinner

	o := &Onboarding{
		ctx:          ctx,
		styles:       tui.GetStyles(),
		keys:         tui.GetKeyMap(),
		step:         StepAuthMethod,
		oauthSpinner: s,
		width:        80,
		height:       24,
	}

	for _, opt := range opts {
		opt(o)
	}

	o.initForms()
	return o
}

// initForms initializes all the forms
func (o *Onboarding) initForms() {
	// Auth method selection form
	o.authForm = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How would you like to authenticate?").
				Description("GitHubby needs access to GitHub to sync repositories.").
				Options(
					huh.NewOption("Browser (OAuth) - Recommended", "oauth"),
					huh.NewOption("Personal Access Token", "token"),
				).
				Value(&o.authMethod),
		),
	).WithTheme(huh.ThemeCharm())

	// Token input form
	o.tokenForm = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Enter your Personal Access Token").
				Description("Create a token at https://github.com/settings/tokens\nRequired scopes: repo, read:org").
				Placeholder("ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx").
				EchoMode(huh.EchoModePassword).
				Value(&o.tokenInput).
				Validate(func(s string) error {
					if len(s) < 10 {
						return fmt.Errorf("token is too short")
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeCharm())

	// Sync config form - initialized separately since values need to be set first
	o.initSyncConfigForm()
}

// initSyncConfigForm creates the sync config form with current values
func (o *Onboarding) initSyncConfigForm() {
	o.syncConfigForm = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Default sync target directory").
				Description("Where should repositories be cloned? (Press Enter to accept)").
				Value(&o.syncTarget),
			huh.NewInput().
				Title("Default GitHub username").
				Description("Your GitHub username for syncing your repositories.").
				Value(&o.syncUser),
		),
	).WithTheme(huh.ThemeCharm())
}

// Title returns the screen title
func (o *Onboarding) Title() string {
	return "Setup Wizard"
}

// ShortHelp returns key bindings for the footer
func (o *Onboarding) ShortHelp() []key.Binding {
	return []key.Binding{
		o.keys.Select,
		o.keys.Back,
	}
}

// Init initializes the onboarding screen
func (o *Onboarding) Init() tea.Cmd {
	// If starting at sync config step (skipped auth), set defaults and recreate form
	if o.step == StepSyncConfig {
		o.setSyncConfigDefaults()
		o.initSyncConfigForm() // Recreate form with populated values
		return o.syncConfigForm.Init()
	}

	return tea.Batch(
		o.authForm.Init(),
		o.oauthSpinner.Tick,
	)
}

// Update handles messages
func (o *Onboarding) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		o.width = msg.Width
		o.height = msg.Height

	case tea.KeyMsg:
		// Handle Ctrl+C - require double press
		if msg.Type == tea.KeyCtrlC {
			if o.exitPending && o.exitKey == "ctrl+c" {
				return o, tea.Quit
			}
			o.exitPending = true
			o.exitKey = "ctrl+c"
			return o, exitTimeoutCmd()
		}
		switch {
		case key.Matches(msg, o.keys.Back):
			// Handle Escape - require double press to exit at first step or during OAuth
			if o.step == StepAuthMethod || o.step == StepOAuthFlow {
				if o.exitPending && o.exitKey == "escape" {
					return o, tea.Quit
				}
				o.exitPending = true
				o.exitKey = "escape"
				return o, exitTimeoutCmd()
			}
			// Go back to previous step
			o.exitPending = false // Reset exit state on navigation
			o.step--
			return o, o.initCurrentForm()
		case key.Matches(msg, o.keys.Quit):
			// Handle 'q' - require double press
			if o.exitPending && o.exitKey == "q" {
				return o, tea.Quit
			}
			o.exitPending = true
			o.exitKey = "q"
			return o, exitTimeoutCmd()
		}
		// Any other key resets exit state
		if o.exitPending {
			o.exitPending = false
		}

	case exitTimeoutMsg:
		o.exitPending = false

	case spinner.TickMsg:
		if o.step == StepOAuthFlow {
			var cmd tea.Cmd
			o.oauthSpinner, cmd = o.oauthSpinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case oauthCodeMsg:
		o.oauthCode = msg.code
		o.oauthStatus = "Waiting for browser authorization..."
		// Copy code to clipboard and open browser
		if o.oauthCode != nil {
			// Try to copy code to clipboard
			if err := clipboard.WriteAll(o.oauthCode.UserCode); err == nil {
				o.codeCopied = true
			}
			// Open browser
			verificationURL := o.oauthCode.VerificationURI
			if verificationURL == "" {
				verificationURL = "https://github.com/login/device"
			}
			_ = auth.OpenBrowser(verificationURL)
			// Start waiting for the OAuth flow to complete
			return o, o.waitForOAuthComplete()
		}

	case oauthCompleteMsg:
		if msg.err != nil {
			o.oauthError = msg.err
			o.oauthStatus = "Authentication failed"
		} else {
			// Store token and move to next step
			storage := auth.NewStorage()
			if err := storage.SetToken("", msg.token); err != nil {
				o.oauthError = err
				o.oauthStatus = "Failed to store token"
			} else {
				o.username = msg.username
				o.token = msg.token // Store for AuthCompleteMsg
				o.step = StepSyncConfig
				// Set defaults for sync config
				o.setSyncConfigDefaults()
				o.initForms() // Re-init forms with new values
				return o, o.syncConfigForm.Init()
			}
		}

	case tokenValidateMsg:
		if msg.err != nil {
			o.err = msg.err
		} else {
			// Store token and move to next step
			storage := auth.NewStorage()
			if err := storage.SetToken("", o.tokenInput); err != nil {
				o.err = err
			} else {
				o.username = msg.username
				o.token = o.tokenInput // Store for AuthCompleteMsg
				o.step = StepSyncConfig
				// Set defaults for sync config
				o.setSyncConfigDefaults()
				o.initForms() // Re-init forms with new values
				return o, o.syncConfigForm.Init()
			}
		}
		o.loading = false
	}

	// Handle form updates based on current step
	switch o.step {
	case StepAuthMethod:
		form, cmd := o.authForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			o.authForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				switch o.authMethod {
				case "oauth":
					o.step = StepOAuthFlow
					return o, o.startOAuthFlow()
				case "token":
					o.step = StepTokenInput
					return o, o.tokenForm.Init()
				}
			}
		}

	case StepTokenInput:
		form, cmd := o.tokenForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			o.tokenForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				o.loading = true
				return o, o.validateToken()
			}
		}

	case StepSyncConfig:
		form, cmd := o.syncConfigForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			o.syncConfigForm = f
			cmds = append(cmds, cmd)
			if f.State == huh.StateCompleted {
				o.saveSyncConfig()
				o.step = StepComplete
				return o, nil
			}
		}

	case StepComplete:
		// Handle Enter to signal dashboard launch
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if key.Matches(keyMsg, o.keys.Select) {
				o.launchDashboard = true
				if o.embeddedMode {
					// In embedded mode, emit AuthCompleteMsg for app to handle
					return o, func() tea.Msg {
						return tui.AuthCompleteMsg{
							Token:    o.token,
							Username: o.username,
						}
					}
				}
				// In standalone mode, quit and let caller check launchDashboard
				return o, tea.Quit
			}
		}
	}

	return o, tea.Batch(cmds...)
}

// View renders the onboarding screen
func (o *Onboarding) View() string {
	var content string

	switch o.step {
	case StepAuthMethod:
		content = o.viewAuthMethod()
	case StepOAuthFlow:
		content = o.viewOAuthFlow()
	case StepTokenInput:
		content = o.viewTokenInput()
	case StepSyncConfig:
		content = o.viewSyncConfig()
	case StepComplete:
		content = o.viewComplete()
	}

	// Add exit confirmation message if pending
	if o.exitPending {
		var msg string
		switch o.exitKey {
		case "ctrl+c":
			msg = "Press Ctrl+C again to quit"
		case "escape":
			msg = "Press Escape again to quit"
		case "q":
			msg = "Press q again to quit"
		}
		content += "\n\n" + o.styles.Warning.Render(msg)
	}

	return o.styles.Content.Render(content)
}

func (o *Onboarding) viewAuthMethod() string {
	title := o.styles.FormTitle.Render("Welcome to GitHubby!")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		o.authForm.View(),
	)
}

func (o *Onboarding) viewOAuthFlow() string {
	title := o.styles.FormTitle.Render("Browser Authentication")

	var content strings.Builder

	if o.oauthError != nil {
		content.WriteString(o.styles.Error.Render("Error: " + o.oauthError.Error()))
		content.WriteString("\n\nPress Esc to go back and try again.")
	} else if o.oauthCode != nil {
		content.WriteString("Your one-time code")
		if o.codeCopied {
			content.WriteString(" " + o.styles.Success.Render("(copied to clipboard)"))
		}
		content.WriteString(":\n\n")
		content.WriteString(o.styles.BoxHighlighted.Render("  " + o.oauthCode.UserCode + "  "))
		content.WriteString("\n\n")
		content.WriteString(o.oauthSpinner.View())
		content.WriteString(" " + o.oauthStatus)
		content.WriteString("\n\nA browser window should have opened. If not, visit:")
		verificationURL := o.oauthCode.VerificationURI
		if verificationURL == "" {
			verificationURL = "https://github.com/login/device"
		}
		content.WriteString("\n" + o.styles.Info.Render(verificationURL))
	} else {
		content.WriteString(o.oauthSpinner.View())
		content.WriteString(" Starting authentication...")
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		content.String(),
	)
}

func (o *Onboarding) viewTokenInput() string {
	title := o.styles.FormTitle.Render("Personal Access Token")

	var content strings.Builder

	if o.err != nil {
		content.WriteString(o.styles.Error.Render("Error: " + o.err.Error()))
		content.WriteString("\n\n")
	}

	if o.loading {
		content.WriteString(o.oauthSpinner.View())
		content.WriteString(" Validating token...")
	} else {
		content.WriteString(o.tokenForm.View())
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		content.String(),
	)
}

func (o *Onboarding) viewSyncConfig() string {
	title := o.styles.FormTitle.Render("Sync Configuration")

	welcome := ""
	if o.username != "" {
		welcome = o.styles.Success.Render("Welcome, "+o.username+"!") + "\n\n"
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		welcome,
		o.syncConfigForm.View(),
	)
}

func (o *Onboarding) viewComplete() string {
	title := o.styles.FormTitle.Render("Setup Complete!")

	var content strings.Builder

	if o.username != "" {
		content.WriteString(o.styles.Success.Render("You're all set, " + o.username + "!"))
	} else {
		content.WriteString(o.styles.Success.Render("You're all set!"))
	}

	content.WriteString("\n\n")
	content.WriteString("Here are some commands to get started:\n\n")

	commands := []struct {
		cmd  string
		desc string
	}{
		{"githubby sync --user <username>", "Sync your repositories"},
		{"githubby sync --org <org>", "Sync an organization's repos"},
		{"githubby clean --repository <owner/repo>", "Clean up releases"},
		{"githubby --help", "Show all commands"},
	}

	for _, c := range commands {
		content.WriteString("  " + o.styles.Info.Render(c.cmd) + "\n")
		content.WriteString("    " + o.styles.Muted.Render(c.desc) + "\n\n")
	}

	content.WriteString("\nPress " + o.styles.HelpKey.Render("q") + " to exit or " +
		o.styles.HelpKey.Render("Enter") + " to open the dashboard.")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		content.String(),
	)
}

// initCurrentForm initializes the form for the current step
func (o *Onboarding) initCurrentForm() tea.Cmd {
	o.initForms() // Reset forms
	switch o.step {
	case StepAuthMethod:
		return o.authForm.Init()
	case StepTokenInput:
		return o.tokenForm.Init()
	case StepSyncConfig:
		return o.syncConfigForm.Init()
	}
	return nil
}

// OAuth flow messages
type oauthCodeMsg struct {
	code *device.CodeResponse
}

type oauthCompleteMsg struct {
	token    string
	username string
	err      error
}

type tokenValidateMsg struct {
	username string
	err      error
}

// Async result types for channel communication
type oauthCodeResult struct {
	code *device.CodeResponse
	err  error
}

type oauthCompleteResult struct {
	token    string
	username string
	err      error
}

type tokenValidateResult struct {
	username string
	err      error
}

// Exit confirmation messages
type exitTimeoutMsg struct{}

const exitConfirmTimeout = 2 * time.Second

func exitTimeoutCmd() tea.Cmd {
	return tea.Tick(exitConfirmTimeout, func(time.Time) tea.Msg {
		return exitTimeoutMsg{}
	})
}

// startOAuthFlow starts the OAuth device flow and returns the device code asynchronously
func (o *Onboarding) startOAuthFlow() tea.Cmd {
	o.oauthCodeChan = make(chan oauthCodeResult, 1)

	// Start OAuth request in background goroutine
	go func() {
		defer close(o.oauthCodeChan)

		// Request the device code from GitHub
		code, err := auth.RequestDeviceCode(o.ctx)
		if err != nil {
			o.oauthCodeChan <- oauthCodeResult{err: fmt.Errorf("failed to get device code: %w", err)}
			return
		}
		o.oauthCodeChan <- oauthCodeResult{code: code}
	}()

	// Return batch with spinner tick to keep UI responsive
	return tea.Batch(
		o.oauthSpinner.Tick,
		o.waitForOAuthCode(),
	)
}

// waitForOAuthCode waits for the device code to be fetched
func (o *Onboarding) waitForOAuthCode() tea.Cmd {
	return func() tea.Msg {
		result := <-o.oauthCodeChan
		if result.err != nil {
			return oauthCompleteMsg{err: result.err}
		}
		return oauthCodeMsg{code: result.code}
	}
}

// waitForOAuthComplete waits for the user to complete OAuth in browser asynchronously
func (o *Onboarding) waitForOAuthComplete() tea.Cmd {
	o.oauthCompleteChan = make(chan oauthCompleteResult, 1)

	// Start polling in background goroutine
	go func() {
		defer close(o.oauthCompleteChan)

		// Poll for the token (this blocks until user authorizes or timeout)
		result, err := auth.PollForToken(o.ctx, o.oauthCode)
		if err != nil {
			o.oauthCompleteChan <- oauthCompleteResult{err: err}
			return
		}

		// Validate token and get username
		user, err := auth.ValidateToken(o.ctx, result.Token, "")
		if err != nil {
			o.oauthCompleteChan <- oauthCompleteResult{err: fmt.Errorf("token validation failed: %w", err)}
			return
		}

		o.oauthCompleteChan <- oauthCompleteResult{token: result.Token, username: user.Login}
	}()

	// Return batch with spinner tick to keep UI responsive
	return tea.Batch(
		o.oauthSpinner.Tick,
		o.waitForOAuthResult(),
	)
}

// waitForOAuthResult waits for OAuth completion result
func (o *Onboarding) waitForOAuthResult() tea.Cmd {
	return func() tea.Msg {
		result := <-o.oauthCompleteChan
		return oauthCompleteMsg(result)
	}
}

// validateToken validates the entered token asynchronously
func (o *Onboarding) validateToken() tea.Cmd {
	o.tokenValidateChan = make(chan tokenValidateResult, 1)

	// Start validation in background goroutine
	go func() {
		defer close(o.tokenValidateChan)

		user, err := auth.ValidateToken(o.ctx, o.tokenInput, "")
		if err != nil {
			o.tokenValidateChan <- tokenValidateResult{err: err}
			return
		}
		o.tokenValidateChan <- tokenValidateResult{username: user.Login}
	}()

	// Return batch with spinner tick to keep UI responsive
	return tea.Batch(
		o.oauthSpinner.Tick,
		o.waitForTokenValidation(),
	)
}

// waitForTokenValidation waits for token validation result
func (o *Onboarding) waitForTokenValidation() tea.Cmd {
	return func() tea.Msg {
		result := <-o.tokenValidateChan
		return tokenValidateMsg(result)
	}
}

// setSyncConfigDefaults sets default values for sync configuration
func (o *Onboarding) setSyncConfigDefaults() {
	// Default target to current working directory
	if cwd, err := os.Getwd(); err == nil {
		o.syncTarget = cwd
	} else {
		o.syncTarget = "."
	}

	// Default username to authenticated user
	if o.username != "" {
		o.syncUser = o.username
	}
}

// saveSyncConfig saves the sync configuration to storage
func (o *Onboarding) saveSyncConfig() {
	if o.storage != nil {
		_ = o.storage.SetDefaults(o.syncTarget, o.syncUser)
	}
}

// OnboardingResult contains the result of the onboarding flow
type OnboardingResult struct {
	LaunchDashboard bool
	Username        string
}

// RunOnboardingProgram runs the onboarding as a standalone program
func RunOnboardingProgram(ctx context.Context) (*OnboardingResult, error) {
	onboarding := NewOnboarding(ctx)

	p := tea.NewProgram(onboarding, tea.WithAltScreen(), tea.WithContext(ctx))
	model, err := p.Run()
	if err != nil {
		return nil, err
	}

	// Extract result from final model state
	if o, ok := model.(*Onboarding); ok {
		return &OnboardingResult{
			LaunchDashboard: o.launchDashboard,
			Username:        o.username,
		}, nil
	}

	return &OnboardingResult{}, nil
}
