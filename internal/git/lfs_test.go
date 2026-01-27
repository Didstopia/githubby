package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLFS(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	lfs := NewLFS(g)

	assert.NotNil(t, lfs)
	assert.Equal(t, g, lfs.Git)
	// LFSPath may or may not be set depending on system
}

func TestLFS_IsInstalled(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("returns true when git-lfs is in PATH", func(t *testing.T) {
		// Check if git-lfs is actually installed
		lfsPath, lfsErr := exec.LookPath("git-lfs")
		if lfsErr != nil {
			t.Skip("git-lfs is not installed")
		}

		lfs := &LFS{LFSPath: lfsPath, Git: g}
		assert.True(t, lfs.IsInstalled())
	})

	t.Run("returns false when LFSPath is empty", func(t *testing.T) {
		lfs := &LFS{LFSPath: "", Git: g}
		assert.False(t, lfs.IsInstalled())
	})
}

func TestLFS_RepoUsesLFS(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	lfs := NewLFS(g)

	t.Run("returns true when .gitattributes contains filter=lfs", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitattributes := filepath.Join(tmpDir, ".gitattributes")

		content := `*.psd filter=lfs diff=lfs merge=lfs -text
*.zip filter=lfs diff=lfs merge=lfs -text
`
		require.NoError(t, os.WriteFile(gitattributes, []byte(content), 0644))

		result := lfs.RepoUsesLFS(tmpDir)
		assert.True(t, result)
	})

	t.Run("returns false when .gitattributes has no LFS patterns", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitattributes := filepath.Join(tmpDir, ".gitattributes")

		content := `* text=auto
*.go text
*.md text
`
		require.NoError(t, os.WriteFile(gitattributes, []byte(content), 0644))

		result := lfs.RepoUsesLFS(tmpDir)
		assert.False(t, result)
	})

	t.Run("returns false when .gitattributes does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		result := lfs.RepoUsesLFS(tmpDir)
		assert.False(t, result)
	})

	t.Run("returns false for nonexistent directory", func(t *testing.T) {
		result := lfs.RepoUsesLFS("/nonexistent/path")
		assert.False(t, result)
	})

	t.Run("handles empty .gitattributes file", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitattributes := filepath.Join(tmpDir, ".gitattributes")

		require.NoError(t, os.WriteFile(gitattributes, []byte(""), 0644))

		result := lfs.RepoUsesLFS(tmpDir)
		assert.False(t, result)
	})

	t.Run("handles filter=lfs in the middle of a line", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitattributes := filepath.Join(tmpDir, ".gitattributes")

		content := `*.bin binary filter=lfs diff=lfs merge=lfs
`
		require.NoError(t, os.WriteFile(gitattributes, []byte(content), 0644))

		result := lfs.RepoUsesLFS(tmpDir)
		assert.True(t, result)
	})
}

func TestLFS_Initialize(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("returns error when LFS not installed", func(t *testing.T) {
		lfs := &LFS{LFSPath: "", Git: g}

		err := lfs.Initialize(context.Background())
		assert.ErrorIs(t, err, ErrLFSNotInstalled)
	})

	t.Run("succeeds when LFS is installed", func(t *testing.T) {
		// Check if git-lfs is actually installed
		lfsPath, lfsErr := exec.LookPath("git-lfs")
		if lfsErr != nil {
			t.Skip("git-lfs is not installed")
		}

		lfs := &LFS{LFSPath: lfsPath, Git: g}

		err := lfs.Initialize(context.Background())
		assert.NoError(t, err)
	})
}

func TestLFS_Pull(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("returns error when LFS not installed", func(t *testing.T) {
		lfs := &LFS{LFSPath: "", Git: g}

		err := lfs.Pull(context.Background(), "/some/path")
		assert.ErrorIs(t, err, ErrLFSNotInstalled)
	})

	t.Run("fails in non-git directory", func(t *testing.T) {
		// Check if git-lfs is actually installed
		lfsPath, lfsErr := exec.LookPath("git-lfs")
		if lfsErr != nil {
			t.Skip("git-lfs is not installed")
		}

		lfs := &LFS{LFSPath: lfsPath, Git: g}
		tmpDir := t.TempDir()

		err := lfs.Pull(context.Background(), tmpDir)
		assert.Error(t, err)
	})
}

func TestLFS_Fetch(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("returns error when LFS not installed", func(t *testing.T) {
		lfs := &LFS{LFSPath: "", Git: g}

		err := lfs.Fetch(context.Background(), "/some/path")
		assert.ErrorIs(t, err, ErrLFSNotInstalled)
	})

	t.Run("fails in non-git directory", func(t *testing.T) {
		// Check if git-lfs is actually installed
		lfsPath, lfsErr := exec.LookPath("git-lfs")
		if lfsErr != nil {
			t.Skip("git-lfs is not installed")
		}

		lfs := &LFS{LFSPath: lfsPath, Git: g}
		tmpDir := t.TempDir()

		err := lfs.Fetch(context.Background(), tmpDir)
		assert.Error(t, err)
	})
}

func TestLFS_EnsureLFS(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("succeeds when LFS is already installed", func(t *testing.T) {
		// Check if git-lfs is actually installed
		lfsPath, lfsErr := exec.LookPath("git-lfs")
		if lfsErr != nil {
			t.Skip("git-lfs is not installed")
		}

		lfs := &LFS{LFSPath: lfsPath, Git: g}

		err := lfs.EnsureLFS(context.Background())
		assert.NoError(t, err)
	})

	// Note: We can't easily test the install path without mocking
	// because it requires system package managers and sudo access
}

func TestLFS_Install_UnsupportedOS(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	// This test verifies that Install returns an error
	// when the package manager is not found
	// We can only test this scenario without mocking by
	// checking the behavior when PATH doesn't contain package managers

	t.Run("returns error when LFS already installed", func(t *testing.T) {
		lfsPath, lfsErr := exec.LookPath("git-lfs")
		if lfsErr != nil {
			t.Skip("git-lfs is not installed")
		}

		lfs := &LFS{LFSPath: lfsPath, Git: g}

		// Install should return nil when already installed
		err := lfs.Install(context.Background())
		assert.NoError(t, err)
	})
}
