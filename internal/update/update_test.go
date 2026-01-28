package update

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewUpdater(t *testing.T) {
	updater := NewUpdater("1.0.0")
	assert.NotNil(t, updater)
	assert.Equal(t, "1.0.0", updater.currentVersion)
	assert.Equal(t, RepoOwner, updater.repoOwner)
	assert.Equal(t, RepoName, updater.repoName)
}

func TestCheckForUpdate_DevBuild(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"empty version", ""},
		{"dev version", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := NewUpdater(tt.version)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := updater.CheckForUpdate(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.False(t, result.Available)
			assert.Equal(t, tt.version, result.CurrentVersion)
		})
	}
}

func TestUpdate_DevBuild(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"empty version", ""},
		{"dev version", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := NewUpdater(tt.version)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := updater.Update(ctx)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "cannot update dev builds")
		})
	}
}

func TestFormatUpdateNotification(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		expected string
	}{
		{
			name:     "nil result",
			result:   nil,
			expected: "",
		},
		{
			name: "no update available",
			result: &Result{
				CurrentVersion: "1.0.0",
				Available:      false,
			},
			expected: "",
		},
		{
			name: "update available",
			result: &Result{
				CurrentVersion: "1.0.0",
				LatestVersion:  "1.1.0",
				Available:      true,
			},
			expected: "Update available: v1.0.0 -> v1.1.0 (run 'githubby update' to upgrade)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatUpdateNotification(tt.result)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestGetPlatform(t *testing.T) {
	platform := GetPlatform()
	assert.NotEmpty(t, platform)
	assert.Contains(t, platform, "/")
}

func TestCheckForUpdate_ConvenienceFunction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with dev version (should not make network call)
	result, err := CheckForUpdate(ctx, "dev")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Available)
}

func TestUpdate_ConvenienceFunction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with dev version (should fail without network call)
	result, err := Update(ctx, "dev")
	assert.Error(t, err)
	assert.Nil(t, result)
}
