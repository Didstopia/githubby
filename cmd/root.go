// Package cmd is the primary entrypoint, and handles command parsing and execution.
package cmd

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/Didstopia/github-release-cleaner/ghapi"
	"github.com/Didstopia/github-release-cleaner/util"
	"github.com/google/go-github/v24/github"
	"github.com/spf13/cobra" // Include the Cobra Commander package
	pb "gopkg.in/cheggaaa/pb.v1"
)

// Verbose can be toggled on/off to enable diagnostic log output
var Verbose bool

// DryRun will simulate the cleanup process without actually deleting anything
var DryRun bool

// Repository is the target GitHub repository
var Repository string

// Token is the GitHub API token
var Token string

// FilterDays sets the maximum amount of days since release
var FilterDays int64

// FilterCount sets the maximum amount of releases to keep
var FilterCount int64

// The primary command object
var rootCmd *cobra.Command

// The progress bar (only used when running non-verbosely)
var progressBar *pb.ProgressBar

func init() {
	// Create the primary command object
	rootCmd = &cobra.Command{
		Use:   "github-release-cleaner",
		Short: "Automated GitHub Release cleanup",
		Long:  `Provider an easy and customizable way to run automatic GitHub Release cleanup`,
		Run: func(cmd *cobra.Command, args []string) {
			/*if Verbose {
				log.Println("Running command with", "Arguments:", args, "-", "Verbose:", Verbose, "-", "Dry Run:", DryRun, "-", "Repository:", Repository, "-", "Token:", Token)
			}*/

			// Track progress bar state
			progressEnabled := !Verbose

			// Keep track of filter state
			filterDaysEnabled := FilterDays != -1
			filterCountEnabled := FilterCount != -1

			// Validate that at least one filter is being used
			if !filterDaysEnabled && !filterCountEnabled {
				err := errors.New("missing at least one filter flag (run with --help for more information)")
				fmt.Println("Error:", err)
				os.Exit(1)
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
			client, err := ghapi.NewGitHub(Token)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}

			// Notify the user
			if !Verbose {
				fmt.Println("\nFetching releases, please wait..")
			}

			// Fetch all releases for the repository
			releases, err := client.GetReleases(owner, repo)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}

			if Verbose {
				log.Println("Found", len(releases), "releases total")
			}

			if DryRun && Verbose {
				log.Println("Dry run detected, simulating cleanup")
			}

			// Notify the user
			if !Verbose {
				fmt.Printf("Found %d release(s) total, applying filters..\n", len(releases))
			}

			// Create a new array of releases that need cleanup
			cleanupReleases := make([]*github.RepositoryRelease, 0)

			// Loop through releases and check them against any enabled filters (newest to oldest)
			for count, release := range releases {
				// Parse the number of days since release (rounded up)
				daysSinceRelease := int64(math.Round(time.Since(release.CreatedAt.Time).Hours() / 24))

				// log.Println("Checking release", release.CreatedAt)

				// Keep track of already added releases
				alreadyAdded := false

				// Apply the count based filter
				if filterCountEnabled {
					if !alreadyAdded {
						if int64(count+1) > FilterCount {
							if Verbose {
								log.Println("Release created at", release.CreatedAt, "falls outside of count filter by", int64(count+1)-FilterCount, "release(s)")
							}
							cleanupReleases = append(cleanupReleases, release)
							alreadyAdded = true
						}
					}
				}

				// Apply the day based filter
				if filterDaysEnabled {
					if !alreadyAdded {
						if daysSinceRelease > FilterDays {
							if Verbose {
								log.Println("Release created at", release.CreatedAt, "falls outside of day filter by", daysSinceRelease-FilterDays, "day(s)")
							}
							cleanupReleases = append(cleanupReleases, release)
							alreadyAdded = true
						}
					}
				}
			}

			// Notify the user
			if !Verbose {
				if !DryRun {
					fmt.Printf("Found %d release(s) matching the filters, starting cleanup..\n\n", len(cleanupReleases))
				} else {
					fmt.Printf("Found %d release(s) matching the filters, starting simulated cleanup..\n\n", len(cleanupReleases))
				}
			}

			// Create a new progress bar based on the total cleanup release count
			if progressEnabled && len(cleanupReleases) > 0 {
				progressBar = pb.StartNew(len(cleanupReleases))
			}

			if Verbose {
				log.Println("Found", len(cleanupReleases), "releases that match cleanup filters")
			}

			// Run the actual cleanup process
			for _, release := range cleanupReleases {
				if Verbose {
					log.Println("Cleaning up release at", release.CreatedAt)
				}

				// Remove the release
				if !DryRun {
					// If an error occurs, we'll simply log it and move on to the next one
					err := client.RemoveRelease(owner, repo, release)
					if err != nil {
						fmt.Println("Error deleting release:", err)
						//os.Exit(1)
					} else {
						if Verbose {
							log.Println("Successfully deleted release at", release.CreatedAt)
						}
					}
				} else {
					if Verbose {
						log.Println("Dry run enabled, simulating cleanup")
					}
					time.Sleep(time.Duration(100) * time.Millisecond)
				}

				// Increment the progress bar
				if progressEnabled && progressBar != nil {
					progressBar.Increment()
				}
			}

			// Mark the progress bar as done
			if progressEnabled && progressBar != nil {
				progressBar.FinishPrint("\nSuccessfully cleaned up " + strconv.Itoa(len(cleanupReleases)) + " release(s)!")
			}
		},
	}

	// Add the "verbose" flag globally, so it's available for all commands
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")

	// Add the "dry-run" flag globally, so it's available for all commands
	rootCmd.PersistentFlags().BoolVarP(&DryRun, "dry-run", "D", false, "dry run (simulate)")

	// Add the "repository" flag and mark it as always required
	rootCmd.Flags().StringVarP(&Repository, "repository", "r", "", "GitHub Repository (required, short format only, eg. user/repo)")
	rootCmd.MarkFlagRequired("repository")

	// Add the "token" flag and mark it as always required
	rootCmd.Flags().StringVarP(&Token, "token", "t", "", "GitHub API Token (required)")
	rootCmd.MarkFlagRequired("token")

	// Add the "filter-days" flag
	rootCmd.Flags().Int64VarP(&FilterDays, "filter-days", "d", -1, "Filter based on maximum days since release (required)")

	// Add the "filter-count" flag
	rootCmd.Flags().Int64VarP(&FilterCount, "filter-count", "c", -1, "Filter to cleanup releases over the set amount (required)")
}

// Execute starts the Cobra commander, which in turn will handle execution and any arguments.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
