package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
)

var authStatusHostname string

// authCmd is the parent command for auth subcommands
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage GitHub authentication",
	Long:  `Manage GitHub authentication, including viewing status and credentials.`,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "View authentication status",
	Long: `Display the authentication status for GitHub.

Shows the logged-in user and token source (keychain, config file, etc.).

Examples:
  # Check authentication status
  githubby auth status

  # Check status for GitHub Enterprise Server
  githubby auth status --hostname github.mycompany.com`,
	RunE: runAuthStatus,
}

func init() {
	authStatusCmd.Flags().StringVar(&authStatusHostname, "hostname", auth.DefaultHostname, "GitHub hostname (for GitHub Enterprise)")

	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Try to get token
	result, err := auth.GetToken(ctx, "", authStatusHostname)
	if err != nil || result.Token == "" {
		fmt.Printf("✗ Not logged in to %s\n", authStatusHostname)
		fmt.Println()
		fmt.Println("To log in, run:")
		fmt.Println("  githubby login")
		return nil
	}

	// Validate token and get user info
	user, err := auth.ValidateToken(ctx, result.Token, result.Hostname)
	if err != nil {
		fmt.Printf("✗ Logged in to %s but token is invalid or expired\n", authStatusHostname)
		fmt.Printf("  Token: %s\n", auth.MaskToken(result.Token))
		fmt.Printf("  Source: %s\n", auth.FormatTokenSource(result.Source))
		fmt.Println()
		fmt.Println("To re-authenticate, run:")
		fmt.Println("  githubby login")
		return nil
	}

	// Display status
	fmt.Printf("✓ Logged in to %s as %s\n", authStatusHostname, user.Login)
	fmt.Printf("  Token: %s\n", auth.MaskToken(result.Token))
	fmt.Printf("  Token source: %s\n", auth.FormatTokenSource(result.Source))

	if user.Name != "" {
		fmt.Printf("  Name: %s\n", user.Name)
	}
	if user.Email != "" {
		fmt.Printf("  Email: %s\n", user.Email)
	}

	return nil
}
