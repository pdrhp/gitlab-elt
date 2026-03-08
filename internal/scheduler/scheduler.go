package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// SyncFunc is the function called to perform a sync cycle.
type SyncFunc func(ctx context.Context) error

// Config holds scheduler configuration.
type Config struct {
	SyncPeak    string // cron expression for peak hours (08-20h)
	SyncOffPeak string // cron expression for off-peak hours (20-08h)
	Discovery   string // cron expression for project discovery
}

// Scheduler orchestrates sync and discovery jobs on cron schedules.
type Scheduler struct {
	cron         *cron.Cron
	config       Config
	syncFunc     SyncFunc
	discoverFunc SyncFunc
}

// New creates a new Scheduler.
func New(cfg Config, syncFunc, discoverFunc SyncFunc, cronLogger cron.Logger) *Scheduler {
	c := cron.New(cron.WithLogger(cronLogger))
	return &Scheduler{
		cron:         c,
		config:       cfg,
		syncFunc:     syncFunc,
		discoverFunc: discoverFunc,
	}
}

// Start registers jobs and starts the cron scheduler.
func (s *Scheduler) Start() error {
	// Register sync job — uses peak/off-peak logic
	// We register BOTH schedules; the job itself checks if it should run
	_, err := s.cron.AddFunc(s.config.SyncPeak, func() {
		if !isPeakHour(time.Now().Hour()) {
			slog.Debug("scheduler: skipping peak sync (currently off-peak)")
			return
		}
		s.runSync()
	})
	if err != nil {
		return fmt.Errorf("register peak sync job: %w", err)
	}

	_, err = s.cron.AddFunc(s.config.SyncOffPeak, func() {
		if isPeakHour(time.Now().Hour()) {
			slog.Debug("scheduler: skipping off-peak sync (currently peak)")
			return
		}
		s.runSync()
	})
	if err != nil {
		return fmt.Errorf("register off-peak sync job: %w", err)
	}

	// Register discovery job
	_, err = s.cron.AddFunc(s.config.Discovery, func() {
		s.runDiscovery()
	})
	if err != nil {
		return fmt.Errorf("register discovery job: %w", err)
	}

	s.cron.Start()
	slog.Info("scheduler: started",
		"sync_peak", s.config.SyncPeak,
		"sync_offpeak", s.config.SyncOffPeak,
		"discovery", s.config.Discovery,
	)
	return nil
}

// Stop stops the cron scheduler and returns a context that is done when all running jobs complete.
func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}

// RunSyncNow triggers a sync cycle immediately (useful for testing/backfill).
func (s *Scheduler) RunSyncNow() {
	s.runSync()
}

// RunDiscoveryNow triggers a discovery cycle immediately.
func (s *Scheduler) RunDiscoveryNow() {
	s.runDiscovery()
}

func (s *Scheduler) runSync() {
	slog.Info("scheduler: sync job triggered")
	ctx := context.Background()
	if err := s.syncFunc(ctx); err != nil {
		slog.Error("scheduler: sync job failed", "error", err)
		return
	}
	slog.Info("scheduler: sync job completed")
}

func (s *Scheduler) runDiscovery() {
	slog.Info("scheduler: discovery job triggered")
	ctx := context.Background()
	if err := s.discoverFunc(ctx); err != nil {
		slog.Error("scheduler: discovery job failed", "error", err)
		return
	}
	slog.Info("scheduler: discovery job completed")
}

// isPeakHour returns true if the given hour (0-23) is within peak hours (08:00-19:59).
func isPeakHour(hour int) bool {
	return hour >= 8 && hour < 20
}
