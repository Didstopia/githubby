package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/metadata"
)

// verifier returns a Verifier on the fixture, for calling single checks
func (f *fixture) verifier(opts Options) (*Verifier, *Report) {
	opts.Target = f.target
	return New(f.api, f.git, &opts), newReport(f.target, nil)
}

func repoOf(url string) *gh.Repository {
	return &gh.Repository{
		Name: gh.Ptr("r"), FullName: gh.Ptr("o/r"), Owner: &gh.User{Login: gh.Ptr("o")},
		CloneURL: gh.Ptr(url),
	}
}

func errorsOf(rep *Report) string { return strings.Join(rep.Errors, "\n") }

func TestRun_ReportThatCannotBeWrittenIsAnError(t *testing.T) {
	f := newFixture(t)
	f.addRepo("a", false)
	f.backup()
	// A directory where the report goes makes the final rename fail
	require.NoError(t, os.MkdirAll(filepath.Join(f.target, ReportFile, "x"), 0755))

	rep, err := New(f.api, f.git, &Options{Target: f.target, Org: "Didstopia"}).Run(context.Background())
	require.Error(t, err)
	require.NotNil(t, rep, "the report is returned even when it can't be saved")
}

func TestRun_ReportRecordsWhatWasChecked(t *testing.T) {
	f := newFixture(t)
	f.addRepo("a", false)
	f.backup()

	rep := f.verify(Options{Gists: true, Metadata: true})
	assert.Contains(t, rep.Checks, "gists")
	assert.Contains(t, rep.Checks, "release counts and assets")

	raw, err := os.ReadFile(filepath.Join(f.target, ReportFile))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"ok":`)
}

func TestListRepos_AllOrgsAccountErrorFails(t *testing.T) {
	f := newFixture(t)
	f.api.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return nil, errors.New("401") }

	rep := f.verify(Options{AllOrgs: true})
	assert.False(t, rep.OK)
	assert.Contains(t, errorsOf(rep), "listing repos")
	assert.Equal(t, 0, rep.Checked)
}

func TestCheckGitCopy_UnreadableLocalBranchesIsAnError(t *testing.T) {
	f := newFixture(t)
	f.addRepo("a", false)
	// A .git folder with nothing in it: not a usable repo
	local := filepath.Join(f.target, "Didstopia", "a")
	require.NoError(t, os.MkdirAll(filepath.Join(local, ".git"), 0755))

	rep := f.verify(Options{})
	assert.False(t, rep.OK)
	assert.NotEmpty(t, rep.Corrupt, "fsck fails on it")
	assert.Contains(t, errorsOf(rep), "Didstopia/a: can't read local branches")
}

func TestCheckCheckout_Edges(t *testing.T) {
	f := newFixture(t)
	dir := f.addRepo("a", false)
	local := filepath.Join(f.target, "clone")
	runGit(t, "clone", "-q", dir, local)
	head := runGit(t, "-C", local, "rev-parse", "HEAD")
	ctx := context.Background()

	t.Run("empty remote has nothing to check out", func(t *testing.T) {
		v, rep := f.verifier(Options{})
		v.checkCheckout(ctx, repoOf(dir), local, map[string]string{}, rep)
		assert.Zero(t, rep.Problems())
	})

	t.Run("matching head is fine, default branch asked from the remote", func(t *testing.T) {
		v, rep := f.verifier(Options{})
		v.checkCheckout(ctx, repoOf(dir), local, map[string]string{"main": head}, rep)
		assert.Zero(t, rep.Problems())
	})

	t.Run("default branch unknown and remote unreadable", func(t *testing.T) {
		v, rep := f.verifier(Options{})
		v.checkCheckout(ctx, repoOf(filepath.Join(t.TempDir(), "gone")), local, map[string]string{"main": head}, rep)
		assert.Contains(t, errorsOf(rep), "can't read the default branch on GitHub")
	})

	t.Run("default branch missing from the branch list", func(t *testing.T) {
		v, rep := f.verifier(Options{})
		r := repoOf(dir)
		r.DefaultBranch = gh.Ptr("trunk")
		v.checkCheckout(ctx, r, local, map[string]string{"main": head}, rep)
		assert.Contains(t, errorsOf(rep), `default branch "trunk" not found on GitHub`)
	})

	t.Run("checkout that git can't read counts as behind", func(t *testing.T) {
		v, rep := f.verifier(Options{})
		r := repoOf(dir)
		r.DefaultBranch = gh.Ptr("main")
		v.checkCheckout(ctx, r, t.TempDir(), map[string]string{"main": head}, rep)
		require.Len(t, rep.Stale, 1)
		assert.Contains(t, rep.Stale[0], "checked out nothing")
	})
}

func TestCheckPullRefs_Errors(t *testing.T) {
	f := newFixture(t)
	dir := f.addRepo("a", false)
	local := filepath.Join(f.target, "clone")
	runGit(t, "clone", "-q", dir, local)
	ctx := context.Background()

	v, rep := f.verifier(Options{})
	v.checkPullRefs(ctx, "o/r", local, filepath.Join(t.TempDir(), "gone"), rep)
	assert.Contains(t, errorsOf(rep), "can't list pull request refs on GitHub")

	v, rep = f.verifier(Options{})
	v.checkPullRefs(ctx, "o/r", t.TempDir(), dir, rep)
	assert.Contains(t, errorsOf(rep), "can't read local pull request refs")
}

func TestCheckSubmodules_StatusErrorAndConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	t.Run("unreadable status is an error", func(t *testing.T) {
		repo := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitmodules"), nil, 0644))
		v, rep := f.verifier(Options{})
		v.checkSubmodules(ctx, "o/r", repo, rep)
		assert.Contains(t, errorsOf(rep), "can't read submodule status")
	})

	t.Run("merge conflict in a submodule fails", func(t *testing.T) {
		gitPath := filepath.Join(t.TempDir(), "git")
		require.NoError(t, os.WriteFile(gitPath, []byte("#!/bin/sh\necho 'Uabcdef0123456789 lib/x'\n"), 0755))
		repo := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitmodules"), nil, 0644))
		v := New(f.api, &git.Git{GitPath: gitPath, Quiet: true}, &Options{Target: f.target})
		rep := newReport(f.target, nil)
		v.checkSubmodules(ctx, "o/r", repo, rep)
		require.Len(t, rep.Submodules, 1)
		assert.Contains(t, rep.Submodules[0], "submodule lib/x has merge conflicts")
	})
}

func TestCheckWiki_OtherErrorsAreNotTakenAsNoWiki(t *testing.T) {
	f := newFixture(t)
	v, rep := f.verifier(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v.checkWiki(ctx, repoOf(filepath.Join(t.TempDir(), "r")), rep)
	assert.Contains(t, errorsOf(rep), "can't tell if the wiki exists")
	assert.Zero(t, rep.Checked, "the wiki was not counted as checked")
}

func TestCheckWiki_NoWikiOnGitHubIsFine(t *testing.T) {
	f := newFixture(t)
	f.addRepo("a", false) // has_wiki is set but the wiki repo was never created
	f.backup()
	rep := f.verify(Options{})
	assert.True(t, rep.OK)
	assert.Equal(t, 1, rep.Checked)
}

func TestCheckMetadata_BothListsUnreadableAndPullMismatch(t *testing.T) {
	f := newFixture(t)
	f.addRepo("a", false)
	f.addRepo("b", false)
	f.backup()

	dirA := metadata.RepoDir(f.target, "Didstopia", "a")
	require.NoError(t, os.Remove(filepath.Join(dirA, metadata.IssuesFile)))
	require.NoError(t, os.Remove(filepath.Join(dirA, metadata.PullsFile)))
	f.api.GetIssuePRCountsFunc = func(ctx context.Context, owner, repo string) (int, int, error) {
		return 1, 3, nil
	}

	rep := f.verify(Options{Metadata: true})
	assert.False(t, rep.OK)
	got := strings.Join(rep.Metadata, "\n")
	assert.Contains(t, got, "Didstopia/a: issues.json missing")
	assert.Contains(t, got, "Didstopia/a: pulls.json missing")
	assert.Contains(t, got, "Didstopia/b: 0 pull requests saved, GitHub has 3")
	assert.NotContains(t, got, "Didstopia/a: 0 pull")
}

func TestCheckMetadata_ReleaseListError(t *testing.T) {
	f := newFixture(t)
	f.addRepo("a", false)
	f.backup()
	f.api.GetReleasesFunc = func(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
		return nil, errors.New("boom")
	}
	rep := f.verify(Options{Metadata: true})
	assert.False(t, rep.OK)
	assert.Contains(t, errorsOf(rep), "can't list releases on GitHub")
}

func TestCheckGists_CancelledStopsAndFails(t *testing.T) {
	f := newFixture(t)
	f.addGist("g1")
	f.addGist("g2")
	ctx, cancel := context.WithCancel(context.Background())
	f.api.ListGistsFunc = func(context.Context) ([]*gh.Gist, error) {
		cancel()
		return f.gists, nil
	}

	v, rep := f.verifier(Options{Gists: true})
	v.checkGists(ctx, rep)
	assert.Zero(t, rep.Checked, "nothing is checked after cancel")

	// And Run never reports OK for a cancelled context
	ctx2, cancel2 := context.WithCancel(context.Background())
	f.api.ListGistsFunc = func(context.Context) ([]*gh.Gist, error) { cancel2(); return f.gists, nil }
	out, err := New(f.api, f.git, &Options{Target: f.target, Org: "Didstopia", Gists: true}).Run(ctx2)
	require.NoError(t, err)
	assert.False(t, out.OK)
}
