package errors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidationError(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		message  string
		expected string
	}{
		{
			name:     "basic validation error",
			field:    "repository",
			message:  "invalid format",
			expected: "validation error for repository: invalid format",
		},
		{
			name:     "empty field",
			field:    "",
			message:  "some error",
			expected: "validation error for : some error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewValidationError(tt.field, tt.message)
			if err.Error() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, err.Error())
			}
			if err.Field != tt.field {
				t.Errorf("expected field %q, got %q", tt.field, err.Field)
			}
			if err.Message != tt.message {
				t.Errorf("expected message %q, got %q", tt.message, err.Message)
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		err        error
		expected   string
	}{
		{
			name:       "with wrapped error",
			statusCode: 404,
			message:    "not found",
			err:        errors.New("original error"),
			expected:   "GitHub API error (status 404): not found: original error",
		},
		{
			name:       "without wrapped error",
			statusCode: 500,
			message:    "server error",
			err:        nil,
			expected:   "GitHub API error (status 500): server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAPIError(tt.statusCode, tt.message, tt.err)
			if err.Error() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, err.Error())
			}
			if err.StatusCode != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, err.StatusCode)
			}
		})
	}
}

func TestAPIErrorUnwrap(t *testing.T) {
	original := errors.New("original error")
	apiErr := NewAPIError(500, "wrapper", original)

	unwrapped := apiErr.Unwrap()
	if unwrapped != original {
		t.Errorf("expected unwrapped error to be original")
	}
}

func TestIsRateLimited(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "403 status is NOT rate limited (permission denied)",
			err:      NewAPIError(403, "permission denied", nil),
			expected: false,
		},
		{
			name:     "429 status",
			err:      NewAPIError(429, "too many requests", nil),
			expected: true,
		},
		{
			name:     "ErrRateLimited",
			err:      ErrRateLimited,
			expected: true,
		},
		{
			name:     "404 status",
			err:      NewAPIError(404, "not found", nil),
			expected: false,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRateLimited(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "404 status",
			err:      NewAPIError(404, "not found", nil),
			expected: true,
		},
		{
			name:     "ErrNotFound",
			err:      ErrNotFound,
			expected: true,
		},
		{
			name:     "500 status",
			err:      NewAPIError(500, "server error", nil),
			expected: false,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNotFound(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsUnauthorized(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "401 status",
			err:      NewAPIError(401, "bad credentials", nil),
			expected: true,
		},
		{
			name:     "ErrUnauthorized",
			err:      ErrUnauthorized,
			expected: true,
		},
		{
			name:     "wrapped ErrUnauthorized",
			err:      fmt.Errorf("request failed: %w", ErrUnauthorized),
			expected: true,
		},
		{
			name:     "403 status",
			err:      NewAPIError(403, "forbidden", nil),
			expected: false,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnauthorized(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsForbidden(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "403 status",
			err:      NewAPIError(403, "forbidden", nil),
			expected: true,
		},
		{
			name:     "ErrForbidden",
			err:      ErrForbidden,
			expected: true,
		},
		{
			name:     "wrapped ErrForbidden",
			err:      fmt.Errorf("request failed: %w", ErrForbidden),
			expected: true,
		},
		{
			name:     "401 status",
			err:      NewAPIError(401, "unauthorized", nil),
			expected: false,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsForbidden(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNewAuthErrorWithReason(t *testing.T) {
	err := NewAuthErrorWithReason("token expired")
	if !strings.Contains(err.Error(), "token expired") {
		t.Error("expected error to contain the reason")
	}
	if !strings.Contains(err.Error(), "githubby logout") {
		t.Error("expected error to contain logout instruction")
	}
	if !strings.Contains(err.Error(), "githubby login") {
		t.Error("expected error to contain login instruction")
	}
}

func TestNewExpiredTokenError(t *testing.T) {
	err := NewExpiredTokenError("keychain")
	if !strings.Contains(err.Error(), "keychain") {
		t.Error("expected error to contain token source")
	}
	if !strings.Contains(err.Error(), "invalid or expired") {
		t.Error("expected error to contain 'invalid or expired'")
	}
	if !strings.Contains(err.Error(), "githubby logout") {
		t.Error("expected error to contain logout instruction")
	}
}
