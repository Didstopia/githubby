package cli

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v86/github"
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
	syncAllOrgs        bool
	syncGists          bool
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

  # Sync your own repositories plus every organization you belong to
  githubby sync --all-orgs --target ~/repos --include-private

  # Also sync your gists
  githubby sync --all-orgs --target ~/repos --include-private --gists

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
	syncCmd.Flags().BoolVar(&syncAllOrgs, "all-orgs", false, "Sync your own repositories plus every organization you belong to")
	syncCmd.Flags().BoolVar(&syncGists, "gists", false, "Also sync your gists (secret ones too) into <target>/<you>/gists")

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
	syncCmd.MarkFlagsMutuallyExclusive("all-orgs", "org")
	syncCmd.MarkFlagsMutuallyExclusive("all-orgs", "profile")
	syncCmd.MarkFlagsMutuallyExclusive("all-orgs", "all-profiles")

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
	if syncUser == "" && syncOrg == "" && !syncAllOrgs {
		return fmt.Errorf("either --user, --org or --all-orgs must be specified (or use --profile/--all-profiles)")
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

	// Initialize git with the token so private repos clone too
	git, err := gitpkg.NewWithToken(authToken)
	if err != nil {
		return fmt.Errorf("git initialization failed: %w", err)
	}
	git.Host = resolvedToken.Hostname

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
		Submodules:     true,
		Wikis:          true,
	}

	// Profiles that picked specific repos sync only those
	useSelected := !profile.SyncAllRepos && len(profile.SelectedRepos) > 0
	if useSelected {
		// Other local repos aren't "archived", just not selected
		opts.SkipArchiveDetection = true
	}

	syncer := sync.New(ghClient, git, opts)

	var result *sync.Result
	var syncErr error

	switch {
	case useSelected:
		fmt.Printf("Syncing %d selected repositories\n", len(profile.SelectedRepos))
		result, syncErr = syncer.SyncSelectedRepos(ctx, profile.SelectedRepos)
	case profile.Type == "user":
		fmt.Printf("Syncing repositories for user: %s\n", profile.Source)
		result, syncErr = syncer.SyncUserRepos(ctx, profile.Source)
	case profile.Type == "org":
		fmt.Printf("Syncing repositories for organization: %s\n", profile.Source)
		result, syncErr = syncer.SyncOrgRepos(ctx, profile.Source)
	default:
		return fmt.Errorf("unknown profile type: %s", profile.Type)
	}

	printSyncSummary(result)

	if syncErr != nil && (gherrors.IsUnauthorized(syncErr) || gherrors.IsForbidden(syncErr)) {
		return gherrors.NewExpiredTokenError(auth.FormatTokenSource(resolvedToken.Source))
	}
	if syncErr != nil {
		return syncErr
	}

	return failedError(result)
}

// executeSyncWithFlags runs sync using CLI flag values (existing behavior)
func executeSyncWithFlags(ctx context.Context) error {
	// Get token using auth resolution (flag > env > stored)
	resolvedToken, err := auth.GetToken(ctx, token, "")
	if err != nil || resolvedToken.Token == "" {
		return gherrors.NewAuthError()
	}
	authToken := resolvedToken.Token

	// Initialize git with the token so private repos clone too
	git, err := gitpkg.NewWithToken(authToken)
	if err != nil {
		return fmt.Errorf("git initialization failed: %w", err)
	}
	git.Host = resolvedToken.Hostname

	// Create GitHub client
	ghClient := github.NewClient(authToken)

	result, syncErr := runFlagSync(ctx, ghClient, git)

	// Print summary
	printSyncSummary(result)

	if syncErr != nil && (gherrors.IsUnauthorized(syncErr) || gherrors.IsForbidden(syncErr)) {
		return gherrors.NewExpiredTokenError(auth.FormatTokenSource(resolvedToken.Source))
	}
	if syncErr != nil {
		return syncErr
	}

	return failedError(result)
}

// runFlagSync syncs everything the flags ask for: repos, then gists
func runFlagSync(ctx context.Context, ghClient github.Client, git *gitpkg.Git) (*sync.Result, error) {
	// Create sync options
	opts := &sync.Options{
		Target:         syncTarget,
		Include:        syncInclude,
		Exclude:        syncExclude,
		IncludePrivate: syncIncludePrivate,
		DryRun:         dryRun,
		Verbose:        verbose,
		Submodules:     true,
		Wikis:          true,
	}

	// Create syncer
	syncer := sync.New(ghClient, git, opts)

	result, syncErr := syncListedRepos(ctx, ghClient, syncer)

	// Gists sync on their own, even when the repos could not be listed
	if syncGists {
		if result == nil {
			result = sync.NewResult()
		}
		if err := syncer.SyncGists(ctx, result); err != nil {
			result.Failed["gists"] = err
		}
	}

	return result, syncErr
}

// syncListedRepos lists the repos the flags ask for and syncs them
func syncListedRepos(ctx context.Context, ghClient github.Client, syncer *sync.Syncer) (*sync.Result, error) {
	listOpts := &github.ListOptions{IncludePrivate: syncIncludePrivate}
	listFailed := map[string]error{}

	var repos []*gh.Repository
	var err error
	switch {
	case syncAllOrgs:
		fmt.Println("Syncing repositories for your account and all its organizations")
		repos, listFailed, err = syncer.ListAccountRepos(ctx, syncUser)
	case syncUser != "":
		fmt.Printf("Syncing repositories for user: %s\n", syncUser)
		repos, err = ghClient.ListUserRepos(ctx, syncUser, listOpts)
	default:
		fmt.Printf("Syncing repositories for organization: %s\n", syncOrg)
		repos, err = ghClient.ListOrgRepos(ctx, syncOrg, listOpts)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list repos: %w", err)
	}

	result, err := syncer.SyncRepos(ctx, repos)
	if result != nil {
		for name, ferr := range listFailed {
			result.Failed[name] = ferr
		}
	}
	return result, err
}

// failedError turns any failed item in a sync result into an error, for the exit code
func failedError(result *sync.Result) error {
	if result == nil || len(result.Failed) == 0 {
		return nil
	}
	return fmt.Errorf("%d item(s) failed to sync", len(result.Failed))
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

	if len(result.Wikis) > 0 {
		fmt.Printf("\nWikis (%d):\n", len(result.Wikis))
		for _, wiki := range result.Wikis {
			fmt.Printf("  - %s\n", wiki)
		}
	}

	if len(result.Gists) > 0 {
		fmt.Printf("\nGists (%d):\n", len(result.Gists))
		for _, gist := range result.Gists {
			fmt.Printf("  - %s\n", gist)
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

	if len(result.Warnings) > 0 {
		fmt.Printf("\nWarnings (%d):\n", len(result.Warnings))
		for _, w := range result.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
	summary := fmt.Sprintf("Total: %d cloned, %d updated, %d skipped, %d failed",
		len(result.Cloned), len(result.Updated), len(result.Skipped), len(result.Failed))
	if len(result.Archived) > 0 {
		summary += fmt.Sprintf(", %d archived", len(result.Archived))
	}
	if len(result.Warnings) > 0 {
		summary += fmt.Sprintf(", %d warnings", len(result.Warnings))
	}
	fmt.Println(summary)
}
