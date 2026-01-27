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

func TestNew(t *testing.T) {
	t.Run("git is installed", func(t *testing.T) {
		// Skip if git is not installed on the test system
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not installed")
		}

		g, err := New()
		require.NoError(t, err)
		assert.NotNil(t, g)
		assert.NotEmpty(t, g.GitPath)
	})

	t.Run("git not found returns error", func(t *testing.T) {
		// Save and modify PATH to exclude git
		originalPath := os.Getenv("PATH")
		defer os.Setenv("PATH", originalPath)

		os.Setenv("PATH", "/nonexistent")

		g, err := New()
		assert.ErrorIs(t, err, ErrGitNotInstalled)
		assert.Nil(t, g)
	})
}

func TestIsGitRepo(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("directory with .git is a git repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		require.NoError(t, os.Mkdir(gitDir, 0755))

		result := g.IsGitRepo(tmpDir)
		assert.True(t, result)
	})

	t.Run("directory without .git is not a git repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		result := g.IsGitRepo(tmpDir)
		assert.False(t, result)
	})

	t.Run("nonexistent directory is not a git repo", func(t *testing.T) {
		result := g.IsGitRepo("/nonexistent/path")
		assert.False(t, result)
	})

	t.Run(".git is a file not directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		require.NoError(t, os.WriteFile(gitFile, []byte("gitdir: ../somewhere"), 0644))

		result := g.IsGitRepo(tmpDir)
		assert.False(t, result) // It's a file, not a directory
	})
}

func TestClone(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("clone fails with invalid URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		targetDir := filepath.Join(tmpDir, "clone-target")

		ctx := context.Background()
		err := g.Clone(ctx, "invalid-url-that-does-not-exist", targetDir)

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrCloneFailed)
	})

	t.Run("clone respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		targetDir := filepath.Join(tmpDir, "clone-target")

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := g.Clone(ctx, "https://github.com/example/repo.git", targetDir)
		assert.Error(t, err)
	})
}

func TestPull(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("pull fails in non-git directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		ctx := context.Background()
		err := g.Pull(ctx, tmpDir)

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrPullFailed)
	})

	t.Run("pull in valid git repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Initialize a git repo
		ctx := context.Background()
		initCmd := exec.CommandContext(ctx, g.GitPath, "init", tmpDir)
		require.NoError(t, initCmd.Run())

		// Set up git config for the repo
		configCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.email", "test@test.com")
		require.NoError(t, configCmd.Run())
		configCmd = exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.name", "Test")
		require.NoError(t, configCmd.Run())

		// Create a commit so we have a branch
		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
		addCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "add", ".")
		require.NoError(t, addCmd.Run())
		commitCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "commit", "-m", "initial")
		require.NoError(t, commitCmd.Run())

		// Pull will fail because there's no remote, but it will run
		err := g.Pull(ctx, tmpDir)
		// This will fail since there's no remote configured
		assert.Error(t, err)
	})
}

func TestFetch(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("fetch fails in non-git directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		ctx := context.Background()
		err := g.Fetch(ctx, tmpDir)

		assert.Error(t, err)
	})

	t.Run("fetch respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := g.Fetch(ctx, tmpDir)
		assert.Error(t, err)
	})
}

func TestGetRemoteURL(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("get remote URL from repo with remote", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize git repo
		initCmd := exec.CommandContext(ctx, g.GitPath, "init", tmpDir)
		require.NoError(t, initCmd.Run())

		// Add a remote
		remoteURL := "https://github.com/example/repo.git"
		addRemoteCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "remote", "add", "origin", remoteURL)
		require.NoError(t, addRemoteCmd.Run())

		result, err := g.GetRemoteURL(ctx, tmpDir)
		require.NoError(t, err)
		assert.Equal(t, remoteURL, result)
	})

	t.Run("get remote URL fails without remote", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize git repo without remote
		initCmd := exec.CommandContext(ctx, g.GitPath, "init", tmpDir)
		require.NoError(t, initCmd.Run())

		_, err := g.GetRemoteURL(ctx, tmpDir)
		assert.Error(t, err)
	})
}

func TestGetDefaultBranch(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("returns main for repo with main branch", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize git repo with main branch
		initCmd := exec.CommandContext(ctx, g.GitPath, "init", "-b", "main", tmpDir)
		require.NoError(t, initCmd.Run())

		// Set up config
		configCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.email", "test@test.com")
		require.NoError(t, configCmd.Run())
		configCmd = exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.name", "Test")
		require.NoError(t, configCmd.Run())

		// Create initial commit
		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
		addCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "add", ".")
		require.NoError(t, addCmd.Run())
		commitCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "commit", "-m", "initial")
		require.NoError(t, commitCmd.Run())

		// Add remote and push a ref so origin/main exists
		remoteURL := "https://github.com/example/repo.git"
		addRemoteCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "remote", "add", "origin", remoteURL)
		require.NoError(t, addRemoteCmd.Run())

		// Simulate origin/main by creating a ref
		refCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "update-ref", "refs/remotes/origin/main", "HEAD")
		require.NoError(t, refCmd.Run())

		result, err := g.GetDefaultBranch(ctx, tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "main", result)
	})

	t.Run("returns master for repo with master branch", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize git repo with master branch
		initCmd := exec.CommandContext(ctx, g.GitPath, "init", "-b", "master", tmpDir)
		require.NoError(t, initCmd.Run())

		// Set up config
		configCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.email", "test@test.com")
		require.NoError(t, configCmd.Run())
		configCmd = exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.name", "Test")
		require.NoError(t, configCmd.Run())

		// Create initial commit
		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
		addCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "add", ".")
		require.NoError(t, addCmd.Run())
		commitCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "commit", "-m", "initial")
		require.NoError(t, commitCmd.Run())

		// Add remote and create refs/remotes/origin/master
		remoteURL := "https://github.com/example/repo.git"
		addRemoteCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "remote", "add", "origin", remoteURL)
		require.NoError(t, addRemoteCmd.Run())

		refCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "update-ref", "refs/remotes/origin/master", "HEAD")
		require.NoError(t, refCmd.Run())

		result, err := g.GetDefaultBranch(ctx, tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "master", result)
	})

	t.Run("returns error when cannot determine branch", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize git repo with a non-standard branch
		initCmd := exec.CommandContext(ctx, g.GitPath, "init", "-b", "development", tmpDir)
		require.NoError(t, initCmd.Run())

		// Set up config
		configCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.email", "test@test.com")
		require.NoError(t, configCmd.Run())
		configCmd = exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "config", "user.name", "Test")
		require.NoError(t, configCmd.Run())

		// Create initial commit
		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
		addCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "add", ".")
		require.NoError(t, addCmd.Run())
		commitCmd := exec.CommandContext(ctx, g.GitPath, "-C", tmpDir, "commit", "-m", "initial")
		require.NoError(t, commitCmd.Run())

		// Don't create origin refs for main or master
		_, err := g.GetDefaultBranch(ctx, tmpDir)
		assert.Error(t, err)
	})
}

func TestMockCommander(t *testing.T) {
	t.Run("records calls", func(t *testing.T) {
		mock := NewMockCommander()

		ctx := context.Background()
		_ = mock.Run(ctx, "/test/dir", "arg1", "arg2")
		_, _ = mock.Output(ctx, "/test/dir2", "arg3")

		assert.Len(t, mock.Calls, 2)
		assert.Equal(t, "Run", mock.Calls[0].Method)
		assert.Equal(t, "/test/dir", mock.Calls[0].Dir)
		assert.Equal(t, []string{"arg1", "arg2"}, mock.Calls[0].Args)
		assert.Equal(t, "Output", mock.Calls[1].Method)
	})

	t.Run("uses custom RunFunc", func(t *testing.T) {
		mock := NewMockCommander()
		expectedErr := assert.AnError
		mock.RunFunc = func(ctx context.Context, dir string, args ...string) error {
			return expectedErr
		}

		err := mock.Run(context.Background(), "/dir", "arg")
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("uses custom OutputFunc", func(t *testing.T) {
		mock := NewMockCommander()
		mock.OutputFunc = func(ctx context.Context, dir string, args ...string) (string, error) {
			return "custom output", nil
		}

		output, err := mock.Output(context.Background(), "/dir", "arg")
		require.NoError(t, err)
		assert.Equal(t, "custom output", output)
	})

	t.Run("reset clears calls", func(t *testing.T) {
		mock := NewMockCommander()
		_ = mock.Run(context.Background(), "/dir", "arg")
		assert.Len(t, mock.Calls, 1)

		mock.Reset()
		assert.Empty(t, mock.Calls)
	})

	t.Run("call count works", func(t *testing.T) {
		mock := NewMockCommander()
		_ = mock.Run(context.Background(), "/dir", "arg")
		_ = mock.Run(context.Background(), "/dir", "arg")
		_, _ = mock.Output(context.Background(), "/dir", "arg")

		assert.Equal(t, 2, mock.CallCount("Run"))
		assert.Equal(t, 1, mock.CallCount("Output"))
		assert.Equal(t, 0, mock.CallCount("NonExistent"))
	})
}
