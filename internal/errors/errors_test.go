package errors

import (
	"errors"
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
			name:     "403 status",
			err:      NewAPIError(403, "rate limited", nil),
			expected: true,
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
