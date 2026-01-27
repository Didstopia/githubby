package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Didstopia/githubby/internal/sync"
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
		result := &sync.Result{
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
		result := &sync.Result{
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
		result := &sync.Result{
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
		result := &sync.Result{
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
		result := &sync.Result{
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
		result := &sync.Result{
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
		result := sync.NewResult()

		assert.Empty(t, result.Cloned)
		assert.Empty(t, result.Updated)
		assert.Empty(t, result.Skipped)
		assert.Empty(t, result.Failed)
	})

	t.Run("counts match appended items", func(t *testing.T) {
		result := sync.NewResult()

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
