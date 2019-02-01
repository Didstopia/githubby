// Package ghapi provides a wrapper for easier access to specific parts the GitHub API.
package ghapi

import (
	"context"
	"log"

	"github.com/google/go-github/github" // Include the GitHub API package
	"golang.org/x/oauth2"
)

// GitHub is an abstraction for the real GitHub API client
type GitHub struct {
	ctx    context.Context
	client *github.Client
}

// NewGitHub creates and returns a reference to a new GitHub object
func NewGitHub(token string) (*GitHub, error) {
	githubClient := &GitHub{}

	// Create an authentication context for the GitHub API client
	githubClient.ctx = context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(githubClient.ctx, ts)

	// Create the actual GitHub API client
	githubClient.client = github.NewClient(tc)

	return githubClient, nil
}

// GetReleases returns all release information for the supplied repository
func (githubClient *GitHub) GetReleases(owner string, repository string) ([]*github.RepositoryRelease, error) {
	/*// Find the repository
	repo, _, err := githubClient.client.Repositories.Get(githubClient.ctx, owner, repository)
	if err != nil {
		return nil, err
	}
	log.Println("Repository:", repo)*/

	// Find all releases
	releases, _, err := githubClient.client.Repositories.ListReleases(githubClient.ctx, owner, repository, nil)
	if err != nil {
		return nil, err
	}
	log.Println("Releases:", releases)

	return releases, nil
}

// TODO: Add a "RemoveRelease" function and implement the rest of the logic for filter-based removal (remove older than <time> etc.)
