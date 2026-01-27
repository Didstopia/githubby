// Package git provides Git operations for repository management
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Common errors
var (
	ErrGitNotInstalled = errors.New("git is not installed or not in PATH")
	ErrNotAGitRepo     = errors.New("not a git repository")
	ErrCloneFailed     = errors.New("git clone failed")
	ErrPullFailed      = errors.New("git pull failed")
	ErrFetchFailed     = errors.New("git fetch failed")
)

// Git provides Git operations
type Git struct {
	// GitPath is the path to the git executable
	GitPath string
	// Quiet suppresses stdout/stderr output (for TUI mode)
	Quiet bool
	// Token is the authentication token for HTTPS operations
	Token string
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
	// If we have a token and it's an HTTPS URL, embed the token for authentication
	cloneURL := g.authenticateURL(url)

	cmd := exec.CommandContext(ctx, g.GitPath, "clone", cloneURL, targetDir)
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Always capture stderr for error reporting
	var stderrBuf strings.Builder
	if g.Quiet {
		cmd.Stderr = &stderrBuf
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

// authenticateURL adds authentication token to HTTPS URLs
func (g *Git) authenticateURL(url string) string {
	if g.Token == "" {
		return url
	}

	// Only modify HTTPS URLs
	if strings.HasPrefix(url, "https://github.com/") {
		// Transform https://github.com/owner/repo.git to https://oauth2:TOKEN@github.com/owner/repo.git
		return strings.Replace(url, "https://github.com/", "https://oauth2:"+g.Token+"@github.com/", 1)
	}

	return url
}

// Pull performs a git pull in the specified directory
func (g *Git) Pull(ctx context.Context, repoDir string) error {
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "pull")
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Always capture stderr for error reporting
	var stderrBuf strings.Builder
	if g.Quiet {
		cmd.Stderr = &stderrBuf
	}

	if err := cmd.Run(); err != nil {
		errMsg := stderrBuf.String()
		if errMsg != "" {
			return fmt.Errorf("%w: %s", ErrPullFailed, strings.TrimSpace(errMsg))
		}
		return fmt.Errorf("%w: %v", ErrPullFailed, err)
	}
	return nil
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

// Fetch performs a git fetch in the specified directory
func (g *Git) Fetch(ctx context.Context, repoDir string) error {
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "fetch", "--all")
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// FetchAll fetches all branches from all remotes with pruning.
// This updates all remote-tracking branches without modifying the working directory.
// Use --prune to remove local references to branches deleted on remote.
func (g *Git) FetchAll(ctx context.Context, repoDir string) error {
	cmd := exec.CommandContext(ctx, g.GitPath, "-C", repoDir, "fetch", "--all", "--prune")
	if !g.Quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Always capture stderr for error reporting
	var stderrBuf strings.Builder
	if g.Quiet {
		cmd.Stderr = &stderrBuf
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
