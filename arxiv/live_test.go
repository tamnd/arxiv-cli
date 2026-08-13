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
	"context"
	"strings"
	"testing"
	"time"
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
