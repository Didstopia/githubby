package sync

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/Didstopia/githubby/internal/git"
)

// GitRetryConfig configures retry behavior for git operations
type GitRetryConfig struct {
	// MaxAttempts is the total number of attempts (initial + retries)
	MaxAttempts int
	// InitialDelay is the delay before the first retry
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration
	// Multiplier is the exponential backoff multiplier
	Multiplier float64
}

// DefaultGitRetryConfig returns sensible defaults for retrying git operations.
// Designed for transient failures like file locking (e.g., Dropbox on Windows).
func DefaultGitRetryConfig() *GitRetryConfig {
	return &GitRetryConfig{
		MaxAttempts:  3,
		InitialDelay: 2 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}
}

// withGitRetry executes operation with retry logic and exponential backoff.
// cleanupFn is called before each retry attempt (can be nil).
// isRetryable determines whether a given error should trigger a retry.
func withGitRetry(ctx context.Context, config *GitRetryConfig, operation func() error, cleanupFn func() error, isRetryable func(error) bool) error {
	if config == nil {
		config = DefaultGitRetryConfig()
	}

	var lastErr error
	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		// Don't retry if the error isn't retryable
		if !isRetryable(lastErr) {
			return lastErr
		}

		// Don't wait/retry after the last attempt
		if attempt == config.MaxAttempts-1 {
			break
		}

		// Run cleanup before retrying (e.g., remove partial clone directory)
		if cleanupFn != nil {
			_ = cleanupFn() // Best-effort cleanup; if it fails, retry may still succeed
		}

		// Calculate backoff delay
		delay := time.Duration(float64(config.InitialDelay) * math.Pow(config.Multiplier, float64(attempt)))
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}

		// Wait with context awareness
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}

// isTransientGitError returns true for git operation errors that are likely
// transient and worth retrying (e.g., file locking by Dropbox, network glitches).
func isTransientGitError(err error) bool {
	if err == nil {
		return false
	}
	// Never retry context cancellation or deadline exceeded
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, git.ErrCloneFailed) ||
		errors.Is(err, git.ErrFetchFailed) ||
		errors.Is(err, git.ErrPullFailed)
}
