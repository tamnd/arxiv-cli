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
	"net/url"
	"strings"
	"time"
)

const (
	apiBase = "https://export.arxiv.org/api/query"
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

	params := url.Values{}
	params.Set("search_query", buildQuery(opts.Query, opts.Category))
	params.Set("max_results", fmt.Sprintf("%d", limit))
	params.Set("sortBy", sortParam(opts.Sort))
	params.Set("sortOrder", "descending")

	feed, err := c.getXML(ctx, apiBase+"?"+params.Encode(), TTLSearch)
	if err != nil {
		return nil, err
	}
	return feedToPapers(feed), nil
}

// Paper fetches the single paper with the given id. The id must be canonical:
// no version, no subject class. Returns ErrNotFound for an empty result.
func (c *Client) Paper(ctx context.Context, id string) (Paper, error) {
	params := url.Values{}
	params.Set("id_list", id)
	params.Set("max_results", "1")

	feed, err := c.getXML(ctx, apiBase+"?"+params.Encode(), TTLPaper)
	if err != nil {
		return Paper{}, err
	}
	if len(feed.Entries) == 0 {
		return Paper{}, fmt.Errorf("%s: %w", id, ErrNotFound)
	}
	return entryToPaper(feed.Entries[0]), nil
}

// SearchByAuthor returns up to n papers whose author field matches name,
// newest first.
func (c *Client) SearchByAuthor(ctx context.Context, name string, n int) ([]Paper, error) {
	if n <= 0 {
		n = 10
	}
	params := url.Values{}
	params.Set("search_query", "au:"+authorQuery(name))
	params.Set("max_results", fmt.Sprintf("%d", n))
	params.Set("sortBy", "submittedDate")
	params.Set("sortOrder", "descending")

	feed, err := c.getXML(ctx, apiBase+"?"+params.Encode(), TTLSearch)
	if err != nil {
		return nil, err
	}
	return feedToPapers(feed), nil
}

// ─── internal helpers ─────────────────────────────────────────────────────────

func buildQuery(query, category string) string {
	// Terms are joined with a real space and a real AND. The encoder escapes
	// them exactly once on the way out, which is the whole fix: writing "+AND+"
	// here and then encoding turned the plus signs into %2B and sent arXiv one
	// nonsense term instead of two.
	words := strings.Fields(query)
	q := "all:" + strings.Join(words, " AND ")
	if category != "" {
		q += " AND cat:" + category
	}
	return q
}

func authorQuery(name string) string {
	if strings.ContainsAny(name, " \t") {
		// wrap in quotes for phrase search
		return `"` + strings.TrimSpace(name) + `"`
	}
	return name
}

func sortParam(s string) string {
	switch s {
	case "date":
		return "submittedDate"
	case "updated":
		return "lastUpdatedDate"
	default:
		return "relevance"
	}
}

func feedToPapers(feed *atomFeed) []Paper {
	out := make([]Paper, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		out = append(out, entryToPaper(e))
	}
	return out
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
