// Package arxiv is the library behind the arxiv command: the HTTP client, the
// pacing, the request shaping, and the typed records.
//
// Nothing here needs a credential. arXiv publishes everything this tool reads
// over anonymous HTTP, and the only thing it asks in return is that a client
// keeps to a pace. That pace is the load-bearing part of this package: see
// planes.go, and `arxiv planes` for the numbers with their evidence.
package arxiv

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// DefaultUserAgent identifies the client to arXiv. The real one is stamped with
// the build version by the CLI; this is the fallback for a library caller.
const DefaultUserAgent = "arxiv-cli/dev (+https://github.com/tamnd/arxiv-cli)"

// Host is the arXiv site this client represents.
const Host = "arxiv.org"

// ErrNotFound is returned when a surface has no record for an id.
var ErrNotFound = errors.New("not found")

// Config holds constructor parameters.
type Config struct {
	// UserAgent is sent on every request. arXiv asks that a client identify
	// itself, and a contactable one is far less likely to get blocked.
	UserAgent string
	// Rate is the minimum gap between API plane requests.
	Rate time.Duration
	// HTMLRate is the minimum gap between arxiv.org requests.
	HTMLRate time.Duration
	// Retries overrides the number of network retries. Zero keeps the table.
	Retries int
	// Timeout is the per-request timeout.
	Timeout time.Duration
	// CacheDir is where response bodies are cached. Empty disables the cache.
	CacheDir string
	// NoCache skips both reads and writes.
	NoCache bool
	// Verbose is the -v count: 1 explains retries, 2 explains cache hits.
	Verbose int
	// Log is where those explanations go. Nil means nowhere.
	Log io.Writer
}

// DefaultConfig returns the paces arXiv asks for, which are the paces to use
// unless there is a reason not to.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      APIPlane.Pace,
		HTMLRate:  HTMLPlane.Pace,
		Retries:   networkRetry.attempts,
		Timeout:   30 * time.Second,
	}
}

// Client reads arXiv's public surfaces.
type Client struct {
	httpClient *http.Client
	userAgent  string

	// planes is this client's plane table, and limiters holds one limiter per
	// plane name. Both are per-client rather than global so a test can pace a
	// local server without reaching into package state.
	planes   []Plane
	limiters map[string]*limiter

	cache   *cache
	verbose int
	log     io.Writer

	sleep func(context.Context, time.Duration) error
	// now stamps a record's retrieved_at. It is a field so a golden test can
	// pin a timestamp without pinning the day it ran.
	now func() time.Time
}

// NewClient builds a client from cfg.
//
// It returns an error when a pace is below its plane's floor, because that is a
// flag the user typed and refusing it with an explanation beats quietly
// ignoring it.
func NewClient(cfg Config) (*Client, error) {
	def := DefaultConfig()
	if cfg.UserAgent == "" {
		cfg.UserAgent = def.UserAgent
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}

	// The flag name goes after a word rather than first, because the surface
	// capitalises the first word of an error and "--Html-Rate" is not a flag.
	apiPace, err := APIPlane.Clamp(cfg.Rate)
	if err != nil {
		return nil, fmt.Errorf("the --rate value %w", err)
	}
	htmlPace, err := HTMLPlane.Clamp(cfg.HTMLRate)
	if err != nil {
		return nil, fmt.Errorf("the --html-rate value %w", err)
	}

	c := &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		userAgent:  cfg.UserAgent,
		planes:     Planes,
		limiters: map[string]*limiter{
			APIPlane.Name:  newLimiter(apiPace),
			HTMLPlane.Name: newLimiter(htmlPace),
		},
		cache:   newCache(cfg.CacheDir, cfg.NoCache),
		verbose: cfg.Verbose,
		log:     cfg.Log,
		sleep:   sleepCtx,
		now:     time.Now,
	}
	return c, nil
}

// SearchOptions controls a Search call.
type SearchOptions struct {
	Query    string // free-text query
	Category string // "" = no category filter
	Sort     string // "relevance" | "date" | "updated"
	Limit    int
}

// Search returns up to opts.Limit papers matching the query.
func (c *Client) Search(ctx context.Context, opts SearchOptions) ([]Paper, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	sort, err := ParseSort(opts.Sort)
	if err != nil {
		return nil, errs.Usage("%s", err.Error())
	}
	return c.searchPapers(ctx, Request{
		Query: buildQuery(opts.Query, opts.Category),
		Max:   limit,
		Sort:  sort,
		Order: Descending,
	})
}

// Paper fetches the single paper with the given id at depth quick. PaperAt is
// the one to reach for when the extra surfaces are wanted.
func (c *Client) Paper(ctx context.Context, id string) (Paper, error) {
	return c.PaperAt(ctx, id, PaperOptions{Depth: DepthQuick})
}

// Papers fetches many papers in one request per batch, which is by far the
// cheapest way to hydrate a list of known ids.
func (c *Client) Papers(ctx context.Context, ids []string) ([]Paper, error) {
	return c.PapersAt(ctx, ids, PaperOptions{Depth: DepthQuick})
}

// SearchByAuthor returns up to n papers whose author field matches name,
// newest first.
func (c *Client) SearchByAuthor(ctx context.Context, name string, n int) ([]Paper, error) {
	if n <= 0 {
		n = 10
	}
	return c.searchPapers(ctx, Request{
		Query: Term(FieldAuthor, name),
		Max:   n,
		Sort:  SortSubmitted,
		Order: Descending,
	})
}

// searchPapers runs one search request and maps the feed to records.
//
// The request URL goes onto every record it produced, because a search result
// that cannot be reproduced by hand is a result you have to take on trust.
func (c *Client) searchPapers(ctx context.Context, req Request) ([]Paper, error) {
	u, err := req.URL()
	if err != nil {
		return nil, err
	}
	feed, err := c.getXML(ctx, u, TTLSearch)
	if err != nil {
		return nil, err
	}
	at := c.now()
	out := make([]Paper, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		out = append(out, entryToPaper(e, u, at))
	}
	return out, nil
}

// Count returns how many results a query has, which is the number the slicer
// makes all of its decisions on.
func (c *Client) Count(ctx context.Context, q Query) (int, error) {
	feed, err := c.Do(ctx, CountRequest(q), TTLSearch)
	if err != nil {
		return 0, err
	}
	return feed.Total, nil
}

// Do sends one request and returns the decoded feed.
func (c *Client) Do(ctx context.Context, req Request, ttl time.Duration) (*atomFeed, error) {
	u, err := req.URL()
	if err != nil {
		return nil, err
	}
	c.logf(1, "GET %s", u)
	return c.getXML(ctx, u, ttl)
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// buildQuery turns the free-text form of a search into a query.
//
// Terms are joined with a real space and a real AND, and the encoder escapes
// them exactly once on the way out. That is the whole of the fix: writing
// "+AND+" here and then encoding turned the plus signs into %2B and asked arXiv
// one nonsense question instead of two real ones.
func buildQuery(query, category string) Query {
	words := strings.Fields(query)
	terms := make([]Query, 0, len(words)+1)
	for _, w := range words {
		terms = append(terms, Term(FieldAll, w))
	}
	if category != "" {
		terms = append(terms, Term(FieldCategory, category))
	}
	return And(terms...)
}

// getXML fetches a URL and decodes the Atom feed, including the error feed
// arXiv answers a bad query with.
func (c *Client) getXML(ctx context.Context, rawURL string, ttl time.Duration) (*atomFeed, error) {
	resp, err := c.fetch(ctx, rawURL, ttl)
	if err != nil {
		return nil, err
	}
	var feed atomFeed
	if err := xml.Unmarshal(resp.Body, &feed); err != nil {
		return nil, fmt.Errorf("decode arxiv response: %w", err)
	}
	// arXiv answers some bad requests with HTTP 200 and an error feed, so the
	// body has to be checked even on success.
	if apiErr := errorEntry(&feed); apiErr != nil {
		return nil, apiErr
	}
	return &feed, nil
}
