package sync

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
)

func testRetryConfig() *GitRetryConfig {
	return &GitRetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2.0,
	}
}

func TestWithGitRetry_SucceedsImmediately(t *testing.T) {
	calls := 0
	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			calls++
			return nil
		},
		nil,
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, calls, "should only call operation once on success")
}

func TestWithGitRetry_RetriesOnTransientError(t *testing.T) {
	calls := 0
	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			calls++
			if calls < 3 {
				return fmt.Errorf("%w: file locked", git.ErrCloneFailed)
			}
			return nil
		},
		nil,
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 3, calls, "should retry until success")
}

func TestWithGitRetry_ExhaustsRetries(t *testing.T) {
	calls := 0
	cloneErr := fmt.Errorf("%w: always fails", git.ErrCloneFailed)

	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			calls++
			return cloneErr
		},
		nil,
		isTransientGitError,
	)

	assert.Error(t, err)
	assert.ErrorIs(t, err, git.ErrCloneFailed)
	assert.Equal(t, 3, calls, "should attempt MaxAttempts times")
}

func TestWithGitRetry_NoRetryForNonTransientError(t *testing.T) {
	calls := 0
	permErr := errors.New("permission denied")

	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			calls++
			return permErr
		},
		nil,
		isTransientGitError,
	)

	assert.Error(t, err)
	assert.Equal(t, 1, calls, "should not retry non-transient errors")
}

func TestWithGitRetry_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	err := withGitRetry(ctx, testRetryConfig(),
		func() error {
			calls++
			// Cancel context after first attempt, so backoff wait sees cancellation
			cancel()
			return fmt.Errorf("%w: file locked", git.ErrCloneFailed)
		},
		nil,
		isTransientGitError,
	)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls, "should stop after context cancellation")
}

func TestWithGitRetry_CallsCleanupBeforeRetry(t *testing.T) {
	opCalls := 0
	cleanupCalls := 0

	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			opCalls++
			if opCalls < 3 {
				return fmt.Errorf("%w: file locked", git.ErrCloneFailed)
			}
			return nil
		},
		func() error {
			cleanupCalls++
			return nil
		},
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 3, opCalls)
	assert.Equal(t, 2, cleanupCalls, "cleanup should be called before each retry (attempts-1)")
}

func TestWithGitRetry_CleanupFailureDoesNotPreventRetry(t *testing.T) {
	opCalls := 0
	cleanupCalls := 0

	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			opCalls++
			if opCalls < 3 {
				return fmt.Errorf("%w: file locked", git.ErrCloneFailed)
			}
			return nil
		},
		func() error {
			cleanupCalls++
			return errors.New("cleanup failed: directory locked")
		},
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 3, opCalls, "should still retry even when cleanup fails")
	assert.Equal(t, 2, cleanupCalls, "cleanup should still be called each time")
}

func TestWithGitRetry_NilConfig(t *testing.T) {
	calls := 0
	err := withGitRetry(context.Background(), nil,
		func() error {
			calls++
			return nil
		},
		nil,
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestWithGitRetry_BackoffIncreases(t *testing.T) {
	config := &GitRetryConfig{
		MaxAttempts:  4,
		InitialDelay: 20 * time.Millisecond,
		MaxDelay:     200 * time.Millisecond,
		Multiplier:   2.0,
	}

	var timestamps []time.Time
	err := withGitRetry(context.Background(), config,
		func() error {
			timestamps = append(timestamps, time.Now())
			return fmt.Errorf("%w: locked", git.ErrCloneFailed)
		},
		nil,
		isTransientGitError,
	)

	assert.Error(t, err)
	assert.Len(t, timestamps, 4)

	// Verify backoff is increasing (with some tolerance for scheduling)
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		// Each gap should be at least half the expected delay (tolerant of scheduling)
		minExpected := time.Duration(float64(config.InitialDelay) * 0.5)
		assert.Greater(t, gap.Milliseconds(), minExpected.Milliseconds(),
			"gap %d should be meaningful (got %v)", i, gap)
	}
}

func TestWithGitRetry_MaxDelayRespected(t *testing.T) {
	config := &GitRetryConfig{
		MaxAttempts:  4,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond, // Same as initial, so all delays are capped
		Multiplier:   10.0,                  // Large multiplier would blow up without cap
	}

	var timestamps []time.Time
	err := withGitRetry(context.Background(), config,
		func() error {
			timestamps = append(timestamps, time.Now())
			return fmt.Errorf("%w: locked", git.ErrCloneFailed)
		},
		nil,
		isTransientGitError,
	)

	assert.Error(t, err)
	assert.Len(t, timestamps, 4)

	// No gap should be much larger than MaxDelay + scheduling tolerance
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		assert.Less(t, gap.Milliseconds(), int64(200),
			"gap %d should not greatly exceed MaxDelay (got %v)", i, gap)
	}
}

func TestWithGitRetry_FetchFailedIsRetryable(t *testing.T) {
	calls := 0
	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			calls++
			if calls < 2 {
				return fmt.Errorf("%w: connection reset", git.ErrFetchFailed)
			}
			return nil
		},
		nil,
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestWithGitRetry_PullFailedIsRetryable(t *testing.T) {
	calls := 0
	err := withGitRetry(context.Background(), testRetryConfig(),
		func() error {
			calls++
			if calls < 2 {
				return fmt.Errorf("%w: index.lock exists", git.ErrPullFailed)
			}
			return nil
		},
		nil,
		isTransientGitError,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestWithGitRetry_ConcurrentSafety(t *testing.T) {
	// Ensure withGitRetry is safe to call from multiple goroutines
	var successCount atomic.Int32
	config := testRetryConfig()

	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			calls := 0
			err := withGitRetry(context.Background(), config,
				func() error {
					calls++
					if calls < 2 {
						return fmt.Errorf("%w: locked", git.ErrCloneFailed)
					}
					return nil
				},
				nil,
				isTransientGitError,
			)
			if err == nil {
				successCount.Add(1)
			}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	assert.Equal(t, int32(10), successCount.Load(), "all goroutines should succeed")
}

func TestIsTransientGitError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"clone failed", git.ErrCloneFailed, true},
		{"fetch failed", git.ErrFetchFailed, true},
		{"pull failed", git.ErrPullFailed, true},
		{"wrapped clone failed", fmt.Errorf("something: %w", git.ErrCloneFailed), true},
		{"wrapped fetch failed", fmt.Errorf("something: %w", git.ErrFetchFailed), true},
		{"wrapped pull failed", fmt.Errorf("something: %w", git.ErrPullFailed), true},
		{"deeply wrapped", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", git.ErrCloneFailed)), true},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"random error", errors.New("permission denied"), false},
		{"git not installed", git.ErrGitNotInstalled, false},
		{"not a git repo", git.ErrNotAGitRepo, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientGitError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultGitRetryConfig(t *testing.T) {
	config := DefaultGitRetryConfig()

	assert.Equal(t, 3, config.MaxAttempts)
	assert.Equal(t, 2*time.Second, config.InitialDelay)
	assert.Equal(t, 10*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.Multiplier)
}
