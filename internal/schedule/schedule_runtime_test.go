package schedule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_RejectsBadSpecAtRuntime(t *testing.T) {
	// New validates specs, so build the Scheduler directly to reach the cron error path.
	s := &Scheduler{spec: "not a spec", syncFn: func(ctx context.Context) error { return nil }}
	err := s.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add cron job")
}
