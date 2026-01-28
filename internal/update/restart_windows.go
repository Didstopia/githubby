//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
)

// restartPlatform implements process restart for Windows by spawning a new process,
// waiting for it to complete, and exiting with the same exit code.
// This ensures scheduled tasks (Task Scheduler) see the correct completion status.
// Windows doesn't support exec-style replacement like Unix.
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

	// Run the new process and wait for it to complete
	err := cmd.Run()
	if err != nil {
		// If the child process exited with a non-zero code, propagate it
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		// For other errors (couldn't start, etc.), return the error
		return fmt.Errorf("failed to run new process: %w", err)
	}

	// Exit with success (child completed successfully)
	os.Exit(0)

	// This line is never reached
	return nil
}
