//go:build !windows

package update

import (
	"fmt"
	"syscall"
)

// restartPlatform implements process restart for Unix systems using syscall.Exec.
// This replaces the current process in-place with the new executable.
func restartPlatform(executable string, args []string, env []string) error {
	if err := syscall.Exec(executable, args, env); err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	// This line is never reached as Exec replaces the process
	return nil
}
