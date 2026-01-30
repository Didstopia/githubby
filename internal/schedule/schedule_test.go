package schedule

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		// Valid specs
		{name: "every minute", spec: "* * * * *", wantErr: false},
		{name: "every 30 minutes", spec: "*/30 * * * *", wantErr: false},
		{name: "every hour descriptor", spec: "@every 1h", wantErr: false},
		{name: "every 5m descriptor", spec: "@every 5m", wantErr: false},
		{name: "hourly descriptor", spec: "@hourly", wantErr: false},
		{name: "daily descriptor", spec: "@daily", wantErr: false},
		{name: "weekly descriptor", spec: "@weekly", wantErr: false},
		{name: "monthly descriptor", spec: "@monthly", wantErr: false},
		{name: "specific time", spec: "0 6 * * *", wantErr: false},
		{name: "complex schedule", spec: "0 */6 * * 1-5", wantErr: false},

		// Invalid specs
		{name: "empty string", spec: "", wantErr: true},
		{name: "too few fields", spec: "* *", wantErr: true},
		{name: "bad characters", spec: "abc def ghi jkl mno", wantErr: true},
		{name: "out of range minute", spec: "61 * * * *", wantErr: true},
		{name: "out of range hour", spec: "* 25 * * *", wantErr: true},
		{name: "random text", spec: "not a cron spec", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSpec(tt.spec)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNew_InvalidSpec(t *testing.T) {
	s, err := New("invalid spec", func(ctx context.Context) error {
		return nil
	})
	assert.Nil(t, s)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron schedule")
}

func TestNew_ValidSpec(t *testing.T) {
	s, err := New("* * * * *", func(ctx context.Context) error {
		return nil
	})
	assert.NotNil(t, s)
	assert.NoError(t, err)
}

func TestRun_ImmediateExecution(t *testing.T) {
	var called atomic.Int32

	s, err := New("@every 1h", func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	// Run in a goroutine since Run blocks
	go func() {
		// Give enough time for immediate execution
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err = s.Run(ctx)
	assert.NoError(t, err)

	// The sync function should have been called at least once (immediately)
	assert.GreaterOrEqual(t, called.Load(), int32(1))
}

func TestRun_ContextCancellation(t *testing.T) {
	s, err := New("@every 1h", func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	// Cancel after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Run should return promptly after cancellation
	select {
	case <-done:
		// Success - Run returned after context cancellation
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRun_SyncErrorContinuesSchedule(t *testing.T) {
	var callCount atomic.Int32

	s, err := New("@every 1s", func(ctx context.Context) error {
		callCount.Add(1)
		return fmt.Errorf("sync failed")
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// Allow time for immediate execution + at least two scheduled executions
		time.Sleep(3 * time.Second)
		cancel()
	}()

	err = s.Run(ctx)
	assert.NoError(t, err)

	// Should have been called multiple times despite errors:
	// once immediately, plus at least once by cron
	assert.GreaterOrEqual(t, callCount.Load(), int32(2))
}
