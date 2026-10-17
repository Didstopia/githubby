package fileutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "f.txt")
	require.NoError(t, WriteFileAtomic(path, []byte("one"), 0o600))
	require.NoError(t, WriteFileAtomic(path, []byte("two"), 0o640))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "two", string(got))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp files are left behind")
}

func TestWriteFileAtomic_Errors(t *testing.T) {
	t.Run("directory cannot be created", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o600))
		assert.Error(t, WriteFileAtomic(filepath.Join(blocker, "x"), []byte("a"), 0o600))
	})
	t.Run("directory is not writable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // read-only directory on purpose
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore for cleanup
		assert.Error(t, WriteFileAtomic(filepath.Join(dir, "x"), []byte("a"), 0o600))
	})
	t.Run("target is a directory", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		require.NoError(t, os.MkdirAll(filepath.Join(target, "child"), 0o700))
		assert.Error(t, WriteFileAtomic(target, []byte("a"), 0o600))
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 1, "temp file is removed after a failed rename")
	})
}
