package auth

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/cli/oauth/device"
)

const (
	// GitHubOAuthClientID is the OAuth App client ID for GitHubby
	// This is intentionally public - device flow doesn't require a secret
	// To register your own OAuth App: https://github.com/settings/developers
	GitHubOAuthClientID = "Ov23liwoBdsJgs7M7kLS"

	// DefaultScopes are the OAuth scopes requested during authentication
	// - repo: Full control of private repositories (needed for sync)
	// - read:org: Read org membership (needed for org sync)
	DefaultScopes = "repo read:org"
)

// DeviceFlowOptions configures the device flow authentication
type DeviceFlowOptions struct {
	// ClientID is the OAuth App client ID
	ClientID string
	// Scopes are the OAuth scopes to request
	Scopes []string
	// Hostname is the GitHub hostname (for Enterprise)
	Hostname string
	// OnCode is called when the user code is ready
	OnCode func(code *device.CodeResponse)
	// OnPoll is called during polling
	OnPoll func(interval time.Duration)
}

// DeviceFlowResult contains the result of a device flow authentication
type DeviceFlowResult struct {
	Token     string
	TokenType string
	Scopes    []string
}

// DefaultDeviceFlowOptions returns options with sensible defaults
func DefaultDeviceFlowOptions() *DeviceFlowOptions {
	return &DeviceFlowOptions{
		ClientID: GitHubOAuthClientID,
		Scopes:   []string{"repo", "read:org"},
		Hostname: DefaultHostname,
	}
}

// RunDeviceFlow performs OAuth device flow authentication
func RunDeviceFlow(ctx context.Context, opts *DeviceFlowOptions) (*DeviceFlowResult, error) {
	if opts == nil {
		opts = DefaultDeviceFlowOptions()
	}

	if opts.ClientID == "" {
		opts.ClientID = GitHubOAuthClientID
	}

	if len(opts.Scopes) == 0 {
		opts.Scopes = []string{"repo", "read:org"}
	}

	if opts.Hostname == "" {
		opts.Hostname = DefaultHostname
	}

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Determine the device code URL based on hostname
	codeURL := "https://github.com/login/device/code"
	if opts.Hostname != DefaultHostname {
		codeURL = fmt.Sprintf("https://%s/login/device/code", opts.Hostname)
	}

	// Request device code
	code, err := device.RequestCode(httpClient, codeURL, opts.ClientID, opts.Scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to request device code: %w", err)
	}

	// Notify caller about the code
	if opts.OnCode != nil {
		opts.OnCode(code)
	}

	// Determine the token URL
	tokenURL := "https://github.com/login/oauth/access_token"
	if opts.Hostname != DefaultHostname {
		tokenURL = fmt.Sprintf("https://%s/login/oauth/access_token", opts.Hostname)
	}

	// Poll for token using Wait function
	waitOpts := device.WaitOptions{
		ClientID:   opts.ClientID,
		DeviceCode: code,
	}

	accessToken, err := device.Wait(ctx, httpClient, tokenURL, waitOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to complete device flow: %w", err)
	}

	return &DeviceFlowResult{
		Token:     accessToken.Token,
		TokenType: accessToken.Type,
	}, nil
}

// OpenBrowser opens the default browser to the specified URL
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// GetDeviceFlowInstructions returns user-friendly instructions for device flow
func GetDeviceFlowInstructions(code *device.CodeResponse, hostname string) string {
	verificationURL := code.VerificationURI
	if verificationURL == "" {
		if hostname == "" || hostname == DefaultHostname {
			verificationURL = "https://github.com/login/device"
		} else {
			verificationURL = fmt.Sprintf("https://%s/login/device", hostname)
		}
	}

	return fmt.Sprintf(`! First, copy your one-time code: %s
Press Enter to open %s in your browser...`, code.UserCode, verificationURL)
}
