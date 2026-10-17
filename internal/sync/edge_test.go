package sync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

type progressEvent struct {
	repo   string
	status ProgressStatus
}

func newSyncer(t *testing.T, opts *Options) *Syncer {
	t.Helper()
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	if opts.Target == "" {
		opts.Target = t.TempDir()
	}
	return New(github.NewMockClient(), g, opts)
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// noPackageManagers hides brew, apt and friends, so git-lfs can never really be installed
func noPackageManagers(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestSyncRepo_GetRepositoryErrorFails(t *testing.T) {
	s := newSyncer(t, &Options{})
	s.ghClient.(*github.MockClient).GetRepositoryFunc = func(ctx context.Context, owner, repo string) (*gh.Repository, error) {
		return nil, errors.New("404")
	}
	result, err := s.SyncRepo(context.Background(), "o", "r")
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "failed to get repository")
}

func TestSyncSelectedRepos_BadNamesAndLookupErrorsAreFailures(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	s := newSyncer(t, &Options{})
	s.ghClient.(*github.MockClient).GetRepositoryFunc = func(ctx context.Context, owner, repo string) (*gh.Repository, error) {
		if repo == "bad" {
			return nil, errors.New("404")
		}
		return localRepo(owner, repo, up), nil
	}

	result, err := s.SyncSelectedRepos(context.Background(), []string{"o/good", "nonsense", "o/bad"})
	require.NoError(t, err)
	assert.Equal(t, []string{"o/good"}, result.Cloned)
	assert.Len(t, result.Failed, 2)
	assert.ErrorContains(t, result.Failed["nonsense"], "invalid repository name")
	assert.ErrorContains(t, result.Failed["o/bad"], "404")
}

func TestSyncRepos_TargetThatIsAFileFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0644))
	s := newSyncer(t, &Options{Target: filepath.Join(file, "sub")})

	result, err := s.SyncRepos(context.Background(), []*gh.Repository{localRepo("o", "r", "x")})
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "failed to create target directory")
}

func TestSyncSingleRepo_ProgressAndVerboseOutput(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	var events []progressEvent
	s := newSyncer(t, &Options{Verbose: true, Exclude: []string{"skipme"}, OnProgress: func(repo string, st ProgressStatus, msg string) {
		events = append(events, progressEvent{repo, st})
	}})
	ctx := context.Background()

	result := NewResult()
	s.syncSingleRepo(ctx, localRepo("o", "skipme", up), result)
	s.syncSingleRepo(ctx, localRepo("o", "r", up), result) // clone
	s.syncSingleRepo(ctx, localRepo("o", "r", up), result) // update
	require.Empty(t, result.Failed)
	assert.Equal(t, []string{"o/skipme"}, result.Skipped)
	assert.Equal(t, []string{"o/r"}, result.Cloned)
	assert.Equal(t, []string{"o/r"}, result.Updated)

	assert.Equal(t, []progressEvent{
		{"o/skipme", ProgressSkipped},
		{"o/r", ProgressInProgress}, {"o/r", ProgressCloned},
		{"o/r", ProgressInProgress}, {"o/r", ProgressUpdated},
	}, events)
}

func TestSyncSingleRepo_DryRunVerbose(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	target := t.TempDir()
	require.NoError(t, exec.Command("git", "clone", "-q", up, filepath.Join(target, "o", "have")).Run())
	var events []progressEvent
	s := newSyncer(t, &Options{Target: target, DryRun: true, Verbose: true, OnProgress: func(repo string, st ProgressStatus, msg string) {
		events = append(events, progressEvent{repo, st})
	}})

	result := NewResult()
	s.syncSingleRepo(context.Background(), localRepo("o", "have", up), result)
	s.syncSingleRepo(context.Background(), localRepo("o", "new", up), result)
	assert.Equal(t, []string{"o/have"}, result.Updated)
	assert.Equal(t, []string{"o/new"}, result.Cloned)
	assert.Equal(t, []progressEvent{{"o/have", ProgressUpdated}, {"o/new", ProgressCloned}}, events)
	assert.NoDirExists(t, filepath.Join(target, "o", "new"))
}

func TestSyncSingleRepo_FailuresAreRecordedNotSwallowed(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	var events []progressEvent
	s := newSyncer(t, &Options{Verbose: true, OnProgress: func(repo string, st ProgressStatus, msg string) {
		events = append(events, progressEvent{repo, st})
	}})

	t.Run("clone of a missing upstream", func(t *testing.T) {
		result := NewResult()
		s.syncSingleRepo(context.Background(), localRepo("o", "gone", filepath.Join(t.TempDir(), "missing")), result)
		assert.Contains(t, result.Failed, "o/gone")
		assert.Empty(t, result.Cloned)
		assert.Equal(t, progressEvent{"o/gone", ProgressFailed}, events[len(events)-1])
	})

	t.Run("fetch that fails", func(t *testing.T) {
		result := NewResult()
		s.syncSingleRepo(context.Background(), localRepo("o", "r", up), result)
		require.Contains(t, result.Cloned, "o/r")

		// Cancelled context makes the fetch fail without waiting for retries
		s.syncSingleRepo(cancelled(), localRepo("o", "r", up), result)
		assert.Contains(t, result.Failed, "o/r")
		assert.ErrorContains(t, result.Failed["o/r"], "fetch failed")
		assert.Empty(t, result.Updated)
	})
}

func TestSyncSingleRepo_WikiSyncsEvenWhenRepoFails(t *testing.T) {
	wikiUp := newUpstream(t, map[string]string{"Home.md": "hi"})
	target := t.TempDir()
	s := newSyncer(t, &Options{Target: target, Wikis: true})
	repo := localRepo("o", "r", filepath.Join(t.TempDir(), "missing-main"))
	repo.HasWiki = gh.Ptr(true)
	// The wiki URL is clone URL minus .git plus .wiki.git
	require.NoError(t, os.Rename(wikiUp, repo.GetCloneURL()+".wiki.git"))

	result := NewResult()
	s.syncSingleRepo(context.Background(), repo, result)
	assert.Contains(t, result.Failed, "o/r")
	assert.Equal(t, []string{"o/r.wiki"}, result.Wikis)
}

func TestSyncWiki_RealFailuresAreReported(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	s := newSyncer(t, &Options{})
	repo := localRepo("o", "r", up)
	repo.HasWiki = gh.Ptr(true)
	wikiURL := WikiURL(repo)
	require.NoError(t, exec.Command("git", "clone", "-q", up, wikiURL).Run())

	result := NewResult()
	s.syncWiki(context.Background(), repo, result)
	require.Equal(t, []string{"o/r.wiki"}, result.Wikis)

	// A fetch that fails for a reason other than a missing wiki is a failure
	s.syncWiki(cancelled(), repo, result)
	assert.Contains(t, result.Failed, "o/r.wiki")
}

func TestSyncGists_Errors(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	g, err := git.NewQuiet()
	require.NoError(t, err)
	gist := &gh.Gist{ID: gh.Ptr("g1"), GitPullURL: gh.Ptr(up)}

	t.Run("user lookup fails", func(t *testing.T) {
		m := gistMock(t, gist)
		m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return nil, errors.New("401") }
		err := New(m, g, &Options{Target: t.TempDir()}).SyncGists(context.Background(), NewResult())
		assert.ErrorContains(t, err, "failed to get authenticated user")
	})
	t.Run("listing fails", func(t *testing.T) {
		m := gistMock(t, gist)
		m.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) { return nil, errors.New("scope") }
		err := New(m, g, &Options{Target: t.TempDir()}).SyncGists(context.Background(), NewResult())
		assert.ErrorContains(t, err, "failed to list gists")
	})
	t.Run("cancelled run stops", func(t *testing.T) {
		err := New(gistMock(t, gist), g, &Options{Target: t.TempDir()}).SyncGists(cancelled(), NewResult())
		assert.ErrorIs(t, err, context.Canceled)
	})
	t.Run("dry run lists without cloning", func(t *testing.T) {
		target := t.TempDir()
		result := NewResult()
		require.NoError(t, New(gistMock(t, gist), g, &Options{Target: target, DryRun: true}).SyncGists(context.Background(), result))
		assert.Equal(t, []string{"gist g1"}, result.Gists)
		assert.NoDirExists(t, filepath.Join(target, "Dids.gists"))
	})
	t.Run("gists dir cannot be created", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0644))
		result := NewResult()
		require.NoError(t, New(gistMock(t, gist), g, &Options{Target: filepath.Join(file, "t")}).SyncGists(context.Background(), result))
		assert.Contains(t, result.Failed, "gist g1")
		assert.Empty(t, result.Gists)
	})
	t.Run("fetch of an existing gist fails", func(t *testing.T) {
		target := t.TempDir()
		s := New(gistMock(t, gist), g, &Options{Target: target})
		result := NewResult()
		require.NoError(t, s.SyncGists(context.Background(), result))
		require.Equal(t, []string{"gist g1"}, result.Gists)

		ctx, cancel := context.WithCancel(context.Background())
		s.ghClient.(*github.MockClient).ListGistsFunc = func(context.Context) ([]*gh.Gist, error) {
			cancel() // the fetch itself runs on a cancelled context
			return []*gh.Gist{gist}, nil
		}
		result = NewResult()
		_ = s.SyncGists(ctx, result)
		assert.Empty(t, result.Gists)
	})
}

func TestUpdateWorkTree_Warnings(t *testing.T) {
	s := newSyncer(t, &Options{})
	ctx := context.Background()

	t.Run("default branch can't be found", func(t *testing.T) {
		repoDir := t.TempDir()
		runGit(t, "init", "-q", repoDir)
		result := NewResult()
		s.updateWorkTree(ctx, localRepo("o", "r", ""), repoDir, result)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "working tree: could not determine default branch")
	})

	t.Run("fast-forward fails", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := filepath.Join(t.TempDir(), "c")
		runGit(t, "clone", "-q", up, clone)
		require.NoError(t, os.WriteFile(filepath.Join(clone, "local"), []byte("x"), 0644))
		runGit(t, "-C", clone, "add", "-A")
		runGit(t, "-C", clone, "commit", "-q", "-m", "local")
		require.NoError(t, os.WriteFile(filepath.Join(up, "b"), []byte("b"), 0644))
		runGit(t, "-C", up, "add", "-A")
		runGit(t, "-C", up, "commit", "-q", "-m", "b")
		runGit(t, "-C", clone, "fetch", "-q")

		repo := localRepo("o", "r", up)
		repo.DefaultBranch = gh.Ptr("main")
		result := NewResult()
		s.updateWorkTree(ctx, repo, clone, result)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "working tree:")
	})
}

func TestAfterSync_SubmoduleAndPullRefFailuresAreWarnings(t *testing.T) {
	s := newSyncer(t, &Options{Submodules: true, PullRefs: true})
	repoDir := t.TempDir()
	runGit(t, "init", "-q", repoDir)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ".gitmodules"), []byte("[submodule \"x\"]\n\tpath = x\n\turl = /nowhere\n"), 0644))

	result := NewResult()
	s.afterSync(cancelled(), localRepo("o", "r", ""), repoDir, true, result)
	require.Len(t, result.Warnings, 2)
	assert.Contains(t, result.Warnings[0], "submodules:")
	assert.Contains(t, result.Warnings[1], "pull request refs:")
}

func TestDetectArchived_Edges(t *testing.T) {
	t.Run("missing target has nothing archived", func(t *testing.T) {
		s := newSyncer(t, &Options{Target: filepath.Join(t.TempDir(), "missing")})
		assert.Empty(t, s.detectArchived(nil))
	})

	t.Run("wikis of listed repos are not archived, others are", func(t *testing.T) {
		target := t.TempDir()
		for _, p := range []string{"o/kept", "o/kept.wiki", "o/old", "o/old.wiki", "u.gists/abc"} {
			require.NoError(t, os.MkdirAll(filepath.Join(target, p, ".git"), 0755))
		}
		s := newSyncer(t, &Options{Target: target})
		kept := localRepo("o", "kept", "")
		kept.HasWiki = gh.Ptr(true)
		assert.ElementsMatch(t, []string{"o/old", "o/old.wiki"}, s.detectArchived([]*gh.Repository{kept}))
	})

	t.Run("unreadable folders are skipped", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads everything")
		}
		target := t.TempDir()
		locked := filepath.Join(target, "locked")
		require.NoError(t, os.MkdirAll(filepath.Join(locked, "r", ".git"), 0755))
		require.NoError(t, os.Chmod(locked, 0000)) //nolint:gosec // unreadable on purpose
		t.Cleanup(func() { _ = os.Chmod(locked, 0755) })
		s := newSyncer(t, &Options{Target: target})
		assert.Empty(t, s.detectArchived(nil))
	})
}

func TestCloneRepo_ParentDirectoryCannotBeCreated(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "o"), nil, 0644))
	s := newSyncer(t, &Options{Target: target})

	err := s.cloneRepo(context.Background(), localRepo("o", "r", "x"), filepath.Join(target, "o", "r"))
	assert.ErrorContains(t, err, "failed to create parent directory")
}

// usesLFSUpstream is an upstream with a .gitattributes LFS rule and no LFS files
func usesLFSUpstream(t *testing.T) string {
	t.Helper()
	return newUpstream(t, map[string]string{".gitattributes": "*.bin filter=lfs diff=lfs merge=lfs -text\n", "a": "a"})
}

func TestCloneAndPull_LFSNotAvailableIsNotFatal(t *testing.T) {
	g, err := git.NewQuiet()
	require.NoError(t, err)
	up := usesLFSUpstream(t)
	target := t.TempDir()
	noPackageManagers(t)
	s := New(github.NewMockClient(), g, &Options{Target: target, Verbose: true})
	s.lfs.LFSPath = ""

	local := filepath.Join(target, "o", "r")
	require.NoError(t, s.cloneRepo(context.Background(), localRepo("o", "r", up), local))
	assert.DirExists(t, filepath.Join(local, ".git"))

	status, err := s.pullRepo(context.Background(), localRepo("o", "r", up), local)
	require.NoError(t, err)
	assert.Equal(t, ProgressUpdated, status)
}

func TestPullRepo_FastSyncVerboseReasons(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	s := newSyncer(t, &Options{Verbose: true})
	local := filepath.Join(t.TempDir(), "c")
	runGit(t, "clone", "-q", up, local)
	ctx := context.Background()
	hour := gh.Timestamp{Time: time.Now().Add(-time.Hour)}

	t.Run("pushed_at missing", func(t *testing.T) {
		status, err := s.pullRepo(ctx, localRepo("o", "r", up), local)
		require.NoError(t, err)
		assert.Equal(t, ProgressUpdated, status)
	})
	t.Run("pushed_at zero", func(t *testing.T) {
		repo := localRepo("o", "r", up)
		repo.PushedAt = &gh.Timestamp{}
		status, err := s.pullRepo(ctx, repo, local)
		require.NoError(t, err)
		assert.Equal(t, ProgressUpdated, status)
	})
	t.Run("no FETCH_HEAD", func(t *testing.T) {
		require.NoError(t, os.Remove(filepath.Join(local, ".git", "FETCH_HEAD")))
		repo := localRepo("o", "r", up)
		repo.PushedAt = &hour
		status, err := s.pullRepo(ctx, repo, local)
		require.NoError(t, err)
		assert.Equal(t, ProgressUpdated, status)
	})
	t.Run("pushed after the last fetch", func(t *testing.T) {
		fh := filepath.Join(local, ".git", "FETCH_HEAD")
		old := time.Now().Add(-2 * time.Hour)
		require.NoError(t, os.Chtimes(fh, old, old))
		repo := localRepo("o", "r", up)
		repo.PushedAt = &hour
		status, err := s.pullRepo(ctx, repo, local)
		require.NoError(t, err)
		assert.Equal(t, ProgressUpdated, status)
	})
	t.Run("not pushed since the last fetch", func(t *testing.T) {
		repo := localRepo("o", "r", up)
		repo.PushedAt = &gh.Timestamp{Time: time.Now().Add(-3 * time.Hour)}
		status, err := s.pullRepo(ctx, repo, local)
		require.NoError(t, err)
		assert.Equal(t, ProgressUpToDate, status)
	})
}

// fastRetries removes the backoff waits, so retry paths run in milliseconds
func fastRetries(t *testing.T) {
	t.Helper()
	orig := gitRetryConfig
	gitRetryConfig = func() *GitRetryConfig {
		return &GitRetryConfig{MaxAttempts: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1}
	}
	t.Cleanup(func() { gitRetryConfig = orig })
}

// busyGit is a git whose every command fails with a transient-looking error
func busyGit(t *testing.T) *git.Git {
	t.Helper()
	script := filepath.Join(t.TempDir(), "git")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n# leave a partial clone behind\nfor last; do :; done\nmkdir -p \"$last\" 2>/dev/null\necho 'file is locked' >&2\nexit 1\n"), 0755))
	return &git.Git{GitPath: script, Quiet: true}
}

func TestRetries_CleanUpPartialClonesBetweenAttempts(t *testing.T) {
	fastRetries(t)
	ctx := context.Background()

	t.Run("repo clone", func(t *testing.T) {
		s := New(github.NewMockClient(), busyGit(t), &Options{Target: t.TempDir()})
		local := filepath.Join(s.opts.Target, "o", "r")
		err := s.cloneRepo(ctx, localRepo("o", "r", "x"), local)
		assert.ErrorIs(t, err, git.ErrCloneFailed)
		assert.ErrorContains(t, err, "file is locked")
	})

	t.Run("wiki clone", func(t *testing.T) {
		s := New(github.NewMockClient(), busyGit(t), &Options{Target: t.TempDir()})
		repo := localRepo("o", "r", "x")
		repo.HasWiki = gh.Ptr(true)
		result := NewResult()
		s.syncWiki(ctx, repo, result)
		assert.Contains(t, result.Failed, "o/r.wiki")
	})

	t.Run("gist clone", func(t *testing.T) {
		gist := &gh.Gist{ID: gh.Ptr("g1"), GitPullURL: gh.Ptr("x")}
		result := NewResult()
		s := New(gistMock(t, gist), busyGit(t), &Options{Target: t.TempDir()})
		require.NoError(t, s.SyncGists(ctx, result))
		assert.Contains(t, result.Failed, "gist g1")
	})
}

func TestSyncSingleRepo_UpToDateVerbose(t *testing.T) {
	up := newUpstream(t, map[string]string{"a": "a"})
	s := newSyncer(t, &Options{Verbose: true})
	repo := localRepo("o", "r", up)
	result := NewResult()
	s.syncSingleRepo(context.Background(), repo, result) // clone
	s.syncSingleRepo(context.Background(), repo, result) // first fetch writes FETCH_HEAD

	repo.PushedAt = &gh.Timestamp{Time: time.Now().Add(-48 * time.Hour)}
	s.syncSingleRepo(context.Background(), repo, result)
	assert.Equal(t, []string{"o/r"}, result.UpToDate)
}

func TestSyncWiki_MissingWikiVerbose(t *testing.T) {
	s := newSyncer(t, &Options{Verbose: true})
	repo := localRepo("o", "r", filepath.Join(t.TempDir(), "missing"))
	repo.HasWiki = gh.Ptr(true)
	result := NewResult()
	s.syncWiki(context.Background(), repo, result)
	assert.Empty(t, result.Failed)
	assert.Empty(t, result.Wikis)
}

// lfsWithFailingPull is an LFS whose install step works but 'lfs pull' fails
func lfsWithFailingPull(t *testing.T) *git.LFS {
	t.Helper()
	script := filepath.Join(t.TempDir(), "git")
	body := "#!/bin/sh\ncase \"$*\" in *\"lfs pull\"*) echo pull broke >&2; exit 1;; esac\nexit 0\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0755))
	return &git.LFS{LFSPath: "/fake/git-lfs", Git: &git.Git{GitPath: script, Quiet: true}, Quiet: true}
}

func TestLFSPullFailureOnCloneAndPullIsNotFatal(t *testing.T) {
	up := usesLFSUpstream(t)
	s := newSyncer(t, &Options{Verbose: true})
	s.lfs = lfsWithFailingPull(t)
	local := filepath.Join(s.opts.Target, "o", "r")

	require.NoError(t, s.cloneRepo(context.Background(), localRepo("o", "r", up), local))
	status, err := s.pullRepo(context.Background(), localRepo("o", "r", up), local)
	require.NoError(t, err)
	assert.Equal(t, ProgressUpdated, status)
}

func TestUpdateWorkTree_LFSPullFailureIsWarning(t *testing.T) {
	up := usesLFSUpstream(t)
	s := newSyncer(t, &Options{})
	clone := filepath.Join(t.TempDir(), "c")
	runGit(t, "clone", "-q", up, clone)
	require.NoError(t, os.WriteFile(filepath.Join(up, "b"), []byte("b"), 0644))
	runGit(t, "-C", up, "add", "-A")
	runGit(t, "-C", up, "commit", "-q", "-m", "b")
	runGit(t, "-C", clone, "fetch", "-q")
	s.lfs = lfsWithFailingPull(t)

	repo := localRepo("o", "r", up)
	repo.DefaultBranch = gh.Ptr("main")
	result := NewResult()
	s.updateWorkTree(context.Background(), repo, clone, result)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "LFS:")
}

func TestAfterSync_LFSFetchFailureIsWarning(t *testing.T) {
	s := newSyncer(t, &Options{LFSAll: true})
	s.lfs = lfsWithFailingFetchAll(t)
	result := NewResult()
	s.afterSync(context.Background(), localRepo("o", "r", ""), t.TempDir(), true, result)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "LFS:")
}

func lfsWithFailingFetchAll(t *testing.T) *git.LFS {
	t.Helper()
	script := filepath.Join(t.TempDir(), "git")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho fetch broke >&2\nexit 1\n"), 0755))
	return &git.LFS{LFSPath: "/fake/git-lfs", Git: &git.Git{GitPath: script, Quiet: true}, Quiet: true}
}
