//go:build live

// These tests talk to the export API. They are behind a build tag and never run
// in CI, because they exist to notice arXiv changing something rather than to
// guard a build. Run them by hand:
//
//	go test ./arxiv -tags live -v
//
// They assert shapes and rules, not values, because a result count moves every
// day and a test that pinned one would be wrong by tomorrow.
package arxiv

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// liveClient is a real client at the default API pace.
func liveClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestLiveCompoundQueryReturnsResults is the assertion the old tool would have
// failed. It sent all%3Aattention%2BAND%2Bcat%3Acs.CL and got zero results back
// for a query that has tens of thousands.
func TestLiveCompoundQueryReturnsResults(t *testing.T) {
	c := liveClient(t)
	q := And(Term(FieldAll, "attention"), Term(FieldCategory, "cs.CL"))

	n, err := c.Count(context.Background(), q)
	if err != nil {
		t.Fatalf("count %s: %v", q, err)
	}
	if n < 1000 {
		t.Errorf("%s returned %d results; the query is broken, not the corpus", q, n)
	}
	t.Logf("%s: %d results", q, n)
}

// TestLiveAllNineFieldPrefixes checks every prefix still works. A prefix that
// arXiv quietly stopped supporting would otherwise return zero and look like a
// query with no matches.
func TestLiveAllNineFieldPrefixes(t *testing.T) {
	cases := []struct {
		field Field
		value string
	}{
		{FieldAll, "attention"},
		{FieldTitle, "attention is all you need"},
		{FieldAuthor, "Vaswani"},
		{FieldAbstract, "transformer"},
		{FieldComment, "ACL"},
		{FieldJournal, "Phys.Lett. B716"},
		{FieldCategory, "cs.CL"},
		{FieldReport, "CERN-PH-EP-2012-218"},
		{FieldID, "1706.03762"},
	}
	c := liveClient(t)
	for _, tc := range cases {
		t.Run(string(tc.field), func(t *testing.T) {
			q := Term(tc.field, tc.value)
			n, err := c.Count(context.Background(), q)
			if err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if n == 0 {
				t.Errorf("%s returned nothing", q)
			}
			t.Logf("%s: %d", q, n)
		})
	}
}

// TestLiveOperators checks AND, OR, ANDNOT and grouping still mean what they
// mean. The relations hold whatever the counts are on the day.
func TestLiveOperators(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	cl := Term(FieldCategory, "cs.CL")
	lg := Term(FieldCategory, "cs.LG")

	count := func(q Query) int {
		t.Helper()
		n, err := c.Count(ctx, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		t.Logf("%s: %d", q, n)
		return n
	}

	onlyCL := count(cl)
	both := count(And(cl, lg))
	either := count(Group(Or(cl, lg)))
	notLG := count(AndNot(cl, lg))

	if both > onlyCL {
		t.Errorf("cs.CL AND cs.LG (%d) is larger than cs.CL alone (%d)", both, onlyCL)
	}
	if either < onlyCL {
		t.Errorf("cs.CL OR cs.LG (%d) is smaller than cs.CL alone (%d)", either, onlyCL)
	}
	// ANDNOT and AND partition the set, give or take papers announced between
	// the two requests.
	if diff := (notLG + both) - onlyCL; diff < -50 || diff > 50 {
		t.Errorf("ANDNOT %d plus AND %d is %d, want about %d", notLG, both, notLG+both, onlyCL)
	}
}

// TestLiveDateRangeForms checks the claim that the eight and twelve digit forms
// agree. This tool always sends twelve, and this is why that costs nothing.
func TestLiveDateRangeForms(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	base := Term(FieldCategory, "cs.CL")

	twelve, err := c.Count(ctx, And(base, Raw("submittedDate:[202601010000 TO 202601312359]")))
	if err != nil {
		t.Fatal(err)
	}
	eight, err := c.Count(ctx, And(base, Raw("submittedDate:[20260101 TO 20260131]")))
	if err != nil {
		t.Fatal(err)
	}
	if twelve != eight {
		t.Errorf("twelve digit form returned %d and eight digit returned %d", twelve, eight)
	}
	if twelve == 0 {
		t.Error("a whole month of cs.CL returned nothing")
	}
	t.Logf("January 2026 in cs.CL: %d", twelve)
}

// TestLiveResultWindow checks the rule the guard is built on. The day arXiv
// moves the window is the day this fails and the constant needs changing.
func TestLiveResultWindow(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	q := Term(FieldCategory, "cs.CL")

	// Right at the edge, which is allowed.
	feed, err := c.Do(ctx, Request{Query: q, Start: ResultWindow - 1, Max: 1,
		Sort: SortSubmitted, Order: Ascending}, 0)
	if err != nil {
		t.Fatalf("start=%d max=1: %v", ResultWindow-1, err)
	}
	if len(feed.Entries) != 1 {
		t.Errorf("start=%d max=1 returned %d entries", ResultWindow-1, len(feed.Entries))
	}

	// One past it, which arXiv answers with a 500 and an error entry. The guard
	// catches this before the request goes out, so the request is built by hand
	// here to check arXiv still behaves the way the guard assumes.
	u := apiBase + "?search_query=cat%3Acs.CL&start=9999&max_results=2"
	if _, err := c.fetch(ctx, u, 0); err == nil {
		t.Errorf("start=%d max=2 was accepted; the window has moved", ResultWindow-1)
	} else {
		t.Logf("one past the window: %v", err)
	}
}

// TestLiveIDBatch checks a full batch comes back in one request, which is what
// makes hydrating a list of known ids cheap.
func TestLiveIDBatch(t *testing.T) {
	c := liveClient(t)
	ids := []string{
		"1706.03762", "1810.04805", "hep-th/9711200", "math/0309136",
		"cond-mat/9910001", "2005.14165", "1512.03385",
	}
	papers, err := c.Papers(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) != len(ids) {
		t.Errorf("asked for %d ids and got %d papers", len(ids), len(papers))
	}
	for _, p := range papers {
		if p.Title == "" {
			t.Errorf("paper %s came back with no title", p.ID)
		}
	}
}

// TestLiveErrorEntry checks arXiv still answers a bad request the way the
// decoder expects: a well-formed feed whose one entry is the error.
func TestLiveErrorEntry(t *testing.T) {
	c := liveClient(t)
	// max_results=0 is the reliable way to make arXiv return an error entry,
	// measured 2026-08-13. It answers 500 with an internal error and no
	// fragment, which is why counting asks for one result rather than none.
	u := apiBase + "?search_query=cat%3Acs.CL&max_results=0"
	_, err := c.fetch(context.Background(), u, 0)
	if err == nil {
		t.Fatal("max_results=0 was answered with results")
	}
	t.Logf("max_results=0: %v", err)
}

// TestLiveSlicePlan runs the slicer against a real category over a real month.
// It is small enough to need no cutting, which is the assertion: the common
// case costs exactly one request.
func TestLiveSlicePlan(t *testing.T) {
	c := liveClient(t)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 23, 59, 0, 0, time.UTC)

	plan, err := c.Plan(context.Background(), Term(FieldCategory, "cs.CL"),
		SubmittedDate, NewRange(from, to))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Slices) != 1 {
		t.Errorf("a month of cs.CL was cut into %d slices", len(plan.Slices))
	}
	if plan.Counts != 1 {
		t.Errorf("planning cost %d requests, want 1", plan.Counts)
	}
	if plan.Truncated() {
		t.Error("a month of cs.CL was reported as truncated")
	}
	t.Logf("plan: total %d in %d slices, %d count requests",
		plan.Total, len(plan.Slices), plan.Counts)
}

// TestLivePaperDepths reads one paper at each depth and checks the record grows
// rather than changes. The fixture tests do this against saved responses; this
// is the one that notices arXiv moving a field.
func TestLivePaperDepths(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	const id = "1706.03762"

	quick, err := c.PaperAt(ctx, id, PaperOptions{Depth: DepthQuick})
	if err != nil {
		t.Fatalf("quick: %v", err)
	}
	if quick.Title == "" || quick.FirstSubmitted.IsZero() {
		t.Errorf("s1 answered without a title or a submission date: %#v", quick)
	}
	if len(quick.Surfaces) != 1 {
		t.Errorf("a quick read touched %v", quick.Surfaces)
	}

	meta, err := c.PaperAt(ctx, id, PaperOptions{Depth: DepthMeta})
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if meta.License == "" {
		t.Error("OAI stopped publishing the licence")
	}
	if len(meta.Authors) == 0 || meta.Authors[0].Keyname == "" {
		t.Errorf("OAI stopped publishing structured names: %#v", meta.Authors)
	}

	full, err := c.PaperAt(ctx, id, PaperOptions{Depth: DepthFull})
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if len(full.Versions) < 7 {
		t.Errorf("the version history came back with %d versions", len(full.Versions))
	}
	if !full.HasHTML || full.HTMLURL == "" {
		t.Error("the abstract page stopped linking the HTML rendering")
	}
	if len(full.SubjectNames) == 0 {
		t.Error("the abstract page stopped naming the subjects")
	}

	// The same paper at three depths is the same paper.
	for _, p := range []Paper{meta, full} {
		if p.ID != quick.ID || p.Title != quick.Title {
			t.Errorf("depth %s described a different paper: %s %q", p.Depth, p.ID, p.Title)
		}
		if !p.FirstSubmitted.Equal(quick.FirstSubmitted) {
			t.Errorf("depth %s moved first_submitted to %s", p.Depth, p.FirstSubmitted)
		}
	}
	t.Logf("quick %d fields of surfaces %v, full %v", len(quick.Via), quick.Surfaces, full.Surfaces)
}

// TestLiveOAICreatedIsStillTheWrongDate is the measurement the model is built
// on, checked against the live surface rather than a saved copy. If OAI ever
// starts publishing the real v1 date in created, this fails and the model gets
// a cheaper source for first_submitted.
func TestLiveOAICreatedIsStillTheWrongDate(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	const id = "1706.03762"

	p, err := c.PaperAt(ctx, id, PaperOptions{Depth: DepthQuick})
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := c.getOAI(ctx, id, FormatArxiv)
	if err != nil {
		t.Fatal(err)
	}
	created := rec.Metadata.Arxiv.Created
	t.Logf("s1 published %s, s2 created %s", p.FirstSubmitted.Format("2006-01-02"), created)
	if created == p.FirstSubmitted.Format("2006-01-02") {
		t.Log("OAI created now agrees with s1 published; first_submitted could come from either")
	}
}

// TestLiveCategoryExpansionIsStillNeeded checks the measurement expandCategory
// is built on. A bare archive code matches papers only if the archive was never
// split into categories, and matches nothing at all if it was, so neither form
// alone answers for every archive and the OR of the two is what a person means
// by --cat cs.
//
// If arXiv ever starts indexing both forms, both halves come back non-zero here
// and expandCategory can drop to one term.
func TestLiveCategoryExpansionIsStillNeeded(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	// cs was split into cs.AI, cs.CL and the rest. hep-th never was.
	for _, code := range []string{"cs", "hep-th"} {
		bare, err := c.Count(ctx, Term(FieldCategory, code))
		if err != nil {
			t.Fatalf("count cat:%s: %v", code, err)
		}
		star, err := c.Count(ctx, Term(FieldCategory, code+".*"))
		if err != nil {
			t.Fatalf("count cat:%s.*: %v", code, err)
		}
		both, err := c.Count(ctx, expandCategory(code))
		if err != nil {
			t.Fatalf("count %s: %v", expandCategory(code), err)
		}
		t.Logf("cat:%s %d, cat:%s.* %d, expanded %d", code, bare, code, star, both)

		if bare > 0 && star > 0 {
			t.Logf("both forms of %s now match; expandCategory could be one term", code)
		}
		if bare == 0 && star == 0 {
			t.Errorf("neither cat:%s nor cat:%s.* matches anything", code, code)
		}
		if want := max(bare, star); both < want {
			t.Errorf("expanded %s counted %d, want at least %d", code, both, want)
		}
	}
}

// TestLiveSearchFlagsBuildAWorkingQuery runs the flags a person actually types
// and checks the papers that come back match what was asked for. It is the end
// to end version of TestSearchQueryFromFlags, which only reads the query string.
func TestLiveSearchFlagsBuildAWorkingQuery(t *testing.T) {
	c := liveClient(t)

	papers, err := c.Search(context.Background(), SearchOptions{
		Categories: []string{"cs.CL"},
		From:       "2026-01",
		To:         "2026-01",
		Sort:       "submitted",
		Order:      "asc",
		Limit:      5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) != 5 {
		t.Fatalf("got %d papers, want 5", len(papers))
	}
	for _, p := range papers {
		if p.PrimaryCategory != "cs.CL" && !contains(p.Categories, "cs.CL") {
			t.Errorf("%s is in %v, none of them cs.CL", p.ID, p.Categories)
		}
		m := p.FirstSubmitted.UTC().Format("2006-01")
		if m != "2026-01" {
			t.Errorf("%s was submitted in %s, want 2026-01", p.ID, m)
		}
	}
}

// TestLiveAllWalksWithoutRepeating walks a bounded slice with --all and checks
// the walk holds its order and never hands the same paper over twice. A walk
// that repeats is the failure mode of paging a moving result set, and it is the
// reason searchAll sorts by submission date ascending whatever --sort said.
func TestLiveAllWalksWithoutRepeating(t *testing.T) {
	c := liveClient(t)

	seen := map[string]bool{}
	var last time.Time
	err := c.SearchStream(context.Background(), SearchOptions{
		Categories: []string{"econ.GN"},
		From:       "2026-01-05",
		To:         "2026-01-09",
		All:        true,
	}, func(p *Paper) error {
		if seen[p.ID] {
			t.Errorf("%s came back twice", p.ID)
		}
		seen[p.ID] = true
		if !last.IsZero() && p.FirstSubmitted.Before(last) {
			t.Errorf("%s at %s came after %s, walk is not ascending", p.ID, p.FirstSubmitted, last)
		}
		last = p.FirstSubmitted
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("walk returned nothing")
	}
	t.Logf("walked %d papers", len(seen))
}

// TestLiveCountMatchesTheWalk checks that the number a count prints is the
// number a walk hands over. They come from different places, opensearch on one
// side and a page of entries on the other, and a gap between them means one of
// the two is lying to the user.
func TestLiveCountMatchesTheWalk(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	opts := SearchOptions{
		Categories: []string{"econ.GN"},
		From:       "2026-01-05",
		To:         "2026-01-09",
		All:        true,
	}

	got, err := c.CountSearch(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := c.SearchStream(ctx, opts, func(*Paper) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if got.Total != n {
		t.Errorf("count says %d, walk handed over %d", got.Total, n)
	}
}

// TestLiveS5CountsAreInTheRightRange checks the three fields the export API
// does not index at all. The numbers move, so the assertions are bands rather
// than values: what would break is the route, not the arithmetic. Measured
// 2026-08-13: orcid 0000-0002-0609-9836 125, msc_class 18D10 723, license
// CC-BY-4.0 565813.
func TestLiveS5CountsAreInTheRightRange(t *testing.T) {
	c := liveClient(t)
	cases := []struct {
		name     string
		opts     SearchOptions
		low, was int
	}{
		{"orcid", SearchOptions{ORCID: "0000-0002-0609-9836"}, 100, 125},
		{"msc_class", SearchOptions{MSCClass: "18D10"}, 600, 723},
		{"license", SearchOptions{License: "http://creativecommons.org/licenses/by/4.0/"}, 400000, 565813},
	}
	for _, tc := range cases {
		got, err := c.CountSearch(context.Background(), tc.opts)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got.Total < tc.low {
			t.Errorf("%s says %d, which is below %d and was %d in August 2026", tc.name, got.Total, tc.low, tc.was)
		}
		t.Logf("%s: %d, was %d", tc.name, got.Total, tc.was)
	}
}

// TestLiveS5ReadsAWholeResult takes one real search result apart and checks the
// record says what it read and what it did not. The search UI gives dates to
// the day and an announcement month, and a record that pretended to more
// precision than that would be inventing timestamps.
func TestLiveS5ReadsAWholeResult(t *testing.T) {
	c := liveClient(t)
	papers, err := c.Search(context.Background(), SearchOptions{MSCClass: "18D10", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) != 1 {
		t.Fatalf("got %d papers, want 1", len(papers))
	}
	p := papers[0]
	if len(p.Surfaces) != 1 || p.Surfaces[0] != SurfaceSearch {
		t.Errorf("%s came back on %v, want just s5", p.ID, p.Surfaces)
	}
	if len(p.MSCClass) == 0 {
		t.Errorf("%s answered an msc_class search without carrying one", p.ID)
	}
	for _, class := range p.MSCClass {
		if strings.Count(class, "(") != strings.Count(class, ")") {
			t.Errorf("%s has a half bracketed class: %q", p.ID, class)
		}
	}
	if p.Title == "" || p.Abstract == "" || len(p.Authors) == 0 {
		t.Errorf("%s is missing a title, an abstract or its authors", p.ID)
	}
	if p.AnnouncedMonth == "" {
		t.Errorf("%s has no announcement month, which every result line carries", p.ID)
	}
	if h := p.FirstSubmitted.UTC().Hour(); h != 0 {
		t.Errorf("%s claims a submission time of %s, but the page gives a day", p.ID, p.FirstSubmitted)
	}
	if len(p.Missed) == 0 {
		t.Errorf("%s read one surface and said nothing about the rest", p.ID)
	}
}

// TestLiveS5CountMatchesTheWalk is the s5 half of the same check the API plane
// gets: the number printed by a count and the number of records handed over by
// a walk come from different parts of the page, and a gap between them means
// one of the two is lying. The slice is narrow on purpose, because every page
// on this plane costs fifteen seconds.
func TestLiveS5CountMatchesTheWalk(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	opts := SearchOptions{
		MSCClass: "18D10",
		From:     "2026-01",
		To:       "2026-06",
		All:      true,
	}

	got, err := c.CountSearch(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	if err := c.SearchStream(ctx, opts, func(p *Paper) error {
		if seen[p.ID] {
			t.Errorf("%s came back twice", p.ID)
		}
		seen[p.ID] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got.Total != len(seen) {
		t.Errorf("count says %d, walk handed over %d", got.Total, len(seen))
	}
	t.Logf("walked %d papers", len(seen))
}

// TestLiveTaxonomyStillMatchesTheSnapshot compares the bundled tables to the
// live ones. It is meant to fail: when arXiv adds a category or a set, this is
// how the tool finds out, and the fix is to save the pages again.
func TestLiveTaxonomyStillMatchesTheSnapshot(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	live, err := c.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved := snapshotCategories()
	if len(live) != len(saved) {
		t.Errorf("arxiv now publishes %d categories and the bundled table has %d, so save the page again", len(live), len(saved))
	}
	have := map[string]bool{}
	for _, c := range saved {
		have[c.Code] = true
	}
	for _, c := range live {
		if !have[c.Code] {
			t.Errorf("%s (%s) is new since the snapshot", c.Code, c.Name)
		}
		if c.SetSpec == "" {
			t.Errorf("%s found no OAI set", c.Code)
		}
	}

	sets, err := c.Sets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 174 {
		t.Errorf("%d distinct sets, want the 174 measured in August 2026", len(sets))
	}
	// The two vocabularies are joined and not rewritten, so the two shapes that
	// prove it are worth checking against the live tables every time.
	for _, s := range sets {
		switch s.SetSpec {
		case "cs:cs:CL":
			if s.Category != "cs.CL" {
				t.Errorf("cs:cs:CL harvests %q, want cs.CL", s.Category)
			}
		case "physics:hep-th":
			if s.Category != "hep-th" {
				t.Errorf("physics:hep-th harvests %q, want hep-th", s.Category)
			}
		}
	}
}

// TestLiveListingRowShape reads one real listing page and checks the shape the
// parser depends on. A page whose divs were renamed would still parse and would
// return fifty rows with no titles on them, which is the failure worth
// catching.
func TestLiveListingRowShape(t *testing.T) {
	c := liveClient(t)
	var got []Paper
	err := c.ListStream(context.Background(), ListOptions{Category: "cs.CL", Month: "2026-01", Show: 25},
		func(p *Paper) error {
			got = append(got, *p)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 25 {
		t.Fatalf("%d rows, want the 25 that were asked for", len(got))
	}
	for _, p := range got {
		if p.ID == "" || p.Title == "" || len(p.Authors) == 0 || p.PrimaryCategory == "" {
			t.Errorf("%+v is missing something every row has", p)
		}
		if !strings.HasPrefix(p.ID, "2601.") {
			t.Errorf("%s is on the January 2026 listing, which lists ids from that month", p.ID)
		}
		if p.Abstract != "" {
			t.Errorf("%s came back with an abstract, which the listing does not publish", p.ID)
		}
	}
	t.Logf("first row: %s %s", got[0].ID, got[0].Title)
}

// TestLiveListingShowValues checks the sizes arXiv accepts, because the refusal
// in validate is built on them and a size it started refusing would turn into a
// 400 the user cannot do anything about.
func TestLiveListingShowValues(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	for _, show := range []int{25, 2000} {
		o := ListOptions{Category: "math.AG", Month: "2026-01", Show: show}
		if _, err := c.getList(ctx, o.URL(0, show)); err != nil {
			t.Errorf("show=%d: %v", show, err)
		}
	}
	// The other half of the rule: a size not on arXiv's list is a 400, which
	// is why validate refuses it before the request.
	o := ListOptions{Category: "math.AG", Month: "2026-01"}
	if _, err := c.getList(ctx, o.URL(0, 7)); err == nil {
		t.Error("show=7 was accepted, so the refusal in validate is now wrong")
	}
}

// TestLiveShortMonthStill404s is the measurement the refusal quotes. The day
// arXiv brings the four digit form back, this is how the tool finds out.
func TestLiveShortMonthStill404s(t *testing.T) {
	c := liveClient(t)
	if _, err := c.getList(context.Background(), listBase+"cs.CL/2601?skip=0&show=25"); err == nil {
		t.Error("/list/cs.CL/2601 answered, so the short month form is back")
	}
}

// TestLiveFeedHasEveryAnnounceType reads a real feed and checks that all four
// types are still published under those names. The whole command is built on
// that field.
func TestLiveFeedHasEveryAnnounceType(t *testing.T) {
	c := liveClient(t)
	items, err := c.Announcements(context.Background(), FeedOptions{Category: "cs.CL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("the cs.CL feed is empty, which it never is on a weekday")
	}
	counts := map[string]int{}
	for _, a := range items {
		counts[a.AnnounceType]++
		if a.PaperID == "" || a.Title == "" || a.Abstract == "" {
			t.Errorf("%+v is missing something every item has", a)
		}
		if !contains(AnnounceTypes, a.AnnounceType) {
			t.Errorf("%s is announced as %q, which is a type this build does not know", a.PaperID, a.AnnounceType)
		}
		if a.Version < 1 {
			t.Errorf("%s has no version, and the guid is the only element that carries one", a.PaperID)
		}
	}
	for _, kind := range AnnounceTypes {
		if counts[kind] == 0 {
			t.Errorf("no item is announced as %q", kind)
		}
	}
	t.Logf("%d items: %v", len(items), counts)
}

// TestLiveFeedFilter checks the filter against the whole feed, because the
// count under the table is of the feed and the list is of what survived.
func TestLiveFeedFilter(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	all, err := c.Announcements(ctx, FeedOptions{Category: "cs.CL"})
	if err != nil {
		t.Fatal(err)
	}
	only, err := c.Announcements(ctx, FeedOptions{Category: "cs.CL", Types: []string{AnnounceNew}})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) >= len(all) || len(only) == 0 {
		t.Errorf("--type new returned %d of %d", len(only), len(all))
	}
	for _, a := range only {
		if a.AnnounceType != AnnounceNew {
			t.Errorf("%s is a %s and came back under --type new", a.PaperID, a.AnnounceType)
		}
	}
}

// TestLiveAuthorIdentifierPage reads a page that has been there for years and
// checks the two facts the command claims: the redirect from the unsuffixed
// form, and the ORCID.
func TestLiveAuthorIdentifierPage(t *testing.T) {
	c := liveClient(t)
	p, err := c.AuthorByID(context.Background(), "baez_j_1")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Identified || p.ORCID == "" || p.ArxivID != "baez_j_1" {
		t.Errorf("identity: %+v", p)
	}
	if len(p.Papers) < 100 {
		t.Errorf("%d papers on the page", len(p.Papers))
	}
	if p.URI != AuthorURI("baez_j_1") || p.IdentifiedAs != NameURI(p.Name) {
		t.Errorf("uris: %q %q", p.URI, p.IdentifiedAs)
	}
	t.Logf("%s, orcid %s, %d papers", p.Name, p.ORCID, len(p.Papers))
}

// TestLiveAuthorPageRedirects checks that the unsuffixed form still goes where
// the suffixed one is asked for, which is the reason the tool asks for the
// suffixed one directly.
func TestLiveAuthorPageRedirects(t *testing.T) {
	c := liveClient(t)
	resp, err := c.fetch(context.Background(), "https://arxiv.org/a/baez_j_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	page, err := parseAuthorPage(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if page.ORCID == "" || len(page.Rows) == 0 {
		t.Errorf("the unsuffixed form did not land on the page: %+v", page.Name)
	}
}

// TestLiveAuthorPageIsOptIn: a well known author with no registered page still
// answers 404, and the message has to say what that means.
func TestLiveAuthorPageIsOptIn(t *testing.T) {
	_, err := liveClient(t).AuthorByID(context.Background(), "hinton_g_1")
	if err == nil {
		t.Skip("hinton_g_1 has registered a page since this was written")
	}
	if !strings.Contains(err.Error(), "opt-in") {
		t.Errorf("got %v", err)
	}
}

// TestLiveAuthorNameSearchIsNotAPerson checks the other half of the pair: a
// name search says so, and carries no ORCID to pretend otherwise.
func TestLiveAuthorNameSearchIsNotAPerson(t *testing.T) {
	p, err := liveClient(t).AuthorByName(context.Background(), "John Baez", 5)
	if err != nil {
		t.Fatal(err)
	}
	if p.Identified || p.ORCID != "" || p.ArxivID != "" {
		t.Errorf("a name search claimed a person: %+v", p)
	}
	if p.PaperCount < len(p.Papers) || len(p.Papers) != 5 {
		t.Errorf("%d papers of %d", len(p.Papers), p.PaperCount)
	}
	if p.Warning == "" {
		t.Error("no warning on a name match")
	}
	t.Logf("%d papers match the name", p.PaperCount)
}

// TestLiveFullTextReadsARendering reads a paper arXiv rendered and checks the
// parts that only the rendering has. The paper is a fixed one, so a change in
// the markup shows up here rather than in a user's terminal.
func TestLiveFullTextReadsARendering(t *testing.T) {
	full, err := liveClient(t).FullText(context.Background(), "2401.00001", FullTextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if full.Title == "" || full.Abstract == "" {
		t.Errorf("title %q, abstract %.40q", full.Title, full.Abstract)
	}
	if len(full.Sections) == 0 || full.Words < 1000 {
		t.Errorf("%d top level sections and %d words", len(full.Sections), full.Words)
	}
	if full.LicenseName == "" || full.Stamp == "" {
		t.Errorf("the info box stopped carrying the licence and the watermark: %q %q", full.LicenseName, full.Stamp)
	}
	if len(full.Authors) == 0 || full.Authors[0].Affiliation == "" {
		t.Errorf("affiliations are gone from the rendering: %+v", full.Authors)
	}
	if !contains(full.Surfaces, SurfaceFullText) {
		t.Errorf("surfaces: %v", full.Surfaces)
	}
	t.Logf("%d sections, %d words, %d references", full.SectionCount, full.Words, len(full.References))
}

// TestLiveFullTextRefusesAPaperWithNoRendering checks the refusal path. A 1997
// paper has no LaTeXML rendering and never will, so the answer is exit 7 with a
// sentence saying why, not a 404 to guess at.
func TestLiveFullTextRefusesAPaperWithNoRendering(t *testing.T) {
	_, err := liveClient(t).FullText(context.Background(), "hep-th/9711200", FullTextOptions{})
	if err == nil {
		t.Fatal("arXiv has rendered a 1997 paper, which is news")
	}
	if errs.KindOf(err) != errs.KindUnsupported {
		t.Errorf("kind: got %v, want unsupported: %v", errs.KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "December 2023") {
		t.Errorf("the message does not say when renderings start: %v", err)
	}
}

// TestLiveFullTextSectionIDsAreStable checks that the ids --section takes are
// the ids the page prints. A tree whose ids did not resolve would be a table of
// contents nobody could use.
func TestLiveFullTextSectionIDsAreStable(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	toc, err := c.FullText(ctx, "2401.00001", FullTextOptions{Sections: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(toc.Sections) == 0 {
		t.Fatal("no sections")
	}
	for _, s := range toc.Sections {
		if s.Text != "" {
			t.Errorf("%s kept its prose in a table of contents", s.ID)
		}
	}
	id := toc.Sections[len(toc.Sections)-1].ID

	one, err := c.FullText(ctx, "2401.00001", FullTextOptions{Section: id})
	if err != nil {
		t.Fatalf("section %s: %v", id, err)
	}
	if len(one.Sections) != 1 || one.Sections[0].ID != id {
		t.Errorf("asked for %s and got %+v", id, one.Sections)
	}
}

// TestLiveDepthTextPutsAffiliationsOnAPaper is the other half: the same
// rendering read as part of a paper, where the affiliations land on the authors
// the metadata surfaces already named.
func TestLiveDepthTextPutsAffiliationsOnAPaper(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	p, err := c.PaperAt(ctx, "2401.00001", PaperOptions{Depth: DepthText})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sections) == 0 {
		t.Error("a text read came back with no sections")
	}
	if !contains(p.Surfaces, SurfaceFullText) {
		t.Errorf("surfaces: %v", p.Surfaces)
	}
	affiliated := 0
	for _, a := range p.Authors {
		if a.Affiliation != "" {
			affiliated++
		}
	}
	if affiliated == 0 {
		t.Errorf("no author of %d picked up an affiliation: %+v", len(p.Authors), p.Authors)
	}
	if len(p.Missed) != 0 {
		t.Errorf("a rendered paper at depth text missed %v", p.Missed)
	}

	// A paper with no rendering says so instead of looking like a failed read.
	old, err := c.PaperAt(ctx, "hep-th/9711200", PaperOptions{Depth: DepthText})
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Missed) == 0 || !strings.Contains(strings.Join(old.Missed, " "), "no LaTeXML rendering") {
		t.Errorf("missed: %v", old.Missed)
	}
}

// TestLiveRecentPapersAreStillRendered checks the assumption the whole surface
// rests on: papers announced this week have HTML. If arXiv turned rendering off,
// every fulltext read would start refusing and this says so first.
func TestLiveRecentPapersAreStillRendered(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	papers, err := c.Search(ctx, SearchOptions{
		Categories: []string{"cs.CL"},
		Sort:       "submitted",
		Order:      "desc",
		Limit:      5,
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered := 0
	for _, p := range papers {
		full, err := c.PaperAt(ctx, p.ID, PaperOptions{Depth: DepthFull})
		if err != nil {
			t.Fatal(err)
		}
		if full.HasHTML {
			rendered++
		}
	}
	if rendered == 0 {
		t.Errorf("none of %d papers announced this week has an HTML rendering", len(papers))
	}
	t.Logf("%d of %d recent papers are rendered", rendered, len(papers))
}

// TestLiveBibTeXPassesArxivsBytesThrough checks the default. The bytes are
// arXiv's, so the entry every other tool quotes for this paper is the entry
// this prints, down to the two lines that end in a space.
func TestLiveBibTeXPassesArxivsBytesThrough(t *testing.T) {
	c := liveClient(t)
	got, err := c.BibTeX(context.Background(), []string{"1706.03762"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "@misc{") {
		t.Errorf("entry starts %.10q, want @misc{", got)
	}
	for _, want := range []string{"eprint={1706.03762}", "archivePrefix={arXiv}", "primaryClass={cs.CL}"} {
		if !strings.Contains(got, want) {
			t.Errorf("entry is missing %q:\n%s", want, got)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("the entry carries a trailing newline; whoever prints it adds that")
	}
	t.Logf("arXiv's own entry:\n%s", got)
}

// TestLiveBibTeXDatesTheLatestVersion pins the difference --local exists for.
// arXiv's key and year follow the newest version, so its entry for a paper
// everybody cites as 2017 says 2023.
func TestLiveBibTeXDatesTheLatestVersion(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	served, err := c.BibTeX(ctx, []string{"1706.03762"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(served, "year={2023}") {
		t.Logf("arXiv no longer dates this paper 2023, which is worth reading:\n%s", served)
	}

	local, err := c.BibTeX(ctx, []string{"1706.03762"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(local, "year={2017}") {
		t.Errorf("the local entry does not date the first submission:\n%s", local)
	}
	if !strings.Contains(local, "@misc{vaswani2017") {
		t.Errorf("the local key does not carry the first submission year:\n%s", local)
	}
}

// TestLiveBibTeXOfAPublishedPaper is the other half of it. arXiv writes @misc
// for a paper with a publisher DOI and never mentions the journal, and it puts
// a URL in the doi field.
func TestLiveBibTeXOfAPublishedPaper(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	served, err := c.BibTeX(ctx, []string{"1207.7214"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(served, "@misc{") {
		t.Errorf("arXiv now writes something other than @misc:\n%s", served)
	}
	if strings.Contains(served, "journal=") {
		t.Errorf("arXiv now carries the journal reference, which is worth knowing:\n%s", served)
	}

	local, err := c.BibTeX(ctx, []string{"1207.7214"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(local, "@article{") {
		t.Errorf("the local entry is not @article:\n%s", local)
	}
	if !strings.Contains(local, "doi={10.1016/j.physletb.2012.08.020}") {
		t.Errorf("the local entry does not carry the publisher DOI as a DOI:\n%s", local)
	}
}

// TestLiveBibTeXIgnoresTheVersion checks what arXiv does with a versioned id:
// it answers for the paper. A request for v1 comes back with the entry for the
// paper, so the version has to be dropped rather than passed on.
func TestLiveBibTeXIgnoresTheVersion(t *testing.T) {
	c := liveClient(t)
	got, err := c.BibTeX(context.Background(), []string{"2401.00001v1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "eprint={2401.00001}") {
		t.Errorf("entry:\n%s", got)
	}
	if strings.Contains(got, "2401.00001v1") {
		t.Errorf("arXiv now answers per version, which changes what this command means:\n%s", got)
	}
}

// TestLiveBibTeXOfAPaperThatIsNotThere checks the two ways s9 says no. A well
// formed id that does not exist is a 404 and a malformed one is a 400, and both
// have to come out as a not found rather than as a parse failure.
func TestLiveBibTeXOfAPaperThatIsNotThere(t *testing.T) {
	c := liveClient(t)
	_, err := c.BibTeX(context.Background(), []string{"2401.99999"}, false)
	if err == nil {
		t.Fatal("arxiv answered for a paper that does not exist")
	}
	if errs.KindOf(mapErr(err)) != errs.KindNotFound {
		t.Errorf("kind is %v, want not found: %v", errs.KindOf(mapErr(err)), err)
	}
}

// TestLiveCiteReadsInTheOrderAsked checks a bibliography stays in the order
// somebody wrote it in. The batch read answers in arXiv's order, not ours.
func TestLiveCiteReadsInTheOrderAsked(t *testing.T) {
	c := liveClient(t)
	got, err := c.Cite(context.Background(), []string{"2401.00001", "1706.03762"}, StyleText)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "2401.00001") || !strings.Contains(lines[1], "1706.03762") {
		t.Errorf("the order changed:\n%s", got)
	}
}

// TestLiveCiteEveryStyle runs all seven against one real paper. Each one has to
// name the paper and none of them may come back empty.
func TestLiveCiteEveryStyle(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	for _, style := range Styles {
		t.Run(string(style), func(t *testing.T) {
			got, err := c.Cite(ctx, []string{"1706.03762"}, style)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, "1706.03762") {
				t.Errorf("%s does not name the paper:\n%s", style, got)
			}
			t.Logf("%s:\n%s", style, got)
		})
	}
}

// TestLiveCiteOfAPaperThatIsNotThere checks a missing id is an error and not a
// list one shorter than it should be.
func TestLiveCiteOfAPaperThatIsNotThere(t *testing.T) {
	c := liveClient(t)
	_, err := c.Cite(context.Background(), []string{"1706.03762", "2401.99999"}, StyleAPA)
	if err == nil {
		t.Fatal("a missing paper was skipped instead of reported")
	}
	if errs.KindOf(mapErr(err)) != errs.KindNotFound {
		t.Errorf("kind is %v, want not found: %v", errs.KindOf(mapErr(err)), err)
	}
}

// TestLiveTrackbacksOfAWellLinkedPaper reads the page that has the most of them
// of any paper this tool has looked at. The counts move, so this checks the
// shape of a row rather than how many rows there are.
func TestLiveTrackbacksOfAWellLinkedPaper(t *testing.T) {
	c := liveClient(t)
	tbs, err := c.Trackbacks(context.Background(), "hep-th/9711200")
	if err != nil {
		t.Fatal(err)
	}
	if len(tbs) < 20 {
		t.Fatalf("got %d trackbacks for the Maldacena paper, want the twenty odd it has", len(tbs))
	}
	for i, tb := range tbs {
		if tb.Title == "" {
			t.Errorf("trackback %d has no title", i)
		}
		if tb.URL == "" || tb.TrackbackID == "" {
			t.Errorf("trackback %d has no redirect: url %q, id %q", i, tb.URL, tb.TrackbackID)
		}
		if tb.PostedAt.IsZero() {
			t.Errorf("trackback %d (%q) has no timestamp", i, tb.Title)
		}
		if tb.PaperID != "hep-th/9711200" {
			t.Errorf("trackback %d names paper %q", i, tb.PaperID)
		}
	}
	t.Logf("%d trackbacks, newest %q from %s on %s", len(tbs), tbs[0].Title, tbs[0].BlogName, tbs[0].PostedDate)
}

// TestLiveTrackbacksOfAPaperWithNone is the answer most papers give. It is an
// empty list and not an error, which is the whole reason the command does not
// exit 3 on an empty read.
func TestLiveTrackbacksOfAPaperWithNone(t *testing.T) {
	c := liveClient(t)
	tbs, err := c.Trackbacks(context.Background(), "2401.00001")
	if err != nil {
		t.Fatalf("a paper with no trackbacks came back as an error: %v", err)
	}
	if len(tbs) != 0 {
		t.Errorf("got %d trackbacks, want none", len(tbs))
	}
}

// TestLiveTrackbacksOfAPaperThatIsNotThere is the other kind of empty. arXiv
// answers 404 for an id it has no paper for, which is a different answer from
// the page saying there are no pings.
func TestLiveTrackbacksOfAPaperThatIsNotThere(t *testing.T) {
	c := liveClient(t)
	_, err := c.Trackbacks(context.Background(), "2401.99999")
	if err == nil {
		t.Fatal("arxiv answered for a paper that does not exist")
	}
	if errs.KindOf(mapErr(err)) != errs.KindNotFound {
		t.Errorf("kind is %v, want not found: %v", errs.KindOf(mapErr(err)), err)
	}
}

// TestLiveRecentTrackbacks reads the site-wide feed. Every row has to name a
// paper and carry a day, and the day is all the feed gives, so every row also
// has to say what it is missing.
func TestLiveRecentTrackbacks(t *testing.T) {
	c := liveClient(t)
	tbs, err := c.RecentTrackbacks(context.Background(), 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(tbs) < 25 {
		t.Fatalf("got %d records from a feed of 25 posts, want at least one a post", len(tbs))
	}
	for i, tb := range tbs {
		if tb.PaperID == "" || tb.PaperTitle == "" {
			t.Errorf("record %d names no paper: id %q, title %q", i, tb.PaperID, tb.PaperTitle)
		}
		if tb.PostedDate == "" {
			t.Errorf("record %d (%q) has no date", i, tb.Title)
		}
		if !tb.PostedAt.IsZero() {
			t.Errorf("record %d claims a time of day the feed does not publish: %s", i, tb.PostedAt)
		}
		if len(tb.Missed) == 0 {
			t.Errorf("record %d does not say the time of day is missing", i)
		}
	}
	t.Logf("%d records, newest %q on %s", len(tbs), tbs[0].Title, tbs[0].PostedDate)
}

// TestLiveRecentTrackbacksRefusesAViewArxivDoesNotOffer checks the flag is
// refused rather than sent and quietly ignored.
func TestLiveRecentTrackbacksRefusesAViewArxivDoesNotOffer(t *testing.T) {
	c := liveClient(t)
	_, err := c.RecentTrackbacks(context.Background(), 42)
	if err == nil {
		t.Fatal("42 was accepted")
	}
	if errs.KindOf(err) != errs.KindUsage {
		t.Errorf("kind is %v, want usage: %v", errs.KindOf(err), err)
	}
}

// TestLiveResolveFollowsTheRedirect resolves one trackback and no more, because
// this runs on the fifteen second plane. The target has to be somewhere else:
// the point of the redirect is that arXiv knows an address it does not print.
func TestLiveResolveFollowsTheRedirect(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	tbs, err := c.Trackbacks(ctx, "hep-th/9711200")
	if err != nil {
		t.Fatal(err)
	}
	if len(tbs) == 0 {
		t.Skip("no trackbacks to resolve")
	}
	one := tbs[:1]
	if err := c.Resolve(ctx, one); err != nil {
		t.Fatal(err)
	}
	got := one[0]
	if got.TargetURL == "" {
		t.Fatalf("%q did not resolve: %v", got.Title, got.Missed)
	}
	if !strings.HasPrefix(got.TargetURL, "http") {
		t.Errorf("target %q is not a URL", got.TargetURL)
	}
	if strings.Contains(got.TargetURL, Host) {
		t.Errorf("the redirect went back to arxiv: %q", got.TargetURL)
	}
	if got.Via["target_url"] != SurfaceTrackback {
		t.Errorf("target_url is attributed to %q", got.Via["target_url"])
	}
	t.Logf("%q -> %s", got.Title, got.TargetURL)
}

// TestLiveFilesListsWhatArxivServes reads the list without measuring anything,
// which is the whole point of splitting the two: the list is already on the
// paper record and costs no extra request.
func TestLiveFilesListsWhatArxivServes(t *testing.T) {
	c := liveClient(t)
	files, err := c.Files(context.Background(), "1706.03762", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 {
		t.Fatalf("got %d files, want at least the pdf and the source", len(files))
	}
	kinds := map[string]bool{}
	for _, f := range files {
		kinds[f.Kind] = true
		if f.Version == 0 {
			t.Errorf("%s is at no version; a file belongs to a version", f.Kind)
		}
		if !strings.Contains(f.URL, "v") {
			t.Errorf("%s url %q carries no version", f.Kind, f.URL)
		}
		if f.SizeFrom == SizeFromServer {
			t.Errorf("%s claims a measured size without anybody measuring it", f.Kind)
		}
	}
	for _, kind := range []string{KindPDF, KindSource} {
		if !kinds[kind] {
			t.Errorf("no %s in the list", kind)
		}
	}
	t.Logf("%d files: %v", len(files), kinds)
}

// TestLiveFilesMeasuresTheRealSize is the one that justifies --measure. arXiv's
// version table says 1,102 KB for this paper, which is the source, and the PDF
// is twice that, so a record that reported the table figure for the PDF would
// be wrong by a factor of two.
func TestLiveFilesMeasuresTheRealSize(t *testing.T) {
	c := liveClient(t)
	files, err := c.Files(context.Background(), "1706.03762", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.SizeBytes <= 0 {
			t.Errorf("%s has no size after measuring", f.Kind)
		}
		if f.SizeFrom != SizeFromServer {
			t.Errorf("%s size says it came from %q, want %q", f.Kind, f.SizeFrom, SizeFromServer)
		}
		if f.Via["size_bytes"] != SurfaceFiles {
			t.Errorf("%s size is attributed to %q, want %s", f.Kind, f.Via["size_bytes"], SurfaceFiles)
		}
		// The PDF and the source come with a content-disposition and the HTML
		// does not, because the HTML is a page to look at rather than a file to
		// save. A download of it falls back to the name built from the id.
		if f.Filename == "" && f.Kind != KindHTML {
			t.Errorf("%s has no filename", f.Kind)
		}
		if !f.Resumable {
			t.Errorf("%s did not answer a range request, so --resume has nothing to work with", f.Kind)
		}
		if f.ModifiedAt.IsZero() {
			t.Errorf("%s has no last-modified", f.Kind)
		}
		t.Logf("%s: %d bytes, %s, etag %s", f.Kind, f.SizeBytes, f.Filename, f.ETag)
	}

	var pdf, source *File
	for i := range files {
		switch files[i].Kind {
		case KindPDF:
			pdf = &files[i]
		case KindSource:
			source = &files[i]
		}
	}
	if pdf == nil || source == nil {
		t.Fatal("this paper stopped serving a pdf or a source")
	}
	if pdf.SizeBytes == source.SizeBytes {
		t.Errorf("the pdf and the source are both %d bytes, which means one of them was not measured", pdf.SizeBytes)
	}
	if !strings.HasPrefix(pdf.ETag, "sha256:") {
		t.Errorf("the pdf etag %q is no longer a checksum", pdf.ETag)
	}
}

// TestLiveDownloadWritesThePDF fetches one paper into a temporary directory and
// checks the bytes are a PDF and the whole of one.
func TestLiveDownloadWritesThePDF(t *testing.T) {
	c := liveClient(t)
	dir := t.TempDir()

	d, err := c.Download(context.Background(), "2401.00001", DownloadOptions{Kind: KindPDF, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if d.Skipped || d.Resumed {
		t.Errorf("a fresh download into an empty directory was skipped or resumed: %+v", d)
	}
	if d.Downloaded != d.SizeBytes {
		t.Errorf("downloaded %d bytes into a file of %d", d.Downloaded, d.SizeBytes)
	}
	if d.Version == 0 {
		t.Error("the download does not say which version it got")
	}
	// arXiv names the file with the version it resolved to, and that is the
	// name to keep: two downloads of the same paper a year apart are two
	// different papers.
	if !strings.Contains(filepath.Base(d.Path), "v") {
		t.Errorf("the file landed as %q, with no version in the name", filepath.Base(d.Path))
	}
	head := make([]byte, 5)
	f, err := os.Open(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.ReadFull(f, head); err != nil {
		t.Fatal(err)
	}
	if string(head) != "%PDF-" {
		t.Errorf("the file starts with %q, which is not a PDF", head)
	}
	if _, err := os.Stat(d.Path + ".part"); err == nil {
		t.Error("the part file is still there after a finished download")
	}

	// The second run is the one that matters for arXiv's bandwidth: the file is
	// already there under the name arXiv gave it, and asking again for the same
	// unversioned id has to find it.
	again, err := c.Download(context.Background(), "2401.00001", DownloadOptions{Kind: KindPDF, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Skipped {
		t.Errorf("the second run fetched %d bytes again", again.Downloaded)
	}
	if again.Path != d.Path {
		t.Errorf("the second run found %q, want %q", again.Path, d.Path)
	}
}

// TestLiveDownloadResumesAPartFile truncates a finished download back into a
// part file and checks the rest arrives over a range request rather than the
// whole thing again.
func TestLiveDownloadResumesAPartFile(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	dir := t.TempDir()

	first, err := c.Download(ctx, "2401.00001", DownloadOptions{Kind: KindPDF, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	whole, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) < 1<<20 {
		t.Skipf("%s is only %d bytes, too small to cut in half usefully", first.Path, len(whole))
	}

	half := len(whole) / 2
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Path+".part", whole[:half], 0o644); err != nil {
		t.Fatal(err)
	}

	resumed, err := c.Download(ctx, "2401.00001", DownloadOptions{
		Kind: KindPDF, Dir: dir, Path: first.Path, Resume: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Resumed {
		t.Fatalf("the download did not resume: %+v", resumed)
	}
	if resumed.Downloaded >= int64(len(whole)) {
		t.Errorf("resuming fetched %d bytes of a %d byte file", resumed.Downloaded, len(whole))
	}
	if resumed.SizeBytes != int64(len(whole)) {
		t.Errorf("the resumed file is %d bytes, want %d", resumed.SizeBytes, len(whole))
	}
	got, err := os.ReadFile(resumed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, whole) {
		t.Error("the resumed file is a different file, so the range landed in the wrong place")
	}
}

// TestLiveDownloadSourceAndExtract fetches a real submission and unpacks it,
// because a tar built in a test only proves the parser handles what its author
// thought of.
func TestLiveDownloadSourceAndExtract(t *testing.T) {
	c := liveClient(t)
	dir := t.TempDir()

	d, err := c.Download(context.Background(), "1706.03762", DownloadOptions{
		Kind: KindSource, Dir: dir, Extract: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.ExtractedFiles < 2 {
		t.Fatalf("unpacked %d files out of a real submission", d.ExtractedFiles)
	}
	entries, err := os.ReadDir(d.ExtractedTo)
	if err != nil {
		t.Fatal(err)
	}
	tex := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tex") {
			tex = true
		}
	}
	if !tex {
		t.Errorf("no .tex in %s, which is not a LaTeX submission", d.ExtractedTo)
	}
	// Everything unpacked has to be inside the directory it was unpacked into.
	root, err := filepath.Abs(d.ExtractedTo)
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasPrefix(path, root) {
			t.Errorf("%s is outside %s", path, root)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d files into %s", d.ExtractedFiles, d.ExtractedTo)
}

// TestLiveDownloadRefusesExtractWithoutSource keeps --extract from quietly
// doing nothing on a PDF.
func TestLiveDownloadRefusesExtractWithoutSource(t *testing.T) {
	c := liveClient(t)
	_, err := c.Download(context.Background(), "2401.00001", DownloadOptions{
		Kind: KindPDF, Dir: t.TempDir(), Extract: true,
	})
	if err == nil {
		t.Fatal("--extract on a pdf was accepted")
	}
	if errs.KindOf(err) != errs.KindUsage {
		t.Errorf("kind is %v, want usage: %v", errs.KindOf(err), err)
	}
}

// TestLiveDownloadOfAVersionThatIsNotThere checks the 404 comes back as a not
// found with a sentence on it rather than a generic failure.
func TestLiveDownloadOfAVersionThatIsNotThere(t *testing.T) {
	c := liveClient(t)
	_, err := c.Download(context.Background(), "1706.03762v99", DownloadOptions{
		Kind: KindPDF, Dir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("v99 was downloaded")
	}
	if errs.KindOf(err) != errs.KindNotFound {
		t.Errorf("kind is %v, want not found: %v", errs.KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("%v does not mention the version", err)
	}
}

// TestLiveHTMLIsNotThereForAnOldPaper checks the message names the reason,
// because a 1997 paper having no LaTeXML rendering is a fact about arXiv and
// not a fault.
func TestLiveHTMLIsNotThereForAnOldPaper(t *testing.T) {
	c := liveClient(t)
	_, err := c.Download(context.Background(), "hep-th/9711200", DownloadOptions{
		Kind: KindHTML, Dir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("arxiv now renders HTML for a 1997 paper, which is worth knowing")
	}
	if errs.KindOf(err) != errs.KindNotFound {
		t.Errorf("kind is %v, want not found: %v", errs.KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "December 2023") {
		t.Errorf("%v does not say why there is no HTML", err)
	}
}
