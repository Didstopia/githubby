// Package auth provides authentication functionality for GitHubby
package auth

import (
	"context"
	"fmt"
	"os"

	gh "github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

const (
	// DefaultHostname is the default GitHub hostname
	DefaultHostname = "github.com"

	// EnvGitHubToken is the environment variable for GitHub token
	EnvGitHubToken = "GITHUB_TOKEN"
)

// TokenSource represents where the token was obtained from
type TokenSource string

const (
	TokenSourceFlag     TokenSource = "flag"
	TokenSourceEnv      TokenSource = "environment"
	TokenSourceKeychain TokenSource = "keychain"
	TokenSourceConfig   TokenSource = "config"
	TokenSourceNone     TokenSource = "none"
)

// TokenResult contains the resolved token and its source
type TokenResult struct {
	Token    string
	Source   TokenSource
	Hostname string
}

// AuthenticatedUser contains information about the authenticated user
type AuthenticatedUser struct {
	Login     string
	Name      string
	Email     string
	AvatarURL string
}

// GetToken resolves the GitHub token using the following priority:
// 1. Explicit token (from --token flag)
// 2. GITHUB_TOKEN environment variable
// 3. Stored token (keychain or config file)
func GetToken(ctx context.Context, explicitToken string, hostname string) (*TokenResult, error) {
	if hostname == "" {
		hostname = DefaultHostname
	}

	// 1. Check explicit token (from --token flag)
	if explicitToken != "" {
		return &TokenResult{
			Token:    explicitToken,
			Source:   TokenSourceFlag,
			Hostname: hostname,
		}, nil
	}

	// 2. Check environment variable
	if envToken := os.Getenv(EnvGitHubToken); envToken != "" {
		return &TokenResult{
			Token:    envToken,
			Source:   TokenSourceEnv,
			Hostname: hostname,
		}, nil
	}

	// 3. Check stored token (keychain first, then config file)
	storage := NewStorage()
	storedToken, source, err := storage.GetToken(hostname)
	if err == nil && storedToken != "" {
		return &TokenResult{
			Token:    storedToken,
			Source:   source,
			Hostname: hostname,
		}, nil
	}

	// No token found
	return &TokenResult{
		Token:    "",
		Source:   TokenSourceNone,
		Hostname: hostname,
	}, nil
}

// ValidateToken checks if a token is valid by making a test API call
func ValidateToken(ctx context.Context, token string, hostname string) (*AuthenticatedUser, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}

	// Create authenticated client
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)

	var client *gh.Client
	if hostname == "" || hostname == DefaultHostname {
		client = gh.NewClient(tc)
	} else {
		// GitHub Enterprise Server
		baseURL := fmt.Sprintf("https://%s/api/v3/", hostname)
		uploadURL := fmt.Sprintf("https://%s/api/uploads/", hostname)
		var err error
		client, err = gh.NewClient(tc).WithEnterpriseURLs(baseURL, uploadURL)
		if err != nil {
			return nil, fmt.Errorf("failed to create enterprise client: %w", err)
		}
	}

	// Get authenticated user
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to validate token: %w", err)
	}

	return &AuthenticatedUser{
		Login:     user.GetLogin(),
		Name:      user.GetName(),
		Email:     user.GetEmail(),
		AvatarURL: user.GetAvatarURL(),
	}, nil
}

// FormatTokenSource returns a human-readable description of the token source
func FormatTokenSource(source TokenSource) string {
	switch source {
	case TokenSourceFlag:
		return "command line flag"
	case TokenSourceEnv:
		return "environment variable (GITHUB_TOKEN)"
	case TokenSourceKeychain:
		return "keychain"
	case TokenSourceConfig:
		return "config file (~/.githubby.yaml)"
	default:
		return "unknown"
	}
}

// MaskToken returns a masked version of the token for display
func MaskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}
