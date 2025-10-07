package jobs

import (
	"context"
	"time"
)

type Job func(ctx context.Context) error

type Runner struct {
	ctx context.Context
}

func New(ctx context.Context) *Runner { return &Runner{ctx: ctx} }

func (r *Runner) Every(interval time.Duration, name string, fn Job) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-t.C:
				start := time.Now()
				if err := fn(r.ctx); err != nil {
					jobErrors.WithLabelValues(name).Inc()
				}
				jobRuns.WithLabelValues(name).Inc()
				jobDuration.WithLabelValues(name).Observe(time.Since(start).Seconds())
			}
		}
	}()
}
