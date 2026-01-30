package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
	gherrors "github.com/Didstopia/githubby/internal/errors"
	gitpkg "github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/schedule"
	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/sync"
)

var (
	syncUser           string
	syncOrg            string
	syncTarget         string
	syncIncludePrivate bool
	syncInclude        []string
	syncExclude        []string
	syncSchedule       string
	syncProfile        string
	syncAllProfiles    bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync GitHub repositories locally",
	Long: `Sync GitHub repositories to a local directory.

Clone repositories that don't exist locally and pull updates for existing ones.
Supports Git LFS with automatic detection and configuration.

Examples:
  # Sync all repositories for a user
  githubby sync --user <username> --target ~/repos

  # Sync all repositories for an organization
  githubby sync --org <orgname> --target ~/repos

  # Include private repositories
  githubby sync --user <username> --target ~/repos --include-private

  # Filter repositories
  githubby sync --user <username> --target ~/repos --include "myproject-*" --exclude "archive-*"

  # Sync using a saved profile
  githubby sync --profile "my-profile"

  # Sync all saved profiles
  githubby sync --all-profiles

  # Schedule recurring sync (cron syntax)
  githubby sync --user <username> --target ~/repos --schedule "0 */6 * * *"

  # Schedule profile-based sync
  githubby sync --all-profiles --schedule "@every 30m"

  # Dry run
  githubby sync --user <username> --target ~/repos --dry-run`,
	RunE: runSync,
}

func init() {
	// User/Org flags
	syncCmd.Flags().StringVarP(&syncUser, "user", "u", "", "GitHub username to sync repositories from")
	syncCmd.Flags().StringVarP(&syncOrg, "org", "o", "", "GitHub organization to sync repositories from")

	// Target directory
	syncCmd.Flags().StringVarP(&syncTarget, "target", "T", "", "Target directory for synced repositories")

	// Include/exclude options
	syncCmd.Flags().BoolVarP(&syncIncludePrivate, "include-private", "p", false, "Include private repositories")
	syncCmd.Flags().StringSliceVarP(&syncInclude, "include", "i", nil, "Include repositories matching pattern (glob-style)")
	syncCmd.Flags().StringSliceVarP(&syncExclude, "exclude", "e", nil, "Exclude repositories matching pattern (glob-style)")

	// Profile flags
	syncCmd.Flags().StringVar(&syncProfile, "profile", "", "Sync using a saved profile")
	syncCmd.Flags().BoolVar(&syncAllProfiles, "all-profiles", false, "Sync all saved profiles")

	// Schedule flag
	syncCmd.Flags().StringVar(&syncSchedule, "schedule", "", "Cron expression for recurring sync (e.g., \"0 */6 * * *\", \"@every 30m\")")

	// Mutual exclusivity
	syncCmd.MarkFlagsMutuallyExclusive("profile", "all-profiles")
	syncCmd.MarkFlagsMutuallyExclusive("profile", "user")
	syncCmd.MarkFlagsMutuallyExclusive("profile", "org")
	syncCmd.MarkFlagsMutuallyExclusive("all-profiles", "user")
	syncCmd.MarkFlagsMutuallyExclusive("all-profiles", "org")

	// Add to root
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Validate schedule spec early if provided
	if syncSchedule != "" {
		if err := schedule.ValidateSpec(syncSchedule); err != nil {
			return err
		}
	}

	// Dispatch based on mode
	if syncProfile != "" || syncAllProfiles {
		return runProfileSync(ctx)
	}

	// Flag-based mode: require --target and --user/--org
	if syncTarget == "" {
		return fmt.Errorf("--target is required (or use --profile/--all-profiles)")
	}
	if syncUser == "" && syncOrg == "" {
		return fmt.Errorf("either --user or --org must be specified (or use --profile/--all-profiles)")
	}
	if syncUser != "" && syncOrg != "" {
		return fmt.Errorf("only one of --user or --org can be specified")
	}

	if syncSchedule != "" {
		return runScheduled(ctx, func(ctx context.Context) error {
			return executeSyncWithFlags(ctx)
		})
	}

	return executeSyncWithFlags(ctx)
}

// runProfileSync handles --profile and --all-profiles modes
func runProfileSync(ctx context.Context) error {
	storage, err := state.NewStorage()
	if err != nil {
		return fmt.Errorf("failed to initialize state storage: %w", err)
	}
	if err := storage.Load(); err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	var profiles []*state.SyncProfile

	if syncAllProfiles {
		profiles = storage.GetProfiles()
		if len(profiles) == 0 {
			return fmt.Errorf("no sync profiles found; create one using the interactive TUI first")
		}
		fmt.Printf("Syncing %d profile(s)\n", len(profiles))
	} else {
		profile := storage.GetProfileByName(syncProfile)
		if profile == nil {
			// List available profiles in error message
			available := storage.GetProfiles()
			if len(available) == 0 {
				return fmt.Errorf("profile %q not found; no profiles exist yet", syncProfile)
			}
			names := make([]string, len(available))
			for i, p := range available {
				names[i] = p.Name
			}
			return fmt.Errorf("profile %q not found; available profiles: %s", syncProfile, strings.Join(names, ", "))
		}
		profiles = []*state.SyncProfile{profile}
	}

	if syncSchedule != "" {
		return runScheduled(ctx, func(ctx context.Context) error {
			return executeSyncForProfiles(ctx, profiles, storage)
		})
	}

	return executeSyncForProfiles(ctx, profiles, storage)
}

// executeSyncForProfiles runs sync for each profile, continuing on per-profile errors
func executeSyncForProfiles(ctx context.Context, profiles []*state.SyncProfile, storage *state.Storage) error {
	var lastErr error

	for _, profile := range profiles {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		fmt.Printf("\nSyncing profile %q (%s: %s -> %s)\n", profile.Name, profile.Type, profile.Source, profile.TargetDir)

		err := executeSyncForProfile(ctx, profile)
		if err != nil {
			log.Warnf("Profile %q sync failed: %v", profile.Name, err)
			lastErr = err
			continue
		}

		// Update last sync time
		if err := storage.UpdateProfileLastSync(profile.ID); err != nil {
			log.Warnf("Failed to update last sync time for profile %q: %v", profile.Name, err)
		}
	}

	return lastErr
}

// executeSyncForProfile runs sync for a single profile
func executeSyncForProfile(ctx context.Context, profile *state.SyncProfile) error {
	// Resolve token
	resolvedToken, err := auth.GetToken(ctx, token, "")
	if err != nil || resolvedToken.Token == "" {
		return gherrors.NewAuthError()
	}
	authToken := resolvedToken.Token

	// Initialize git
	git, err := gitpkg.New()
	if err != nil {
		return fmt.Errorf("git initialization failed: %w", err)
	}

	// Create GitHub client
	ghClient := github.NewClient(authToken)

	// Build sync options from profile
	opts := &sync.Options{
		Target:         profile.TargetDir,
		Include:        profile.IncludeFilter,
		Exclude:        profile.ExcludeFilter,
		IncludePrivate: profile.IncludePrivate,
		DryRun:         dryRun,
		Verbose:        verbose,
	}

	syncer := sync.New(ghClient, git, opts)

	var result *sync.Result
	var syncErr error

	switch profile.Type {
	case "user":
		fmt.Printf("Syncing repositories for user: %s\n", profile.Source)
		result, syncErr = syncer.SyncUserRepos(ctx, profile.Source)
	case "org":
		fmt.Printf("Syncing repositories for organization: %s\n", profile.Source)
		result, syncErr = syncer.SyncOrgRepos(ctx, profile.Source)
	default:
		return fmt.Errorf("unknown profile type: %s", profile.Type)
	}

	printSyncSummary(result)

	if syncErr != nil && (gherrors.IsUnauthorized(syncErr) || gherrors.IsForbidden(syncErr)) {
		return gherrors.NewExpiredTokenError(auth.FormatTokenSource(resolvedToken.Source))
	}

	return syncErr
}

// executeSyncWithFlags runs sync using CLI flag values (existing behavior)
func executeSyncWithFlags(ctx context.Context) error {
	// Get token using auth resolution (flag > env > stored)
	resolvedToken, err := auth.GetToken(ctx, token, "")
	if err != nil || resolvedToken.Token == "" {
		return gherrors.NewAuthError()
	}
	authToken := resolvedToken.Token

	// Initialize git
	git, err := gitpkg.New()
	if err != nil {
		return fmt.Errorf("git initialization failed: %w", err)
	}

	// Create GitHub client
	ghClient := github.NewClient(authToken)

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

	if syncErr != nil && (gherrors.IsUnauthorized(syncErr) || gherrors.IsForbidden(syncErr)) {
		return gherrors.NewExpiredTokenError(auth.FormatTokenSource(resolvedToken.Source))
	}

	return syncErr
}

// runScheduled wraps a sync function in a cron scheduler
func runScheduled(ctx context.Context, syncFn func(ctx context.Context) error) error {
	fmt.Printf("Starting scheduled sync with schedule: %s\n", syncSchedule)
	fmt.Println("Press Ctrl+C to stop")

	scheduler, err := schedule.New(syncSchedule, syncFn)
	if err != nil {
		return err
	}

	return scheduler.Run(ctx)
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

	if len(result.Archived) > 0 {
		fmt.Printf("\nArchived (%d) - preserved locally, no longer on remote:\n", len(result.Archived))
		for _, repo := range result.Archived {
			fmt.Printf("  - %s\n", repo)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
	summary := fmt.Sprintf("Total: %d cloned, %d updated, %d skipped, %d failed",
		len(result.Cloned), len(result.Updated), len(result.Skipped), len(result.Failed))
	if len(result.Archived) > 0 {
		summary += fmt.Sprintf(", %d archived", len(result.Archived))
	}
	fmt.Println(summary)
}
