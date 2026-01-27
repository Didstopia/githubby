package cli

import (
	"fmt"
	"math"
	"os"
	"time"

	gh "github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"gopkg.in/cheggaaa/pb.v3"

	gherrors "github.com/Didstopia/githubby/internal/errors"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/pkg/util"
)

var (
	filterDays  int64
	filterCount int64
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Filter and remove GitHub Releases",
	Long: `Use one or more filters to remove GitHub Releases.

Examples:
  githubby clean --token <token> --repository owner/repo --filter-days 30
  githubby clean --token <token> --repository owner/repo --filter-count 10
  githubby clean --token <token> --repository owner/repo --filter-days 30 --dry-run`,
	RunE: runClean,
}

func init() {
	// Repository flag
	cleanCmd.Flags().StringVarP(&repository, "repository", "r", "", "GitHub Repository (required, format: owner/repo)")
	cleanCmd.MarkFlagRequired("repository")

	// Filter flags
	cleanCmd.Flags().Int64VarP(&filterDays, "filter-days", "d", -1, "Filter releases older than N days")
	cleanCmd.Flags().Int64VarP(&filterCount, "filter-count", "c", -1, "Keep only the N most recent releases")

	// Add to root
	rootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Track filter state
	filterDaysEnabled := filterDays != -1
	filterCountEnabled := filterCount != -1

	// Validate that at least one filter is used
	if !filterDaysEnabled && !filterCountEnabled {
		return gherrors.ErrMissingFilter
	}

	// Validate token
	if token == "" {
		return gherrors.ErrMissingToken
	}

	// Validate repository
	owner, repo, err := util.ValidateGitHubRepository(repository)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Validation succeeded for repository %s/%s\n", owner, repo)
	}

	// Create GitHub client
	client := github.NewClient(token)

	// Notify user
	if !verbose {
		fmt.Println("\nFetching releases, please wait...")
	}

	// Fetch all releases
	releases, err := client.GetReleases(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("failed to fetch releases: %w", err)
	}

	if verbose {
		fmt.Printf("Found %d releases total\n", len(releases))
	}

	if dryRun && verbose {
		fmt.Println("Dry run detected, simulating cleanup")
	}

	if !verbose {
		fmt.Printf("Found %d release(s) total, applying filters...\n", len(releases))
	}

	// Filter releases for cleanup
	cleanupReleases := filterReleases(releases, filterDaysEnabled, filterCountEnabled)

	// Notify user
	if !verbose {
		if !dryRun {
			fmt.Printf("Found %d release(s) matching the filters, starting cleanup...\n\n", len(cleanupReleases))
		} else {
			fmt.Printf("Found %d release(s) matching the filters, starting simulated cleanup...\n\n", len(cleanupReleases))
		}
	}

	if verbose {
		fmt.Printf("Found %d releases that match cleanup filters\n", len(cleanupReleases))
	}

	// Create progress bar
	var progressBar *pb.ProgressBar
	progressEnabled := !verbose && len(cleanupReleases) > 0
	if progressEnabled {
		progressBar = pb.New(len(cleanupReleases))
		progressBar.Start()
	}

	// Run cleanup
	deletedCount := 0
	for _, release := range cleanupReleases {
		select {
		case <-ctx.Done():
			if progressBar != nil {
				progressBar.Finish()
			}
			return ctx.Err()
		default:
		}

		if verbose {
			fmt.Printf("Cleaning up release at %v\n", release.GetCreatedAt())
		}

		if !dryRun {
			if err := client.RemoveRelease(ctx, owner, repo, release); err != nil {
				fmt.Printf("Error deleting release: %v\n", err)
			} else {
				deletedCount++
				if verbose {
					fmt.Printf("Successfully deleted release at %v\n", release.GetCreatedAt())
				}
			}
		} else {
			if verbose {
				fmt.Println("Dry run enabled, simulating cleanup")
			}
			time.Sleep(100 * time.Millisecond)
			deletedCount++
		}

		if progressBar != nil {
			progressBar.Increment()
		}
	}

	// Finish progress bar
	if progressBar != nil {
		progressBar.Finish()
		fmt.Printf("\nSuccessfully cleaned up %d release(s)!\n", deletedCount)
	}

	return nil
}

func filterReleases(releases []*gh.RepositoryRelease, filterDaysEnabled, filterCountEnabled bool) []*gh.RepositoryRelease {
	var cleanupReleases []*gh.RepositoryRelease

	for count, release := range releases {
		// Parse days since release (rounded up to include partial days)
		daysSinceRelease := int64(math.Ceil(time.Since(release.GetCreatedAt().Time).Hours() / 24))

		alreadyAdded := false

		// Apply count filter
		if filterCountEnabled && !alreadyAdded {
			if int64(count+1) > filterCount {
				if verbose {
					fmt.Printf("Release at %v falls outside of count filter by %d release(s)\n",
						release.GetCreatedAt(), int64(count+1)-filterCount)
				}
				cleanupReleases = append(cleanupReleases, release)
				alreadyAdded = true
			}
		}

		// Apply days filter
		if filterDaysEnabled && !alreadyAdded {
			if daysSinceRelease > filterDays {
				if verbose {
					fmt.Printf("Release at %v falls outside of day filter by %d day(s)\n",
						release.GetCreatedAt(), daysSinceRelease-filterDays)
				}
				cleanupReleases = append(cleanupReleases, release)
			}
		}
	}

	return cleanupReleases
}
