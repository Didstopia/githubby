// Package schedule provides cron-based scheduling for sync operations
package schedule

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

// SyncFunc is the function signature for sync callbacks
type SyncFunc func(ctx context.Context) error

// Scheduler manages cron-based sync execution
type Scheduler struct {
	spec   string
	syncFn SyncFunc
}

// ValidateSpec validates a cron expression
func ValidateSpec(spec string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	_, err := parser.Parse(spec)
	if err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", spec, err)
	}
	return nil
}

// New creates a new Scheduler. Returns an error if the cron spec is invalid.
func New(spec string, syncFn SyncFunc) (*Scheduler, error) {
	if err := ValidateSpec(spec); err != nil {
		return nil, err
	}
	return &Scheduler{
		spec:   spec,
		syncFn: syncFn,
	}, nil
}

// Run executes an immediate sync, then starts the cron loop.
// It blocks until the context is cancelled.
// Sync errors are logged but do not stop the scheduler.
// Overlapping runs are skipped via cron.SkipIfStillRunning.
func (s *Scheduler) Run(ctx context.Context) error {
	// Run an immediate sync
	log.Printf("[schedule] Running immediate sync...")
	if err := s.syncFn(ctx); err != nil {
		log.Printf("[schedule] Sync error: %v", err)
	}

	// Check if context was cancelled during immediate sync
	if ctx.Err() != nil {
		return nil
	}

	// Set up cron with SkipIfStillRunning to prevent overlapping runs
	c := cron.New(
		cron.WithParser(cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)),
		cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)),
	)

	_, err := c.AddFunc(s.spec, func() {
		log.Printf("[schedule] Running scheduled sync...")
		if err := s.syncFn(ctx); err != nil {
			log.Printf("[schedule] Sync error: %v", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	c.Start()
	log.Printf("[schedule] Scheduler started with schedule: %s", s.spec)

	// Block until context is cancelled
	<-ctx.Done()

	// Stop the cron scheduler gracefully
	stopCtx := c.Stop()
	<-stopCtx.Done()

	log.Printf("[schedule] Scheduler stopped")
	return nil
}
