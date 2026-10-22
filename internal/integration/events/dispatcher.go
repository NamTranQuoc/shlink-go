// Package events ports Shlink's event dispatching (shlink-event-dispatcher): "regular" listeners run inline, "async"
// ones run on background workers, the way RoadRunner's in-memory jobs pipeline runs them upstream.
package events

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// Listeners groups the listeners registered for each event. Only enabled listeners are registered, like
// EnabledListenerChecker does.
type Listeners struct {
	// ShortURLCreated are async listeners of ShortUrlCreated.
	ShortURLCreated []func(ctx context.Context, shortURLID int64)
	// URLVisited are async listeners of UrlVisited.
	URLVisited []func(ctx context.Context, visitID int64, originalIPAddress *string)
	// GeoLiteDbCreated are regular listeners of GeoLiteDbCreated.
	GeoLiteDbCreated []func(ctx context.Context)
}

// Dispatcher implements core.EventDispatcher.
type Dispatcher struct {
	Listeners Listeners
	Logger    *slog.Logger

	jobs    chan func(context.Context)
	wg      sync.WaitGroup
	mu      sync.RWMutex
	stopped bool
	ctx     context.Context
	cancel  context.CancelFunc
	// sync runs every listener inline, for processes without workers (the CLI).
	sync bool
}

// queueSize bounds the pending async jobs; dispatching blocks when the queue is full.
const queueSize = 10000

// NewDispatcher creates a dispatcher and starts its workers. workers == 0 means one per CPU, like RoadRunner's
// TASK_WORKER_NUM=0 default; workers < 0 runs listeners inline.
func NewDispatcher(l Listeners, workers int, logger *slog.Logger) *Dispatcher {
	if workers < 0 {
		return &Dispatcher{Listeners: l, Logger: logger, sync: true, ctx: context.Background(), cancel: func() {}}
	}
	if workers == 0 {
		workers = runtime.NumCPU()
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{Listeners: l, Logger: logger, jobs: make(chan func(context.Context), queueSize), ctx: ctx, cancel: cancel}
	for i := 0; i < workers; i++ {
		d.wg.Add(1)
		go d.work()
	}
	return d
}

func (d *Dispatcher) work() {
	defer d.wg.Done()
	for job := range d.jobs {
		d.run(job)
	}
}

func (d *Dispatcher) run(job func(context.Context)) {
	defer func() {
		if rec := recover(); rec != nil {
			d.Logger.Error("An async event listener failed", "e", rec)
		}
	}()
	// The jobs pipeline timeout is 5 minutes (jobs.timeout in .rr.yml)
	ctx, cancel := context.WithTimeout(d.ctx, 5*time.Minute)
	defer cancel()
	job(ctx)
}

func (d *Dispatcher) enqueue(job func(context.Context)) {
	if d.sync {
		d.run(job)
		return
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.stopped {
		return
	}
	d.jobs <- job
}

// ShortURLCreated dispatches ShortUrlCreated.
func (d *Dispatcher) ShortURLCreated(shortURLID int64) {
	for _, l := range d.Listeners.ShortURLCreated {
		d.enqueue(func(ctx context.Context) { l(ctx, shortURLID) })
	}
}

// URLVisited dispatches UrlVisited.
func (d *Dispatcher) URLVisited(visitID int64, originalIPAddress *string) {
	for _, l := range d.Listeners.URLVisited {
		d.enqueue(func(ctx context.Context) { l(ctx, visitID, originalIPAddress) })
	}
}

// GeoLiteDbCreated dispatches GeoLiteDbCreated to its regular listeners, inline.
func (d *Dispatcher) GeoLiteDbCreated(ctx context.Context) {
	for _, l := range d.Listeners.GeoLiteDbCreated {
		l(ctx)
	}
}

// Shutdown stops accepting events and waits for pending jobs, or cancels them when ctx expires.
func (d *Dispatcher) Shutdown(ctx context.Context) {
	if d.sync {
		return
	}
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	close(d.jobs)
	d.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		d.cancel()
		<-done
	}
}
