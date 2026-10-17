package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withArgs0(t *testing.T, arg0 string) {
	t.Helper()
	orig := os.Args
	os.Args = []string{arg0}
	t.Cleanup(func() { os.Args = orig })
}

func TestIsDev(t *testing.T) {
	assert.True(t, IsDev(""))
	assert.True(t, IsDev("dev"))
	assert.False(t, IsDev("1.0.0"))
}

func TestResolveExecutablePath(t *testing.T) {
	t.Run("absolute path is used as is", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "githubby")
		withArgs0(t, abs)
		got, err := resolveExecutablePath()
		require.NoError(t, err)
		assert.Equal(t, abs, got)
	})

	t.Run("relative path becomes absolute", func(t *testing.T) {
		withArgs0(t, "./bin/githubby")
		got, err := resolveExecutablePath()
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got))
		assert.Equal(t, "githubby", filepath.Base(got))
	})

	t.Run("bare name is found in PATH", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "my-tool")
		require.NoError(t, os.WriteFile(exe, []byte("x"), 0o755)) //nolint:gosec // test executable
		t.Setenv("PATH", dir)
		withArgs0(t, "my-tool")
		got, err := resolveExecutablePath()
		require.NoError(t, err)
		assert.Equal(t, exe, got)
	})

	t.Run("bare name missing from PATH falls back to os.Executable", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		withArgs0(t, "no-such-tool")
		got, err := resolveExecutablePath()
		require.NoError(t, err)
		self, err := os.Executable()
		require.NoError(t, err)
		assert.Equal(t, self, got)
	})
}

func TestFindInPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := findInPath("missing")
	assert.Error(t, err)
}

func TestRestart_FailsForMissingBinary(t *testing.T) {
	// A bare name that exists nowhere in PATH resolves to os.Executable, so point
	// Args[0] at a missing absolute path to make exec fail instead of replacing the test process.
	withArgs0(t, filepath.Join(t.TempDir(), "does-not-exist"))
	err := Restart()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to exec")
}
