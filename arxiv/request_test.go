package arxiv

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit/errs"
)

// TestWindowGuard is the reason the guard lives in Values rather than at the
// call sites: a request this tool knows arXiv will reject with a 500 should
// never leave the process.
//
// The numbers come from arXiv, measured 2026-08-13 on cat:cs.CL. start=9999
// with max_results=1 returns 200 and one entry; max_results=2 returns 500.
func TestWindowGuard(t *testing.T) {
	q := Term(FieldCategory, "cs.CL")
	cases := []struct {
		start, max int
		ok         bool
	}{
		{0, 100, true},
		{9900, 100, true},
		{9999, 1, true},
		{9999, 2, false},
		{9990, 10, true},
		{9990, 11, false},
		{0, 10001, false},
		{10000, 1, false},
	}
	for _, tc := range cases {
		_, err := Request{Query: q, Start: tc.start, Max: tc.max}.Values()
		if tc.ok && err != nil {
			t.Errorf("start=%d max=%d was refused: %v", tc.start, tc.max, err)
		}
		if !tc.ok {
			if err == nil {
				t.Errorf("start=%d max=%d was allowed through", tc.start, tc.max)
				continue
			}
			if !strings.Contains(err.Error(), "10000") {
				t.Errorf("error %q does not name the window", err)
			}
		}
	}
}

func TestRequestNeedsSomethingToAskFor(t *testing.T) {
	_, err := Request{}.Values()
	if err == nil {
		t.Fatal("a request with no query and no ids was allowed")
	}
	if errs.KindOf(err) != errs.KindUsage {
		t.Errorf("kind = %v, want usage", errs.KindOf(err))
	}
}

func TestRequestRejectsABadSort(t *testing.T) {
	_, err := Request{Query: Term(FieldAll, "x"), Sort: "nope"}.Values()
	if err == nil {
		t.Fatal("a sort arXiv would reject was allowed through")
	}
	if errs.KindOf(err) != errs.KindUsage {
		t.Errorf("kind = %v, want usage", errs.KindOf(err))
	}
	_, err = Request{Query: Term(FieldAll, "x"), Order: "sideways"}.Values()
	if err == nil {
		t.Fatal("a direction arXiv would reject was allowed through")
	}
}

func TestRequestDefaults(t *testing.T) {
	v, err := Request{Query: Term(FieldAll, "x")}.Values()
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get("max_results"); got != "100" {
		t.Errorf("max_results = %q, want the page size 100", got)
	}
	if got := v.Get("sortBy"); got != string(SortRelevance) {
		t.Errorf("sortBy = %q, want relevance", got)
	}
	if got := v.Get("sortOrder"); got != string(Descending) {
		t.Errorf("sortOrder = %q, want descending", got)
	}
	// start is left off when it is zero, so the common URL is the short one and
	// two calls for the same thing share a cache key.
	if _, ok := v["start"]; ok {
		t.Errorf("start=0 was sent: %v", v)
	}
}

func TestRequestIDList(t *testing.T) {
	v, err := Request{IDs: []string{"1706.03762", "hep-th/9711200"}, Max: 2}.Values()
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get("id_list"); got != "1706.03762,hep-th/9711200" {
		t.Errorf("id_list = %q", got)
	}
	if _, ok := v["search_query"]; ok {
		t.Error("an id-only request sent an empty search_query")
	}
	// Over the batch ceiling is a usage error rather than a 400 from arXiv with
	// an HTML body nobody can read.
	ids := make([]string, MaxIDsPerRequest+1)
	for i := range ids {
		ids[i] = "1706.03762"
	}
	if _, err := (Request{IDs: ids}).Values(); err == nil {
		t.Error("a batch over the ceiling was allowed through")
	}
}

// TestCountRequest pins the one that is not obvious. max_results=0 answers 500
// with an internal error entry, measured 2026-08-13, so a count asks for one
// result and reads totalResults off the feed.
func TestCountRequest(t *testing.T) {
	v, err := CountRequest(Term(FieldCategory, "cs.CL")).Values()
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get("max_results"); got != "1" {
		t.Errorf("max_results = %q, want 1 because 0 is a server error", got)
	}
}

// TestBatchIDs checks both ceilings. The count ceiling is the obvious one; the
// length ceiling is the real one, because arXiv's limit is on the request line.
func TestBatchIDs(t *testing.T) {
	if got := BatchIDs(nil); got != nil {
		t.Errorf("BatchIDs(nil) = %v, want nothing", got)
	}

	ids := make([]string, 500)
	for i := range ids {
		ids[i] = "1706.03762"
	}
	batches := BatchIDs(ids)
	seen := 0
	for i, b := range batches {
		if len(b) > MaxIDsPerRequest {
			t.Errorf("batch %d has %d ids, over the %d ceiling", i, len(b), MaxIDsPerRequest)
		}
		if len(b) == 0 {
			t.Errorf("batch %d is empty", i)
		}
		seen += len(b)
	}
	if seen != len(ids) {
		t.Errorf("batching returned %d ids, want %d", seen, len(ids))
	}

	// Old-style ids carry a slash, which encodes to three characters, so the
	// same count of them makes a much longer request line.
	old := make([]string, 300)
	for i := range old {
		old[i] = "cond-mat/9910001"
	}
	for i, b := range BatchIDs(old) {
		u, err := (Request{IDs: b, Max: len(b)}).URL()
		if err != nil {
			t.Fatalf("batch %d does not build: %v", i, err)
		}
		if len(u) > maxURLLen+len(apiBase) {
			t.Errorf("batch %d builds a %d character URL", i, len(u))
		}
	}
}

// TestOnlyOnePlaceBuildsTheURL checks the URL is the base plus one encoded query
// string, with no hand-assembled parameters anywhere in it.
func TestOnlyOnePlaceBuildsTheURL(t *testing.T) {
	u, err := Request{Query: Term(FieldAll, "quantum computing"), Max: 5}.URL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, apiBase+"?") {
		t.Fatalf("url does not start at the API base: %s", u)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("the url this package builds does not parse: %v", err)
	}
	v, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		t.Fatalf("the query string this package builds does not parse: %v", err)
	}
	// Round-tripping gets the spaces and the colon back exactly as they went in,
	// which is the whole point of escaping once.
	if got := v.Get("search_query"); got != `all:"quantum computing"` {
		t.Errorf("round-tripped query = %q", got)
	}
}
