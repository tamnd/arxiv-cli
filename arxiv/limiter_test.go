package arxiv

import (
	"context"
	"sync"
	"testing"
	"time"
)

// clock is a fake clock. Pacing is measured in seconds and holds are measured in
// minutes, so a test that used the real one would take longer than the whole
// suite is allowed to.
type clock struct {
	mu    sync.Mutex
	t     time.Time
	slept []time.Duration
}

func newClock() *clock {
	// Any fixed instant will do; this one is just readable in a failure.
	return &clock{t: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if d > 0 {
		c.slept = append(c.slept, d)
		c.t = c.t.Add(d)
	}
	return nil
}

func (c *clock) waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, len(c.slept))
	copy(out, c.slept)
	return out
}

func newFakeLimiter(pace time.Duration, c *clock) *limiter {
	l := newLimiter(pace)
	l.now, l.sleep = c.now, c.sleep
	return l
}

// TestLimiterFirstRequestDoesNotWait matters because a one-shot command is the
// common case, and paying a pace before saying anything would make every run
// feel broken.
func TestLimiterFirstRequestDoesNotWait(t *testing.T) {
	c := newClock()
	l := newFakeLimiter(3*time.Second, c)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.waits(); len(got) != 0 {
		t.Errorf("first wait slept %v, want nothing", got)
	}
}

func TestLimiterPacesLaterRequests(t *testing.T) {
	c := newClock()
	l := newFakeLimiter(3*time.Second, c)
	for i := 0; i < 4; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got := c.waits()
	want := []time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("wait %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestLimiterZeroPace covers the test client and any caller that has a reason to
// turn pacing off, which must then be nobody talking to arxiv.org.
func TestLimiterZeroPace(t *testing.T) {
	c := newClock()
	l := newFakeLimiter(0, c)
	for i := 0; i < 5; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.waits(); len(got) != 0 {
		t.Errorf("zero pace slept %v, want nothing", got)
	}
}

// TestLimiterHold is the 429 path: the hold applies to the plane, so the next
// request waits the held time rather than the ordinary pace.
func TestLimiterHold(t *testing.T) {
	c := newClock()
	l := newFakeLimiter(3*time.Second, c)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.hold(time.Minute)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := c.waits()
	if len(got) != 1 || got[0] != time.Minute {
		t.Fatalf("waits = %v, want [1m0s]", got)
	}
}

// TestLimiterHoldKeepsTheLongest stops a short hold from cancelling a long one
// when two requests trip the limit together.
func TestLimiterHoldKeepsTheLongest(t *testing.T) {
	c := newClock()
	l := newFakeLimiter(0, c)
	l.hold(10 * time.Minute)
	l.hold(time.Second)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := c.waits()
	if len(got) != 1 || got[0] != 10*time.Minute {
		t.Fatalf("waits = %v, want [10m0s]", got)
	}
}

func TestLimiterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := newLimiter(time.Hour)
	l.last = time.Now()
	if err := l.wait(ctx); err == nil {
		t.Fatal("expected a cancelled wait to return an error")
	}
}

// TestLimiterSerializes checks the lock is held across the sleep. Two goroutines
// racing through a limiter that released the lock before sleeping would both
// fire at once, which is exactly what pacing exists to prevent.
func TestLimiterSerializes(t *testing.T) {
	c := newClock()
	l := newFakeLimiter(time.Second, c)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.wait(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	// Eight requests at one second each: the first goes straight through and the
	// other seven each pay the pace.
	if got := c.waits(); len(got) != 7 {
		t.Errorf("slept %d times for 8 requests, want 7: %v", len(got), got)
	}
}
