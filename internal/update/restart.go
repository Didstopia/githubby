// Package update provides automatic update functionality for GitHubby
package update

import (
	"fmt"
	"os"
)

// Restart replaces the current process with a new instance of the application.
// On Unix systems, this uses syscall.Exec() for seamless replacement.
// On Windows, this spawns a new process then exits the current one.
// Returns an error if the restart fails.
func Restart() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	return restartPlatform(executable, os.Args, os.Environ())
}

// IsDev returns true if this is a development build that should skip updates
func IsDev(version string) bool {
	return version == "" || version == "dev"
}
