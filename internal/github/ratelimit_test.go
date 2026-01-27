package github

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.InitialDelay)
	assert.Equal(t, 30*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.Multiplier)
}

func TestNewRetryableClient(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		mockClient := NewMockClient()
		retryable := NewRetryableClient(mockClient, nil)

		require.NotNil(t, retryable)
		assert.Equal(t, mockClient, retryable.client)
		assert.NotNil(t, retryable.config)
		assert.Equal(t, 3, retryable.config.MaxRetries)
	})

	t.Run("with custom config", func(t *testing.T) {
		mockClient := NewMockClient()
		customConfig := &RetryConfig{
			MaxRetries:   5,
			InitialDelay: 500 * time.Millisecond,
			MaxDelay:     10 * time.Second,
			Multiplier:   1.5,
		}

		retryable := NewRetryableClient(mockClient, customConfig)

		require.NotNil(t, retryable)
		assert.Equal(t, 5, retryable.config.MaxRetries)
		assert.Equal(t, 500*time.Millisecond, retryable.config.InitialDelay)
		assert.Equal(t, 10*time.Second, retryable.config.MaxDelay)
		assert.Equal(t, 1.5, retryable.config.Multiplier)
	})
}

func TestWithRetry_Success(t *testing.T) {
	mockClient := NewMockClient()
	config := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	retryable := NewRetryableClient(mockClient, config)

	callCount := 0
	err := retryable.withRetry(context.Background(), func() error {
		callCount++
		return nil // Success immediately
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount) // Only called once, no retries needed
}

func TestWithRetry_RateLimited(t *testing.T) {
	mockClient := NewMockClient()
	config := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	retryable := NewRetryableClient(mockClient, config)

	callCount := 0
	err := retryable.withRetry(context.Background(), func() error {
		callCount++
		if callCount < 3 {
			return gherrors.ErrRateLimited // First 2 calls fail
		}
		return nil // Third call succeeds
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount) // Called 3 times (2 retries)
}

func TestWithRetry_ExhaustsRetries(t *testing.T) {
	mockClient := NewMockClient()
	config := &RetryConfig{
		MaxRetries:   2,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	retryable := NewRetryableClient(mockClient, config)

	callCount := 0
	err := retryable.withRetry(context.Background(), func() error {
		callCount++
		return gherrors.ErrRateLimited // Always fail with rate limit
	})

	assert.ErrorIs(t, err, gherrors.ErrRateLimited)
	assert.Equal(t, 3, callCount) // Initial call + 2 retries
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	mockClient := NewMockClient()
	config := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	retryable := NewRetryableClient(mockClient, config)

	callCount := 0
	err := retryable.withRetry(context.Background(), func() error {
		callCount++
		return gherrors.ErrNotFound // Not a retryable error
	})

	assert.ErrorIs(t, err, gherrors.ErrNotFound)
	assert.Equal(t, 1, callCount) // Only called once, no retries for non-retryable errors
}

func TestWithRetry_ContextCancelled(t *testing.T) {
	mockClient := NewMockClient()
	config := &RetryConfig{
		MaxRetries:   10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}
	retryable := NewRetryableClient(mockClient, config)

	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := retryable.withRetry(ctx, func() error {
		callCount++
		return gherrors.ErrRateLimited
	})

	assert.ErrorIs(t, err, context.Canceled)
	// Should have been called at least once but not all retries due to cancellation
	assert.GreaterOrEqual(t, callCount, 1)
	assert.Less(t, callCount, 10)
}

func TestCalculateBackoff(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:   5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{attempt: 0, expected: 1 * time.Second},
		{attempt: 1, expected: 2 * time.Second},
		{attempt: 2, expected: 4 * time.Second},
		{attempt: 3, expected: 8 * time.Second},
		{attempt: 4, expected: 16 * time.Second},
		{attempt: 5, expected: 30 * time.Second}, // Capped at MaxDelay
		{attempt: 10, expected: 30 * time.Second}, // Still capped
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := calculateBackoff(tt.attempt, config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateBackoff_CustomMultiplier(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:   5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   1.5,
	}

	// With multiplier 1.5:
	// attempt 0: 100ms * 1.5^0 = 100ms
	// attempt 1: 100ms * 1.5^1 = 150ms
	// attempt 2: 100ms * 1.5^2 = 225ms
	// attempt 3: 100ms * 1.5^3 = 337.5ms

	result0 := calculateBackoff(0, config)
	assert.Equal(t, 100*time.Millisecond, result0)

	result1 := calculateBackoff(1, config)
	assert.Equal(t, 150*time.Millisecond, result1)

	result2 := calculateBackoff(2, config)
	assert.Equal(t, 225*time.Millisecond, result2)
}
