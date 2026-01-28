// Package update provides automatic update functionality for GitHubby
package update

import (
	"fmt"
	"os"
	"path/filepath"
)

// Restart replaces the current process with a new instance of the application.
// On Unix systems, this uses syscall.Exec() for seamless replacement.
// On Windows, this spawns a new process then exits the current one.
// Returns an error if the restart fails.
func Restart() error {
	// We need to find the path to the NEW executable (after update).
	// os.Executable() won't work on Linux because it reads /proc/self/exe
	// which points to the OLD (now deleted) inode after the binary was replaced.
	//
	// Instead, we resolve os.Args[0] to get the path that was used to invoke
	// the program, which should now point to the updated binary.
	executable, err := resolveExecutablePath()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	return restartPlatform(executable, os.Args, os.Environ())
}

// resolveExecutablePath finds the path to the executable for restart.
// It uses os.Args[0] and resolves it to an absolute path, which works
// correctly even after the binary has been replaced during an update.
func resolveExecutablePath() (string, error) {
	// Start with the command used to invoke the program
	arg0 := os.Args[0]

	// If it's already absolute, use it directly
	if filepath.IsAbs(arg0) {
		return arg0, nil
	}

	// If it contains a path separator, it's relative - make it absolute
	if filepath.Dir(arg0) != "." {
		return filepath.Abs(arg0)
	}

	// Otherwise, it was found via PATH - look it up
	path, err := findInPath(arg0)
	if err != nil {
		// Fallback to os.Executable() as last resort
		return os.Executable()
	}
	return path, nil
}

// findInPath searches for an executable in PATH
func findInPath(name string) (string, error) {
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("executable %q not found in PATH", name)
}

// IsDev returns true if this is a development build that should skip updates
func IsDev(version string) bool {
	return version == "" || version == "dev"
}
