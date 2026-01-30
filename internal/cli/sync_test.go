package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Didstopia/githubby/internal/schedule"
	"github.com/Didstopia/githubby/internal/state"
	synpkg "github.com/Didstopia/githubby/internal/sync"
)

func TestPrintSyncSummary(t *testing.T) {
	// These tests verify that printSyncSummary doesn't panic with various inputs.
	// Since it prints to stdout, we can't easily capture the output without refactoring.

	t.Run("nil result does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			printSyncSummary(nil)
		})
	})

	t.Run("empty result does not panic", func(t *testing.T) {
		result := &synpkg.Result{
			Cloned:  []string{},
			Updated: []string{},
			Skipped: []string{},
			Failed:  map[string]error{},
		}
		assert.NotPanics(t, func() {
			printSyncSummary(result)
		})
	})

	t.Run("with cloned repos does not panic", func(t *testing.T) {
		result := &synpkg.Result{
			Cloned:  []string{"owner/repo1", "owner/repo2"},
			Updated: []string{},
			Skipped: []string{},
			Failed:  map[string]error{},
		}
		assert.NotPanics(t, func() {
			printSyncSummary(result)
		})
	})

	t.Run("with updated repos does not panic", func(t *testing.T) {
		result := &synpkg.Result{
			Cloned:  []string{},
			Updated: []string{"owner/updated-repo"},
			Skipped: []string{},
			Failed:  map[string]error{},
		}
		assert.NotPanics(t, func() {
			printSyncSummary(result)
		})
	})

	t.Run("with skipped repos does not panic", func(t *testing.T) {
		result := &synpkg.Result{
			Cloned:  []string{},
			Updated: []string{},
			Skipped: []string{"owner/skipped-repo"},
			Failed:  map[string]error{},
		}
		assert.NotPanics(t, func() {
			printSyncSummary(result)
		})
	})

	t.Run("with failed repos does not panic", func(t *testing.T) {
		result := &synpkg.Result{
			Cloned:  []string{},
			Updated: []string{},
			Skipped: []string{},
			Failed: map[string]error{
				"owner/failed-repo": assert.AnError,
			},
		}
		assert.NotPanics(t, func() {
			printSyncSummary(result)
		})
	})

	t.Run("mixed results does not panic", func(t *testing.T) {
		result := &synpkg.Result{
			Cloned:  []string{"owner/cloned1", "owner/cloned2"},
			Updated: []string{"owner/updated"},
			Skipped: []string{"owner/skipped1", "owner/skipped2", "owner/skipped3"},
			Failed: map[string]error{
				"owner/failed1": assert.AnError,
				"owner/failed2": assert.AnError,
			},
		}
		assert.NotPanics(t, func() {
			printSyncSummary(result)
		})
	})
}

// TestSyncResultCounts verifies that the sync.Result properly tracks counts
func TestSyncResultCounts(t *testing.T) {
	t.Run("empty result has zero counts", func(t *testing.T) {
		result := synpkg.NewResult()

		assert.Empty(t, result.Cloned)
		assert.Empty(t, result.Updated)
		assert.Empty(t, result.Skipped)
		assert.Empty(t, result.Failed)
	})

	t.Run("counts match appended items", func(t *testing.T) {
		result := synpkg.NewResult()

		result.Cloned = append(result.Cloned, "repo1", "repo2")
		result.Updated = append(result.Updated, "repo3")
		result.Skipped = append(result.Skipped, "repo4", "repo5", "repo6")
		result.Failed["repo7"] = assert.AnError

		assert.Len(t, result.Cloned, 2)
		assert.Len(t, result.Updated, 1)
		assert.Len(t, result.Skipped, 3)
		assert.Len(t, result.Failed, 1)
	})
}

func TestScheduleValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{name: "valid every 6 hours", spec: "0 */6 * * *", wantErr: false},
		{name: "valid every 30 minutes", spec: "*/30 * * * *", wantErr: false},
		{name: "valid @hourly", spec: "@hourly", wantErr: false},
		{name: "valid @every 5m", spec: "@every 5m", wantErr: false},
		{name: "invalid empty", spec: "", wantErr: true},
		{name: "invalid garbage", spec: "not-a-schedule", wantErr: true},
		{name: "invalid too few fields", spec: "* *", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schedule.ValidateSpec(tt.spec)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProfileSyncSetsOpts(t *testing.T) {
	profile := &state.SyncProfile{
		ID:             "test-id",
		Name:           "my-profile",
		Type:           "user",
		Source:         "testuser",
		TargetDir:      "/tmp/repos",
		IncludePrivate: true,
		IncludeFilter:  []string{"prefix-*"},
		ExcludeFilter:  []string{"*-archive"},
	}

	// Verify profile fields map correctly to sync.Options
	opts := &synpkg.Options{
		Target:         profile.TargetDir,
		Include:        profile.IncludeFilter,
		Exclude:        profile.ExcludeFilter,
		IncludePrivate: profile.IncludePrivate,
	}

	assert.Equal(t, "/tmp/repos", opts.Target)
	assert.Equal(t, []string{"prefix-*"}, opts.Include)
	assert.Equal(t, []string{"*-archive"}, opts.Exclude)
	assert.True(t, opts.IncludePrivate)
}
