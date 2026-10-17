package verify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/metadata"
	"github.com/Didstopia/githubby/internal/sync"
)

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0644))
	runGit(t, "-C", dir, "add", "-A")
	runGit(t, "-C", dir, "commit", "-q", "-m", "change "+file)
}

func upstream(t *testing.T, path string) string {
	t.Helper()
	runGit(t, "init", "-q", path)
	commit(t, path, "README", "hello")
	return path
}

// fixture is a fake GitHub (mock API plus local upstream repos) and a backup of it
type fixture struct {
	t      *testing.T
	api    *github.MockClient
	git    *git.Git
	target string
	repos  []*gh.Repository
	gists  []*gh.Gist
}

func newFixture(t *testing.T) *fixture {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	f := &fixture{t: t, api: github.NewMockClient(), git: g, target: t.TempDir()}
	f.api.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		if !o.IncludePrivate {
			return nil, errors.New("verify must always include private repos")
		}
		return f.repos, nil
	}
	f.api.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) {
		return &gh.User{Login: gh.Ptr("Dids")}, nil
	}
	f.api.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) { return f.gists, nil }
	f.api.GetIssuePRCountsFunc = func(ctx context.Context, owner, repo string) (int, int, error) { return 1, 0, nil }
	return f
}

// addRepo creates an upstream repo (and wiki) and lists it on the fake GitHub
func (f *fixture) addRepo(name string, wiki bool) string {
	dir := filepath.Join(f.t.TempDir(), name)
	upstream(f.t, dir)
	if wiki {
		upstream(f.t, dir+".wiki.git")
	}
	f.repos = append(f.repos, &gh.Repository{
		Name: gh.Ptr(name), FullName: gh.Ptr("Didstopia/" + name),
		Owner: &gh.User{Login: gh.Ptr("Didstopia")}, CloneURL: gh.Ptr(dir), HasWiki: gh.Ptr(true),
	})
	return dir
}

func (f *fixture) addGist(id string) string {
	dir := upstream(f.t, filepath.Join(f.t.TempDir(), id))
	f.gists = append(f.gists, &gh.Gist{ID: gh.Ptr(id), GitPullURL: gh.Ptr(dir)})
	return dir
}

// backup runs a real sync of everything into the target, plus a metadata stand-in
func (f *fixture) backup() {
	s := sync.New(f.api, f.git, &sync.Options{Target: f.target, Wikis: true, LFSAll: true, PullRefs: true, Submodules: true})
	result, err := s.SyncRepos(context.Background(), f.repos)
	require.NoError(f.t, err)
	require.Empty(f.t, result.Failed)
	require.NoError(f.t, s.SyncGists(context.Background(), result))
	require.Empty(f.t, result.Failed)

	for _, r := range f.repos {
		dir := metadata.RepoDir(f.target, "Didstopia", r.GetName())
		require.NoError(f.t, os.MkdirAll(dir, 0755))
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, metadata.IssuesFile), []byte(`[{"issue":{}}]`), 0644))
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, metadata.PullsFile), []byte(`[]`), 0644))
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, metadata.ReleasesFile), []byte(`[]`), 0644))
	}
	f.writeOwnerMeta("Didstopia", metadata.OrgFiles)
	f.writeOwnerMeta("Dids", metadata.UserFiles)
}

func (f *fixture) writeOwnerMeta(login string, files []string) {
	dir := metadata.OwnerDir(f.target, login)
	require.NoError(f.t, os.MkdirAll(dir, 0755))
	for _, file := range files {
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, file), []byte(`[]`), 0644))
	}
}

func (f *fixture) verify(opts Options) *Report {
	opts.Target = f.target
	if opts.Org == "" && opts.User == "" && !opts.AllOrgs {
		opts.Org = "Didstopia"
	}
	rep, err := New(f.api, f.git, &opts).Run(context.Background())
	require.NoError(f.t, err)
	return rep
}

func needLFS(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs is not installed")
	}
}

func TestVerify_CleanBackupIsOK(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	f.addRepo("tool", true)
	f.addRepo("site", false)
	f.addGist("g1")
	f.backup()

	rep := f.verify(Options{Gists: true, Metadata: true})
	assert.True(t, rep.OK, "problems: %+v", rep)
	assert.Equal(t, []string{"Didstopia/site", "Didstopia/tool", "Didstopia/tool.wiki", "gist g1"}, rep.Items)
	assert.Contains(t, rep.Checks, "gists")
	assert.Contains(t, rep.Checks, "metadata issue and pull request counts")

	var saved Report
	raw, err := os.ReadFile(filepath.Join(f.target, ReportFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &saved))
	assert.True(t, saved.OK)
	assert.False(t, saved.Time.IsZero())
}

func TestVerify_FindsProblems(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	stale := f.addRepo("stale", false)
	f.addRepo("corrupt", false)
	f.addRepo("wiki", true)
	f.addRepo("gone", false)
	f.backup()

	// GitHub moves on after the backup
	commit(t, stale, "new.txt", "new")
	runGit(t, "-C", stale, "branch", "feature")

	// A loose object in the backup goes bad
	clone := filepath.Join(f.target, "Didstopia", "corrupt")
	blob := runGit(t, "-C", clone, "rev-parse", "origin/main:README")
	obj := filepath.Join(clone, ".git", "objects", blob[:2], blob[2:])
	require.NoError(t, os.Chmod(obj, 0644))
	require.NoError(t, os.WriteFile(obj, []byte("garbage"), 0644))

	// The wiki backup disappears, and a repo appears that was never backed up
	require.NoError(t, os.RemoveAll(filepath.Join(f.target, "Didstopia", "wiki.wiki")))
	f.addRepo("new", false)

	// A repo's upstream can't be reached
	require.NoError(t, os.RemoveAll(f.repos[3].GetCloneURL()))

	rep := f.verify(Options{})
	assert.False(t, rep.OK)
	assert.Equal(t, []string{"Didstopia/new", "Didstopia/wiki.wiki"}, rep.Missing)
	require.Len(t, rep.Corrupt, 1)
	assert.Contains(t, rep.Corrupt[0], "Didstopia/corrupt")
	assert.Len(t, rep.Stale, 2)
	assert.Contains(t, strings.Join(rep.Stale, "\n"), "branch feature missing locally")
	assert.Contains(t, strings.Join(rep.Stale, "\n"), "branch main is at")
	require.Len(t, rep.Errors, 1)
	assert.Contains(t, rep.Errors[0], "Didstopia/gone: can't list branches on GitHub")
}

func TestVerify_LFS(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	dir := f.addRepo("lfs", false)
	runGit(t, "lfs", "install", "--skip-repo")
	runGit(t, "-C", dir, "lfs", "track", "*.bin")
	runGit(t, "-C", dir, "checkout", "-q", "-b", "side")
	commit(t, dir, "big.bin", "lfs data")
	runGit(t, "-C", dir, "checkout", "-q", "main")
	f.backup()

	assert.True(t, f.verify(Options{}).OK, "sync fetched the side branch's LFS object")

	// The LFS object vanishes from the backup
	require.NoError(t, os.RemoveAll(filepath.Join(f.target, "Didstopia", "lfs", ".git", "lfs", "objects")))
	rep := f.verify(Options{})
	assert.False(t, rep.OK)
	require.Len(t, rep.LFS, 1)
	assert.Contains(t, rep.LFS[0], "Didstopia/lfs: big.bin")

	// Without git-lfs the check can't run, which is a failure
	v := New(f.api, f.git, &Options{Target: f.target, Org: "Didstopia"})
	v.lfs.LFSPath = ""
	rep, err := v.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, rep.OK)
	assert.Contains(t, strings.Join(rep.Errors, "\n"), "can't check LFS objects")
}

func TestVerify_Metadata(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	f.addRepo("a", false)
	f.addRepo("b", false)
	f.addRepo("c", false)
	f.addRepo("d", false)
	f.backup()

	require.NoError(t, os.RemoveAll(metadata.RepoDir(f.target, "Didstopia", "b")))
	require.NoError(t, os.Remove(filepath.Join(metadata.RepoDir(f.target, "Didstopia", "c"), metadata.PullsFile)))
	require.NoError(t, os.WriteFile(filepath.Join(metadata.RepoDir(f.target, "Didstopia", "d"), metadata.IssuesFile), []byte("{"), 0644))
	f.api.GetIssuePRCountsFunc = func(ctx context.Context, owner, repo string) (int, int, error) {
		if repo == "a" {
			return 2, 0, nil
		}
		return 1, 0, nil
	}

	// Without --metadata nothing is said about it
	assert.True(t, f.verify(Options{}).OK)

	rep := f.verify(Options{Metadata: true})
	assert.False(t, rep.OK)
	got := strings.Join(rep.Metadata, "\n")
	assert.Contains(t, got, "Didstopia/a: 1 issues saved, GitHub has 2")
	assert.Contains(t, got, "Didstopia/b: metadata directory missing")
	assert.Contains(t, got, "Didstopia/c: pulls.json missing")
	assert.Contains(t, got, "Didstopia/d: issues.json unreadable")

	f.api.GetIssuePRCountsFunc = func(ctx context.Context, owner, repo string) (int, int, error) {
		return 0, 0, errors.New("rate limited")
	}
	rep = f.verify(Options{Metadata: true})
	assert.Contains(t, strings.Join(rep.Errors, "\n"), "can't count issues and pull requests")
}

func TestVerify_Gists(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	f.addGist("g1")
	f.backup()
	f.addGist("g2")

	rep := f.verify(Options{Gists: true})
	assert.Equal(t, []string{"gist g2"}, rep.Missing)

	f.api.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) { return nil, errors.New("no gist scope") }
	rep = f.verify(Options{Gists: true})
	assert.False(t, rep.OK)
	assert.Contains(t, strings.Join(rep.Errors, "\n"), "gists: can't list")

	f.api.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return nil, errors.New("401") }
	rep = f.verify(Options{Gists: true})
	assert.Contains(t, strings.Join(rep.Errors, "\n"), "gists: can't get the token's account")
}

func TestVerify_ListingAndTargetErrorsFail(t *testing.T) {
	f := newFixture(t)

	f.api.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return nil, errors.New("api down")
	}
	rep := f.verify(Options{})
	assert.False(t, rep.OK)
	assert.Contains(t, rep.Errors[0], "listing repos: api down")

	// All orgs: one org that can't be listed fails the run
	f.api.ListUserOrgsFunc = func(ctx context.Context) ([]*gh.Organization, error) {
		return []*gh.Organization{{Login: gh.Ptr("Didstopia")}}, nil
	}
	rep = f.verify(Options{AllOrgs: true})
	assert.False(t, rep.OK)
	assert.Contains(t, strings.Join(rep.Errors, "\n"), "listing repos of Didstopia")

	// User mode lists that user's repos
	rep = f.verify(Options{User: "Dids"})
	assert.True(t, rep.OK)
	assert.Equal(t, 2, f.api.CallCount("ListUserRepos"), "all-orgs and user mode each list the user's repos once")

	rep, err := New(f.api, f.git, &Options{Target: filepath.Join(t.TempDir(), "missing"), Org: "x"}).Run(context.Background())
	require.NoError(t, err)
	assert.False(t, rep.OK)
	assert.Contains(t, rep.Errors[0], "is not a directory")

	rep, err = New(f.api, f.git, &Options{Target: t.TempDir()}).Run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, rep.Errors[0], "no account chosen")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.api.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{{Name: gh.Ptr("x"), FullName: gh.Ptr("o/x"), Owner: &gh.User{Login: gh.Ptr("o")}}}, nil
	}
	rep, err = New(f.api, f.git, &Options{Target: t.TempDir(), Org: "o"}).Run(ctx)
	require.NoError(t, err)
	assert.False(t, rep.OK)
}

func TestStaleBranchesAndShort(t *testing.T) {
	assert.Empty(t, staleBranches("r", map[string]string{"main": "a"}, map[string]string{"main": "a", "old": "b"}))
	assert.Equal(t, "abc", short("abc"))
	assert.Equal(t, "0123456789ab", short("0123456789abcdef"))
}

func TestVerify_PullRefs(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	dir := f.addRepo("tool", false)
	runGit(t, "-C", dir, "update-ref", "refs/pull/1/head", "HEAD")
	f.backup()
	assert.True(t, f.verify(Options{}).OK, "sync fetched the pull request ref")

	// A pull request from a deleted fork branch shows up, and #1 moves on
	commit(t, dir, "fork.txt", "fork")
	runGit(t, "-C", dir, "update-ref", "refs/pull/1/head", "HEAD")
	runGit(t, "-C", dir, "update-ref", "refs/pull/2/head", "HEAD")
	runGit(t, "-C", dir, "reset", "-q", "--hard", "HEAD~1")

	rep := f.verify(Options{})
	assert.False(t, rep.OK)
	got := strings.Join(rep.Stale, "\n")
	assert.Contains(t, got, "Didstopia/tool: pull request #2 missing locally")
	assert.Contains(t, got, "Didstopia/tool: pull request #1 is at")
	assert.Len(t, rep.Stale, 2)
}

func TestVerify_Releases(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	f.addRepo("a", false)
	f.addRepo("b", false)
	f.backup()

	release := &gh.RepositoryRelease{ID: gh.Ptr(int64(1)), TagName: gh.Ptr("v1"), Assets: []*gh.ReleaseAsset{
		{Name: gh.Ptr("app.zip"), Size: gh.Ptr(3)},
		{Name: gh.Ptr("app.sig"), Size: gh.Ptr(2)},
	}}
	f.api.GetReleasesFunc = func(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
		if repo == "a" {
			return []*gh.RepositoryRelease{release}, nil
		}
		return nil, nil
	}

	// GitHub has a release the export doesn't
	rep := f.verify(Options{Metadata: true})
	assert.False(t, rep.OK)
	got := strings.Join(rep.Metadata, "\n")
	assert.Contains(t, got, "Didstopia/a: 0 releases saved, GitHub has 1")
	assert.Contains(t, got, "Didstopia/a: release v1 asset app.zip missing")
	assert.Contains(t, rep.Checks, "release counts and assets")

	// Saved, but one asset is cut short
	dir := metadata.RepoDir(f.target, "Didstopia", "a")
	require.NoError(t, os.WriteFile(filepath.Join(dir, metadata.ReleasesFile), []byte(`[{"id":1}]`), 0644))
	for name, body := range map[string]string{"app.zip": "abc", "app.sig": "x"} {
		path := metadata.AssetPath(dir, release, &gh.ReleaseAsset{Name: gh.Ptr(name)})
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	}
	rep = f.verify(Options{Metadata: true})
	assert.Equal(t, []string{"Didstopia/a: release v1 asset app.sig is 1 bytes, GitHub has 2"}, rep.Metadata)

	path := metadata.AssetPath(dir, release, &gh.ReleaseAsset{Name: gh.Ptr("app.sig")})
	require.NoError(t, os.WriteFile(path, []byte("xy"), 0644))
	assert.True(t, f.verify(Options{Metadata: true}).OK)

	// A missing releases.json, or a listing that fails, is a failure
	require.NoError(t, os.Remove(filepath.Join(dir, metadata.ReleasesFile)))
	rep = f.verify(Options{Metadata: true})
	assert.Equal(t, []string{"Didstopia/a: releases.json missing"}, rep.Metadata)

	f.api.GetReleasesFunc = func(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
		return nil, errors.New("api down")
	}
	rep = f.verify(Options{Metadata: true})
	assert.False(t, rep.OK)
	assert.Contains(t, strings.Join(rep.Errors, "\n"), "can't list releases on GitHub")
}

func TestVerify_Submodules(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	child := upstream(t, filepath.Join(t.TempDir(), "child"))
	gone := upstream(t, filepath.Join(t.TempDir(), "gone"))
	dir := f.addRepo("tool", false)
	runGit(t, "-C", dir, "submodule", "add", "-q", child, "lib/child")
	runGit(t, "-C", dir, "commit", "-q", "-m", "add child")
	f.backup()

	assert.True(t, f.verify(Options{}).OK, "sync checked out the submodule")
	assert.Contains(t, f.verify(Options{}).Checks, "submodules")

	// Moved off the recorded commit
	sub := filepath.Join(f.target, "Didstopia", "tool", "lib", "child")
	runGit(t, "-C", sub, "commit", "-q", "--allow-empty", "-m", "local change")
	rep := f.verify(Options{})
	assert.False(t, rep.OK)
	require.Len(t, rep.Submodules, 1)
	assert.Contains(t, rep.Submodules[0], "Didstopia/tool: submodule lib/child is not at commit")

	// Its object store goes bad
	runGit(t, "-C", sub, "checkout", "-q", "HEAD~1")
	tree := runGit(t, "-C", sub, "rev-parse", "HEAD^{tree}")
	gitDir := runGit(t, "-C", sub, "rev-parse", "--absolute-git-dir")
	obj := filepath.Join(gitDir, "objects", tree[:2], tree[2:])
	// Local clones hardlink objects, so replace the file instead of writing through to upstream
	require.NoError(t, os.Remove(obj))
	require.NoError(t, os.WriteFile(obj, []byte("garbage"), 0644))
	rep = f.verify(Options{})
	require.Len(t, rep.Submodules, 1)
	assert.Contains(t, rep.Submodules[0], "submodule lib/child fails git fsck")

	// A submodule whose repo was deleted upstream never got backed up
	runGit(t, "-C", dir, "submodule", "add", "-q", gone, "lib/gone")
	runGit(t, "-C", dir, "commit", "-q", "-m", "add gone")
	require.NoError(t, os.RemoveAll(gone))
	f2 := newFixture(t)
	f2.repos = f.repos
	f2.backup()
	rep = f2.verify(Options{})
	assert.False(t, rep.OK)
	require.Len(t, rep.Submodules, 1)
	assert.Contains(t, rep.Submodules[0], "Didstopia/tool: submodule lib/gone is not checked out")
	assert.Contains(t, rep.Submodules[0], "gone upstream")
}

func TestVerify_OwnerMetadata(t *testing.T) {
	needLFS(t)
	f := newFixture(t)
	f.addRepo("a", false)
	f.backup()
	f.api.ListUserOrgsFunc = func(ctx context.Context) ([]*gh.Organization, error) {
		return []*gh.Organization{{Login: gh.Ptr("Didstopia")}, {Login: gh.Ptr("Other")}}, nil
	}

	assert.True(t, f.verify(Options{Metadata: true}).OK)

	org := metadata.OwnerDir(f.target, "Didstopia")
	require.NoError(t, os.Remove(filepath.Join(org, "teams.json")))
	require.NoError(t, os.WriteFile(filepath.Join(org, metadata.RulesetsFile), []byte("[{"), 0644))
	rep := f.verify(Options{Metadata: true})
	assert.False(t, rep.OK)
	assert.Equal(t, []string{"org Didstopia: rulesets.json is not valid JSON", "org Didstopia: teams.json missing"}, rep.Metadata)

	// All orgs: the user's own export and every org's are checked
	require.NoError(t, os.RemoveAll(metadata.OwnerDir(f.target, "Dids")))
	rep = f.verify(Options{Metadata: true, AllOrgs: true})
	got := strings.Join(rep.Metadata, "\n")
	assert.Contains(t, got, "user Dids: metadata directory missing")
	assert.Contains(t, got, "org Other: metadata directory missing")
	assert.Contains(t, got, "org Didstopia: teams.json missing")

	// Without --metadata nothing is said about it
	assert.Empty(t, f.verify(Options{AllOrgs: true}).Metadata)
}
