package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	gherrors "github.com/Didstopia/githubby/internal/errors"
	gitpkg "github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/sync"
)

var (
	syncUser           string
	syncOrg            string
	syncTarget         string
	syncIncludePrivate bool
	syncInclude        []string
	syncExclude        []string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync GitHub repositories locally",
	Long: `Sync GitHub repositories to a local directory.

Clone repositories that don't exist locally and pull updates for existing ones.
Supports Git LFS with automatic detection and configuration.

Examples:
  # Sync all repositories for a user
  githubby sync --token <token> --user <username> --target ~/repos

  # Sync all repositories for an organization
  githubby sync --token <token> --org <orgname> --target ~/repos

  # Include private repositories
  githubby sync --token <token> --user <username> --target ~/repos --include-private

  # Filter repositories
  githubby sync --token <token> --user <username> --target ~/repos --include "myproject-*" --exclude "archive-*"

  # Dry run
  githubby sync --token <token> --user <username> --target ~/repos --dry-run`,
	RunE: runSync,
}

func init() {
	// User/Org flags
	syncCmd.Flags().StringVarP(&syncUser, "user", "u", "", "GitHub username to sync repositories from")
	syncCmd.Flags().StringVarP(&syncOrg, "org", "o", "", "GitHub organization to sync repositories from")

	// Target directory
	syncCmd.Flags().StringVarP(&syncTarget, "target", "T", "", "Target directory for synced repositories (required)")
	syncCmd.MarkFlagRequired("target")

	// Include/exclude options
	syncCmd.Flags().BoolVarP(&syncIncludePrivate, "include-private", "p", false, "Include private repositories")
	syncCmd.Flags().StringSliceVarP(&syncInclude, "include", "i", nil, "Include repositories matching pattern (glob-style)")
	syncCmd.Flags().StringSliceVarP(&syncExclude, "exclude", "e", nil, "Exclude repositories matching pattern (glob-style)")

	// Add to root
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Validate that either user or org is specified
	if syncUser == "" && syncOrg == "" {
		return fmt.Errorf("either --user or --org must be specified")
	}

	if syncUser != "" && syncOrg != "" {
		return fmt.Errorf("only one of --user or --org can be specified")
	}

	// Validate token
	if token == "" {
		return gherrors.ErrMissingToken
	}

	// Initialize git
	git, err := gitpkg.New()
	if err != nil {
		return fmt.Errorf("git initialization failed: %w", err)
	}

	// Create GitHub client
	ghClient := github.NewClient(token)

	// Create sync options
	opts := &sync.Options{
		Target:         syncTarget,
		Include:        syncInclude,
		Exclude:        syncExclude,
		IncludePrivate: syncIncludePrivate,
		DryRun:         dryRun,
		Verbose:        verbose,
	}

	// Create syncer
	syncer := sync.New(ghClient, git, opts)

	// Determine what to sync
	var result *sync.Result
	var syncErr error

	if syncUser != "" {
		fmt.Printf("Syncing repositories for user: %s\n", syncUser)
		result, syncErr = syncer.SyncUserRepos(ctx, syncUser)
	} else {
		fmt.Printf("Syncing repositories for organization: %s\n", syncOrg)
		result, syncErr = syncer.SyncOrgRepos(ctx, syncOrg)
	}

	// Print summary
	printSyncSummary(result)

	return syncErr
}

func printSyncSummary(result *sync.Result) {
	if result == nil {
		return
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("Sync Summary")
	fmt.Println(strings.Repeat("=", 50))

	if len(result.Cloned) > 0 {
		fmt.Printf("\nCloned (%d):\n", len(result.Cloned))
		for _, repo := range result.Cloned {
			fmt.Printf("  - %s\n", repo)
		}
	}

	if len(result.Updated) > 0 {
		fmt.Printf("\nUpdated (%d):\n", len(result.Updated))
		for _, repo := range result.Updated {
			fmt.Printf("  - %s\n", repo)
		}
	}

	if len(result.Skipped) > 0 {
		fmt.Printf("\nSkipped (%d):\n", len(result.Skipped))
		for _, repo := range result.Skipped {
			fmt.Printf("  - %s\n", repo)
		}
	}

	if len(result.Failed) > 0 {
		fmt.Printf("\nFailed (%d):\n", len(result.Failed))
		for repo, err := range result.Failed {
			fmt.Printf("  - %s: %v\n", repo, err)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Total: %d cloned, %d updated, %d skipped, %d failed\n",
		len(result.Cloned), len(result.Updated), len(result.Skipped), len(result.Failed))
}
