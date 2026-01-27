// Package cli provides the command-line interface for GitHubby
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
	"github.com/Didstopia/githubby/internal/config"
)

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

	// Check if running with no arguments - launch TUI
	if len(os.Args) == 1 {
		if err := RunTUI(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
