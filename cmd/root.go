// Package cmd is the primary entrypoint, and handles command parsing and execution.
package cmd

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra" // Include the Cobra Commander package
)

// Verbose can be toggled on/off to enable diagnostic log output
var Verbose bool

// DryRun will simulate the cleanup process without actually deleting anything
var DryRun bool

// Repository is the target GitHub repository
var Repository string

// Token is the GitHub API token
var Token string

// The primary logger
var log = logrus.New()

// The primary cobra command object
var rootCmd *cobra.Command

func init() {
	// Initialize the configuration
	cobra.OnInitialize(initConfig)

	// Create the primary command object
	rootCmd = &cobra.Command{
		Use:   "githubby",
		Short: "GitHub CLI utility",
		Long:  `A multi-purpose CLI utility for interacting with GitHub`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Inject config file variables to all child commands
			injectViper(viperConfig, cmd)

			// Validate token
			if Token == "" {
				fmt.Println("Missing required argument 'token'")
				os.Exit(1)
			}

			// Validate repository
			if Repository == "" {
				fmt.Println("Missing required argument 'repository'")
				os.Exit(1)
			}
		},
	}

	// Add the clean command
	rootCmd.AddCommand(cleanCmd)

	// Add the "verbose" flag globally, so it's available for all commands
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "Enable verbose output")
	viperConfig.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viperConfig.SetDefault("verbose", false)

	// Add the "dry-run" flag globally, so it's available for all commands
	rootCmd.PersistentFlags().BoolVarP(&DryRun, "dry-run", "D", false, "Simulate running")
	viperConfig.BindPFlag("dry-run", rootCmd.PersistentFlags().Lookup("dry-run"))
	viperConfig.SetDefault("dry-run", false)

	// Add the "token" flag globally, and mark it as always required
	rootCmd.PersistentFlags().StringVarP(&Token, "token", "t", "", "GitHub API Token (required)")
	rootCmd.MarkPersistentFlagRequired("token")
	viperConfig.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))
	viperConfig.SetDefault("token", "")

	// Add the "repository" flag to the clean command and mark it as always required
	cleanCmd.Flags().StringVarP(&Repository, "repository", "r", "", "GitHub Repository (required, short format only, eg. user/repo)")
	cleanCmd.MarkFlagRequired("repository")
	viperConfig.BindPFlag("repository", cleanCmd.Flags().Lookup("repository"))
	viperConfig.SetDefault("repository", "")

	// Add the "filter-days" flag to the clean command
	cleanCmd.Flags().Int64VarP(&FilterDays, "filter-days", "d", -1, "Filter based on maximum days since release (at least one filter is required)")
	viperConfig.BindPFlag("filter-days", cleanCmd.Flags().Lookup("filter-days"))
	viperConfig.SetDefault("filter-days", -1)

	// Add the "filter-count" flag to the clean command
	cleanCmd.Flags().Int64VarP(&FilterCount, "filter-count", "c", -1, "Filter to cleanup releases over the set amount (at least one filter is required)")
	viperConfig.BindPFlag("filter-count", cleanCmd.Flags().Lookup("filter-count"))
	viperConfig.SetDefault("filter-count", -1)
}

// Execute starts the Cobra commander, which in turn will handle execution and any arguments
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
