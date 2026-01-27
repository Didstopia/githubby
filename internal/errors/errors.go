// Package errors provides custom error types for GitHubby
package errors

import (
	"errors"
	"fmt"
)

// Common errors
var (
	ErrMissingToken      = errors.New("missing required argument 'token'")
	ErrMissingRepository = errors.New("missing required argument 'repository'")
	ErrMissingFilter     = errors.New("missing at least one filter flag (run with --help for more information)")
	ErrInvalidRepository = errors.New("invalid repository format (expected owner/repo)")
	ErrRateLimited       = errors.New("GitHub API rate limit exceeded")
	ErrNotFound          = errors.New("resource not found")
	ErrUnauthorized      = errors.New("unauthorized: invalid or expired token")
)

// AuthError represents an authentication error with helpful guidance
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

// NewAuthError creates a user-friendly authentication error
func NewAuthError() *AuthError {
	return &AuthError{
		Message: `not authenticated with GitHub

To log in interactively:
  githubby login

Or provide a token:
  githubby <command> --token <your-token>
  export GITHUB_TOKEN=<your-token>`,
	}
}

// ValidationError represents an input validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for %s: %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// APIError represents a GitHub API error
type APIError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("GitHub API error (status %d): %s: %v", e.StatusCode, e.Message, e.Err)
	}
	return fmt.Sprintf("GitHub API error (status %d): %s", e.StatusCode, e.Message)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// NewAPIError creates a new API error
func NewAPIError(statusCode int, message string, err error) *APIError {
	return &APIError{StatusCode: statusCode, Message: message, Err: err}
}

// IsRateLimited checks if the error is a rate limit error
func IsRateLimited(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 403 || apiErr.StatusCode == 429
	}
	return errors.Is(err, ErrRateLimited)
}

// IsNotFound checks if the error is a not found error
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 404
	}
	return errors.Is(err, ErrNotFound)
}
