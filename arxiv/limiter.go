package arxiv

import (
	"context"
	"sync"
	"time"
)

// limiter paces one plane.
//
// There is one of these per plane rather than one per client, because a read
// that falls back from the export API to the abstract page crosses planes
// halfway through and the two paces are five times apart.
type limiter struct {
	mu    sync.Mutex
	pace  time.Duration
	last  time.Time
	until time.Time // the plane is held until this instant, set by a 429

	// now and sleep are injected so the retry tests do not take ten minutes.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func newLimiter(pace time.Duration) *limiter {
	return &limiter{pace: pace, now: time.Now, sleep: sleepCtx}
}

// wait blocks until the plane is ready for another request.
//
// It holds the lock across the sleep on purpose. Two goroutines that each
// checked the gap, then each slept, then each fired, would send two requests at
// once and trip the very limit this exists to respect.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	ready := l.last.Add(l.pace)
	if l.until.After(ready) {
		ready = l.until
	}
	if wait := ready.Sub(now); wait > 0 {
		if err := l.sleep(ctx, wait); err != nil {
			return err
		}
	}
	l.last = l.now()
	return nil
}

// hold puts the whole plane to sleep for d, which is what a 429 means: the host
// is not asking this one request to wait, it is asking us to stop.
func (l *limiter) hold(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := l.now().Add(d); until.After(l.until) {
		l.until = until
	}
}

// sleepCtx waits for d, or returns early if the context is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
