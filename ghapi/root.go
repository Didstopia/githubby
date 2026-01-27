// Package ghapi provides a wrapper for easier access to specific parts the GitHub API.
package ghapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/go-github/v68/github"
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
	// Find all releases (handles pagination behind the scenes, starting at page 1)
	releases, err := githubClient.getAllReleases(owner, repository, 1, nil)
	if err != nil {
		return nil, err
	}

	// log.Println("Got", len(releases), "releases total")

	return releases, nil
}

// RemoveRelease will attempt to delete a release from GitHub
func (githubClient *GitHub) RemoveRelease(owner string, repo string, release *github.RepositoryRelease) error {
	// Delete the release
	deleteReleaseErr := githubClient.deleteRelease(release)
	if deleteReleaseErr != nil {
		return deleteReleaseErr
	}

	// Delete the tag
	deleteTagErr := githubClient.deleteTag(owner, repo, release)
	if deleteTagErr != nil {
		return deleteTagErr
	}

	// Return nil on success
	return nil
}

func (githubClient *GitHub) deleteRelease(release *github.RepositoryRelease) error {
	//log.Println("Deleting release:", release.TagName)

	// Create the release deletion request
	req, reqErr := githubClient.client.NewRequest("DELETE", *release.URL, nil)
	if reqErr != nil {
		return reqErr
	}

	// Run the request
	_, doErr := githubClient.client.Do(githubClient.ctx, req, nil)
	if doErr != nil {
		return doErr
	}

	//log.Println("Delete release response:", res)

	// Return nil on success
	return nil
}

func (githubClient *GitHub) deleteTag(owner string, repo string, release *github.RepositoryRelease) error {
	// Construct the API endpoint URL using proper path encoding for security
	tagURL := fmt.Sprintf("repos/%s/%s/git/refs/tags/%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(*release.TagName))

	// Create the tag deletion request
	req, reqErr := githubClient.client.NewRequest("DELETE", tagURL, nil)
	if reqErr != nil {
		return reqErr
	}

	// Run the request
	_, doErr := githubClient.client.Do(githubClient.ctx, req, nil)
	if doErr != nil {
		return doErr
	}

	//log.Println("Delete tag response:", res)

	// Return nil on success
	return nil
}

func (githubClient *GitHub) getAllReleases(owner string, repository string, page int, existingReleases []*github.RepositoryRelease) ([]*github.RepositoryRelease, error) {
	//log.Println("Getting releases for page ", page)

	// Create an array that will eventually contain all releases
	allReleases := make([]*github.RepositoryRelease, 0)

	// Use existing releases if necessary
	if existingReleases != nil {
		allReleases = existingReleases
	}

	// Get releases for the current page
	releases, res, err := githubClient.client.Repositories.ListReleases(githubClient.ctx, owner, repository, &github.ListOptions{Page: page, PerPage: 100})
	if err != nil {
		return nil, err
	}

	// Add the current releases
	for _, release := range releases {
		allReleases = append(allReleases, release)
	}

	// Recursively move to the next page if there are any more pages left
	if res.NextPage > 0 && res.NextPage > page {
		//log.Println("Moving from page", page, "to", res.NextPage)
		return githubClient.getAllReleases(owner, repository, res.NextPage, allReleases)
	}

	// Return all releases if we're done
	return allReleases, nil
}
