package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v86/github"
)

// IssueExport is an issue with its comments
type IssueExport struct {
	Issue    *gh.Issue          `json:"issue"`
	Comments []*gh.IssueComment `json:"comments"`
}

// PullExport is a pull request with its conversation, review comments and reviews
type PullExport struct {
	Pull           *gh.PullRequest          `json:"pull"`
	Comments       []*gh.IssueComment       `json:"comments"`
	ReviewComments []*gh.PullRequestComment `json:"review_comments"`
	Reviews        []*gh.PullRequestReview  `json:"reviews"`
}

// Files written per repo; verify reads the issue and pull counts back
const (
	IssuesFile   = "issues.json"
	PullsFile    = "pulls.json"
	ReleasesFile = "releases.json"
	RulesetsFile = "rulesets.json"
)

// ExportRepo writes all metadata of one repo into <target>/<owner>/<repo>.meta
func (e *Exporter) ExportRepo(ctx context.Context, repo *gh.Repository, rep *Report) {
	owner, name := repo.GetOwner().GetLogin(), repo.GetName()
	full := repo.GetFullName()
	dir := RepoDir(e.target, owner, name)

	// Issue comments for issues and pulls come from one repo-wide listing
	var comments map[int][]*gh.IssueComment
	issueComments := func(ctx context.Context) (map[int][]*gh.IssueComment, error) {
		if comments != nil {
			return comments, nil
		}
		list, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.IssueComment, *gh.Response, error) {
			return e.gh.Issues.ListComments(ctx, owner, name, 0, &gh.IssueListCommentsOptions{ListOptions: o})
		})
		if err != nil {
			return nil, err
		}
		comments = make(map[int][]*gh.IssueComment)
		for _, c := range list {
			n := numberFromURL(c.GetIssueURL())
			comments[n] = append(comments[n], c)
		}
		return comments, nil
	}

	parts := []part{
		{"repo.json", "repo", func(ctx context.Context) (any, error) {
			var r *gh.Repository
			err := e.call(ctx, func() error {
				var err error
				r, _, err = e.gh.Repositories.Get(ctx, owner, name)
				return err
			})
			return r, err
		}},
		{IssuesFile, "repo", func(ctx context.Context) (any, error) {
			return e.issues(ctx, owner, name, issueComments)
		}},
		{PullsFile, "repo", func(ctx context.Context) (any, error) {
			return e.pulls(ctx, owner, name, issueComments)
		}},
		{ReleasesFile, "repo", func(ctx context.Context) (any, error) {
			return e.releases(ctx, owner, name, dir)
		}},
		{"labels.json", "repo", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Label, *gh.Response, error) {
				return e.gh.Issues.ListLabels(ctx, owner, name, &o)
			})
		}},
		{"milestones.json", "repo", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Milestone, *gh.Response, error) {
				return e.gh.Issues.ListMilestones(ctx, owner, name, &gh.MilestoneListOptions{State: "all", ListOptions: o})
			})
		}},
		{"topics.json", "repo", func(ctx context.Context) (any, error) {
			var topics []string
			err := e.call(ctx, func() error {
				var err error
				topics, _, err = e.gh.Repositories.ListAllTopics(ctx, owner, name, nil)
				return err
			})
			return topics, err
		}},
		{"branch-protection.json", "repo and admin access to the repo", func(ctx context.Context) (any, error) {
			return e.branchProtection(ctx, owner, name)
		}},
		{"hooks.json", "admin:repo_hook or admin access to the repo", func(ctx context.Context) (any, error) {
			hooks, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Hook, *gh.Response, error) {
				return e.gh.Repositories.ListHooks(ctx, owner, name, &o)
			})
			stripHookSecrets(hooks)
			return hooks, err
		}},
		{"collaborators.json", "repo and push access to the repo", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.User, *gh.Response, error) {
				return e.gh.Repositories.ListCollaborators(ctx, owner, name, &gh.ListCollaboratorsOptions{Affiliation: "all", ListOptions: o})
			})
		}},
		{"deploy-keys.json", "repo and admin access to the repo", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Key, *gh.Response, error) {
				return e.gh.Repositories.ListKeys(ctx, owner, name, &o)
			})
		}},
		{"actions-variables.json", "repo and admin access to the repo", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.ActionsVariable, *gh.Response, error) {
				v, resp, err := e.gh.Actions.ListRepoVariables(ctx, owner, name, &o)
				if v == nil {
					return nil, resp, err
				}
				return v.Variables, resp, err
			})
		}},
		{"actions-secrets.json", "repo and admin access to the repo", func(ctx context.Context) (any, error) {
			// The API only ever returns secret names, never values
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Secret, *gh.Response, error) {
				s, resp, err := e.gh.Actions.ListRepoSecrets(ctx, owner, name, &o)
				if s == nil {
					return nil, resp, err
				}
				return s.Secrets, resp, err
			})
		}},
		{RulesetsFile, "repo and admin access to the repo", func(ctx context.Context) (any, error) {
			// Org rulesets are exported with the org
			return e.rulesets(ctx, func(o gh.ListOptions) ([]*gh.RepositoryRuleset, *gh.Response, error) {
				return e.gh.Repositories.GetAllRulesets(ctx, owner, name, &gh.RepositoryListRulesetsOptions{IncludesParents: gh.Ptr(false), ListOptions: o})
			}, func(id int64) (*gh.RepositoryRuleset, *gh.Response, error) {
				return e.gh.Repositories.GetRuleset(ctx, owner, name, id, false)
			})
		}},
		{"environments.json", "repo and admin access to the repo", func(ctx context.Context) (any, error) {
			return listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Environment, *gh.Response, error) {
				env, resp, err := e.gh.Repositories.ListEnvironments(ctx, owner, name, &gh.EnvironmentListOptions{ListOptions: o})
				if env == nil {
					return nil, resp, err
				}
				return env.Environments, resp, err
			})
		}},
	}

	if repo.GetHasDiscussions() {
		parts = append(parts, part{"discussions.json", "repo (read:discussion for public repos)", func(ctx context.Context) (any, error) {
			return e.discussions(ctx, owner, name)
		}})
	}

	e.writeParts(ctx, full, dir, parts, rep)
	if rep.Failed[full] == nil {
		rep.Exported = append(rep.Exported, full)
	}
}

// issues returns every issue (not pull requests) with its comments.
// Issues turned off for the repo (410 Gone) give an empty list.
func (e *Exporter) issues(ctx context.Context, owner, name string, comments func(context.Context) (map[int][]*gh.IssueComment, error)) ([]IssueExport, error) {
	list, err := listAllAfter(ctx, e, func(o gh.ListOptions, after string) ([]*gh.Issue, *gh.Response, error) {
		return e.gh.Issues.ListByRepo(ctx, owner, name, &gh.IssueListByRepoOptions{
			State:             "all",
			ListOptions:       o,
			ListCursorOptions: gh.ListCursorOptions{After: after},
		})
	})
	if statusOf(err) == http.StatusGone {
		return []IssueExport{}, nil
	}
	if err != nil {
		return nil, err
	}

	byNumber, err := comments(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]IssueExport, 0, len(list))
	for _, issue := range list {
		if issue.IsPullRequest() {
			continue
		}
		out = append(out, IssueExport{Issue: issue, Comments: nonNil(byNumber[issue.GetNumber()])})
	}
	return out, nil
}

// pulls returns every pull request with its comments, review comments and reviews
func (e *Exporter) pulls(ctx context.Context, owner, name string, comments func(context.Context) (map[int][]*gh.IssueComment, error)) ([]PullExport, error) {
	list, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.PullRequest, *gh.Response, error) {
		return e.gh.PullRequests.List(ctx, owner, name, &gh.PullRequestListOptions{State: "all", ListOptions: o})
	})
	if err != nil {
		return nil, err
	}

	byNumber, err := comments(ctx)
	if err != nil {
		return nil, err
	}

	reviewComments, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.PullRequestComment, *gh.Response, error) {
		return e.gh.PullRequests.ListComments(ctx, owner, name, 0, &gh.PullRequestListCommentsOptions{ListOptions: o})
	})
	if err != nil {
		return nil, err
	}
	reviewByNumber := make(map[int][]*gh.PullRequestComment)
	for _, c := range reviewComments {
		n := numberFromURL(c.GetPullRequestURL())
		reviewByNumber[n] = append(reviewByNumber[n], c)
	}

	out := make([]PullExport, 0, len(list))
	for _, pr := range list {
		n := pr.GetNumber()
		reviews, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error) {
			return e.gh.PullRequests.ListReviews(ctx, owner, name, n, &o)
		})
		if err != nil {
			return nil, fmt.Errorf("reviews of #%d: %w", n, err)
		}
		out = append(out, PullExport{
			Pull:           pr,
			Comments:       nonNil(byNumber[n]),
			ReviewComments: nonNil(reviewByNumber[n]),
			Reviews:        reviews,
		})
	}
	return out, nil
}

// releases returns every release and downloads its assets into <dir>/releases/<tag>/
func (e *Exporter) releases(ctx context.Context, owner, name, dir string) ([]*gh.RepositoryRelease, error) {
	list, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.RepositoryRelease, *gh.Response, error) {
		return e.gh.Repositories.ListReleases(ctx, owner, name, &o)
	})
	if err != nil {
		return nil, err
	}

	for _, release := range list {
		for _, asset := range release.Assets {
			path := AssetPath(dir, release, asset)
			if err := e.downloadAsset(ctx, owner, name, asset, path); err != nil {
				return nil, fmt.Errorf("asset %s of %s: %w", asset.GetName(), release.GetTagName(), err)
			}
		}
	}
	return list, nil
}

// AssetPath returns where a release asset is stored under a repo's metadata dir
func AssetPath(metaDir string, release *gh.RepositoryRelease, asset *gh.ReleaseAsset) string {
	tag := release.GetTagName()
	if tag == "" {
		tag = "release-" + strconv.FormatInt(release.GetID(), 10)
	}
	return filepath.Join(metaDir, "releases", safeName(tag), safeName(asset.GetName()))
}

// safeName makes a tag or asset name usable as one path element
func safeName(s string) string {
	s = strings.NewReplacer("/", "_", "\\", "_").Replace(s)
	if s == "" || s == "." || s == ".." {
		return "_" + s
	}
	return s
}

// downloadAsset fetches one release asset, skipping it when a file of the same size exists
func (e *Exporter) downloadAsset(ctx context.Context, owner, name string, asset *gh.ReleaseAsset, path string) error {
	if info, err := os.Stat(path); err == nil && info.Size() == int64(asset.GetSize()) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	var rc io.ReadCloser
	err := e.call(ctx, func() error {
		var err error
		rc, _, err = e.gh.Repositories.DownloadReleaseAsset(ctx, owner, name, asset.GetID(), e.download)
		return err
	})
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	written, err := io.Copy(tmp, rc)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && written != int64(asset.GetSize()) {
		err = fmt.Errorf("got %d bytes, want %d", written, asset.GetSize())
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// branchProtection returns the protection settings of each protected branch.
// A branch protected only by a ruleset has no classic protection and is left out.
func (e *Exporter) branchProtection(ctx context.Context, owner, name string) (map[string]*gh.Protection, error) {
	branches, err := listAll(ctx, e, func(o gh.ListOptions) ([]*gh.Branch, *gh.Response, error) {
		return e.gh.Repositories.ListBranches(ctx, owner, name, &gh.BranchListOptions{Protected: gh.Ptr(true), ListOptions: o})
	})
	if err != nil {
		return nil, err
	}

	out := make(map[string]*gh.Protection, len(branches))
	for _, b := range branches {
		var p *gh.Protection
		err := e.call(ctx, func() error {
			var err error
			p, _, err = e.gh.Repositories.GetBranchProtection(ctx, owner, name, b.GetName())
			return err
		})
		if errors.Is(err, gh.ErrBranchNotProtected) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("branch %s: %w", b.GetName(), err)
		}
		out[b.GetName()] = p
	}
	return out, nil
}

// rulesets lists rulesets, then fetches each one, since the list leaves out the rules
func (e *Exporter) rulesets(ctx context.Context,
	list func(o gh.ListOptions) ([]*gh.RepositoryRuleset, *gh.Response, error),
	get func(id int64) (*gh.RepositoryRuleset, *gh.Response, error),
) ([]*gh.RepositoryRuleset, error) {
	summaries, err := listAll(ctx, e, list)
	if err != nil {
		return nil, err
	}

	out := make([]*gh.RepositoryRuleset, 0, len(summaries))
	for _, r := range summaries {
		var full *gh.RepositoryRuleset
		err := e.call(ctx, func() error {
			var err error
			full, _, err = get(r.GetID())
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("ruleset %s: %w", r.Name, err)
		}
		out = append(out, full)
	}
	return out, nil
}

// stripHookSecrets drops webhook secrets; GitHub masks them anyway
func stripHookSecrets(hooks []*gh.Hook) {
	for _, h := range hooks {
		if h.Config != nil {
			h.Config.Secret = nil
		}
	}
}

// nonNil turns a nil slice into an empty one, so the JSON says [] not null
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
