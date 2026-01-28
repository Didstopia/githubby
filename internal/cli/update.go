package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/update"
)

var (
	checkOnly bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install updates",
	Long: `Check for new versions of GitHubby and optionally update to the latest version.

By default, this command will check for updates and prompt before installing.
Use --check to only check for updates without installing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Create updater with current version
		updater := update.NewUpdater(Version)

		// Check for updates
		fmt.Println("Checking for updates...")
		result, err := updater.CheckForUpdate(ctx)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		// Handle dev builds
		if Version == "" || Version == "dev" {
			fmt.Println("You're running a development build. Auto-update is disabled.")
			fmt.Println("To update, please build from source or download a release from:")
			fmt.Println("  https://github.com/Didstopia/githubby/releases")
			return nil
		}

		// No update available
		if !result.Available {
			fmt.Printf("You're running the latest version (v%s)\n", result.CurrentVersion)
			return nil
		}

		// Update available
		fmt.Printf("New version available: v%s -> v%s\n", result.CurrentVersion, result.LatestVersion)

		if result.ReleaseURL != "" {
			fmt.Printf("Release: %s\n", result.ReleaseURL)
		}

		// Check-only mode
		if checkOnly {
			fmt.Println("\nRun 'githubby update' to install the update.")
			return nil
		}

		// Perform update
		fmt.Println("\nDownloading and installing update...")
		updateResult, err := updater.Update(ctx)
		if err != nil {
			return fmt.Errorf("failed to update: %w", err)
		}

		fmt.Printf("\nSuccessfully updated to v%s!\n", updateResult.LatestVersion)
		fmt.Println("Please restart githubby to use the new version.")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
}
