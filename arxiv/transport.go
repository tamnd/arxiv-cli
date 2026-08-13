package arxiv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// maxBody caps a response read. The biggest thing on any surface is a LaTeXML
// page and those run to a few megabytes, so 16 MB is generous rather than tight.
const maxBody = 16 << 20

// retryPolicy is one row of the retry table in spec 3006 doc 02 section 5.3.
type retryPolicy struct {
	attempts int           // how many retries after the first try
	initial  time.Duration // first backoff
	max      time.Duration // ceiling the doubling stops at
}

// The three policies. The 429 numbers are not guesses: tripping arxiv.org's
// limit returned a fourteen byte "Rate exceeded." body with no Retry-After, and
// the host then stalled rather than answering for about 45 seconds. Starting at
// a minute is the smallest backoff that is comfortably past that.
var (
	networkRetry = retryPolicy{attempts: 5, initial: time.Second, max: 30 * time.Second}
	rateRetry    = retryPolicy{attempts: 5, initial: time.Minute, max: 10 * time.Minute}
	serverRetry  = retryPolicy{attempts: 3, initial: 5 * time.Second, max: 40 * time.Second}
)

// backoff is the delay before retry number n, counting from 1, doubling from
// the policy's initial value and stopping at its ceiling.
func (p retryPolicy) backoff(n int) time.Duration {
	d := p.initial
	for i := 1; i < n; i++ {
		d *= 2
		if d >= p.max {
			return p.max
		}
	}
	return d
}

// response is what a fetch returns: the bytes, and where they came from.
type response struct {
	Body []byte
	// FromCache is true when no request was made. It is deliberately not part
	// of any record: whether a byte came off disk is not a property of a paper.
	FromCache bool
	// Plane is the plane the request went to, empty on a cache hit.
	Plane string
}

// fetch gets a URL through the cache, the right plane's limiter, and the retry
// table, in that order.
//
// The order matters. A cache hit is not a request: it does not wait for the
// limiter and it does not count against a crawl budget, because pacing exists to
// be kind to arXiv and reading a local file is not something arXiv can feel.
func (c *Client) fetch(ctx context.Context, rawURL string, ttl time.Duration) (response, error) {
	if body, ok := c.cache.get(rawURL, ttl); ok {
		c.logf(2, "cache hit %s", rawURL)
		return response{Body: body, FromCache: true}, nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return response{}, errs.Wrap(errs.KindGeneric, err, "bad URL %q", rawURL)
	}
	plane, lim, err := c.planeFor(u.Host)
	if err != nil {
		return response{}, err
	}

	var last error
	for attempt := 0; ; attempt++ {
		if err := lim.wait(ctx); err != nil {
			return response{}, ctxErr(err)
		}
		body, verdict := c.try(ctx, rawURL)
		if verdict.err == nil {
			c.cache.put(rawURL, body)
			return response{Body: body, Plane: plane.Name}, nil
		}
		last = verdict.err

		if verdict.policy == nil || attempt >= verdict.policy.attempts {
			if verdict.policy != nil {
				c.logf(1, "giving up on %s after %d attempts", rawURL, attempt+1)
			}
			return response{}, last
		}
		wait := verdict.policy.backoff(attempt + 1)
		if verdict.holdPlane {
			// A 429 is aimed at the client, not at the request, so the whole
			// plane waits and concurrent reads do not each trip it again.
			lim.hold(wait)
			c.logf(0, "arxiv is rate limiting the %s plane, waiting %s before retry %d of %d",
				plane.Name, wait, attempt+1, verdict.policy.attempts)
		} else {
			c.logf(1, "retry %d of %d for %s in %s: %v",
				attempt+1, verdict.policy.attempts, rawURL, wait, last)
			if err := c.sleep(ctx, wait); err != nil {
				return response{}, ctxErr(err)
			}
		}
	}
}

// planeFor resolves a host to its plane and that plane's limiter.
func (c *Client) planeFor(host string) (Plane, *limiter, error) {
	plane, ok := planeIn(c.planes, host)
	if !ok {
		// Not a user error and not arXiv's fault: the tool built a URL for a
		// host it has no pace for, which is a bug in the tool.
		return Plane{}, nil, errs.New(errs.KindGeneric,
			"internal error: %q is not in the plane table, so there is no pace for it", host)
	}
	lim := c.limiters[plane.Name]
	if lim == nil {
		return Plane{}, nil, errs.New(errs.KindGeneric,
			"internal error: the %s plane has no limiter", plane.Name)
	}
	return plane, lim, nil
}

// verdict is what one attempt decided: the error, whether to retry it and how,
// and whether the whole plane should stand down.
type verdict struct {
	err       error
	policy    *retryPolicy
	holdPlane bool
}

// try makes one request and classifies the outcome against the retry table.
func (c *Client) try(ctx context.Context, rawURL string) ([]byte, verdict) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, verdict{err: errs.Wrap(errs.KindGeneric, err, "build request")}
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, verdict{err: ctxErr(ctx.Err())}
		}
		return nil, verdict{
			err:    errs.Wrap(errs.KindNetwork, err, "get %s", rawURL),
			policy: &networkRetry,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if readErr != nil {
		return nil, verdict{
			err:    errs.Wrap(errs.KindNetwork, readErr, "read %s", rawURL),
			policy: &networkRetry,
		}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, verdict{}

	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, verdict{
			err: errs.RateLimited("arxiv returned 429 for %s: %s",
				rawURL, strings.TrimSpace(string(body))),
			policy:    &rateRetry,
			holdPlane: true,
		}

	case resp.StatusCode == http.StatusNotFound:
		return nil, verdict{err: errs.NotFound("arxiv has nothing at %s", rawURL)}

	case resp.StatusCode == http.StatusBadRequest:
		// arXiv answers a malformed query with a well-formed Atom feed whose
		// one entry is the error, and answers an over-long id_list with an
		// HTML page instead. Both are the caller's fault, neither is worth a
		// retry, and the message differs so the user can tell them apart.
		if apiErr := parseAPIError(body); apiErr != nil {
			return nil, verdict{err: errs.Wrap(errs.KindUsage, apiErr, "arxiv rejected the query")}
		}
		return nil, verdict{err: errs.Usage(
			"arxiv returned 400 for %s with an HTML body, which usually means the id list was too long", rawURL)}

	case resp.StatusCode == http.StatusServiceUnavailable:
		return nil, verdict{
			err:    errs.New(errs.KindGeneric, "arxiv returned 503 for %s", rawURL),
			policy: &serverRetry,
		}

	case resp.StatusCode >= 500:
		// A 500 carrying an Atom error entry is arXiv telling us the request
		// was wrong, so retrying it would just ask the same wrong question
		// five more times.
		if apiErr := parseAPIError(body); apiErr != nil {
			return nil, verdict{err: errs.Wrap(errs.KindGeneric, apiErr,
				"arxiv returned %d and an error entry", resp.StatusCode)}
		}
		return nil, verdict{
			err:    errs.New(errs.KindGeneric, "arxiv returned %d for %s", resp.StatusCode, rawURL),
			policy: &serverRetry,
		}
	}
	return nil, verdict{err: errs.New(errs.KindGeneric,
		"arxiv returned %d for %s", resp.StatusCode, rawURL)}
}

// ctxErr turns a cancelled or timed out context into a classified error, so a
// ctrl-c and a timeout do not both come out as a generic failure.
func ctxErr(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errs.Wrap(errs.KindNetwork, err, "timed out")
	case errors.Is(err, context.Canceled):
		return errs.Wrap(errs.KindGeneric, err, "cancelled")
	}
	return err
}

// notice writes a message whatever the verbosity is.
//
// It is for the handful of things a person needs to be told without asking,
// which so far is one: that the read they just started will take minutes.
// Everything else goes through logf and waits for -v.
func (c *Client) notice(format string, args ...any) {
	if c.log == nil {
		return
	}
	fmt.Fprintf(c.log, format+"\n", args...)
}

// logf writes a message when the verbosity is at least level.
func (c *Client) logf(level int, format string, args ...any) {
	if c.verbose < level || c.log == nil {
		return
	}
	fmt.Fprintf(c.log, format+"\n", args...)
}
