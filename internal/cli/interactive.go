package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/tui"
	"github.com/Didstopia/githubby/internal/tui/screens"
	tuiutil "github.com/Didstopia/githubby/internal/tui/util"
)

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Launch interactive TUI mode",
	Long: `Launch GitHubby in interactive TUI mode.

The interactive mode provides a visual interface for:
  - Browsing and syncing repositories
  - Managing releases
  - Configuring settings

This command is equivalent to running 'githubby' with no arguments
in an interactive terminal.`,
	Aliases: []string{"tui", "ui"},
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunTUI()
	},
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}

// RunTUI launches the unified TUI application
func RunTUI() error {
	ctx := rootCmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Check if terminal is interactive
	if !tuiutil.IsInteractive() {
		tui.PrintNonInteractiveHelp()
		return nil
	}

	// Initialize state storage
	storage, err := state.NewStorage()
	if err != nil {
		return fmt.Errorf("failed to initialize state storage: %w", err)
	}

	// Load existing state
	if err := storage.Load(); err != nil {
		log.WithError(err).Debug("Failed to load state, starting fresh")
		// Continue with fresh state
	}

	// Build app options
	opts := []tui.AppOption{
		tui.WithContext(ctx),
		tui.WithStorage(storage),
		tui.WithVersion(Version),
	}

	// Check authentication status
	result, _ := auth.GetToken(ctx, token, "")
	var ghClient github.Client
	var username string
	var authToken string
	isAuthenticated := false

	if result.Source != auth.TokenSourceNone {
		// Validate token and get username
		user, err := auth.ValidateToken(ctx, result.Token, "")
		if err == nil {
			isAuthenticated = true
			username = user.Login
			authToken = result.Token
			ghClient = github.NewClient(result.Token)
			log.WithField("user", username).Debug("Token valid")
		} else {
			log.WithError(err).Debug("Token validation failed")
		}
	} else {
		log.Debug("No token found")
	}

	// Add auth state to options
	if isAuthenticated {
		opts = append(opts, tui.WithAuth(true, username, authToken))
		opts = append(opts, tui.WithGitHubClient(ghClient))
	}

	// Create the app
	app := tui.NewApp(opts...)

	// Register screen factories for lazy initialization
	registerScreenFactories(app)

	// Set initial screen based on auth state AND config completeness
	// Config is "complete" if at least one profile exists
	hasProfiles := len(storage.GetProfiles()) > 0

	if isAuthenticated && hasProfiles {
		// Fully set up - go to dashboard
		app.RegisterScreen(tui.ScreenDashboard, screens.NewDashboardV2(ctx, app))
	} else if isAuthenticated && !hasProfiles {
		// Auth done but no profiles - resume onboarding from sync config
		onboarding := screens.NewOnboarding(ctx, screens.WithEmbeddedMode(), screens.WithStorage(storage), screens.WithSkipToSyncConfig(username))
		app.RegisterScreen(tui.ScreenOnboarding, onboarding)
	} else {
		// Not authenticated - start fresh onboarding
		app.RegisterScreen(tui.ScreenOnboarding, screens.NewOnboarding(ctx, screens.WithEmbeddedMode(), screens.WithStorage(storage)))
	}

	// Run the app directly (not RunApp which creates a new instance)
	return tui.RunAppInstance(ctx, app)
}

// registerScreenFactories registers factory functions for lazy screen creation
func registerScreenFactories(app *tui.App) {
	// Onboarding screen factory
	app.RegisterScreenFactory(tui.ScreenOnboarding, func(ctx context.Context, a *tui.App) tui.ScreenModel {
		return screens.NewOnboarding(ctx, screens.WithEmbeddedMode())
	})

	// Dashboard screen factory
	app.RegisterScreenFactory(tui.ScreenDashboard, func(ctx context.Context, a *tui.App) tui.ScreenModel {
		return screens.NewDashboardV2(ctx, a)
	})

	// Sync wizard screen factory
	app.RegisterScreenFactory(tui.ScreenSyncWizard, func(ctx context.Context, a *tui.App) tui.ScreenModel {
		return screens.NewSyncWizard(ctx, a)
	})

	// Clean screen factory
	app.RegisterScreenFactory(tui.ScreenClean, func(ctx context.Context, a *tui.App) tui.ScreenModel {
		return screens.NewCleanScreen(ctx, a.GitHubClient())
	})

	// Sync progress screen factory (for profile quick sync)
	app.RegisterScreenFactory(tui.ScreenSyncProgress, func(ctx context.Context, a *tui.App) tui.ScreenModel {
		return screens.NewSyncProgress(ctx, a)
	})

	// Confirm delete screen factory
	app.RegisterScreenFactory(tui.ScreenConfirmDelete, func(ctx context.Context, a *tui.App) tui.ScreenModel {
		return screens.NewConfirmDeleteScreen(ctx, a)
	})
}
