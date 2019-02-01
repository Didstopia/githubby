// Package cmd is the primary entrypoint, and handles command parsing and execution.
package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/Didstopia/github-release-cleaner/ghapi"
	"github.com/Didstopia/github-release-cleaner/util"
	"github.com/spf13/cobra" // Include the Cobra Commander package
)

// Verbose can be toggled on/off to enable diagnostic log output
var Verbose bool

// Repository is the target GitHub repository
var Repository string

// Token is the GitHub API token
var Token string

// The primary command object
var rootCmd *cobra.Command

func init() {
	// Create the primary command object
	rootCmd = &cobra.Command{
		Use:   "github-release-cleaner",
		Short: "Automated GitHub Release cleanup",
		Long:  `Provider an easy and customizable way to run automatic GitHub Release cleanup`,
		Run: func(cmd *cobra.Command, args []string) {
			if Verbose {
				log.Println("Running command with", "Arguments:", args, "-", "Verbose:", Verbose, "-", "Repository:", Repository, "-", "Token:", Token)
			}

			// Validate the repository
			owner, repo, err := util.ValidateGitHubRepository(Repository)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}

			if Verbose {
				log.Println("Validation succeeded for repository", owner+"/"+repo)
			}

			// Create a new GitHub client
			github, err := ghapi.NewGitHub(Token)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}

			// TODO: Run some GitHub API stuff
			releases, err := github.GetReleases(owner, repo)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}

			if Verbose {
				log.Println("Fetched releases for repository", owner+"/"+repo, "->", releases)
			}
		},
	}

	// Add the "verbose" flag globally, so it's available for all commands
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")

	// Add the "repository" flag and mark it as always required
	rootCmd.Flags().StringVarP(&Repository, "repository", "r", "", "GitHub Repository (required, short format only, eg. user/repo)")
	rootCmd.MarkFlagRequired("repository")

	// Add the "token" flag and mark it as always required
	rootCmd.Flags().StringVarP(&Token, "token", "t", "", "GitHub API Token (required)")
	rootCmd.MarkFlagRequired("token")
}

// Execute starts the Cobra commander, which in turn will handle execution and any arguments.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
