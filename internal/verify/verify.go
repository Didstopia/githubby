// Package verify checks that a backup made by sync holds everything GitHub has.
// It is what decides whether a repo is safe to delete from GitHub, so a check
// that cannot run counts as a failure, never as a pass.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	gh "github.com/google/go-github/v86/github"

	"github.com/Didstopia/githubby/internal/fileutil"
	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/metadata"
	"github.com/Didstopia/githubby/internal/sync"
)

// ReportFile is the name of the report written into the target directory
const ReportFile = "verify-report.json"

// Options says what to check
type Options struct {
	// Target is the backup directory
	Target string
	// AllOrgs checks the token's account plus all its orgs (User may name the account)
	AllOrgs bool
	// User checks one user's repos
	User string
	// Org checks one org's repos
	Org string
	// Gists also checks the token account's gists
	Gists bool
	// Metadata also checks the metadata export
	Metadata bool
}

// Report is the outcome of a check; OK is true only when every list is empty
type Report struct {
	Time   time.Time `json:"time"`
	Target string    `json:"target"`
	OK     bool      `json:"ok"`
	// Checks names every check that ran
	Checks []string `json:"checks"`
	// Checked counts repos, wikis and gists looked at
	Checked int `json:"checked"`
	// Items names every repo, wiki and gist looked at, so a repo can be found here before deleting it
	Items []string `json:"items"`

	Missing  []string `json:"missing"`
	Corrupt  []string `json:"corrupt"`
	Stale    []string `json:"stale"`
	LFS      []string `json:"lfs"`
	// Submodules not checked out at the recorded commit, or failing git fsck
	Submodules []string `json:"submodules"`
	Metadata []string `json:"metadata"`
	// Errors are checks that could not run; each one fails the backup
	Errors []string `json:"errors"`
}

func newReport(target string, checks []string) *Report {
	return &Report{
		Time: time.Now().UTC(), Target: target, Checks: checks, Items: []string{},
		Missing: []string{}, Corrupt: []string{}, Stale: []string{},
		LFS: []string{}, Submodules: []string{}, Metadata: []string{}, Errors: []string{},
	}
}

func (r *Report) errorf(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

// Problems counts every entry that makes the backup not OK
func (r *Report) Problems() int {
	return len(r.Missing) + len(r.Corrupt) + len(r.Stale) + len(r.LFS) + len(r.Submodules) + len(r.Metadata) + len(r.Errors)
}

// Verifier runs the checks
type Verifier struct {
	gh   github.Client
	git  *git.Git
	lfs  *git.LFS
	opts *Options
}

// New returns a Verifier; g should carry the token so private repos can be listed
func New(client github.Client, g *git.Git, opts *Options) *Verifier {
	return &Verifier{gh: client, git: g, lfs: git.NewLFS(g), opts: opts}
}

// Run checks the backup and writes the report to <target>/verify-report.json.
// The report is returned even when the backup is not OK.
func (v *Verifier) Run(ctx context.Context) (*Report, error) {
	checks := []string{"repos exist", "git fsck", "branch heads match GitHub", "pull request refs match GitHub", "LFS objects", "submodules", "wikis"}
	if v.opts.Gists {
		checks = append(checks, "gists")
	}
	if v.opts.Metadata {
		checks = append(checks, "metadata issue and pull request counts", "release counts and assets")
	}
	rep := newReport(v.opts.Target, checks)

	if info, err := os.Stat(v.opts.Target); err != nil || !info.IsDir() {
		rep.errorf("target %s is not a directory", v.opts.Target)
	} else {
		v.check(ctx, rep)
	}

	sortAll(rep)
	rep.OK = rep.Problems() == 0
	if ctx.Err() != nil {
		rep.OK = false
	}

	if err := v.writeReport(rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func (v *Verifier) check(ctx context.Context, rep *Report) {
	repos, err := v.listRepos(ctx, rep)
	if err != nil {
		rep.errorf("listing repos: %v", err)
	}
	for _, repo := range repos {
		if ctx.Err() != nil {
			rep.errorf("stopped: %v", ctx.Err())
			return
		}
		v.checkRepo(ctx, repo, rep)
	}

	if v.opts.Gists {
		v.checkGists(ctx, rep)
	}
}

// listRepos lists every repo GitHub has for the chosen account, private ones included
func (v *Verifier) listRepos(ctx context.Context, rep *Report) ([]*gh.Repository, error) {
	listOpts := &github.ListOptions{IncludePrivate: true}
	switch {
	case v.opts.AllOrgs:
		account, err := sync.New(v.gh, v.git, &sync.Options{IncludePrivate: true}).ListAccount(ctx, v.opts.User)
		if err != nil {
			return nil, err
		}
		for org, ferr := range account.Failed {
			rep.errorf("listing repos of %s: %v", org, ferr)
		}
		return account.Repos, nil
	case v.opts.User != "":
		return v.gh.ListUserRepos(ctx, v.opts.User, listOpts)
	case v.opts.Org != "":
		return v.gh.ListOrgRepos(ctx, v.opts.Org, listOpts)
	}
	return nil, fmt.Errorf("no account chosen")
}

func (v *Verifier) checkRepo(ctx context.Context, repo *gh.Repository, rep *Report) {
	owner, name, full := repo.GetOwner().GetLogin(), repo.GetName(), repo.GetFullName()
	local := filepath.Join(v.opts.Target, owner, name)

	if v.checkGitCopy(ctx, full, local, repo.GetCloneURL(), true, rep) {
		v.checkPullRefs(ctx, full, local, repo.GetCloneURL(), rep)
	}
	if v.git.IsGitRepo(local) {
		v.checkSubmodules(ctx, full, local, rep)
	}

	if repo.GetHasWiki() {
		v.checkWiki(ctx, repo, rep)
	}

	if v.opts.Metadata {
		v.checkMetadata(ctx, owner, name, rep)
	}
}

// checkGitCopy checks that a local clone exists, is not corrupt, has every remote
// branch at the same head, and (withLFS) has all its LFS objects. It returns true when
// the local copy exists and GitHub could be reached, so further checks make sense.
func (v *Verifier) checkGitCopy(ctx context.Context, name, local, url string, withLFS bool, rep *Report) bool {
	rep.Checked++
	rep.Items = append(rep.Items, name)

	if !v.git.IsGitRepo(local) {
		rep.Missing = append(rep.Missing, name)
		return false
	}

	if err := v.git.Fsck(ctx, local); err != nil {
		rep.Corrupt = append(rep.Corrupt, fmt.Sprintf("%s: %v", name, err))
	}

	remote, err := v.git.LsRemoteHeads(ctx, url)
	reachable := err == nil
	if err != nil {
		rep.errorf("%s: can't list branches on GitHub: %v", name, err)
	} else if localHeads, err := v.git.RemoteBranches(ctx, local); err != nil {
		rep.errorf("%s: can't read local branches: %v", name, err)
	} else {
		rep.Stale = append(rep.Stale, staleBranches(name, remote, localHeads)...)
	}

	if !withLFS {
		return reachable
	}
	missing, err := v.lfs.MissingLFSObjects(ctx, local)
	if err != nil {
		rep.errorf("%s: can't check LFS objects: %v", name, err)
		return reachable
	}
	for _, m := range missing {
		rep.LFS = append(rep.LFS, name+": "+m)
	}
	return reachable
}

// checkPullRefs checks that every pull request head on GitHub is kept locally at the same commit
func (v *Verifier) checkPullRefs(ctx context.Context, name, local, url string, rep *Report) {
	remote, err := v.git.LsRemotePulls(ctx, url)
	if err != nil {
		rep.errorf("%s: can't list pull request refs on GitHub: %v", name, err)
		return
	}
	localRefs, err := v.git.PullRefs(ctx, local)
	if err != nil {
		rep.errorf("%s: can't read local pull request refs: %v", name, err)
		return
	}
	for n, sha := range remote {
		got, ok := localRefs[n]
		switch {
		case !ok:
			rep.Stale = append(rep.Stale, fmt.Sprintf("%s: pull request #%s missing locally", name, n))
		case got != sha:
			rep.Stale = append(rep.Stale, fmt.Sprintf("%s: pull request #%s is at %s locally, %s on GitHub", name, n, short(got), short(sha)))
		}
	}
}

// checkSubmodules checks that every submodule the checked-out commit records (nested
// ones too) is checked out at that commit and passes git fsck. A submodule sync could
// not fetch is not in the backup at all, so it fails here.
func (v *Verifier) checkSubmodules(ctx context.Context, name, local string, rep *Report) {
	subs, err := v.git.SubmoduleStatus(ctx, local)
	if err != nil {
		rep.errorf("%s: can't read submodule status: %v", name, err)
		return
	}
	for _, sub := range subs {
		switch sub.State {
		case ' ':
			if err := v.git.Fsck(ctx, filepath.Join(local, sub.Path)); err != nil {
				rep.Submodules = append(rep.Submodules, fmt.Sprintf("%s: submodule %s fails git fsck: %v", name, sub.Path, err))
			}
		case '-':
			rep.Submodules = append(rep.Submodules, fmt.Sprintf(
				"%s: submodule %s is not checked out: commit %s could not be fetched, its repo or that commit may be gone upstream",
				name, sub.Path, short(sub.SHA)))
		case '+':
			rep.Submodules = append(rep.Submodules, fmt.Sprintf("%s: submodule %s is not at commit %s the repo records", name, sub.Path, short(sub.SHA)))
		default:
			rep.Submodules = append(rep.Submodules, fmt.Sprintf("%s: submodule %s has merge conflicts", name, sub.Path))
		}
	}
}

// staleBranches lists remote branches that are absent locally or at another commit
func staleBranches(name string, remote, local map[string]string) []string {
	var stale []string
	for branch, sha := range remote {
		got, ok := local[branch]
		switch {
		case !ok:
			stale = append(stale, fmt.Sprintf("%s: branch %s missing locally", name, branch))
		case got != sha:
			stale = append(stale, fmt.Sprintf("%s: branch %s is at %s locally, %s on GitHub", name, branch, short(got), short(sha)))
		}
	}
	return stale
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// checkWiki checks the wiki only when GitHub has one; has_wiki is also true for empty wikis
func (v *Verifier) checkWiki(ctx context.Context, repo *gh.Repository, rep *Report) {
	name := repo.GetFullName() + ".wiki"
	url := sync.WikiURL(repo)

	if _, err := v.git.LsRemoteHeads(ctx, url); err != nil {
		if sync.IsRepoNotFound(err) {
			return
		}
		rep.errorf("%s: can't tell if the wiki exists: %v", name, err)
		return
	}
	v.checkGitCopy(ctx, name, sync.WikiPath(v.opts.Target, repo), url, false, rep)
}

// checkMetadata checks the export exists and holds as many issues and pulls as GitHub
func (v *Verifier) checkMetadata(ctx context.Context, owner, name string, rep *Report) {
	full := owner + "/" + name
	dir := metadata.RepoDir(v.opts.Target, owner, name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		rep.Metadata = append(rep.Metadata, full+": metadata directory missing")
		return
	}

	v.checkIssuePRCounts(ctx, owner, name, dir, rep)
	v.checkReleases(ctx, owner, name, dir, rep)
}

// checkIssuePRCounts checks the export holds as many issues and pulls as GitHub
func (v *Verifier) checkIssuePRCounts(ctx context.Context, owner, name, dir string, rep *Report) {
	full := owner + "/" + name
	issues, ierr := countEntries(filepath.Join(dir, metadata.IssuesFile))
	pulls, perr := countEntries(filepath.Join(dir, metadata.PullsFile))
	if ierr != nil {
		rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: %v", full, ierr))
	}
	if perr != nil {
		rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: %v", full, perr))
	}
	if ierr != nil && perr != nil {
		return
	}

	wantIssues, wantPulls, err := v.gh.GetIssuePRCounts(ctx, owner, name)
	if err != nil {
		rep.errorf("%s: can't count issues and pull requests on GitHub: %v", full, err)
		return
	}
	if ierr == nil && issues != wantIssues {
		rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: %d issues saved, GitHub has %d", full, issues, wantIssues))
	}
	if perr == nil && pulls != wantPulls {
		rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: %d pull requests saved, GitHub has %d", full, pulls, wantPulls))
	}
}

// checkReleases checks the export holds as many releases as GitHub, and every
// release asset GitHub lists is saved with the same size
func (v *Verifier) checkReleases(ctx context.Context, owner, name, dir string, rep *Report) {
	full := owner + "/" + name
	saved, serr := countEntries(filepath.Join(dir, metadata.ReleasesFile))
	if serr != nil {
		rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: %v", full, serr))
	}

	releases, err := v.gh.GetReleases(ctx, owner, name)
	if err != nil {
		rep.errorf("%s: can't list releases on GitHub: %v", full, err)
		return
	}
	if serr == nil && saved != len(releases) {
		rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: %d releases saved, GitHub has %d", full, saved, len(releases)))
	}

	for _, release := range releases {
		for _, asset := range release.Assets {
			path := metadata.AssetPath(dir, release, asset)
			info, err := os.Stat(path)
			switch {
			case err != nil:
				rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: release %s asset %s missing", full, release.GetTagName(), asset.GetName()))
			case info.Size() != int64(asset.GetSize()):
				rep.Metadata = append(rep.Metadata, fmt.Sprintf("%s: release %s asset %s is %d bytes, GitHub has %d",
					full, release.GetTagName(), asset.GetName(), info.Size(), asset.GetSize()))
			}
		}
	}
}

// countEntries returns the length of the JSON list in a file
func countEntries(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("%s missing", filepath.Base(path))
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return 0, fmt.Errorf("%s unreadable: %v", filepath.Base(path), err)
	}
	return len(list), nil
}

func (v *Verifier) checkGists(ctx context.Context, rep *Report) {
	user, err := v.gh.GetAuthenticatedUser(ctx)
	if err != nil {
		rep.errorf("gists: can't get the token's account: %v", err)
		return
	}
	gists, err := v.gh.ListGists(ctx)
	if err != nil {
		rep.errorf("gists: can't list: %v", err)
		return
	}

	dir := sync.GistsDir(v.opts.Target, user.GetLogin())
	for _, gist := range gists {
		if ctx.Err() != nil {
			return
		}
		v.checkGitCopy(ctx, "gist "+gist.GetID(), filepath.Join(dir, gist.GetID()), gist.GetGitPullURL(), false, rep)
	}
}

func (v *Verifier) writeReport(rep *Report) error {
	if info, err := os.Stat(v.opts.Target); err != nil || !info.IsDir() {
		return nil
	}
	return fileutil.WriteJSON(filepath.Join(v.opts.Target, ReportFile), rep)
}

func sortAll(rep *Report) {
	for _, list := range [][]string{rep.Items, rep.Missing, rep.Corrupt, rep.Stale, rep.LFS, rep.Submodules, rep.Metadata, rep.Errors} {
		sort.Strings(list)
	}
}
