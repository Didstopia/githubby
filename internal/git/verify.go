package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Errors from the backup checks
var (
	ErrLsRemoteFailed = errors.New("git ls-remote failed")
	ErrFsckFailed     = errors.New("git fsck failed")
	ErrLFSListFailed  = errors.New("git lfs ls-files failed")
)

// output runs a git command and returns its stdout, wrapping failures in base with stderr
func (g *Git) output(ctx context.Context, env []string, base error, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.GitPath, args...)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", base, msg)
		}
		return "", fmt.Errorf("%w: %v", base, err)
	}
	return string(out), nil
}

// LsRemoteHeads returns branch name -> head SHA as the remote has them
func (g *Git) LsRemoteHeads(ctx context.Context, url string) (map[string]string, error) {
	out, err := g.output(ctx, g.authEnv(), ErrLsRemoteFailed, "ls-remote", "--heads", url)
	if err != nil {
		return nil, err
	}
	heads := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok {
			heads[strings.TrimPrefix(ref, "refs/heads/")] = sha
		}
	}
	return heads, nil
}

// RemoteBranches returns branch name -> SHA of the local origin/* remote-tracking refs
func (g *Git) RemoteBranches(ctx context.Context, repoDir string) (map[string]string, error) {
	out, err := g.output(ctx, nil, ErrNotAGitRepo, "-C", repoDir, "for-each-ref", "--format=%(objectname) %(refname)", "refs/remotes/origin/")
	if err != nil {
		return nil, err
	}
	branches := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || ref == "refs/remotes/origin/HEAD" {
			continue
		}
		branches[strings.TrimPrefix(ref, "refs/remotes/origin/")] = sha
	}
	return branches, nil
}

// Fsck checks a repository's object store for corruption
func (g *Git) Fsck(ctx context.Context, repoDir string) error {
	_, err := g.output(ctx, nil, ErrFsckFailed, "-C", repoDir, "fsck", "--no-progress")
	return err
}

// MissingLFSObjects returns the LFS objects referenced anywhere in history that are
// not stored locally with the right content. It needs git-lfs.
func (l *LFS) MissingLFSObjects(ctx context.Context, repoDir string) ([]string, error) {
	if !l.IsInstalled() {
		return nil, ErrLFSNotInstalled
	}

	out, err := l.Git.output(ctx, nil, ErrLFSListFailed, "-C", repoDir, "lfs", "ls-files", "--all", "--long")
	if err != nil {
		return nil, err
	}

	var missing []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || len(fields[0]) != 64 || seen[fields[0]] {
			continue
		}
		oid := fields[0]
		seen[oid] = true
		path := strings.Join(fields[2:], " ")
		if !lfsObjectOK(filepath.Join(repoDir, ".git", "lfs", "objects", oid[0:2], oid[2:4], oid), oid) {
			missing = append(missing, path+" ("+oid[:12]+")")
		}
	}
	return missing, nil
}

// lfsObjectOK reports whether the object file exists and hashes to its oid
func lfsObjectOK(path, oid string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == oid
}
