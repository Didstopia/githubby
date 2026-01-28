package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

// TestPullRepo_FastSync tests the fast sync optimization in pullRepo.
// The fast sync should skip git fetch when the repo's PushedAt timestamp
// indicates no changes since the last fetch.
func TestPullRepo_FastSync(t *testing.T) {
	gitInstance, err := git.New()
	if err != nil {
		t.Skip("git is not installed")
	}

	// Base time for all tests
	baseTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		pushedAt       *time.Time              // nil means PushedAt not set
		fetchHeadTime  *time.Time              // nil means no FETCH_HEAD file
		expectedStatus ProgressStatus
		description    string
	}{
		{
			name:           "up-to-date: pushed 1 hour before fetch",
			pushedAt:       timePtr(baseTime.Add(-1 * time.Hour)),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpToDate,
			description:    "Should skip fetch when pushed_at is before last fetch",
		},
		{
			name:           "up-to-date: pushed at same time as fetch",
			pushedAt:       timePtr(baseTime),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpToDate,
			description:    "Should skip fetch when pushed_at equals last fetch (within buffer)",
		},
		{
			name:           "up-to-date: pushed 1 second after fetch (within buffer)",
			pushedAt:       timePtr(baseTime.Add(1 * time.Second)),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpToDate,
			description:    "Should skip fetch when pushed_at is within clock skew buffer",
		},
		{
			name:           "up-to-date: pushed 2 seconds after fetch (at buffer boundary)",
			pushedAt:       timePtr(baseTime.Add(2 * time.Second)),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpToDate,
			description:    "Should skip fetch when pushed_at is at the buffer boundary",
		},
		{
			name:           "needs update: pushed 3 seconds after fetch (outside buffer)",
			pushedAt:       timePtr(baseTime.Add(3 * time.Second)),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpdated,
			description:    "Should fetch when pushed_at is outside the clock skew buffer",
		},
		{
			name:           "needs update: pushed 1 hour after fetch",
			pushedAt:       timePtr(baseTime.Add(1 * time.Hour)),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpdated,
			description:    "Should fetch when pushed_at is significantly after last fetch",
		},
		{
			name:           "nil PushedAt: falls through to fetch",
			pushedAt:       nil,
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpdated,
			description:    "Should fetch when PushedAt is nil (can't determine if up-to-date)",
		},
		{
			name:           "zero PushedAt: falls through to fetch",
			pushedAt:       timePtr(time.Time{}),
			fetchHeadTime:  timePtr(baseTime),
			expectedStatus: ProgressUpdated,
			description:    "Should fetch when PushedAt is zero time",
		},
		{
			name:           "missing FETCH_HEAD: falls through to fetch",
			pushedAt:       timePtr(baseTime.Add(-1 * time.Hour)),
			fetchHeadTime:  nil, // No FETCH_HEAD file
			expectedStatus: ProgressUpdated,
			description:    "Should fetch when FETCH_HEAD doesn't exist (never fetched before)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Set up the mock client
			mockClient := github.NewMockClient()

			// Create syncer with test options
			opts := &Options{
				Target:  tmpDir,
				Verbose: false,
			}
			syncer := New(mockClient, gitInstance, opts)

			// Create mock repo with the test's PushedAt value
			repo := createMockRepoWithPushedAt("test-repo", "owner/test-repo", false, tt.pushedAt)

			// Set up the local repo directory
			repoPath := filepath.Join(tmpDir, "owner", "test-repo")
			gitDir := filepath.Join(repoPath, ".git")
			require.NoError(t, os.MkdirAll(gitDir, 0755))

			// Create FETCH_HEAD if specified
			if tt.fetchHeadTime != nil {
				fetchHeadPath := filepath.Join(gitDir, "FETCH_HEAD")
				require.NoError(t, os.WriteFile(fetchHeadPath, []byte("abc123\n"), 0644))
				require.NoError(t, os.Chtimes(fetchHeadPath, *tt.fetchHeadTime, *tt.fetchHeadTime))
			}

			// For tests that expect ProgressUpdated, we need a real git repo
			// because pullRepo will call FetchAll which requires a valid git repo
			if tt.expectedStatus == ProgressUpdated {
				// Initialize a real git repo for the fetch to work
				initCmd := gitInstance.GitPath
				cmd := exec.CommandContext(context.Background(), initCmd, "init", repoPath)
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to init git repo: %v", err)
				}

				// Set up a dummy remote so fetch doesn't fail
				cmd = exec.CommandContext(context.Background(), initCmd, "-C", repoPath, "remote", "add", "origin", "https://github.com/owner/test-repo.git")
				_ = cmd.Run() // Ignore error if remote already exists

				// Create FETCH_HEAD again after init (init may have removed it)
				if tt.fetchHeadTime != nil {
					fetchHeadPath := filepath.Join(gitDir, "FETCH_HEAD")
					require.NoError(t, os.WriteFile(fetchHeadPath, []byte("abc123\n"), 0644))
					require.NoError(t, os.Chtimes(fetchHeadPath, *tt.fetchHeadTime, *tt.fetchHeadTime))
				}
			}

			// Call pullRepo directly
			status, err := syncer.pullRepo(context.Background(), repo, repoPath)

			// For ProgressUpdated cases, we expect either success or a fetch error
			// (since we don't have a real remote). The important thing is that
			// it didn't return ProgressUpToDate.
			if tt.expectedStatus == ProgressUpdated {
				// Either it succeeded (ProgressUpdated) or failed (fetch error)
				// but it should NOT be ProgressUpToDate
				assert.NotEqual(t, ProgressUpToDate, status,
					"Expected fetch to be attempted (not up-to-date): %s", tt.description)
			} else {
				// For ProgressUpToDate cases, we expect success
				require.NoError(t, err, "Unexpected error: %s", tt.description)
				assert.Equal(t, tt.expectedStatus, status, tt.description)
			}
		})
	}
}

// timePtr returns a pointer to the given time value
func timePtr(t time.Time) *time.Time {
	return &t
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

// createMockRepoWithPushedAt creates a mock repository with a PushedAt timestamp.
// If pushedAt is nil, PushedAt will not be set (tests nil case for fast sync).
func createMockRepoWithPushedAt(name, fullName string, private bool, pushedAt *time.Time) *gh.Repository {
	repo := createMockRepo(name, fullName, private)
	if pushedAt != nil {
		repo.PushedAt = &gh.Timestamp{Time: *pushedAt}
	}
	return repo
}
