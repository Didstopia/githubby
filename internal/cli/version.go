package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, commit, and build date of GitHubby.`,
	Run: func(cmd *cobra.Command, args []string) {
		platform := runtime.GOOS + "/" + runtime.GOARCH

		if Version == "dev" {
			// Dev/manual build - show all details
			fmt.Printf("githubby %s (%s)\n", Version, platform)
			fmt.Printf("  commit: %s\n", Commit)
			fmt.Printf("  built:  %s\n", BuildDate)
		} else {
			// Production build - clean output
			fmt.Printf("githubby v%s (%s)\n", Version, platform)
			// Only show commit/date if verbose or they're meaningful
			if Commit != "unknown" && Commit != "" {
				fmt.Printf("  commit: %s\n", Commit)
			}
			if BuildDate != "unknown" && BuildDate != "" {
				fmt.Printf("  built:  %s\n", BuildDate)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// SetVersionInfo sets the version information (called from main)
func SetVersionInfo(version, commit, buildDate string) {
	if version != "" {
		Version = version
	}
	if commit != "" {
		Commit = commit
	}
	if buildDate != "" {
		BuildDate = buildDate
	}
}
