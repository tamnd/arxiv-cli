// Package arxiv is the library behind the arxiv command: the HTTP client,
// request shaping, and the typed data models for the arXiv Open Access API.
//
// The API endpoint is https://export.arxiv.org/api/query. It returns Atom XML
// and requires no key. The arXiv ToS recommends no more than 3 requests per
// second; the default rate is 400 ms between calls.
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
	"sync"
	"time"
)

const (
	apiBase = "https://export.arxiv.org/api/query"
)

// DefaultUserAgent identifies the client to arXiv.
const DefaultUserAgent = "arxiv/dev (+https://github.com/tamnd/arxiv-cli)"

// Host is the arXiv site this client represents.
const Host = "arxiv.org"

// ErrNotFound is returned when the API returns an empty entry list for a given id.
var ErrNotFound = errors.New("not found")

// Config holds constructor parameters.
type Config struct {
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      400 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Client talks to the arXiv Open Access API.
type Client struct {
	httpClient *http.Client
	userAgent  string
	rate       time.Duration
	retries    int
	mu         sync.Mutex
	last       time.Time
}

// NewClient returns a Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		userAgent:  cfg.UserAgent,
		rate:       cfg.Rate,
		retries:    cfg.Retries,
	}
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
	q := buildQuery(opts.Query, opts.Category)
	sortBy := sortParam(opts.Sort)
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	params := url.Values{}
	params.Set("search_query", q)
	params.Set("max_results", fmt.Sprintf("%d", limit))
	params.Set("sortBy", sortBy)
	params.Set("sortOrder", "descending")

	rawURL := apiBase + "?" + params.Encode()
	feed, err := c.getXML(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return feedToPapers(feed), nil
}

// Paper fetches the single paper with the given id (version suffix stripped).
// Returns ErrNotFound if the id produces an empty result.
func (c *Client) Paper(ctx context.Context, id string) (Paper, error) {
	params := url.Values{}
	params.Set("id_list", id)
	params.Set("max_results", "1")

	rawURL := apiBase + "?" + params.Encode()
	feed, err := c.getXML(ctx, rawURL)
	if err != nil {
		return Paper{}, err
	}
	if len(feed.Entries) == 0 {
		return Paper{}, ErrNotFound
	}
	return entryToPaper(feed.Entries[0]), nil
}

// SearchByAuthor returns up to n papers authored by name, sorted newest first.
func (c *Client) SearchByAuthor(ctx context.Context, name string, n int) ([]Paper, error) {
	if n <= 0 {
		n = 10
	}
	// quote the name if it contains spaces so the API treats it as a phrase
	q := "au:" + authorQuery(name)

	params := url.Values{}
	params.Set("search_query", q)
	params.Set("max_results", fmt.Sprintf("%d", n))
	params.Set("sortBy", "submittedDate")
	params.Set("sortOrder", "descending")

	rawURL := apiBase + "?" + params.Encode()
	feed, err := c.getXML(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return feedToPapers(feed), nil
}

// ─── internal helpers ─────────────────────────────────────────────────────────

func buildQuery(query, category string) string {
	// join words with +AND+ for AND semantics
	words := strings.Fields(query)
	q := "all:" + strings.Join(words, "+AND+")
	if category != "" {
		q += "+AND+cat:" + category
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

// getXML fetches a URL and XML-decodes the response into an atomFeed.
func (c *Client) getXML(ctx context.Context, rawURL string) (*atomFeed, error) {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("decode arxiv response: %w", err)
	}
	// check for API-level error entry
	if len(feed.Entries) == 1 {
		title := strings.TrimSpace(feed.Entries[0].Title)
		if strings.HasPrefix(title, "Error ") {
			return nil, fmt.Errorf("arxiv api error: %s", cleanText(feed.Entries[0].Summary))
		}
	}
	return &feed, nil
}

// get fetches a URL with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/atom+xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate <= 0 {
		return
	}
	if wait := c.rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
