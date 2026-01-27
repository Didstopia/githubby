package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cli/oauth/device"
	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
)

var (
	loginWithToken bool
	loginHostname  string
	loginScopes    []string
	loginNoPrompt  bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with GitHub",
	Long: `Authenticate with GitHub using OAuth device flow or a personal access token.

The device flow opens your browser to complete authentication - no need to create
or copy tokens manually.

Examples:
  # Interactive login (recommended)
  githubby login

  # Login with a personal access token (for automation)
  echo "ghp_xxxx" | githubby login --with-token

  # Login to GitHub Enterprise Server
  githubby login --hostname github.mycompany.com

  # Login without browser prompt
  githubby login --no-prompt`,
	RunE: runLogin,
}

func init() {
	loginCmd.Flags().BoolVar(&loginWithToken, "with-token", false, "Read token from stdin")
	loginCmd.Flags().StringVar(&loginHostname, "hostname", auth.DefaultHostname, "GitHub hostname (for GitHub Enterprise)")
	loginCmd.Flags().StringSliceVar(&loginScopes, "scopes", []string{"repo", "read:org"}, "OAuth scopes to request")
	loginCmd.Flags().BoolVar(&loginNoPrompt, "no-prompt", false, "Skip browser open prompt")

	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Check if user wants to provide token via stdin
	if loginWithToken {
		return loginWithTokenFromStdin(ctx)
	}

	// Otherwise, use device flow
	return loginWithDeviceFlow(ctx)
}

func loginWithTokenFromStdin(ctx context.Context) error {
	// Read token from stdin
	reader := bufio.NewReader(os.Stdin)
	token, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read token from stdin: %w", err)
	}
	token = strings.TrimSpace(token)

	if token == "" {
		return fmt.Errorf("no token provided")
	}

	// Validate token
	fmt.Println("Validating token...")
	user, err := auth.ValidateToken(ctx, token, loginHostname)
	if err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}

	// Store token
	storage := auth.NewStorage()
	if err := storage.SetToken(loginHostname, token); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	fmt.Printf("✓ Logged in as %s\n", user.Login)
	fmt.Printf("✓ Token stored in %s\n", storage.GetStorageLocation())

	return nil
}

func loginWithDeviceFlow(ctx context.Context) error {
	fmt.Printf("Logging in to %s...\n\n", loginHostname)

	opts := &auth.DeviceFlowOptions{
		ClientID: auth.GitHubOAuthClientID,
		Scopes:   loginScopes,
		Hostname: loginHostname,
		OnCode: func(code *device.CodeResponse) {
			instructions := auth.GetDeviceFlowInstructions(code, loginHostname)
			fmt.Println(instructions)

			// Wait for user to press Enter (unless --no-prompt)
			if !loginNoPrompt {
				reader := bufio.NewReader(os.Stdin)
				_, _ = reader.ReadString('\n')
			}

			// Open browser
			verificationURL := code.VerificationURI
			if verificationURL == "" {
				verificationURL = "https://github.com/login/device"
			}
			if err := auth.OpenBrowser(verificationURL); err != nil {
				fmt.Printf("Failed to open browser: %v\n", err)
				fmt.Printf("Please open %s manually\n", verificationURL)
			}
		},
		OnPoll: func(interval time.Duration) {
			// Show waiting message
			fmt.Print(".")
		},
	}

	result, err := auth.RunDeviceFlow(ctx, opts)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println() // New line after polling dots

	// Validate the token to get user info
	user, err := auth.ValidateToken(ctx, result.Token, loginHostname)
	if err != nil {
		return fmt.Errorf("failed to validate token: %w", err)
	}

	// Store the token
	storage := auth.NewStorage()
	if err := storage.SetToken(loginHostname, result.Token); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Authentication complete.")
	fmt.Printf("✓ Logged in as %s\n", user.Login)
	fmt.Printf("✓ Token stored in %s\n", storage.GetStorageLocation())

	return nil
}
