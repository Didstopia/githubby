package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, commit, and build date of GitHubby.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("githubby %s\n", Version)
		fmt.Printf("  commit: %s\n", Commit)
		fmt.Printf("  built:  %s\n", BuildDate)
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
