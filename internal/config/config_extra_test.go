package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateHome points the home directory at a temp dir and turns off go-homedir's cache.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	prev := homedir.DisableCache
	homedir.DisableCache = true
	t.Cleanup(func() { homedir.DisableCache = prev })
	return home
}

func TestGetConfigFilePath(t *testing.T) {
	home := isolateHome(t)
	p, err := GetConfigFilePath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".githubby.yaml"), p)
}

func TestEnsureConfigFile(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, ".githubby.yaml")

	require.NoError(t, EnsureConfigFile())
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// A second call keeps the existing file untouched.
	require.NoError(t, os.WriteFile(path, []byte("verbose: true\n"), 0o600))
	require.NoError(t, EnsureConfigFile())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "verbose: true\n", string(data))
}

func TestEnsureConfigFile_StatError(t *testing.T) {
	home := isolateHome(t)
	// A regular file in place of the home directory makes stat fail with ENOTDIR.
	file := filepath.Join(home, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("HOME", file)
	assert.Error(t, EnsureConfigFile())
}

func TestLoad_ReadsDefaultFile(t *testing.T) {
	home := isolateHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".githubby.yaml"), []byte("user: alice\nfilter-days: 7\n"), 0o600))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "alice", cfg.User)
	assert.Equal(t, 7, cfg.FilterDays)
	assert.Equal(t, -1, cfg.FilterCount, "unset fields keep their defaults")
}

func TestLoad_MissingFile(t *testing.T) {
	isolateHome(t)
	_, err := Load()
	assert.True(t, os.IsNotExist(err))
}

func TestSaveTo_UnwritablePath(t *testing.T) {
	err := DefaultConfig().SaveTo(filepath.Join(t.TempDir(), "missing", "cfg.yaml"))
	assert.Error(t, err)
}

