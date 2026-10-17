package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Errors from updating the working tree
var (
	ErrDirtyWorkTree  = errors.New("local changes, working tree not updated")
	ErrCheckoutFailed = errors.New("working tree not updated")
)

// UpdateWorkTree moves the checkout to origin/<branch>: it switches to branch if
// another one is checked out, then fast-forwards it. It never resets anything:
// with local changes it returns ErrDirtyWorkTree, and a branch that can't
// fast-forward returns ErrCheckoutFailed. moved is true when HEAD changed.
// A branch that was never fetched (an empty repo) is left alone.
func (g *Git) UpdateWorkTree(ctx context.Context, repoDir, branch string) (moved bool, err error) {
	remote, err := g.revParse(ctx, repoDir, "refs/remotes/origin/"+branch)
	if err != nil {
		return false, nil
	}
	head, _ := g.revParse(ctx, repoDir, "HEAD")
	current, _ := g.output(ctx, nil, ErrCheckoutFailed, "-C", repoDir, "symbolic-ref", "-q", "--short", "HEAD")
	onBranch := strings.TrimSpace(current) == branch
	if onBranch && head == remote {
		return false, nil
	}

	// Submodules are left out: the submodule update that follows puts them right
	status, err := g.output(ctx, nil, ErrCheckoutFailed, "-C", repoDir, "status", "--porcelain", "--untracked-files=no", "--ignore-submodules=all")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(status) != "" {
		return false, ErrDirtyWorkTree
	}

	if !onBranch {
		args := []string{"-C", repoDir, "checkout", "-q", branch}
		if _, err := g.revParse(ctx, repoDir, "refs/heads/"+branch); err != nil {
			args = []string{"-C", repoDir, "checkout", "-q", "-b", branch, "--track", "origin/" + branch}
		}
		if err := g.run(ctx, nil, ErrCheckoutFailed, args...); err != nil {
			return false, err
		}
	}
	if err := g.run(ctx, nil, ErrCheckoutFailed, "-C", repoDir, "merge", "-q", "--ff-only", "origin/"+branch); err != nil {
		return !onBranch, err
	}
	return true, nil
}

// revParse returns the SHA a ref points at
func (g *Git) revParse(ctx context.Context, repoDir, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "rev-parse", "-q", "--verify", ref+"^{commit}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// LsRemoteDefaultBranch returns the branch the remote's HEAD points at, or "" for an empty repo
func (g *Git) LsRemoteDefaultBranch(ctx context.Context, url string) (string, error) {
	out, err := g.output(ctx, g.authEnv(), ErrLsRemoteFailed, "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			branch, _, _ := strings.Cut(ref, "\t")
			return branch, nil
		}
	}
	return "", nil
}
