// Package cli provides the command-line interface for GitHubby
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
	"github.com/Didstopia/githubby/internal/config"
	"github.com/Didstopia/githubby/internal/update"
)

// autoUpdateEnabled controls whether auto-update on launch is enabled
var autoUpdateEnabled = true

// Version information (set via ldflags)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Global flags
var (
	verbose    bool
	dryRun     bool
	token      string
	repository string
)

// Global logger
var log = logrus.New()

// Config loader
var configLoader *config.Loader

// Root command
var rootCmd = &cobra.Command{
	Use:   "githubby",
	Short: "GitHub CLI utility",
	Long:  `A multi-purpose CLI utility for interacting with GitHub.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Inject config file values
		configLoader.InjectToCommand(cmd)

		// Re-read flags after injection
		verbose, _ = cmd.Flags().GetBool("verbose")
		dryRun, _ = cmd.Flags().GetBool("dry-run")
		token, _ = cmd.Flags().GetString("token")
		repository, _ = cmd.Flags().GetString("repository")

		// Set log level
		if verbose {
			log.SetLevel(logrus.DebugLevel)
		} else {
			log.SetLevel(logrus.InfoLevel)
		}

		return nil
	},
}

func init() {
	// Initialize config loader
	configLoader = config.NewLoader()
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "D", false, "Simulate running without making changes")
	rootCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "GitHub API token")

	// Bind to viper after initialization
}

func initConfig() {
	if err := configLoader.Initialize(); err != nil {
		// Config initialization failure is not fatal for all commands
		log.Debugf("Config initialization: %v", err)
	}

	// Bind flags to viper
	viper := configLoader.Viper()
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("dry-run", rootCmd.PersistentFlags().Lookup("dry-run"))
	viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))

	viper.SetDefault("verbose", false)
	viper.SetDefault("dry-run", false)
	viper.SetDefault("token", "")
}

// Execute runs the root command
func Execute() {
	// Create context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down...")
		cancel()
	}()

	// Store context for subcommands
	rootCmd.SetContext(ctx)

	// Check if running with no arguments - launch TUI (TUI handles its own update)
	if len(os.Args) == 1 {
		if err := RunTUI(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Perform blocking auto-update check for CLI commands
	if autoUpdateEnabled && shouldAutoUpdate() {
		if restarted := checkAndAutoUpdate(ctx); restarted {
			// If we restarted, execution continues in the new process
			return
		}
	}

	// Start background update check for non-update CLI commands (fallback notification)
	updateChan := startBackgroundUpdateCheck(ctx)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Show update notification if available (after command completes)
	showUpdateNotification(updateChan)
}

// shouldAutoUpdate returns true if auto-update should be performed for this command
func shouldAutoUpdate() bool {
	// Skip for dev builds
	if update.IsDev(Version) {
		return false
	}

	// Skip for certain commands
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "update", "version", "help", "--help", "-h", "--version", "-v":
			return false
		case "interactive", "tui", "ui":
			// TUI handles its own update
			return false
		}
	}

	return true
}

// checkAndAutoUpdate checks for updates and performs auto-update if available
// Returns true if the app was restarted (caller should exit)
func checkAndAutoUpdate(ctx context.Context) bool {
	fmt.Println("Checking for updates...")

	// Create a timeout context for the update check
	checkCtx, checkCancel := context.WithTimeout(ctx, 10*time.Second)
	defer checkCancel()

	result, err := update.CheckForUpdate(checkCtx, Version)
	if err != nil {
		// Silent failure, continue with command
		return false
	}

	if result == nil || !result.Available {
		return false
	}

	fmt.Printf("Updating to v%s...\n", result.LatestVersion)

	// Perform the update with a longer timeout
	updateCtx, updateCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer updateCancel()

	if _, err := update.Update(updateCtx, Version); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		fmt.Println("Continuing with current version...")
		return false
	}

	fmt.Println("Update complete! Restarting...")
	time.Sleep(500 * time.Millisecond) // Brief pause for user to see message

	if err := update.Restart(); err != nil {
		fmt.Fprintf(os.Stderr, "Restart failed: %v\n", err)
		fmt.Println("Please restart the application manually.")
		return false
	}

	// Restart should not return, but if it does, indicate we tried
	return true
}

// startBackgroundUpdateCheck starts a background goroutine to check for updates
// Returns a channel that will receive the update result (or nil on error/timeout)
func startBackgroundUpdateCheck(ctx context.Context) <-chan *update.Result {
	resultChan := make(chan *update.Result, 1)

	// Skip for update command itself, version command, or dev builds
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		if cmd == "update" || cmd == "version" || cmd == "help" || cmd == "--help" || cmd == "-h" {
			close(resultChan)
			return resultChan
		}
	}

	// Skip for dev builds
	if Version == "" || Version == "dev" {
		close(resultChan)
		return resultChan
	}

	go func() {
		defer close(resultChan)

		// Create a timeout context for the update check
		checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
		defer checkCancel()

		result, err := update.CheckForUpdate(checkCtx, Version)
		if err != nil {
			// Silently ignore errors (network issues, etc.)
			return
		}

		if result != nil && result.Available {
			resultChan <- result
		}
	}()

	return resultChan
}

// showUpdateNotification displays the update notification if one is available
func showUpdateNotification(updateChan <-chan *update.Result) {
	// Wait briefly for result (should be ready by now since command completed)
	select {
	case result := <-updateChan:
		if result != nil && result.Available {
			fmt.Fprintf(os.Stderr, "\n%s\n", update.FormatUpdateNotification(result))
		}
	case <-time.After(100 * time.Millisecond):
		// Don't block if update check is still running
	}
}

// GetLogger returns the global logger
func GetLogger() *logrus.Logger {
	return log
}

// GetToken returns the configured token, checking multiple sources
// Priority: CLI flag > environment variable > stored token (keychain/config)
func GetToken() string {
	return token
}

// GetTokenWithAuth returns the token using the auth package's resolution logic
// This checks CLI flag, environment variable, and stored tokens
func GetTokenWithAuth(ctx context.Context) (string, error) {
	result, err := auth.GetToken(ctx, token, "")
	if err != nil {
		return "", err
	}
	return result.Token, nil
}

// GetVerbose returns the verbose flag
func GetVerbose() bool {
	return verbose
}

// GetDryRun returns the dry-run flag
func GetDryRun() bool {
	return dryRun
}
