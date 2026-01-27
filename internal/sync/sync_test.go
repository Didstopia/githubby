package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{
			name:     "wildcard matches all",
			pattern:  "*",
			input:    "anything",
			expected: true,
		},
		{
			name:     "prefix match",
			pattern:  "prefix*",
			input:    "prefix-something",
			expected: true,
		},
		{
			name:     "prefix no match",
			pattern:  "prefix*",
			input:    "other-something",
			expected: false,
		},
		{
			name:     "suffix match",
			pattern:  "*-suffix",
			input:    "something-suffix",
			expected: true,
		},
		{
			name:     "suffix no match",
			pattern:  "*-suffix",
			input:    "something-other",
			expected: false,
		},
		{
			name:     "exact match",
			pattern:  "exact",
			input:    "exact",
			expected: true,
		},
		{
			name:     "exact no match",
			pattern:  "exact",
			input:    "different",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchGlob(tt.pattern, tt.input)
			if result != tt.expected {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewResult(t *testing.T) {
	result := NewResult()

	if result.Cloned == nil {
		t.Error("Cloned should be initialized")
	}
	if result.Updated == nil {
		t.Error("Updated should be initialized")
	}
	if result.Skipped == nil {
		t.Error("Skipped should be initialized")
	}
	if result.Failed == nil {
		t.Error("Failed should be initialized")
	}

	if len(result.Cloned) != 0 {
		t.Error("Cloned should be empty")
	}
	if len(result.Updated) != 0 {
		t.Error("Updated should be empty")
	}
	if len(result.Skipped) != 0 {
		t.Error("Skipped should be empty")
	}
	if len(result.Failed) != 0 {
		t.Error("Failed should be empty")
	}
}

func TestSyncer_shouldSync(t *testing.T) {
	tests := []struct {
		name     string
		opts     *Options
		repoName string
		expected bool
	}{
		{
			name:     "no filters - include all",
			opts:     &Options{},
			repoName: "any-repo",
			expected: true,
		},
		{
			name: "include pattern matches",
			opts: &Options{
				Include: []string{"my-*"},
			},
			repoName: "my-project",
			expected: true,
		},
		{
			name: "include pattern no match",
			opts: &Options{
				Include: []string{"my-*"},
			},
			repoName: "other-project",
			expected: false,
		},
		{
			name: "exclude pattern matches",
			opts: &Options{
				Exclude: []string{"*-archive"},
			},
			repoName: "old-archive",
			expected: false,
		},
		{
			name: "exclude pattern no match",
			opts: &Options{
				Exclude: []string{"*-archive"},
			},
			repoName: "my-project",
			expected: true,
		},
		{
			name: "exclude overrides include",
			opts: &Options{
				Include: []string{"my-*"},
				Exclude: []string{"my-old*"},
			},
			repoName: "my-old-project",
			expected: false,
		},
		{
			name: "both patterns - included",
			opts: &Options{
				Include: []string{"my-*"},
				Exclude: []string{"*-archive"},
			},
			repoName: "my-project",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := &Syncer{opts: tt.opts}
			result := syncer.shouldSync(tt.repoName)
			if result != tt.expected {
				t.Errorf("shouldSync(%q) = %v, want %v", tt.repoName, result, tt.expected)
			}
		})
	}
}

func TestNew(t *testing.T) {
	mockClient := github.NewMockClient()
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	opts := &Options{
		Target:         "/tmp/test",
		Include:        []string{"include-*"},
		Exclude:        []string{"*-exclude"},
		IncludePrivate: true,
		DryRun:         false,
		Verbose:        true,
	}

	syncer := New(mockClient, gitInstance, opts)

	assert.NotNil(t, syncer)
	assert.Equal(t, mockClient, syncer.ghClient)
	assert.Equal(t, gitInstance, syncer.git)
	assert.NotNil(t, syncer.lfs)
	assert.Equal(t, opts, syncer.opts)
}

func TestSyncUserRepos(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("calls ListUserRepos", func(t *testing.T) {
		mockClient := github.NewMockClient()
		mockClient.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
			return []*gh.Repository{}, nil
		}

		tmpDir := t.TempDir()
		opts := &Options{Target: tmpDir, DryRun: true}
		syncer := New(mockClient, gitInstance, opts)

		result, err := syncer.SyncUserRepos(context.Background(), "testuser")

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 1, mockClient.CallCount("ListUserRepos"))
	})

	t.Run("handles error from API", func(t *testing.T) {
		mockClient := github.NewMockClient()
		mockClient.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
			return nil, assert.AnError
		}

		tmpDir := t.TempDir()
		opts := &Options{Target: tmpDir}
		syncer := New(mockClient, gitInstance, opts)

		_, err := syncer.SyncUserRepos(context.Background(), "testuser")

		assert.Error(t, err)
	})
}

func TestSyncOrgRepos(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("calls ListOrgRepos", func(t *testing.T) {
		mockClient := github.NewMockClient()
		mockClient.ListOrgReposFunc = func(ctx context.Context, org string, opts *github.ListOptions) ([]*gh.Repository, error) {
			return []*gh.Repository{}, nil
		}

		tmpDir := t.TempDir()
		opts := &Options{Target: tmpDir, DryRun: true}
		syncer := New(mockClient, gitInstance, opts)

		result, err := syncer.SyncOrgRepos(context.Background(), "testorg")

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 1, mockClient.CallCount("ListOrgRepos"))
	})

	t.Run("handles error from API", func(t *testing.T) {
		mockClient := github.NewMockClient()
		mockClient.ListOrgReposFunc = func(ctx context.Context, org string, opts *github.ListOptions) ([]*gh.Repository, error) {
			return nil, assert.AnError
		}

		tmpDir := t.TempDir()
		opts := &Options{Target: tmpDir}
		syncer := New(mockClient, gitInstance, opts)

		_, err := syncer.SyncOrgRepos(context.Background(), "testorg")

		assert.Error(t, err)
	})
}

func TestSyncRepo(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	t.Run("calls GetRepository", func(t *testing.T) {
		mockClient := github.NewMockClient()
		repoName := "test-repo"
		fullName := "owner/test-repo"
		owner := &gh.User{Login: strPtr("owner")}
		mockClient.GetRepositoryFunc = func(ctx context.Context, o, r string) (*gh.Repository, error) {
			return &gh.Repository{
				Name:     &repoName,
				FullName: &fullName,
				Owner:    owner,
				Private:  boolPtr(false),
				CloneURL: strPtr("https://github.com/owner/test-repo.git"),
			}, nil
		}

		tmpDir := t.TempDir()
		opts := &Options{Target: tmpDir, DryRun: true}
		syncer := New(mockClient, gitInstance, opts)

		result, err := syncer.SyncRepo(context.Background(), "owner", "test-repo")

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 1, mockClient.CallCount("GetRepository"))
	})
}

func TestSyncRepos_DryRun(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	mockClient := github.NewMockClient()

	repoName := "test-repo"
	fullName := "owner/test-repo"
	owner := &gh.User{Login: strPtr("owner")}
	repos := []*gh.Repository{
		{
			Name:     &repoName,
			FullName: &fullName,
			Owner:    owner,
			Private:  boolPtr(false),
			CloneURL: strPtr("https://github.com/owner/test-repo.git"),
		},
	}

	mockClient.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
		return repos, nil
	}

	tmpDir := t.TempDir()
	opts := &Options{Target: tmpDir, DryRun: true, Verbose: false}
	syncer := New(mockClient, gitInstance, opts)

	result, err := syncer.SyncUserRepos(context.Background(), "owner")

	require.NoError(t, err)
	assert.NotNil(t, result)
	// In dry-run mode, new repos are counted as "would be cloned"
	assert.Len(t, result.Cloned, 1)
	assert.Contains(t, result.Cloned, "owner/test-repo")

	// Verify no actual files were created
	repoPath := filepath.Join(tmpDir, "owner", "test-repo")
	_, err = os.Stat(repoPath)
	assert.True(t, os.IsNotExist(err))
}

func TestSyncRepos_Filtered(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	mockClient := github.NewMockClient()

	repos := []*gh.Repository{
		createMockRepo("include-this", "owner/include-this", false),
		createMockRepo("exclude-this", "owner/exclude-this", false),
		createMockRepo("other-repo", "owner/other-repo", false),
	}

	mockClient.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
		return repos, nil
	}

	tmpDir := t.TempDir()
	opts := &Options{
		Target:  tmpDir,
		DryRun:  true,
		Include: []string{"include-*"},
	}
	syncer := New(mockClient, gitInstance, opts)

	result, err := syncer.SyncUserRepos(context.Background(), "owner")

	require.NoError(t, err)
	assert.Len(t, result.Cloned, 1) // Only include-this
	assert.Len(t, result.Skipped, 2) // exclude-this and other-repo
}

func TestSyncRepos_ContextCancelled(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	mockClient := github.NewMockClient()

	repos := []*gh.Repository{
		createMockRepo("repo1", "owner/repo1", false),
		createMockRepo("repo2", "owner/repo2", false),
		createMockRepo("repo3", "owner/repo3", false),
	}

	mockClient.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
		return repos, nil
	}

	tmpDir := t.TempDir()
	opts := &Options{Target: tmpDir}
	syncer := New(mockClient, gitInstance, opts)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := syncer.SyncUserRepos(ctx, "owner")

	assert.ErrorIs(t, err, context.Canceled)
	// Should have stopped before processing all repos
	totalProcessed := len(result.Cloned) + len(result.Updated) + len(result.Skipped) + len(result.Failed)
	assert.Less(t, totalProcessed, 3)
}

func TestSyncRepos_ExistingRepo(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	mockClient := github.NewMockClient()

	repos := []*gh.Repository{
		createMockRepo("existing-repo", "owner/existing-repo", false),
	}

	mockClient.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
		return repos, nil
	}

	tmpDir := t.TempDir()

	// Create an existing repo directory with .git
	repoPath := filepath.Join(tmpDir, "owner", "existing-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))
	gitDir := filepath.Join(repoPath, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0755))

	opts := &Options{Target: tmpDir, DryRun: true}
	syncer := New(mockClient, gitInstance, opts)

	result, err := syncer.SyncUserRepos(context.Background(), "owner")

	require.NoError(t, err)
	// Should be counted as update, not clone
	assert.Len(t, result.Updated, 1)
	assert.Empty(t, result.Cloned)
}

func TestOptions_Defaults(t *testing.T) {
	opts := &Options{}

	assert.Empty(t, opts.Target)
	assert.Empty(t, opts.Include)
	assert.Empty(t, opts.Exclude)
	assert.False(t, opts.IncludePrivate)
	assert.False(t, opts.DryRun)
	assert.False(t, opts.Verbose)
}

// Helper functions

func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func createMockRepo(name, fullName string, private bool) *gh.Repository {
	owner := &gh.User{Login: strPtr("owner")}
	return &gh.Repository{
		Name:     strPtr(name),
		FullName: strPtr(fullName),
		Owner:    owner,
		Private:  boolPtr(private),
		CloneURL: strPtr("https://github.com/" + fullName + ".git"),
		SSHURL:   strPtr("git@github.com:" + fullName + ".git"),
	}
}
