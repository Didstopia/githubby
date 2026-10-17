// Package git provides Git operations for repository management
package git

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Common errors
var (
	ErrGitNotInstalled = errors.New("git is not installed or not in PATH")
	ErrNotAGitRepo     = errors.New("not a git repository")
	ErrCloneFailed     = errors.New("git clone failed")
	ErrPullFailed      = errors.New("git pull failed")
	ErrFetchFailed     = errors.New("git fetch failed")
	ErrSubmoduleFailed = errors.New("git submodule update failed")
)

// Git provides Git operations
type Git struct {
	// GitPath is the path to the git executable
	GitPath string
	// Quiet suppresses stdout/stderr output (for TUI mode)
	Quiet bool
	// Token is the authentication token for HTTPS operations
	Token string
	// Host is the GitHub host the token is sent to (default github.com)
	Host string
}

// New creates a new Git instance
func New() (*Git, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrGitNotInstalled
	}
	return &Git{GitPath: gitPath}, nil
}

// NewQuiet creates a new Git instance that suppresses output
func NewQuiet() (*Git, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrGitNotInstalled
	}
	return &Git{GitPath: gitPath, Quiet: true}, nil
}

// NewWithToken creates a new Git instance with authentication token
func NewWithToken(token string) (*Git, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrGitNotInstalled
	}
	return &Git{GitPath: gitPath, Token: token}, nil
}

// NewQuietWithToken creates a new Git instance that suppresses output and uses auth token
func NewQuietWithToken(token string) (*Git, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrGitNotInstalled
	}
	return &Git{GitPath: gitPath, Quiet: true, Token: token}, nil
}

// Clone clones a repository to the target directory
func (g *Git) Clone(ctx context.Context, url, targetDir string) error {
	cmd := exec.CommandContext(ctx, g.GitPath, "clone", url, targetDir)
	cmd.Env = g.authEnv()

	// Always capture stderr for error reporting, and show it too unless quiet
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}

	if err := cmd.Run(); err != nil {
		errMsg := stderrBuf.String()
		if errMsg != "" {
			return fmt.Errorf("%w: %s", ErrCloneFailed, strings.TrimSpace(errMsg))
		}
		return fmt.Errorf("%w: %v", ErrCloneFailed, err)
	}
	return nil
}

// authEnv returns the environment for a git command, with the token passed as a
// one-off auth header so it is never written to .git/config. Nil means inherit.
func (g *Git) authEnv() []string {
	pairs := g.authConfig()
	if pairs == nil {
		return nil
	}
	return withConfig(pairs...)
}

// host returns the GitHub host the token is sent to
func (g *Git) host() string {
	if g.Host == "" {
		return "github.com"
	}
	return g.Host
}

// authConfig returns git config key/value pairs that send the token, or nil without one
func (g *Git) authConfig() []string {
	if g.Token == "" {
		return nil
	}
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+g.Token))
	// Gists live on their own host
	return []string{
		"http.https://" + g.host() + "/.extraheader", header,
		"http.https://gist." + g.host() + "/.extraheader", header,
	}
}

// withConfig returns the current environment plus git config key/value pairs for one command
func withConfig(pairs ...string) []string {
	env := append(os.Environ(), fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(pairs)/2))
	for i := 0; i+1 < len(pairs); i += 2 {
		env = append(env,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i/2, pairs[i]),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i/2, pairs[i+1]),
		)
	}
	return env
}

// run runs a git command with the given env, wrapping failures in base with git's stderr
func (g *Git) run(ctx context.Context, env []string, base error, args ...string) error {
	cmd := exec.CommandContext(ctx, g.GitPath, args...)
	cmd.Env = env

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderrBuf.String()); msg != "" {
			return fmt.Errorf("%w: %s", base, msg)
		}
		return fmt.Errorf("%w: %v", base, err)
	}
	return nil
}

// scrubRemoteURL removes credentials that older versions embedded in the origin URL
func (g *Git) scrubRemoteURL(ctx context.Context, repoDir string) error {
	remote, err := g.GetRemoteURL(ctx, repoDir)
	if err != nil {
		return err
	}

	u, err := neturl.Parse(remote)
	if err != nil || u.Scheme != "https" || u.User == nil {
		return nil
	}

	u.User = nil
	return exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "remote", "set-url", "origin", u.String()).Run()
}

// IsGitRepo checks if a directory is a git repository
func (g *Git) IsGitRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// GetRemoteURL returns the remote origin URL for a repository
func (g *Git) GetRemoteURL(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// FetchAll fetches all branches from all remotes with pruning.
// This updates all remote-tracking branches without modifying the working directory.
// Use --prune to remove local references to branches deleted on remote.
func (g *Git) FetchAll(ctx context.Context, repoDir string) error {
	// Best effort: a stale token in the URL would override the fresh one
	_ = g.scrubRemoteURL(ctx, repoDir)

	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "fetch", "--all", "--prune")
	cmd.Env = g.authEnv()

	// Always capture stderr for error reporting, and show it too unless quiet
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}

	if err := cmd.Run(); err != nil {
		errMsg := stderrBuf.String()
		if errMsg != "" {
			return fmt.Errorf("%w: %s", ErrFetchFailed, strings.TrimSpace(errMsg))
		}
		return fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}
	return nil
}

// GetHEAD returns the SHA of HEAD in the repository
func (g *Git) GetHEAD(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetRemoteBranchSHA returns the SHA of a remote-tracking branch (e.g., origin/main)
func (g *Git) GetRemoteBranchSHA(ctx context.Context, repoDir, remote, branch string) (string, error) {
	ref := fmt.Sprintf("%s/%s", remote, branch)
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "rev-parse", ref)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetLastFetchTime returns the modification time of .git/FETCH_HEAD, which indicates
// when the repository was last fetched. Returns zero time if never fetched.
func (g *Git) GetLastFetchTime(repoDir string) (time.Time, error) {
	fetchHeadPath := filepath.Join(repoDir, ".git", "FETCH_HEAD")
	info, err := os.Stat(fetchHeadPath)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// GetDefaultBranch returns the default branch of a repository
func (g *Git) GetDefaultBranch(ctx context.Context, repoDir string) (string, error) {
	// Try to get the symbolic ref for HEAD
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		// Extract branch name from refs/remotes/origin/main
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fallback: check for common default branch names
	for _, branch := range []string{"main", "master"} {
		checkCmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "rev-parse", "--verify", "refs/remotes/origin/"+branch)
		if err := checkCmd.Run(); err == nil {
			return branch, nil
		}
	}

	return "", errors.New("could not determine default branch")
}
