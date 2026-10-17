package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func resetKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
}

func TestCheckKeychainAvailable(t *testing.T) {
	resetKeyring(t)
	assert.True(t, NewStorage().IsKeychainAvailable())

	keyring.MockInitWithError(errors.New("no keychain"))
	assert.False(t, NewStorage().IsKeychainAvailable())
}

func TestStorage_Keychain_RoundTrip(t *testing.T) {
	resetKeyring(t)
	s := NewStorageWithConfig(filepath.Join(t.TempDir(), "cfg.yaml"), true)

	require.NoError(t, s.SetToken("", "secret-token"))
	got, err := keyring.Get(KeyringServiceName, DefaultHostname)
	require.NoError(t, err)
	assert.Equal(t, "secret-token", got)

	tok, src, err := s.GetToken("")
	require.NoError(t, err)
	assert.Equal(t, "secret-token", tok)
	assert.Equal(t, TokenSourceKeychain, src)

	require.NoError(t, s.DeleteToken(""))
	_, err = keyring.Get(KeyringServiceName, DefaultHostname)
	assert.ErrorIs(t, err, keyring.ErrNotFound)
}

func TestStorage_SetToken_FallsBackToConfigWhenKeychainFails(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "cfg.yaml")
	s := NewStorageWithConfig(cfg, true)
	keyring.MockInitWithError(errors.New("locked"))
	t.Cleanup(keyring.MockInit)

	require.NoError(t, s.SetToken("github.com", "cfg-token"))

	tok, src, err := s.GetToken("github.com")
	require.NoError(t, err)
	assert.Equal(t, "cfg-token", tok)
	assert.Equal(t, TokenSourceConfig, src)
}

func TestStorage_GetToken_KeychainMissFallsBackToConfig(t *testing.T) {
	resetKeyring(t)
	cfg := filepath.Join(t.TempDir(), "cfg.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("token: from-file\n"), 0o600))
	s := NewStorageWithConfig(cfg, true)

	tok, src, err := s.GetToken("github.com")
	require.NoError(t, err)
	assert.Equal(t, "from-file", tok)
	assert.Equal(t, TokenSourceConfig, src)
}

func TestStorage_DeleteToken_ErrorWhenBothFail(t *testing.T) {
	resetKeyring(t)
	// Config path is a directory with a bad yaml file inside? Simpler: path is a directory.
	dir := t.TempDir()
	s := NewStorageWithConfig(dir, true) // reading a directory fails with a non-NotExist error

	err := s.DeleteToken("github.com") // keychain has nothing to delete either
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete token")
}

func TestStorage_DeleteToken_OkWhenOnlyConfigFails(t *testing.T) {
	resetKeyring(t)
	require.NoError(t, keyring.Set(KeyringServiceName, DefaultHostname, "x"))
	s := NewStorageWithConfig(t.TempDir(), true)
	assert.NoError(t, s.DeleteToken(""))
}

func TestStorage_GetStorageLocation_Keychain(t *testing.T) {
	loc := NewStorageWithConfig("", true).GetStorageLocation()
	assert.NotEqual(t, "config file (~/.githubby.yaml)", loc)
	assert.NotEmpty(t, loc)
}

func TestStorage_ConfigPath_DefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homedir.DisableCache = true
	t.Cleanup(func() { homedir.DisableCache = false })

	s := NewStorageWithConfig("", false)
	require.NoError(t, s.SetToken("", "home-token"))

	data, err := os.ReadFile(filepath.Join(home, ConfigFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), "home-token")

	info, err := os.Stat(filepath.Join(home, ConfigFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestStorage_ConfigErrors(t *testing.T) {
	t.Run("invalid yaml on read", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "cfg.yaml")
		require.NoError(t, os.WriteFile(cfg, []byte("token: [unclosed"), 0o600))
		s := NewStorageWithConfig(cfg, false)
		_, _, err := s.GetToken("")
		assert.Error(t, err)
		assert.Error(t, s.clearTokenFromConfig())
	})
	t.Run("set into unwritable path", func(t *testing.T) {
		s := NewStorageWithConfig(filepath.Join(t.TempDir(), "missing", "cfg.yaml"), false)
		assert.Error(t, s.SetToken("", "x"))
	})
	t.Run("set over invalid yaml replaces it", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "cfg.yaml")
		require.NoError(t, os.WriteFile(cfg, []byte("token: [unclosed"), 0o600))
		s := NewStorageWithConfig(cfg, false)
		require.NoError(t, s.SetToken("", "fresh"))
		tok, _, err := s.GetToken("")
		require.NoError(t, err)
		assert.Equal(t, "fresh", tok)
	})
}

func TestGetToken_StoredKeychainToken(t *testing.T) {
	resetKeyring(t)
	t.Setenv(EnvGitHubToken, "")
	require.NoError(t, keyring.Set(KeyringServiceName, "ghe.example.com", "kc-token"))

	res, err := GetToken(context.Background(), "", "ghe.example.com")
	require.NoError(t, err)
	assert.Equal(t, "kc-token", res.Token)
	assert.Equal(t, TokenSourceKeychain, res.Source)
}

func TestGetToken_NoneFound(t *testing.T) {
	resetKeyring(t)
	t.Setenv(EnvGitHubToken, "")
	t.Setenv("HOME", t.TempDir())
	homedir.DisableCache = true
	t.Cleanup(func() { homedir.DisableCache = false })

	res, err := GetToken(context.Background(), "", "")
	require.NoError(t, err)
	assert.Empty(t, res.Token)
	assert.Equal(t, TokenSourceNone, res.Source)
}

func TestFormatTokenSource_Unknown(t *testing.T) {
	assert.Equal(t, "unknown", FormatTokenSource(TokenSourceNone))
}
