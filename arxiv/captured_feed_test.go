package arxiv

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captured_feed_test.go runs the two feeds that are easy to confuse through the
// client, from bytes arXiv served rather than from a sample written here.
//
// They look almost the same. Both are a well-formed Atom feed, both come back
// with a totalResults element, and one of them has an entry in it. The
// difference is that one is arXiv answering the question and finding nothing,
// and the other is arXiv refusing the question, and a tool that reads them the
// same way either invents a paper called Error or reports a bad query as an
// empty result set.

// feedServer serves one saved feed with the status arXiv answered with.
func feedServer(t *testing.T, name string, status int) *httptest.Server {
	t.Helper()
	body := fixture(t, name)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// A well-formed query that matches nothing. arXiv answers 200 with a feed
// carrying totalResults 0 and no entries, which is a result and not a failure:
// the caller gets an empty list and decides what to say about it.
func TestAQueryThatMatchesNothingIsNotAnError(t *testing.T) {
	ts := feedServer(t, "api_zero_results.xml", http.StatusOK)
	c := newTestClient(t, ts)

	feed, err := c.getXML(context.Background(), ts.URL, 0)
	if err != nil {
		t.Fatalf("a feed with no results came back as an error: %v", err)
	}
	if feed.Total != 0 {
		t.Errorf("totalResults = %d, want zero", feed.Total)
	}
	if len(feed.Entries) != 0 {
		t.Errorf("%d entries in a feed with no results", len(feed.Entries))
	}
}

// A malformed request. arXiv answers 400 with a feed holding one entry titled
// Error, whose id carries the fragment naming what was wrong, and the client
// turns that into a typed error rather than into a paper.
func TestTheErrorFeedIsAnError(t *testing.T) {
	ts := feedServer(t, "api_error_start.xml", http.StatusBadRequest)
	c := newTestClient(t, ts)

	_, err := c.getXML(context.Background(), ts.URL, 0)
	if err == nil {
		t.Fatal("a 400 with an error feed came back as a result")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want an *APIError", err)
	}
	if apiErr.Fragment != "start_must_be_an_integer" {
		t.Errorf("fragment = %q, want the one arXiv named", apiErr.Fragment)
	}
	if apiErr.Message != "start must be an integer" {
		t.Errorf("message = %q, and arXiv writes these well enough to quote", apiErr.Message)
	}
	if !apiErr.IsUsage() {
		t.Error("arXiv named the problem, so this is the caller's mistake and exits 2")
	}
}

// The same bytes read as a feed. This is the trap: the error feed parses
// perfectly and its entry has a title, a summary and an author, so a parser
// that maps entries to papers hands back a paper called Error written by
// arXiv api core.
func TestTheErrorFeedWouldOtherwiseBeAPaper(t *testing.T) {
	body := fixture(t, "api_error_start.xml")
	if _, err := paperFromFeed(body, "https://export.arxiv.org/api/query", testTime); err == nil {
		t.Error("the error feed produced a paper")
	}
	apiErr := parseAPIError(body)
	if apiErr == nil {
		t.Fatal("the saved error feed is no longer recognised as an error")
	}
	if apiErr.Message == "" {
		t.Error("the error carries no message")
	}
}
