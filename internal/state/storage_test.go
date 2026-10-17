package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorage_UpdateProfileLastSync_NoSyncRecord(t *testing.T) {
	storage := NewStorageWithPath(filepath.Join(t.TempDir(), StateFileName))
	profile := NewProfile("p", "user", "someone", t.TempDir(), false)
	require.NoError(t, storage.AddProfile(profile))

	// The CLI never writes sync records, so this must not need one
	require.NoError(t, storage.UpdateProfileLastSync(profile.ID))
	assert.False(t, storage.GetProfile(profile.ID).LastSyncAt.IsZero())
}

func newTestStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", StateFileName)
	return NewStorageWithPath(path), path
}

func TestNewStorage_UsesHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := NewStorage()
	require.NoError(t, err)
	require.NoError(t, s.SetDefaults("/t", "u"))
	_, err = os.Stat(filepath.Join(home, ConfigDirName, StateFileName))
	assert.NoError(t, err)
}

func TestNewStorage_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "")
	_, err := NewStorage()
	assert.Error(t, err)
}

func TestStorage_LoadMissingFileGivesEmptyState(t *testing.T) {
	s, _ := newTestStorage(t)
	require.NoError(t, s.Load())
	assert.Empty(t, s.GetProfiles())
	assert.Equal(t, 1, s.State().Version)
}

func TestStorage_LoadErrors(t *testing.T) {
	t.Run("unreadable path", func(t *testing.T) {
		s := NewStorageWithPath(t.TempDir()) // a directory
		err := s.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read state file")
	})
	t.Run("bad yaml", func(t *testing.T) {
		s, path := newTestStorage(t)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("profiles: [unclosed"), 0o600))
		err := s.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse state file")
	})
}

func TestStorage_LoadMigratesVersion(t *testing.T) {
	s, path := newTestStorage(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("version: 0\nprofiles: []\n"), 0o600))
	require.NoError(t, s.Load())
	assert.Equal(t, 1, s.State().Version)
}

func TestStorage_SaveRoundTripAndPermissions(t *testing.T) {
	s, path := newTestStorage(t)
	p := NewProfile("p", "user", "alice", "/a", true)
	p.IncludeFilter = []string{"x-*"}
	require.NoError(t, s.AddProfile(p))
	require.NoError(t, s.SetDefaults("/target", "alice"))
	require.NoError(t, s.SetOnboardingComplete(true))
	require.NoError(t, s.UpdateRepoCache(&CachedRepo{FullName: "a/b", Stars: 3}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "temp file must be gone after save")

	s2 := NewStorageWithPath(path)
	require.NoError(t, s2.Load())
	got := s2.GetProfileByName("p")
	require.NotNil(t, got)
	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, []string{"x-*"}, got.IncludeFilter)
	assert.True(t, got.IncludePrivate)
	assert.Equal(t, "/target", s2.GetDefaultTargetDir())
	assert.Equal(t, "alice", s2.GetDefaultUsername())
	assert.True(t, s2.IsOnboardingComplete())
	assert.Equal(t, 3, s2.GetCachedRepo("a/b").Stars)
}

func TestStorage_SaveErrors(t *testing.T) {
	t.Run("cannot create directory", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o600))
		s := NewStorageWithPath(filepath.Join(blocker, "state.yaml"))
		err := s.Save()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create config directory")
	})
	t.Run("cannot rename onto a directory", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "state.yaml")
		require.NoError(t, os.MkdirAll(filepath.Join(target, "child"), 0o700))
		s := NewStorageWithPath(target)
		err := s.Save()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to rename temp state file")
		_, statErr := os.Stat(target + ".tmp")
		assert.True(t, os.IsNotExist(statErr), "temp file is cleaned up on failure")
	})
	t.Run("cannot write temp file", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "state.yaml")
		require.NoError(t, os.MkdirAll(target+".tmp", 0o700)) // temp path is a directory
		s := NewStorageWithPath(target)
		err := s.Save()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write temp state file")
	})
}

func TestStorage_ProfileOperations(t *testing.T) {
	s, _ := newTestStorage(t)
	p := NewProfile("p", "user", "alice", "/a", false)
	require.NoError(t, s.AddProfile(p))
	assert.Same(t, p, s.GetProfile(p.ID))

	changed := *p
	changed.Name = "renamed"
	require.NoError(t, s.UpdateProfile(&changed))
	assert.Equal(t, "renamed", s.GetProfile(p.ID).Name)

	err := s.UpdateProfile(&SyncProfile{ID: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile not found")

	// GetProfiles returns a copy of the slice
	list := s.GetProfiles()
	list[0] = nil
	assert.NotNil(t, s.GetProfile(p.ID))

	require.NoError(t, s.DeleteProfile(p.ID))
	assert.Empty(t, s.GetProfiles())
	assert.Error(t, s.DeleteProfile(p.ID))
}

func TestStorage_SyncHistory(t *testing.T) {
	s, _ := newTestStorage(t)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		require.NoError(t, s.AddSyncRecord(&SyncRecord{ProfileID: "p", TotalRepos: i, Cloned: 1, CompletedAt: base.Add(time.Duration(i) * time.Hour)}))
	}

	assert.Len(t, s.GetSyncHistory(), 5)
	assert.Equal(t, 4, s.GetLatestSyncForProfile("p").TotalRepos)
	assert.Equal(t, 5, s.GetSyncStats().TotalSyncs)

	recent := s.GetRecentSyncHistory(2)
	require.Len(t, recent, 2)
	assert.Equal(t, 3, recent[0].TotalRepos)
	assert.Equal(t, 4, recent[1].TotalRepos)

	assert.Len(t, s.GetRecentSyncHistory(50), 5, "n larger than history returns everything")
}

func TestStorage_UpdateProfileLastSync(t *testing.T) {
	s, _ := newTestStorage(t)
	p := NewProfile("p", "user", "alice", "/a", false)
	require.NoError(t, s.AddProfile(p))

	before := time.Now()
	require.NoError(t, s.UpdateProfileLastSync(p.ID))
	assert.False(t, s.GetProfile(p.ID).LastSyncAt.Before(before))

	err := s.UpdateProfileLastSync("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile not found")
}
