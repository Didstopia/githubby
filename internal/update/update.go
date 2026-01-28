// Package update provides automatic update functionality for GitHubby
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/creativeprojects/go-selfupdate/update"
)

// semverPattern matches semantic version strings (used in executable name matching)
// This mirrors the pattern from go-selfupdate library
const semverPattern = `\d+\.\d+\.\d+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?`

const (
	// RepoOwner is the GitHub repository owner
	RepoOwner = "Didstopia"
	// RepoName is the GitHub repository name
	RepoName = "githubby"
	// ExecutableName is the name of the executable in the release archive.
	// This allows renamed binaries (e.g., "githubby-test") to still update correctly.
	ExecutableName = "githubby"
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

// Update downloads and installs the latest version.
// This uses a custom extraction that looks for ExecutableName ("githubby") in the archive,
// allowing renamed binaries (e.g., "githubby-test") to still update correctly.
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

	// Get the current executable path (where we'll install the update)
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Download the release asset
	reader, err := source.DownloadReleaseAsset(ctx, latest, latest.AssetID)
	if err != nil {
		return nil, fmt.Errorf("failed to download asset: %w", err)
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read asset data: %w", err)
	}

	// Download and validate checksum
	validator := &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}
	if latest.ValidationAssetID != 0 {
		checksumReader, err := source.DownloadReleaseAsset(ctx, latest, latest.ValidationAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to download checksum file: %w", err)
		}
		defer func() { _ = checksumReader.Close() }()

		checksumData, err := io.ReadAll(checksumReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read checksum data: %w", err)
		}

		if err := validator.Validate(latest.AssetName, data, checksumData); err != nil {
			return nil, fmt.Errorf("checksum validation failed: %w", err)
		}
	}

	// Extract the executable using our custom extraction that looks for ExecutableName
	executableData, err := extractExecutable(data, latest.AssetName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract executable: %w", err)
	}

	// Apply the update to the current executable path
	if err := update.Apply(bytes.NewReader(executableData), update.Options{
		TargetPath: exe,
	}); err != nil {
		return nil, fmt.Errorf("failed to apply update: %w", err)
	}

	return &Result{
		CurrentVersion: u.currentVersion,
		LatestVersion:  latest.Version(),
		Available:      true,
		ReleaseURL:     latest.URL,
		ReleaseNotes:   latest.ReleaseNotes,
	}, nil
}

// extractExecutable extracts the executable from an archive.
// It looks for ExecutableName (or ExecutableName.exe on Windows) in the archive,
// regardless of what the current executable is named.
func extractExecutable(data []byte, assetName string) ([]byte, error) {
	// Determine the expected executable name in the archive
	targetName := ExecutableName
	if runtime.GOOS == "windows" {
		targetName = ExecutableName + ".exe"
	}

	// Try different archive formats based on the asset name
	assetLower := strings.ToLower(assetName)

	switch {
	case strings.HasSuffix(assetLower, ".tar.gz") || strings.HasSuffix(assetLower, ".tgz"):
		return extractFromTarGz(data, targetName)
	case strings.HasSuffix(assetLower, ".zip"):
		return extractFromZip(data, targetName)
	case strings.HasSuffix(assetLower, ".gz") && !strings.HasSuffix(assetLower, ".tar.gz"):
		return extractFromGzip(data)
	default:
		// Assume it's a raw binary
		return data, nil
	}
}

// extractFromTarGz extracts the target executable from a .tar.gz archive
func extractFromTarGz(data []byte, targetName string) ([]byte, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Get the base name of the file
		_, name := filepath.Split(header.Name)

		// Check if this is the executable we're looking for
		if !header.FileInfo().IsDir() && matchesExecutableName(name, targetName) {
			execData, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("failed to read executable from archive: %w", err)
			}
			return execData, nil
		}
	}

	return nil, fmt.Errorf("executable %q not found in tar.gz archive", targetName)
}

// extractFromZip extracts the target executable from a .zip archive
func extractFromZip(data []byte, targetName string) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}

	for _, file := range zipReader.File {
		// Get the base name of the file
		_, name := filepath.Split(file.Name)

		// Check if this is the executable we're looking for
		if !file.FileInfo().IsDir() && matchesExecutableName(name, targetName) {
			rc, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file in archive: %w", err)
			}
			defer func() { _ = rc.Close() }()

			execData, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("failed to read executable from archive: %w", err)
			}
			return execData, nil
		}
	}

	return nil, fmt.Errorf("executable %q not found in zip archive", targetName)
}

// extractFromGzip extracts data from a plain .gz file (not tar.gz)
func extractFromGzip(data []byte) ([]byte, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	return io.ReadAll(gzReader)
}

// matchesExecutableName checks if a filename matches the expected executable name.
// This uses the same pattern matching logic as the go-selfupdate library to handle
// variations like "githubby", "githubby.exe", "githubby_v1.0.0", "githubby_linux_amd64", etc.
func matchesExecutableName(filename, targetName string) bool {
	// Remove .exe suffix from target name for pattern matching
	cmd := strings.TrimSuffix(targetName, ".exe")

	// Build the same regex pattern used by go-selfupdate's matchExecutableName
	// Pattern: ^cmd([_-]v?semver)?([_-]os[_-]arch)?(\.exe)?$
	pattern := regexp.MustCompile(
		fmt.Sprintf(
			`^%s([_-]v?%s)?([_-]%s[_-]%s)?(\.exe)?$`,
			regexp.QuoteMeta(cmd),
			semverPattern,
			regexp.QuoteMeta(runtime.GOOS),
			regexp.QuoteMeta(runtime.GOARCH),
		),
	)

	return pattern.MatchString(filename)
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
