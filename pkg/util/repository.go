// Package util provides shared utility functions
package util

import (
	"strings"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

// ValidateGitHubRepository validates and parses a GitHub repository string
// Expected format: owner/repo
// Returns the owner and repository name, or an error if invalid
func ValidateGitHubRepository(repository string) (string, string, error) {
	// Check minimum length (at least "a/b")
	if len(repository) < 3 {
		return "", "", gherrors.NewValidationError("repository",
			"invalid format (expected owner/repo, got: "+repository+")")
	}

	// Check that it's not a URL or full path
	if strings.Contains(repository, "://") || strings.Contains(repository, "github.com") {
		return "", "", gherrors.NewValidationError("repository",
			"use short format (owner/repo), not a URL")
	}

	// Check for exactly one slash
	if strings.Count(repository, "/") != 1 {
		return "", "", gherrors.NewValidationError("repository",
			"invalid format (expected owner/repo, got: "+repository+")")
	}

	// Parse owner and repo
	parts := strings.SplitN(repository, "/", 2)
	owner := parts[0]
	repo := parts[1]

	// Validate parts
	if len(owner) < 1 || len(repo) < 1 {
		return "", "", gherrors.NewValidationError("repository",
			"owner and repo name cannot be empty")
	}

	// Check for invalid characters
	if strings.ContainsAny(owner, "/@#$%^&*()") {
		return "", "", gherrors.NewValidationError("repository",
			"owner contains invalid characters")
	}

	if strings.ContainsAny(repo, "/@#$%^&*()") {
		return "", "", gherrors.NewValidationError("repository",
			"repo name contains invalid characters")
	}

	return owner, repo, nil
}

// ParseRepositoryURL extracts owner and repo from a GitHub URL
// Supports HTTPS and SSH URLs
func ParseRepositoryURL(url string) (string, string, error) {
	// Handle SSH URL: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		return ValidateGitHubRepository(path)
	}

	// Handle HTTPS URL: https://github.com/owner/repo.git
	if strings.Contains(url, "github.com/") {
		// Find the path after github.com/
		idx := strings.Index(url, "github.com/")
		if idx == -1 {
			return "", "", gherrors.NewValidationError("url", "not a GitHub URL")
		}
		path := url[idx+len("github.com/"):]
		path = strings.TrimSuffix(path, ".git")
		path = strings.TrimSuffix(path, "/")
		return ValidateGitHubRepository(path)
	}

	return "", "", gherrors.NewValidationError("url", "not a GitHub URL")
}
