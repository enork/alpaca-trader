package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/trading"
)

type runner struct {
	cfg    *config.Config
	engine *trading.Engine
	bc     *broker.Client
	log    *slog.Logger
}

// start executes the configured run modes and blocks until ctx is cancelled.
// run_on_startup fires once immediately; run_on_open and run_on_cron run
// concurrently in the background.
func (r *runner) start(ctx context.Context) {
	if r.cfg.Trading.RunOnStartup {
		r.log.Info("run_on_startup: running cycle now")
		r.runCycle("startup")
	}

	var wg sync.WaitGroup

	if r.cfg.Trading.RunOnOpen {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.marketOpenLoop(ctx)
		}()
	}

	if r.cfg.Trading.RunOnCron != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.cronLoop(ctx)
		}()
	}

	wg.Wait()
}

// marketOpenLoop waits for each market open and runs one cycle per open.
func (r *runner) marketOpenLoop(ctx context.Context) {
	for {
		clock, err := r.bc.GetClock()
		if err != nil {
			r.log.Error("market open loop: failed to get clock", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
				continue
			}
		}

		wait := time.Until(clock.NextOpen)
		r.log.Info("run_on_open: waiting for market open", "next_open", clock.NextOpen, "wait", wait.Round(time.Second))

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		r.log.Info("run_on_open: market opened, running cycle")
		r.runCycle("market_open")

		// Brief pause so the next GetClock call sees the updated NextOpen.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
		}
	}
}

// cronLoop registers the cron expression and blocks until ctx is cancelled.
func (r *runner) cronLoop(ctx context.Context) {
	c := cron.New()
	_, err := c.AddFunc(r.cfg.Trading.RunOnCron, func() {
		r.log.Info("run_on_cron: triggered, running cycle", "expr", r.cfg.Trading.RunOnCron)
		r.runCycle("cron")
	})
	if err != nil {
		r.log.Error("run_on_cron: invalid cron expression", "expr", r.cfg.Trading.RunOnCron, "error", err)
		return
	}

	r.log.Info("run_on_cron: scheduled", "expr", r.cfg.Trading.RunOnCron)
	c.Start()
	<-ctx.Done()
	c.Stop()
}

func (r *runner) runCycle(trigger string) {
	clock, err := r.bc.GetClock()
	if err != nil {
		r.log.Error("trading cycle aborted: could not fetch clock", "trigger", trigger, "error", err)
		return
	}
	if !clock.IsOpen {
		r.log.Info("trading cycle skipped: market is closed", "trigger", trigger, "next_open", clock.NextOpen)
		return
	}

	if err := r.engine.Run(); err != nil {
		r.log.Error("trading cycle failed", "trigger", trigger, "error", err)
	}
}
