// Package util provides reusable utility functions
package util

import (
	"errors"
	"strings"
)

// ValidateGitHubRepository will check if the supplied repository is a valid GitHub repository (supports short format only, eg. user/repo)
func ValidateGitHubRepository(repository string) (string, string, error) {
	//log.Println("Validating repository string:", repository)

	// Validates that the repository string length is at least 3 characters
	if len(repository) < 3 {
		return "", "", errors.New("supplied repository \"" + repository + "\" string is not a valid GitHub repository (short format only, eg. user/repo)")
	}

	// Start with some empty values
	parsedOwner := ""
	parsedRepo := ""

	// Check that the repository string is in the correct format
	if strings.Contains(repository, "://") || strings.Contains(repository, "github.com") || strings.Count(repository, "/") != 1 {
		return "", "", errors.New("supplied repository \"" + repository + "\" string is not a valid GitHub repository (short format only, eg. user/repo)")
	}

	// Parse the owner and repository
	separatorIndex := strings.Index(repository, "/")
	parsedOwner = repository[0:separatorIndex]
	parsedRepo = repository[separatorIndex+1 : len(repository)]

	// Validate the owner and repository
	if len(parsedOwner) < 1 || len(parsedRepo) < 1 || strings.Contains(parsedOwner, "/") || strings.Contains(parsedRepo, "/") {
		return "", "", errors.New("supplied repository \"" + repository + "\" string is not a valid GitHub repository (short format only, eg. user/repo)")
	}

	// Return the parsed "owner" and "repo" on success
	return parsedOwner, parsedRepo, nil
}
