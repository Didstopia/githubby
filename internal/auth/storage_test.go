package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTempConfig creates a temporary config file for testing
func createTempConfig(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "githubby-test-*")
	require.NoError(t, err)

	configPath := filepath.Join(tmpDir, ".githubby.yaml")

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return configPath, cleanup
}

func TestNewStorage(t *testing.T) {
	storage := NewStorage()
	assert.NotNil(t, storage)
}

func TestNewStorageWithConfig(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)
	assert.NotNil(t, storage)
	assert.False(t, storage.IsKeychainAvailable())
	assert.Equal(t, configPath, storage.configPath)
}

func TestStorage_GetStorageLocation(t *testing.T) {
	storage := NewStorage()
	location := storage.GetStorageLocation()
	assert.NotEmpty(t, location)
}

func TestStorage_GetStorageLocation_ConfigFallback(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)
	location := storage.GetStorageLocation()
	assert.Equal(t, "config file (~/.githubby.yaml)", location)
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "short token",
			token:    "abc",
			expected: "****",
		},
		{
			name:     "exactly 8 chars",
			token:    "12345678",
			expected: "****",
		},
		{
			name:     "normal token",
			token:    "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			expected: "ghp_****xxxx",
		},
		{
			name:     "empty token",
			token:    "",
			expected: "****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskToken(tt.token)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTokenSource(t *testing.T) {
	tests := []struct {
		source   TokenSource
		expected string
	}{
		{TokenSourceFlag, "command line flag"},
		{TokenSourceEnv, "environment variable (GITHUB_TOKEN)"},
		{TokenSourceKeychain, "keychain"},
		{TokenSourceConfig, "config file (~/.githubby.yaml)"},
		{TokenSourceNone, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			result := FormatTokenSource(tt.source)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStorage_SetAndGetToken_ConfigFile(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)

	// Set a token
	testToken := "ghp_test_token_12345"
	err := storage.SetToken("github.com", testToken)
	require.NoError(t, err)

	// Get it back
	token, source, err := storage.GetToken("github.com")
	require.NoError(t, err)
	assert.Equal(t, testToken, token)
	assert.Equal(t, TokenSourceConfig, source)

	// Verify file permissions (skip on Windows as it doesn't support Unix permissions)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
}

func TestStorage_DeleteToken_ConfigFile(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)

	// Set a token
	testToken := "ghp_test_token_to_delete"
	err := storage.SetToken("github.com", testToken)
	require.NoError(t, err)

	// Verify it's set
	token, _, err := storage.GetToken("github.com")
	require.NoError(t, err)
	assert.Equal(t, testToken, token)

	// Delete it
	err = storage.DeleteToken("github.com")
	require.NoError(t, err)

	// Verify it's gone
	token, source, err := storage.GetToken("github.com")
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Equal(t, TokenSourceNone, source)
}

func TestStorage_GetToken_NoTokenFound(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)

	// Try to get a token that doesn't exist
	token, source, err := storage.GetToken("github.com")
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Equal(t, TokenSourceNone, source)
}

func TestStorage_GetToken_DefaultHostname(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)

	// Set token with empty hostname (should use default)
	testToken := "ghp_default_hostname_token"
	err := storage.SetToken("", testToken)
	require.NoError(t, err)

	// Get with empty hostname (should use default)
	token, _, err := storage.GetToken("")
	require.NoError(t, err)
	assert.Equal(t, testToken, token)
}

func TestStorage_PreservesOtherConfigFields(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	// Write initial config with other fields
	initialConfig := `verbose: true
dry-run: true
user: testuser
token: old_token
`
	err := os.WriteFile(configPath, []byte(initialConfig), 0600)
	require.NoError(t, err)

	storage := NewStorageWithConfig(configPath, false)

	// Set a new token
	newToken := "ghp_new_token"
	err = storage.SetToken("github.com", newToken)
	require.NoError(t, err)

	// Read the file and verify other fields are preserved
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "verbose: true")
	assert.Contains(t, content, "dry-run: true")
	assert.Contains(t, content, "user: testuser")
	assert.Contains(t, content, newToken)
}

func TestStorage_FallbackToConfigWhenKeychainUnavailable(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	// Create storage with keychain disabled (simulates headless Linux)
	storage := NewStorageWithConfig(configPath, false)

	// Verify it reports config file as storage location
	location := storage.GetStorageLocation()
	assert.Equal(t, "config file (~/.githubby.yaml)", location)

	// Verify IsKeychainAvailable returns false
	assert.False(t, storage.IsKeychainAvailable())
}

func TestStorage_KeychainAvailableReporting(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	// Create storage with keychain enabled (for testing the reporting logic)
	storage := NewStorageWithConfig(configPath, true)

	// Verify IsKeychainAvailable returns true
	assert.True(t, storage.IsKeychainAvailable())

	// GetStorageLocation should return platform-specific keychain name
	location := storage.GetStorageLocation()
	assert.NotEqual(t, "config file (~/.githubby.yaml)", location)
}

func TestStorage_ClearTokenFromNonExistentConfig(t *testing.T) {
	configPath, cleanup := createTempConfig(t)
	defer cleanup()

	storage := NewStorageWithConfig(configPath, false)

	// Clear token from non-existent config should not error
	err := storage.clearTokenFromConfig()
	assert.NoError(t, err)
}
