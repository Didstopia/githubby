package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Common LFS errors
var (
	ErrLFSNotInstalled = errors.New("git-lfs is not installed")
	ErrLFSInstallFail  = errors.New("failed to install git-lfs")
)

// LFS provides Git LFS operations
type LFS struct {
	// LFSPath is the path to the git-lfs executable
	LFSPath string
	// Git is the underlying git instance
	Git *Git
}

// NewLFS creates a new LFS instance
func NewLFS(git *Git) *LFS {
	lfsPath, _ := exec.LookPath("git-lfs")
	return &LFS{
		LFSPath: lfsPath,
		Git:     git,
	}
}

// IsInstalled checks if git-lfs is installed
func (l *LFS) IsInstalled() bool {
	return l.LFSPath != ""
}

// Install attempts to install git-lfs using the system package manager
func (l *LFS) Install(ctx context.Context) error {
	if l.IsInstalled() {
		return nil
	}

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		// macOS: use Homebrew
		if _, err := exec.LookPath("brew"); err == nil {
			cmd = exec.CommandContext(ctx, "brew", "install", "git-lfs")
		} else {
			return fmt.Errorf("%w: Homebrew not found on macOS", ErrLFSInstallFail)
		}

	case "linux":
		// Linux: try different package managers
		if _, err := exec.LookPath("apt-get"); err == nil {
			// Debian/Ubuntu
			cmd = exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", "git-lfs")
		} else if _, err := exec.LookPath("dnf"); err == nil {
			// Fedora
			cmd = exec.CommandContext(ctx, "sudo", "dnf", "install", "-y", "git-lfs")
		} else if _, err := exec.LookPath("yum"); err == nil {
			// RHEL/CentOS
			cmd = exec.CommandContext(ctx, "sudo", "yum", "install", "-y", "git-lfs")
		} else if _, err := exec.LookPath("pacman"); err == nil {
			// Arch Linux
			cmd = exec.CommandContext(ctx, "sudo", "pacman", "-S", "--noconfirm", "git-lfs")
		} else {
			return fmt.Errorf("%w: no supported package manager found on Linux", ErrLFSInstallFail)
		}

	case "windows":
		// Windows: use winget
		if _, err := exec.LookPath("winget"); err == nil {
			cmd = exec.CommandContext(ctx, "winget", "install", "--id", "GitHub.GitLFS", "-e", "--source", "winget")
		} else {
			return fmt.Errorf("%w: winget not found on Windows", ErrLFSInstallFail)
		}

	default:
		return fmt.Errorf("%w: unsupported operating system: %s", ErrLFSInstallFail, runtime.GOOS)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %v", ErrLFSInstallFail, err)
	}

	// Update LFS path after installation
	l.LFSPath, _ = exec.LookPath("git-lfs")
	if !l.IsInstalled() {
		return ErrLFSNotInstalled
	}

	return nil
}

// Initialize runs 'git lfs install' to set up Git LFS hooks
func (l *LFS) Initialize(ctx context.Context) error {
	if !l.IsInstalled() {
		return ErrLFSNotInstalled
	}

	cmd := exec.CommandContext(ctx, l.Git.GitPath, "lfs", "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RepoUsesLFS checks if a repository uses Git LFS
func (l *LFS) RepoUsesLFS(repoDir string) bool {
	// Check for .gitattributes with LFS patterns
	gitattributes := filepath.Join(repoDir, ".gitattributes")
	if _, err := os.Stat(gitattributes); err != nil {
		return false
	}

	file, err := os.Open(gitattributes)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "filter=lfs") {
			return true
		}
	}

	return false
}

// Pull runs 'git lfs pull' to download LFS objects
func (l *LFS) Pull(ctx context.Context, repoDir string) error {
	if !l.IsInstalled() {
		return ErrLFSNotInstalled
	}

	cmd := exec.CommandContext(ctx, l.Git.GitPath, "-C", repoDir, "lfs", "pull")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Fetch runs 'git lfs fetch' to download LFS objects
func (l *LFS) Fetch(ctx context.Context, repoDir string) error {
	if !l.IsInstalled() {
		return ErrLFSNotInstalled
	}

	cmd := exec.CommandContext(ctx, l.Git.GitPath, "-C", repoDir, "lfs", "fetch")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// EnsureLFS ensures Git LFS is installed and configured
func (l *LFS) EnsureLFS(ctx context.Context) error {
	if !l.IsInstalled() {
		fmt.Println("Git LFS is not installed. Attempting to install...")
		if err := l.Install(ctx); err != nil {
			return err
		}
		fmt.Println("Git LFS installed successfully.")
	}

	return l.Initialize(ctx)
}
