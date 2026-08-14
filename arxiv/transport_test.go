package arxiv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// newRetryClient is newTestClient with the clock faked, so a test can watch the
// backoff table without waiting out ten minutes of it.
func newRetryClient(t *testing.T, ts *httptest.Server) (*Client, *clock) {
	t.Helper()
	c := newTestClient(t, ts)
	clk := newClock()
	c.sleep = clk.sleep
	lim := newFakeLimiter(0, clk)
	c.limiters = map[string]*limiter{"test": lim}
	return c, clk
}

func TestBackoffDoublesToTheCeiling(t *testing.T) {
	cases := []struct {
		policy retryPolicy
		want   []time.Duration
	}{
		{networkRetry, []time.Duration{
			time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		}},
		{rateRetry, []time.Duration{
			time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute,
		}},
		{serverRetry, []time.Duration{
			5 * time.Second, 10 * time.Second, 20 * time.Second,
		}},
	}
	for _, tc := range cases {
		for i, want := range tc.want {
			if got := tc.policy.backoff(i + 1); got != want {
				t.Errorf("backoff(%d) with initial %s = %s, want %s",
					i+1, tc.policy.initial, got, want)
			}
		}
		// Past the table the delay stops growing rather than running away.
		if got := tc.policy.backoff(20); got != tc.policy.max {
			t.Errorf("backoff(20) = %s, want the ceiling %s", got, tc.policy.max)
		}
	}
}

// TestRetryOn503 walks the server policy: three retries, then the error.
func TestRetryOn503(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	c, clk := newRetryClient(t, ts)
	_, err := c.fetch(context.Background(), ts.URL, 0)
	if err == nil {
		t.Fatal("expected an error after the retries ran out")
	}
	if got := atomic.LoadInt32(&hits); got != int32(serverRetry.attempts)+1 {
		t.Errorf("server saw %d requests, want %d", got, serverRetry.attempts+1)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	got := clk.waits()
	if len(got) != len(want) {
		t.Fatalf("backoffs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("backoff %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestRetrySucceedsAfterFailure is the case the retries exist for: a blip, then
// the answer, with no error reaching the caller.
func TestRetrySucceedsAfterFailure(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer ts.Close()

	c, _ := newRetryClient(t, ts)
	resp, err := c.fetch(context.Background(), ts.URL, 0)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(string(resp.Body), "Attention Is All You Need") {
		t.Errorf("body is not the feed: %.60q", resp.Body)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("server saw %d requests, want 3", got)
	}
}

// TestRateLimitHoldsThePlane checks a 429 stands the plane down rather than
// sleeping the one request. The limiter's hold is what makes a concurrent read
// back off too, so this asserts the wait went through the limiter.
func TestRateLimitHoldsThePlane(t *testing.T) {
	body := fixture(t, "rate_exceeded.txt")
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	c, clk := newRetryClient(t, ts)
	_, err := c.fetch(context.Background(), ts.URL, 0)
	if err == nil {
		t.Fatal("expected an error after the rate retries ran out")
	}
	if kind := errs.KindOf(err); kind != errs.KindRateLimited {
		t.Errorf("error kind = %v, want rate limited", kind)
	}
	if !strings.Contains(err.Error(), "Rate exceeded.") {
		t.Errorf("error %q does not quote arxiv's body", err)
	}
	if got := atomic.LoadInt32(&hits); got != int32(rateRetry.attempts)+1 {
		t.Errorf("server saw %d requests, want %d", got, rateRetry.attempts+1)
	}
	want := []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute,
	}
	got := clk.waits()
	if len(got) != len(want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("wait %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestRateLimitRecovers is the case that matters more than the give-up path. A
// 429 is arXiv asking us to slow down, not arXiv refusing, so the read waits and
// then completes instead of coming back as a failure.
func TestRateLimitRecovers(t *testing.T) {
	body := fixture(t, "rate_exceeded.txt")
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer ts.Close()

	c, clk := newRetryClient(t, ts)
	resp, err := c.fetch(context.Background(), ts.URL, 0)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(string(resp.Body), "Attention Is All You Need") {
		t.Errorf("body is not the feed: %.60q", resp.Body)
	}
	got := clk.waits()
	if len(got) != 1 || got[0] != time.Minute {
		t.Errorf("waits = %v, want one minute through the limiter", got)
	}
}

// TestNoRetryOnClientError pins the other half of the table. A 404 or a rejected
// query is not going to become true by asking again.
func TestNoRetryOnClientError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		kind   errs.Kind
	}{
		{"not found", http.StatusNotFound, "not found", errs.KindNotFound},
		{"bad query", http.StatusBadRequest, errorFeed("#incorrect_id_format_for_notanid",
			"incorrect id format for notanid"), errs.KindUsage},
		{"long id list", http.StatusBadRequest, "<html><body>too long</body></html>", errs.KindUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()

			c, _ := newRetryClient(t, ts)
			if _, err := c.fetch(context.Background(), ts.URL, 0); err == nil {
				t.Fatal("expected an error")
			} else if kind := errs.KindOf(err); kind != tc.kind {
				t.Errorf("error kind = %v, want %v: %v", kind, tc.kind, err)
			}
			if got := atomic.LoadInt32(&hits); got != 1 {
				t.Errorf("server saw %d requests, want 1", got)
			}
		})
	}
}

// TestNoRetryOn500WithErrorEntry is the subtle one. arXiv answers some bad
// requests with a 500 carrying an error entry, and retrying that just asks the
// same wrong question three more times.
func TestNoRetryOn500WithErrorEntry(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(errorFeed("", "The server encountered an internal error.")))
	}))
	defer ts.Close()

	c, _ := newRetryClient(t, ts)
	if _, err := c.fetch(context.Background(), ts.URL, 0); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server saw %d requests, want 1", got)
	}
}

// TestUnknownHostHasNoPace is the safety net under the whole scheme. A URL
// nobody set a pace for is a bug in this tool, and it fails loudly instead of
// hammering a host at no pace at all.
func TestUnknownHostHasNoPace(t *testing.T) {
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.fetch(context.Background(), "https://example.com/whatever", 0)
	if err == nil {
		t.Fatal("expected a fetch from an unpaced host to fail")
	}
	if !strings.Contains(err.Error(), "plane table") {
		t.Errorf("error %q does not explain the missing plane", err)
	}
}

// TestCacheHitSkipsTheRequest is the reason fetch checks the cache before the
// limiter: a cached read costs nothing and arXiv cannot feel it.
func TestCacheHitSkipsTheRequest(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer ts.Close()

	c, _ := newRetryClient(t, ts)
	c.cache = newCache(t.TempDir(), false)

	first, err := c.fetch(context.Background(), ts.URL, time.Hour)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.FromCache {
		t.Error("the first fetch came from the cache")
	}
	second, err := c.fetch(context.Background(), ts.URL, time.Hour)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !second.FromCache {
		t.Error("the second fetch went to the network")
	}
	if string(second.Body) != string(first.Body) {
		t.Error("the cached body is not the fetched body")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server saw %d requests, want 1", got)
	}
}

// TestContextCancelStopsRetries checks a ctrl-c during a backoff comes back as a
// cancellation rather than as a retry that never ends.
func TestContextCancelStopsRetries(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := c.fetch(ctx, ts.URL, 0); err == nil {
		t.Fatal("expected a cancelled fetch to return an error")
	}
}
