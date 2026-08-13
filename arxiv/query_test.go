package arxiv

import (
	"strings"
	"testing"
	"time"
)

// TestQueryCarriesRealSpaces is the regression test for the bug this rewrite
// exists to fix. A query holds plain text, and nothing on the way to the
// encoder is allowed to pre-escape any of it.
func TestQueryCarriesRealSpaces(t *testing.T) {
	q := And(Term(FieldAll, "attention"), Term(FieldCategory, "cs.CL"))
	want := "all:attention AND cat:cs.CL"
	if q.String() != want {
		t.Errorf("query = %q, want %q", q.String(), want)
	}
	for _, bad := range []string{"%2B", "%3A", "%20", "+"} {
		if strings.Contains(q.String(), bad) {
			t.Errorf("query %q carries escaped text %q before the encoder sees it", q, bad)
		}
	}
}

// TestEncodedExactlyOnce is the assertion the old tool would have failed. A
// space becomes +, a colon %3A and a quote %22, each exactly once.
func TestEncodedExactlyOnce(t *testing.T) {
	req := Request{
		Query: And(Phrase(FieldTitle, "attention is all you need"), Term(FieldCategory, "cs.CL")),
		Max:   1,
		Sort:  SortRelevance,
		Order: Descending,
	}
	u, err := req.URL()
	if err != nil {
		t.Fatal(err)
	}
	const want = "search_query=ti%3A%22attention+is+all+you+need%22+AND+cat%3Acs.CL"
	if !strings.Contains(u, want) {
		t.Fatalf("url = %s\nwant it to contain %s", u, want)
	}
	// The plus signs in that string are encoded spaces. A %2B anywhere means a
	// literal plus reached the encoder, which is exactly the old bug.
	if strings.Contains(u, "%2B") {
		t.Errorf("url has a %%2B, so a plus was in the query before encoding: %s", u)
	}
	if strings.Contains(u, "%253A") || strings.Contains(u, "%2522") {
		t.Errorf("url is double-encoded: %s", u)
	}
}

func TestFieldPrefixes(t *testing.T) {
	want := []Field{"all", "ti", "au", "abs", "co", "jr", "cat", "rn", "id"}
	if len(Fields) != len(want) {
		t.Fatalf("Fields has %d prefixes, want %d", len(Fields), len(want))
	}
	for i, f := range want {
		if Fields[i] != f {
			t.Errorf("Fields[%d] = %q, want %q", i, Fields[i], f)
		}
	}
	for _, tc := range []struct {
		in   string
		want Field
	}{
		{"ti", FieldTitle}, {"title", FieldTitle}, {"AU", FieldAuthor},
		{"abstract", FieldAbstract}, {"comments", FieldComment},
		{"journal", FieldJournal}, {"category", FieldCategory},
		{"report", FieldReport}, {"id", FieldID},
	} {
		got, ok := ParseField(tc.in)
		if !ok || got != tc.want {
			t.Errorf("ParseField(%q) = (%q, %v), want %q", tc.in, got, ok, tc.want)
		}
	}
	if _, ok := ParseField("nope"); ok {
		t.Error("ParseField accepted a field arXiv does not have")
	}
}

// TestTermQuoting covers the difference between a phrase and a bag of words.
// Unquoted, ti:attention is all you need matches hundreds of thousands of
// papers; quoted it matches 35.
func TestTermQuoting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Vaswani", "au:Vaswani"},
		{"Yann LeCun", `au:"Yann LeCun"`},
		{`"Yann LeCun"`, `au:"Yann LeCun"`},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := Term(FieldAuthor, tc.in).String(); got != tc.want {
			t.Errorf("Term(au, %q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := Phrase(FieldTitle, "attention").String(); got != `ti:"attention"` {
		t.Errorf("Phrase quotes a single word wrong: %q", got)
	}
}

func TestOperators(t *testing.T) {
	cl := Term(FieldCategory, "cs.CL")
	lg := Term(FieldCategory, "cs.LG")
	ti := Term(FieldTitle, "transformer")

	if got := AndNot(Term(FieldAuthor, "Vaswani"), cl).String(); got != "au:Vaswani ANDNOT cat:cs.CL" {
		t.Errorf("ANDNOT = %q", got)
	}
	if got := And(Group(Or(cl, lg)), ti).String(); got != "(cat:cs.CL OR cat:cs.LG) AND ti:transformer" {
		t.Errorf("grouped OR = %q", got)
	}
	// An empty leg drops out rather than leaving a dangling operator, because a
	// query built from optional flags has empty legs all the time.
	if got := And(cl, Query{}, ti).String(); got != "cat:cs.CL AND ti:transformer" {
		t.Errorf("empty leg leaked: %q", got)
	}
	if !And(Query{}, Query{}).Empty() {
		t.Error("joining nothing produced a query")
	}
	if got := AndNot(cl, Query{}).String(); got != "cat:cs.CL" {
		t.Errorf("ANDNOT with nothing to subtract = %q", got)
	}
	if got := Group(Group(cl)).String(); got != "(cat:cs.CL)" {
		t.Errorf("Group double-wrapped: %q", got)
	}
}

// TestDateRange pins the twelve-digit form. The eight-digit form is accepted by
// arXiv and agrees with it, but only minute resolution lets the slicer cut a
// busy day in half.
func TestDateRange(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 23, 59, 0, 0, time.UTC)
	q := Between(SubmittedDate, NewRange(from, to))
	want := "submittedDate:[202601010000 TO 202601312359]"
	if q.String() != want {
		t.Errorf("range = %q, want %q", q.String(), want)
	}
	if got := Between(LastUpdatedDate, NewRange(from, to)).String(); !strings.HasPrefix(got, "lastUpdatedDate:[") {
		t.Errorf("updated range = %q", got)
	}
	// A range with the ends the wrong way round is nothing, not a clause that
	// silently matches everything.
	if !Between(SubmittedDate, NewRange(to, from)).Empty() {
		t.Error("a backwards range produced a clause")
	}
}

// TestRangeIsUTC matters because arXiv's timestamps are UTC and a local time
// silently shifted by a timezone would slice the wrong minutes.
func TestRangeIsUTC(t *testing.T) {
	loc := time.FixedZone("plus7", 7*3600)
	r := NewRange(
		time.Date(2026, 1, 1, 7, 30, 0, 0, loc),
		time.Date(2026, 1, 1, 8, 30, 0, 0, loc),
	)
	if got := Stamp(r.From); got != "202601010030" {
		t.Errorf("from = %s, want 202601010030", got)
	}
	if got := r.Minutes(); got != 61 {
		t.Errorf("Minutes = %d, want 61", got)
	}
}

func TestParseSort(t *testing.T) {
	cases := []struct {
		in   string
		want Sort
	}{
		{"", SortRelevance},
		{"relevance", SortRelevance},
		{"date", SortSubmitted},
		{"submittedDate", SortSubmitted},
		{"updated", SortUpdated},
		{"lastUpdatedDate", SortUpdated},
	}
	for _, tc := range cases {
		got, err := ParseSort(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseSort(%q) = (%q, %v), want %q", tc.in, got, err, tc.want)
		}
	}
	// arXiv answers a bad sort with a 400 that names the three it takes, so the
	// same three are checked here and a typo costs no round trip.
	err := func() error { _, err := ParseSort("nope"); return err }()
	if err == nil {
		t.Fatal("ParseSort accepted a sort arXiv would reject")
	}
	for _, want := range []string{"relevance", "date", "updated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
