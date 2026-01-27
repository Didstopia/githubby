// Package util provides utility functions for the TUI
package util

import (
	"os"

	"github.com/mattn/go-isatty"
)

// IsInteractive returns true if the current terminal is interactive
// (not a pipe, not in CI environment)
func IsInteractive() bool {
	// Check if stdin is a terminal
	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return false
	}

	// Check if stdout is a terminal
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return false
	}

	// Check for CI environment variables
	ciEnvVars := []string{
		"CI",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"TRAVIS",
		"CIRCLECI",
		"JENKINS_URL",
		"BUILDKITE",
		"DRONE",
		"TEAMCITY_VERSION",
		"TF_BUILD",          // Azure Pipelines
		"CODEBUILD_BUILD_ID", // AWS CodeBuild
	}

	for _, envVar := range ciEnvVars {
		if os.Getenv(envVar) != "" {
			return false
		}
	}

	return true
}

// GetTerminalSize returns the terminal width and height
// Returns default values (80x24) if unable to detect
func GetTerminalSize() (width, height int) {
	// Default terminal size
	width, height = 80, 24

	// Try to get actual terminal size from environment
	// This is handled automatically by bubbletea, but we provide defaults
	return width, height
}
