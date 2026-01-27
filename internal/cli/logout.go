package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
)

var logoutHostname string

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out of GitHub",
	Long: `Remove authentication credentials for GitHub.

This removes the stored token from your system keychain or config file.

Examples:
  # Log out of github.com
  githubby logout

  # Log out of GitHub Enterprise Server
  githubby logout --hostname github.mycompany.com`,
	RunE: runLogout,
}

func init() {
	logoutCmd.Flags().StringVar(&logoutHostname, "hostname", auth.DefaultHostname, "GitHub hostname (for GitHub Enterprise)")

	rootCmd.AddCommand(logoutCmd)
}

func runLogout(cmd *cobra.Command, args []string) error {
	storage := auth.NewStorage()

	// Check if there's a token to remove
	existingToken, _, err := storage.GetToken(logoutHostname)
	if err != nil || existingToken == "" {
		fmt.Printf("Not logged in to %s\n", logoutHostname)
		return nil
	}

	// Remove the token
	if err := storage.DeleteToken(logoutHostname); err != nil {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	fmt.Printf("✓ Logged out of %s\n", logoutHostname)
	return nil
}
