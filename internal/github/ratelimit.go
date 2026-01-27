package github

import (
	"context"
	"math"
	"time"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

// RetryConfig configures retry behavior for API calls
type RetryConfig struct {
	// MaxRetries is the maximum number of retries for transient errors
	MaxRetries int

	// InitialDelay is the initial delay between retries
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration

	// Multiplier is the factor by which delay increases after each retry
	Multiplier float64
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryableClient wraps a Client with retry logic
type RetryableClient struct {
	client Client
	config *RetryConfig
}

// NewRetryableClient creates a new client with retry support
func NewRetryableClient(c Client, config *RetryConfig) *RetryableClient {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetryableClient{
		client: c,
		config: config,
	}
}

// withRetry executes a function with exponential backoff retry logic
func (r *RetryableClient) withRetry(ctx context.Context, operation func() error) error {
	var lastErr error
	delay := r.config.InitialDelay

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retrying
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * r.config.Multiplier)
			if delay > r.config.MaxDelay {
				delay = r.config.MaxDelay
			}
		}

		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		// Only retry on rate limit errors
		if !gherrors.IsRateLimited(lastErr) {
			return lastErr
		}
	}

	return lastErr
}

// calculateBackoff calculates the delay for a given retry attempt
func calculateBackoff(attempt int, config *RetryConfig) time.Duration {
	delay := float64(config.InitialDelay) * math.Pow(config.Multiplier, float64(attempt))
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}
	return time.Duration(delay)
}
