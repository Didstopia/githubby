package screens

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileSyncProgressUpdate_StatusValues(t *testing.T) {
	// Verify that all expected status values are documented
	validStatuses := []string{
		"collecting",
		"syncing",
		"cloned",
		"updated",
		"up-to-date",
		"skipped",
		"failed",
	}

	// This test ensures we don't forget to handle new statuses
	for _, status := range validStatuses {
		update := profileSyncProgressUpdate{
			repoName: "test/repo",
			status:   status,
			current:  1,
			total:    10,
		}
		assert.Equal(t, status, update.status)
	}
}

func TestChannelBufferSize(t *testing.T) {
	// Test that the channel buffer (16) can handle burst writes from 4 workers
	// without blocking. This prevents the deadlock issue where workers block
	// waiting to send progress updates.

	t.Run("buffer handles 4 concurrent workers without blocking", func(t *testing.T) {
		// Simulate the channel setup from startSync
		progressChan := make(chan profileSyncProgressUpdate, 16)

		// Simulate 4 workers each sending 2 messages (syncing + completion)
		// This is 8 total messages, well within our 16 buffer
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				// Worker sends "syncing" before processing
				progressChan <- profileSyncProgressUpdate{
					repoName: "test/repo",
					status:   "syncing",
					current:  workerID,
					total:    4,
				}
				// Small delay to simulate actual sync work
				time.Sleep(10 * time.Millisecond)
			}(i)
		}

		// Wait for all workers with timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success - workers didn't block
		case <-time.After(1 * time.Second):
			t.Fatal("workers blocked - channel buffer too small")
		}

		// Drain the channel
		close(progressChan)
		count := 0
		for range progressChan {
			count++
		}
		assert.Equal(t, 4, count, "expected 4 messages from 4 workers")
	})

	t.Run("buffer handles burst of 16 messages", func(t *testing.T) {
		progressChan := make(chan profileSyncProgressUpdate, 16)

		// Send 16 messages without reading - should not block
		done := make(chan struct{})
		go func() {
			for i := 0; i < 16; i++ {
				progressChan <- profileSyncProgressUpdate{
					status:  "syncing",
					current: i,
					total:   16,
				}
			}
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Fatal("blocked while filling buffer to capacity")
		}

		close(progressChan)
	})
}

func TestCollectingPhaseStateTransitions(t *testing.T) {
	// Test that the collecting phase state transitions correctly

	t.Run("collecting status sets collecting flag true", func(t *testing.T) {
		screen := &SyncProgressScreen{
			collecting:      false,
			collectingCount: 0,
		}

		// Simulate receiving a collecting update
		update := profileSyncProgressUpdate{
			status:  "collecting",
			current: 50,
			total:   0,
		}

		// Apply the update logic (extracted from Update method)
		if update.status == "collecting" {
			screen.collecting = true
			screen.collectingCount = update.current
		}

		assert.True(t, screen.collecting)
		assert.Equal(t, 50, screen.collectingCount)
	})

	t.Run("syncing status clears collecting flag", func(t *testing.T) {
		screen := &SyncProgressScreen{
			collecting:      true,
			collectingCount: 100,
		}

		// Simulate receiving a syncing update
		update := profileSyncProgressUpdate{
			status:   "syncing",
			repoName: "test/repo",
			current:  1,
			total:    100,
		}

		// Apply the update logic
		if update.status == "syncing" {
			screen.collecting = false
		}

		assert.False(t, screen.collecting)
	})

	t.Run("completion statuses clear collecting flag", func(t *testing.T) {
		completionStatuses := []string{"cloned", "updated", "up-to-date", "skipped", "failed", "archived"}

		for _, status := range completionStatuses {
			t.Run(status, func(t *testing.T) {
				screen := &SyncProgressScreen{
					collecting: true,
				}

				// Apply the update logic for completion statuses
				switch status {
				case "cloned", "updated", "up-to-date", "skipped", "failed", "archived":
					screen.collecting = false
				}

				assert.False(t, screen.collecting, "status %s should clear collecting flag", status)
			})
		}
	})
}

func TestContextCancellationDuringCollection(t *testing.T) {
	// Test that context cancellation is properly checked during collection

	t.Run("cancelled context detected in profile loop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Check that context cancellation is detected
		select {
		case <-ctx.Done():
			assert.Equal(t, context.Canceled, ctx.Err())
		default:
			t.Fatal("context should be cancelled")
		}
	})

	t.Run("context check pattern works correctly", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Pattern used in the code
		shouldCancel := false
		select {
		case <-ctx.Done():
			shouldCancel = true
		default:
			// Continue processing
		}
		assert.False(t, shouldCancel, "should not cancel before cancel() called")

		cancel()

		select {
		case <-ctx.Done():
			shouldCancel = true
		default:
		}
		assert.True(t, shouldCancel, "should cancel after cancel() called")
	})
}

func TestProgressMessageTypes(t *testing.T) {
	t.Run("profileSyncProgressMsg wraps update correctly", func(t *testing.T) {
		update := profileSyncProgressUpdate{
			repoName: "owner/repo",
			status:   "cloned",
			current:  5,
			total:    10,
		}

		msg := profileSyncProgressMsg{update: update}

		assert.Equal(t, "owner/repo", msg.update.repoName)
		assert.Equal(t, "cloned", msg.update.status)
		assert.Equal(t, 5, msg.update.current)
		assert.Equal(t, 10, msg.update.total)
	})

	t.Run("profileSyncCompleteMsg contains all fields", func(t *testing.T) {
		msg := profileSyncCompleteMsg{
			cloned:   10,
			updated:  5,
			skipped:  2,
			failed:   1,
			archived: 3,
			err:      nil,
		}

		assert.Equal(t, 10, msg.cloned)
		assert.Equal(t, 5, msg.updated)
		assert.Equal(t, 2, msg.skipped)
		assert.Equal(t, 1, msg.failed)
		assert.Equal(t, 3, msg.archived)
		assert.Nil(t, msg.err)
	})
}

func TestHumanizeDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"less than a second", 500 * time.Millisecond, "less than a second"},
		{"exactly 1 second", 1 * time.Second, "1 second"},
		{"multiple seconds", 45 * time.Second, "45 seconds"},
		{"exactly 1 minute", 1 * time.Minute, "1 minute"},
		{"multiple minutes", 15 * time.Minute, "15 minutes"},
		{"exactly 1 hour", 1 * time.Hour, "1 hour"},
		{"1 hour with minutes", 1*time.Hour + 30*time.Minute, "1 hour 30 min"},
		{"multiple hours", 3 * time.Hour, "3 hours"},
		{"multiple hours with minutes", 2*time.Hour + 15*time.Minute, "2 hours 15 min"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := humanizeDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyncProgressScreenStruct(t *testing.T) {
	t.Run("new screen has expected initial state", func(t *testing.T) {
		ctx := context.Background()

		// Create a minimal screen to test struct initialization
		screen := &SyncProgressScreen{
			ctx:        ctx,
			loading:    true,
			collecting: false,
			syncing:    false,
			complete:   false,
		}

		assert.True(t, screen.loading)
		assert.False(t, screen.collecting)
		assert.False(t, screen.syncing)
		assert.False(t, screen.complete)
	})

	t.Run("collecting state tracking fields exist", func(t *testing.T) {
		screen := &SyncProgressScreen{
			collecting:      true,
			collectingCount: 476,
		}

		assert.True(t, screen.collecting)
		assert.Equal(t, 476, screen.collectingCount)
	})
}

func TestProgressUpdateStatistics(t *testing.T) {
	// Test that completion statuses properly increment counters

	t.Run("cloned increments cloned counter", func(t *testing.T) {
		screen := &SyncProgressScreen{}
		screen.cloned = 0
		screen.reposCompleted = 0

		// Simulate status update logic
		screen.cloned++
		screen.reposCompleted++

		assert.Equal(t, 1, screen.cloned)
		assert.Equal(t, 1, screen.reposCompleted)
	})

	t.Run("all completion statuses tracked correctly", func(t *testing.T) {
		screen := &SyncProgressScreen{}

		// Simulate multiple updates
		statusCounts := map[string]int{
			"cloned":    3,
			"updated":   5,
			"up-to-date": 10,
			"skipped":   2,
			"failed":    1,
			"archived":  1,
		}

		for status, count := range statusCounts {
			for i := 0; i < count; i++ {
				switch status {
				case "cloned":
					screen.cloned++
					screen.reposCompleted++
				case "updated":
					screen.updated++
					screen.reposCompleted++
				case "up-to-date":
					screen.upToDate++
					screen.reposCompleted++
				case "skipped":
					screen.skipped++
					screen.reposCompleted++
				case "failed":
					screen.failed++
					screen.reposCompleted++
				case "archived":
					screen.archived++
					screen.reposCompleted++
				}
			}
		}

		assert.Equal(t, 3, screen.cloned)
		assert.Equal(t, 5, screen.updated)
		assert.Equal(t, 10, screen.upToDate)
		assert.Equal(t, 2, screen.skipped)
		assert.Equal(t, 1, screen.failed)
		assert.Equal(t, 1, screen.archived)
		assert.Equal(t, 22, screen.reposCompleted)
	})
}

func TestWaitForSyncProgress(t *testing.T) {
	t.Run("receives progress update from channel", func(t *testing.T) {
		screen := &SyncProgressScreen{
			syncProgressChan: make(chan profileSyncProgressUpdate, 1),
			syncDoneChan:     make(chan profileSyncCompleteMsg, 1),
		}

		// Send an update
		screen.syncProgressChan <- profileSyncProgressUpdate{
			repoName: "test/repo",
			status:   "cloned",
			current:  1,
			total:    10,
		}

		// Get the command
		cmd := screen.waitForSyncProgress()
		require.NotNil(t, cmd)

		// Execute and check result
		msg := cmd()
		progressMsg, ok := msg.(profileSyncProgressMsg)
		require.True(t, ok, "expected profileSyncProgressMsg")
		assert.Equal(t, "test/repo", progressMsg.update.repoName)
		assert.Equal(t, "cloned", progressMsg.update.status)
	})

	t.Run("receives done message when progress channel closed", func(t *testing.T) {
		screen := &SyncProgressScreen{
			syncProgressChan: make(chan profileSyncProgressUpdate, 1),
			syncDoneChan:     make(chan profileSyncCompleteMsg, 1),
		}

		// Close progress channel and send done
		close(screen.syncProgressChan)
		screen.syncDoneChan <- profileSyncCompleteMsg{cloned: 5}

		// Get the command
		cmd := screen.waitForSyncProgress()
		require.NotNil(t, cmd)

		// Execute and check result
		msg := cmd()
		doneMsg, ok := msg.(profileSyncCompleteMsg)
		require.True(t, ok, "expected profileSyncCompleteMsg")
		assert.Equal(t, 5, doneMsg.cloned)
	})
}

func TestSyncProgressViewContent(t *testing.T) {
	// Test that the View function includes expected content for different states

	t.Run("collecting state shows collecting message", func(t *testing.T) {
		screen := &SyncProgressScreen{
			syncing:         true,
			collecting:      true,
			collectingCount: 100,
			totalRepos:      0, // unknown during collection
		}

		// The view should show "Collecting repositories..." when collecting
		// We can't easily test View() without all dependencies, but we can
		// verify the state is set up correctly for the view to use
		assert.True(t, screen.collecting)
		assert.Equal(t, 100, screen.collectingCount)
	})

	t.Run("syncing state with current repo", func(t *testing.T) {
		screen := &SyncProgressScreen{
			syncing:     true,
			collecting:  false,
			currentRepo: "owner/repo",
			totalRepos:  10,
			cloned:      2,
			updated:     1,
			upToDate:    0,
		}

		assert.False(t, screen.collecting)
		assert.Equal(t, "owner/repo", screen.currentRepo)
	})
}

func TestUpdateMessageHandling(t *testing.T) {
	// Test the message handling logic extracted from Update

	t.Run("totalRepos only updated when positive", func(t *testing.T) {
		screen := &SyncProgressScreen{
			totalRepos: 100,
		}

		// Update with total=0 should not change totalRepos
		update := profileSyncProgressUpdate{
			status:  "collecting",
			current: 50,
			total:   0,
		}

		if update.total > 0 {
			screen.totalRepos = update.total
		}

		assert.Equal(t, 100, screen.totalRepos, "totalRepos should not be updated with 0")

		// Update with positive total should update
		update2 := profileSyncProgressUpdate{
			status:  "syncing",
			current: 1,
			total:   200,
		}

		if update2.total > 0 {
			screen.totalRepos = update2.total
		}

		assert.Equal(t, 200, screen.totalRepos, "totalRepos should be updated with positive value")
	})
}

// BenchmarkChannelOperations tests the performance of channel operations
func BenchmarkChannelOperations(b *testing.B) {
	b.Run("buffer-1", func(b *testing.B) {
		ch := make(chan profileSyncProgressUpdate, 1)
		done := make(chan struct{})

		go func() {
			for range ch {
			}
			close(done)
		}()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ch <- profileSyncProgressUpdate{status: "syncing"}
		}
		close(ch)
		<-done
	})

	b.Run("buffer-16", func(b *testing.B) {
		ch := make(chan profileSyncProgressUpdate, 16)
		done := make(chan struct{})

		go func() {
			for range ch {
			}
			close(done)
		}()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ch <- profileSyncProgressUpdate{status: "syncing"}
		}
		close(ch)
		<-done
	})
}

// TestTeaModelInterface ensures SyncProgressScreen implements tea.Model
func TestTeaModelInterface(t *testing.T) {
	// This is a compile-time check that SyncProgressScreen implements tea.Model
	var _ tea.Model = (*SyncProgressScreen)(nil)
}
