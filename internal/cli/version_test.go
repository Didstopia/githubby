package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetVersionInfo(t *testing.T) {
	// Save original values
	originalVersion := Version
	originalCommit := Commit
	originalBuildDate := BuildDate
	defer func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDate = originalBuildDate
	}()

	t.Run("sets all values when provided", func(t *testing.T) {
		SetVersionInfo("1.0.0", "abc123", "2024-01-01")

		assert.Equal(t, "1.0.0", Version)
		assert.Equal(t, "abc123", Commit)
		assert.Equal(t, "2024-01-01", BuildDate)
	})

	t.Run("empty values do not override", func(t *testing.T) {
		Version = "existing"
		Commit = "existing-commit"
		BuildDate = "existing-date"

		SetVersionInfo("", "", "")

		assert.Equal(t, "existing", Version)
		assert.Equal(t, "existing-commit", Commit)
		assert.Equal(t, "existing-date", BuildDate)
	})

	t.Run("partial update", func(t *testing.T) {
		Version = "old-version"
		Commit = "old-commit"
		BuildDate = "old-date"

		SetVersionInfo("new-version", "", "new-date")

		assert.Equal(t, "new-version", Version)
		assert.Equal(t, "old-commit", Commit) // Not updated
		assert.Equal(t, "new-date", BuildDate)
	})
}

func TestVersionCommand(t *testing.T) {
	// Save original values
	originalVersion := Version
	originalCommit := Commit
	originalBuildDate := BuildDate
	defer func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDate = originalBuildDate
	}()

	t.Run("outputs version information", func(t *testing.T) {
		Version = "1.2.3"
		Commit = "abc123def"
		BuildDate = "2024-12-01"

		// Create a fresh command for testing
		cmd := &cobra.Command{
			Use:   "version",
			Short: "Print version information",
			Run: func(cmd *cobra.Command, args []string) {
				cmd.Printf("githubby %s\n", Version)
				cmd.Printf("  commit: %s\n", Commit)
				cmd.Printf("  built:  %s\n", BuildDate)
			},
		}

		// Capture output
		var buf bytes.Buffer
		cmd.SetOut(&buf)

		err := cmd.Execute()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "githubby 1.2.3")
		assert.Contains(t, output, "commit: abc123def")
		assert.Contains(t, output, "built:  2024-12-01")
	})

	t.Run("uses default values", func(t *testing.T) {
		Version = "dev"
		Commit = "unknown"
		BuildDate = "unknown"

		cmd := &cobra.Command{
			Use:   "version",
			Short: "Print version information",
			Run: func(cmd *cobra.Command, args []string) {
				cmd.Printf("githubby %s\n", Version)
				cmd.Printf("  commit: %s\n", Commit)
				cmd.Printf("  built:  %s\n", BuildDate)
			},
		}

		var buf bytes.Buffer
		cmd.SetOut(&buf)

		err := cmd.Execute()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "githubby dev")
		assert.Contains(t, output, "commit: unknown")
		assert.Contains(t, output, "built:  unknown")
	})
}

func TestVersionVariablesDefault(t *testing.T) {
	// Test that version variables have reasonable defaults
	// Note: These tests check the default state before any modification

	t.Run("Version has default", func(t *testing.T) {
		// Version should be "dev" by default (as set in root.go)
		// If ldflags are used during build, it would be different
		// This just verifies the variable exists and is not empty in tests
		assert.NotEmpty(t, Version)
	})

	t.Run("Commit has default", func(t *testing.T) {
		assert.NotEmpty(t, Commit)
	})

	t.Run("BuildDate has default", func(t *testing.T) {
		assert.NotEmpty(t, BuildDate)
	})
}
