package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetToken_FromEnvVar(t *testing.T) {
	// Save and restore original env var
	original := os.Getenv(EnvGitHubToken)
	defer os.Setenv(EnvGitHubToken, original)

	// Set test token
	testToken := "ghp_test_token_from_env"
	os.Setenv(EnvGitHubToken, testToken)

	ctx := context.Background()
	result, err := GetToken(ctx, "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, testToken, result.Token)
	assert.Equal(t, TokenSourceEnv, result.Source)
	assert.Equal(t, DefaultHostname, result.Hostname)
}

func TestGetToken_ExplicitTokenTakesPrecedence(t *testing.T) {
	// Save and restore original env var
	original := os.Getenv(EnvGitHubToken)
	defer os.Setenv(EnvGitHubToken, original)

	// Set env var token
	os.Setenv(EnvGitHubToken, "ghp_env_token")

	// Explicit token should take precedence
	explicitToken := "ghp_explicit_token"
	ctx := context.Background()
	result, err := GetToken(ctx, explicitToken, "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, explicitToken, result.Token)
	assert.Equal(t, TokenSourceFlag, result.Source)
}

func TestGetToken_CustomHostname(t *testing.T) {
	explicitToken := "ghp_enterprise_token"
	customHostname := "github.mycompany.com"

	ctx := context.Background()
	result, err := GetToken(ctx, explicitToken, customHostname)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, explicitToken, result.Token)
	assert.Equal(t, customHostname, result.Hostname)
}

func TestGetToken_DefaultHostname(t *testing.T) {
	explicitToken := "ghp_test_token"

	ctx := context.Background()
	result, err := GetToken(ctx, explicitToken, "")

	assert.NoError(t, err)
	assert.Equal(t, DefaultHostname, result.Hostname)
}

func TestDefaultHostname(t *testing.T) {
	assert.Equal(t, "github.com", DefaultHostname)
}

func TestEnvGitHubToken(t *testing.T) {
	assert.Equal(t, "GITHUB_TOKEN", EnvGitHubToken)
}

func TestGetToken_FallsBackToStorage(t *testing.T) {
	// Save and restore original env var
	original := os.Getenv(EnvGitHubToken)
	defer os.Setenv(EnvGitHubToken, original)

	// Clear env var
	os.Unsetenv(EnvGitHubToken)

	ctx := context.Background()
	result, err := GetToken(ctx, "", "")

	// Should not error - just may not find a token
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// Source should NOT be flag or env since we didn't provide those
	assert.NotEqual(t, TokenSourceFlag, result.Source)
	assert.NotEqual(t, TokenSourceEnv, result.Source)
}

func TestValidateToken_EmptyToken(t *testing.T) {
	ctx := context.Background()
	_, err := ValidateToken(ctx, "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty token")
}

// TestGetTokenWithStoredToken tests the full flow with a stored token
func TestGetTokenWithStoredToken(t *testing.T) {
	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "githubby-auth-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, ".githubby.yaml")
	testToken := "ghp_stored_token_test"

	// Write a token to the config file
	configContent := "token: " + testToken + "\n"
	err = os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	// Create storage with the temp config
	storage := NewStorageWithConfig(configPath, false)

	// Verify we can retrieve it
	token, source, err := storage.GetToken("github.com")
	require.NoError(t, err)
	assert.Equal(t, testToken, token)
	assert.Equal(t, TokenSourceConfig, source)
}
