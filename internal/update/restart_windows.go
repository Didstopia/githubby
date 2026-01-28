//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
)

// restartPlatform implements process restart for Windows by spawning a new process
// and then exiting the current one. Windows doesn't support exec-style replacement.
func restartPlatform(executable string, args []string, _ []string) error {
	// Create command with all arguments except the executable name itself
	var cmdArgs []string
	if len(args) > 1 {
		cmdArgs = args[1:]
	}

	cmd := exec.Command(executable, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	// Exit the current process
	os.Exit(0)

	// This line is never reached
	return nil
}
