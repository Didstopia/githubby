package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/Didstopia/githubby/ghapi"
	"github.com/Didstopia/githubby/util"
	"github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"gopkg.in/cheggaaa/pb.v3"
)

// FilterDays sets the maximum amount of days since release
var FilterDays int64

// FilterCount sets the maximum amount of releases to keep
var FilterCount int64

// The progress bar (only used when running non-verbosely)
var progressBar *pb.ProgressBar

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Filter and remove GitHub Releases",
	Long:  `Use one or more filters to remove GitHub Releases`,
	Run: func(cmd *cobra.Command, args []string) {
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
			fmt.Println("Validation succeeded for repository", owner+"/"+repo)
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
			fmt.Println("Found", len(releases), "releases total")
		}

		if DryRun && Verbose {
			fmt.Println("Dry run detected, simulating cleanup")
		}

		// Notify the user
		if !Verbose {
			fmt.Printf("Found %d release(s) total, applying filters..\n", len(releases))
		}

		// Create a new array of releases that need cleanup
		cleanupReleases := make([]*github.RepositoryRelease, 0)

		// Loop through releases and check them against any enabled filters (newest to oldest)
		for count, release := range releases {
			// Parse the number of days since release (rounded up to include partial days)
			daysSinceRelease := int64(math.Ceil(time.Since(release.CreatedAt.Time).Hours() / 24))

			// fmt.Println("Checking release", release.CreatedAt)

			// Keep track of already added releases
			alreadyAdded := false

			// Apply the count based filter
			if filterCountEnabled {
				if !alreadyAdded {
					if int64(count+1) > FilterCount {
						if Verbose {
							fmt.Println("Release created at", release.CreatedAt, "falls outside of count filter by", int64(count+1)-FilterCount, "release(s)")
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
							fmt.Println("Release created at", release.CreatedAt, "falls outside of day filter by", daysSinceRelease-FilterDays, "day(s)")
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
			progressBar = pb.New(len(cleanupReleases))
			progressBar.Start()
		}

		if Verbose {
			fmt.Println("Found", len(cleanupReleases), "releases that match cleanup filters")
		}

		// Run the actual cleanup process
		for _, release := range cleanupReleases {
			if Verbose {
				fmt.Println("Cleaning up release at", release.CreatedAt)
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
						fmt.Println("Successfully deleted release at", release.CreatedAt)
					}
				}
			} else {
				if Verbose {
					fmt.Println("Dry run enabled, simulating cleanup")
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
			progressBar.Finish()
			fmt.Printf("\nSuccessfully cleaned up %d release(s)!\n", len(cleanupReleases))
		}
	},
}
