// Package update provides automatic update functionality for GitHubby
package update

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
)

const (
	// RepoOwner is the GitHub repository owner
	RepoOwner = "Didstopia"
	// RepoName is the GitHub repository name
	RepoName = "githubby"
)

// Result contains information about an available update
type Result struct {
	// CurrentVersion is the currently running version
	CurrentVersion string
	// LatestVersion is the latest available version
	LatestVersion string
	// Available indicates if an update is available
	Available bool
	// ReleaseURL is the URL to the release page
	ReleaseURL string
	// ReleaseNotes contains the release notes (if available)
	ReleaseNotes string
}

// Updater handles checking and performing updates
type Updater struct {
	repoOwner      string
	repoName       string
	currentVersion string
}

// NewUpdater creates a new Updater instance
func NewUpdater(currentVersion string) *Updater {
	return &Updater{
		repoOwner:      RepoOwner,
		repoName:       RepoName,
		currentVersion: currentVersion,
	}
}

// CheckForUpdate checks if a new version is available
func (u *Updater) CheckForUpdate(ctx context.Context) (*Result, error) {
	// Skip update check for dev builds
	if u.currentVersion == "" || u.currentVersion == "dev" {
		return &Result{
			CurrentVersion: u.currentVersion,
			Available:      false,
		}, nil
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		// Explicitly set OS and arch to ensure correct binary is selected
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create updater: %w", err)
	}

	// Find the latest release for current OS/arch
	// The library matches against GoReleaser's naming: githubby_VERSION_OS_ARCH.tar.gz
	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(u.repoOwner, u.repoName))
	if err != nil {
		return nil, fmt.Errorf("failed to detect latest version: %w", err)
	}

	if !found {
		return &Result{
			CurrentVersion: u.currentVersion,
			Available:      false,
		}, nil
	}

	// Parse versions for comparison
	currentVer, err := semver.NewVersion(u.currentVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse current version %q: %w", u.currentVersion, err)
	}

	latestVer, err := semver.NewVersion(latest.Version())
	if err != nil {
		return nil, fmt.Errorf("failed to parse latest version %q: %w", latest.Version(), err)
	}

	available := latestVer.GreaterThan(currentVer)

	return &Result{
		CurrentVersion: u.currentVersion,
		LatestVersion:  latest.Version(),
		Available:      available,
		ReleaseURL:     latest.URL,
		ReleaseNotes:   latest.ReleaseNotes,
	}, nil
}

// Update downloads and installs the latest version
func (u *Updater) Update(ctx context.Context) (*Result, error) {
	// Prevent updates for dev builds
	if u.currentVersion == "" || u.currentVersion == "dev" {
		return nil, errors.New("cannot update dev builds")
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		// Explicitly set OS and arch to ensure correct binary is downloaded
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create updater: %w", err)
	}

	// Find the latest release for current OS/arch
	// The library matches against GoReleaser's naming: githubby_VERSION_OS_ARCH.tar.gz
	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(u.repoOwner, u.repoName))
	if err != nil {
		return nil, fmt.Errorf("failed to detect latest version: %w", err)
	}

	if !found {
		return &Result{
			CurrentVersion: u.currentVersion,
			Available:      false,
		}, nil
	}

	// Parse versions for comparison
	currentVer, err := semver.NewVersion(u.currentVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse current version %q: %w", u.currentVersion, err)
	}

	latestVer, err := semver.NewVersion(latest.Version())
	if err != nil {
		return nil, fmt.Errorf("failed to parse latest version %q: %w", latest.Version(), err)
	}

	if !latestVer.GreaterThan(currentVer) {
		return &Result{
			CurrentVersion: u.currentVersion,
			LatestVersion:  latest.Version(),
			Available:      false,
		}, nil
	}

	// Get the current executable path
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Perform the update
	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return nil, fmt.Errorf("failed to update: %w", err)
	}

	return &Result{
		CurrentVersion: u.currentVersion,
		LatestVersion:  latest.Version(),
		Available:      true,
		ReleaseURL:     latest.URL,
		ReleaseNotes:   latest.ReleaseNotes,
	}, nil
}

// CheckForUpdate is a convenience function that creates an Updater and checks for updates
func CheckForUpdate(ctx context.Context, currentVersion string) (*Result, error) {
	return NewUpdater(currentVersion).CheckForUpdate(ctx)
}

// Update is a convenience function that creates an Updater and performs an update
func Update(ctx context.Context, currentVersion string) (*Result, error) {
	return NewUpdater(currentVersion).Update(ctx)
}

// FormatUpdateNotification returns a formatted string for the update notification
func FormatUpdateNotification(result *Result) string {
	if result == nil || !result.Available {
		return ""
	}
	return fmt.Sprintf("Update available: v%s -> v%s (run 'githubby update' to upgrade)",
		result.CurrentVersion, result.LatestVersion)
}

// GetPlatform returns the current platform string (os/arch)
func GetPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
