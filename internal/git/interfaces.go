package git

import "context"

// Commander provides an interface for executing git commands
// This interface is used for mocking in tests
type Commander interface {
	// Run executes a git command and returns an error if it fails
	Run(ctx context.Context, dir string, args ...string) error

	// Output executes a git command and returns its output
	Output(ctx context.Context, dir string, args ...string) (string, error)
}
