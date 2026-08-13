package arxiv

import (
	"strings"
	"testing"
	"time"
)

// The three fixtures are the three shapes the trackback pages come in: a paper
// with a lot of pings, a paper with none, and the site-wide feed. All three
// were saved on 2026-08-14.

func trackbacksFixture(t *testing.T, name, paperID string) []Trackback {
	t.Helper()
	tbs, err := parseTrackbacks(fixture(t, name), paperID, trackbackBase+paperID, testTime)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return tbs
}

func recentFixture(t *testing.T) []Trackback {
	t.Helper()
	tbs, err := parseRecentTrackbacks(fixture(t, "tb_recent.html"), trackbackRecent+"?views=25", testTime)
	if err != nil {
		t.Fatalf("parse tb_recent.html: %v", err)
	}
	return tbs
}

func TestParseTrackbacksReadsThePage(t *testing.T) {
	tbs := trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200")
	if len(tbs) != 28 {
		t.Fatalf("got %d trackbacks, want the 28 on the page", len(tbs))
	}

	got := tbs[0]
	if got.Title != "Physicists Discover String Theory and Extra Dimensions in a Laboratory!" {
		t.Errorf("title = %q", got.Title)
	}
	if got.BlogName != "Of Particular Significance" {
		t.Errorf("blog = %q", got.BlogName)
	}
	if got.PaperID != "hep-th/9711200" {
		t.Errorf("paper = %q", got.PaperID)
	}
	if got.URL != "https://arxiv.org/tb/redirect/1845295/8591d4034" {
		t.Errorf("url = %q", got.URL)
	}
	if got.TrackbackID != "1845295" {
		t.Errorf("trackback id = %q", got.TrackbackID)
	}
	want := time.Date(2022, 12, 5, 13, 13, 0, 0, time.UTC)
	if !got.PostedAt.Equal(want) {
		t.Errorf("posted at = %s, want %s", got.PostedAt, want)
	}
	if got.PostedDate != "2022-12-05" {
		t.Errorf("posted date = %q", got.PostedDate)
	}
	if got.Kind != "trackback" {
		t.Errorf("kind = %q", got.Kind)
	}
	if len(got.Surfaces) != 1 || got.Surfaces[0] != SurfaceTrackback {
		t.Errorf("surfaces = %v, want just %s", got.Surfaces, SurfaceTrackback)
	}
	if len(got.Sources) != 1 || got.Sources[0] != trackbackBase+"hep-th/9711200" {
		t.Errorf("sources = %v", got.Sources)
	}
}

// The day of the month is not zero padded on this page, so a row posted on the
// thirtieth and a row posted on the fifth are written differently and both have
// to parse.
func TestParseTrackbacksReadsAnUnpaddedDay(t *testing.T) {
	tbs := trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200")
	want := time.Date(2022, 11, 30, 16, 0, 34, 0, time.UTC)
	if !tbs[1].PostedAt.Equal(want) {
		t.Errorf("posted at = %s, want %s", tbs[1].PostedAt, want)
	}
	for i, tb := range tbs {
		if tb.PostedAt.IsZero() {
			t.Errorf("trackback %d (%q) has no timestamp", i, tb.Title)
		}
	}
}

// The oldest row on this page is "[ INVALID-URL ]" with no name and no
// separator, which is the case that would otherwise put arXiv's placeholder in
// the blog name.
func TestParseTrackbacksLeavesOutAMissingBlogName(t *testing.T) {
	tbs := trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200")
	last := tbs[len(tbs)-1]
	if last.Title != "This Week's Finds in Mathematical Physics (Week 154)" {
		t.Fatalf("the last row is %q, so this test is reading the wrong one", last.Title)
	}
	if last.BlogName != "" {
		t.Errorf("blog name = %q, want empty", last.BlogName)
	}
	if last.BlogURL != "" {
		t.Errorf("blog url = %q, want empty", last.BlogURL)
	}
}

// arXiv stores the string INVALID-URL where a blog's own address should be, on
// every row of every page measured. It is a placeholder and not an address, so
// it never reaches a record.
func TestParseTrackbacksNeverKeepsThePlaceholder(t *testing.T) {
	all := append(trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200"), recentFixture(t)...)
	for _, tb := range all {
		if strings.Contains(tb.BlogName, invalidURL) || strings.Contains(tb.BlogURL, invalidURL) {
			t.Errorf("%q carries the placeholder: name %q, url %q", tb.Title, tb.BlogName, tb.BlogURL)
		}
	}
}

// A paper nobody has blogged about gets a page saying so, and that is an answer
// rather than a failure. The command turns this into exit 0 and an empty list.
func TestParseTrackbacksOfAPaperWithNone(t *testing.T) {
	tbs := trackbacksFixture(t, "tb_2401.00001.html", "2401.00001")
	if len(tbs) != 0 {
		t.Fatalf("got %d trackbacks, want none", len(tbs))
	}
}

func TestParseRecentTrackbacksReadsTheFeed(t *testing.T) {
	tbs := recentFixture(t)
	// Twenty five posts linking thirty four papers, because several posts link
	// more than one.
	if len(tbs) != 34 {
		t.Fatalf("got %d records, want the 34 (post, paper) pairs on the page", len(tbs))
	}
	posts := map[string]bool{}
	for _, tb := range tbs {
		posts[tb.TrackbackID] = true
	}
	if len(posts) != 25 {
		t.Errorf("got %d posts, want 25", len(posts))
	}

	got := tbs[0]
	if got.Title != "The Dying of the Light" {
		t.Errorf("title = %q", got.Title)
	}
	if got.BlogName != "astrobites" {
		t.Errorf("blog = %q", got.BlogName)
	}
	if got.PaperID != "2608.02757" {
		t.Errorf("paper = %q", got.PaperID)
	}
	if got.PaperTitle != "Rings in the Sky: Orbital Data Centres and Potential Impacts to Astronomy and the Sky" {
		t.Errorf("paper title = %q", got.PaperTitle)
	}
	if got.TrackbackID != "1872570" {
		t.Errorf("trackback id = %q", got.TrackbackID)
	}
	if got.PostedDate != "2026-08-13" {
		t.Errorf("posted date = %q", got.PostedDate)
	}
	// The feed says which day a ping arrived and not what time, so the record
	// says the day and leaves the timestamp alone rather than inventing
	// midnight.
	if !got.PostedAt.IsZero() {
		t.Errorf("posted at = %s, want zero on a feed row", got.PostedAt)
	}
	if len(got.Missed) != 1 || !strings.Contains(got.Missed[0], "time of day") {
		t.Errorf("missed = %v", got.Missed)
	}
}

// The title span in the list is never closed, so the id's own link text lands
// inside the title and comes out as "Title [2608.02757]" unless it is cut off.
func TestParseRecentTrackbacksCutsTheIdOffTheTitle(t *testing.T) {
	for _, tb := range recentFixture(t) {
		if strings.HasSuffix(tb.PaperTitle, "]") {
			t.Errorf("paper title still carries the id: %q", tb.PaperTitle)
		}
	}
}

// A post that links two papers is two records, sharing everything about the
// post and differing in the paper. Flattening them into one row would make the
// second paper invisible to anything joining on an id.
func TestParseRecentTrackbacksSplitsAPostPerPaper(t *testing.T) {
	tbs := recentFixture(t)
	var pair []Trackback
	for _, tb := range tbs {
		if tb.TrackbackID == "1872516" {
			pair = append(pair, tb)
		}
	}
	if len(pair) != 2 {
		t.Fatalf("got %d records for the post linking two papers, want 2", len(pair))
	}
	if pair[0].Title != pair[1].Title || pair[0].PostedDate != pair[1].PostedDate {
		t.Errorf("the two records disagree about the post: %+v", pair)
	}
	if pair[0].PaperID == pair[1].PaperID {
		t.Errorf("both records name the same paper %q", pair[0].PaperID)
	}
	if pair[0].PaperID != "2607.08834" || pair[1].PaperID != "2607.08836" {
		t.Errorf("papers = %q and %q", pair[0].PaperID, pair[1].PaperID)
	}
}

// The date heading appears once per day, on the first column only, and later
// posts that day get a column with the heading left blank. Reading the heading
// off each column would drop the date from four of these twenty five posts.
func TestParseRecentTrackbacksCarriesTheDayForward(t *testing.T) {
	tbs := recentFixture(t)
	for i, tb := range tbs {
		if tb.PostedDate == "" {
			t.Errorf("record %d (%q) has no date", i, tb.Title)
		}
	}
	// August 7 has two posts and only the first column carries the heading.
	seventh := 0
	for _, tb := range tbs {
		if tb.PostedDate == "2026-08-07" {
			seventh++
		}
	}
	if seventh != 3 {
		t.Errorf("got %d records dated 2026-08-07, want 3", seventh)
	}
}

func TestParseTrackbackDay(t *testing.T) {
	cases := map[string]string{
		"August 13, 2026": "2026-08-13",
		"July 31, 2026":   "2026-07-31",
		// A heading this parser cannot read is still a date a reader can, so
		// it is kept as arXiv wrote it.
		"sometime last week": "sometime last week",
	}
	for in, want := range cases {
		if got := parseTrackbackDay(in); got != want {
			t.Errorf("parseTrackbackDay(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackbackIDOf(t *testing.T) {
	cases := map[string]string{
		"/tb/redirect/1845295/8591d4034": "1845295",
		"/tb/redirect/1231/ca1b76249":    "1231",
		"/abs/2401.00001":                "",
	}
	for in, want := range cases {
		if got := trackbackIDOf(in); got != want {
			t.Errorf("trackbackIDOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAbsoluteTrackbackURL(t *testing.T) {
	cases := map[string]string{
		"/tb/redirect/1231/ca1b76249":     "https://arxiv.org/tb/redirect/1231/ca1b76249",
		"https://example.org/a-blog-post": "https://example.org/a-blog-post",
	}
	for in, want := range cases {
		if got := absoluteTrackbackURL(in); got != want {
			t.Errorf("absoluteTrackbackURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackbackURI(t *testing.T) {
	if got := TrackbackURI("1845295"); got != "ax://trackback/1845295" {
		t.Errorf("TrackbackURI = %q", got)
	}
}

// squash exists because the markup writes titles across lines inside their own
// elements, so raw text comes back with runs of spaces and newlines in it.
func TestSquash(t *testing.T) {
	cases := map[string]string{
		"  a  title \n  across lines ": "a title across lines",
		"":                             "",
		"one":                          "one",
	}
	for in, want := range cases {
		if got := squash(in); got != want {
			t.Errorf("squash(%q) = %q, want %q", in, got, want)
		}
	}
}
