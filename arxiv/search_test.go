package arxiv

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// query is the whole query a set of options produces, date clause included,
// which is the string that actually goes on the wire.
func query(t *testing.T, o SearchOptions) string {
	t.Helper()
	p, err := buildSearch(o)
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	return p.full().String()
}

func TestSearchQueryFromFlags(t *testing.T) {
	cases := []struct {
		name string
		opts SearchOptions
		want string
	}{
		{
			"free text is ANDed word by word",
			SearchOptions{Query: "attention transformer"},
			"all:attention AND all:transformer",
		},
		{
			"the field flags are the prefixes under readable names",
			SearchOptions{Title: "attention", Author: "vaswani"},
			"au:vaswani AND ti:attention",
		},
		{
			"a multi word value is quoted, because unquoted is an OR of terms",
			SearchOptions{Title: "attention is all you need"},
			`ti:"attention is all you need"`,
		},
		{
			"one category is a plain clause",
			SearchOptions{Query: "attention", Categories: []string{"cs.CL"}},
			"all:attention AND cat:cs.CL",
		},
		{
			"several are OR'd and grouped, so the following AND binds right",
			SearchOptions{Query: "attention", Categories: []string{"cs.CL", "cs.LG"}},
			"all:attention AND (cat:cs.CL OR cat:cs.LG)",
		},
		{
			"a bare archive code matches split and unsplit archives alike",
			SearchOptions{Categories: []string{"hep-th"}},
			"(cat:hep-th OR cat:hep-th.*)",
		},
		{
			"raw goes through untouched",
			SearchOptions{Raw: "(cat:cs.CL OR cat:cs.LG) ANDNOT ti:survey"},
			"(cat:cs.CL OR cat:cs.LG) ANDNOT ti:survey",
		},
		{
			"a quoted phrase in the positional stays one term",
			SearchOptions{Query: `"attention is all you need"`},
			`all:"attention is all you need"`,
		},
		{
			"a positional written in arXiv's grammar goes out as written",
			SearchOptions{Query: `ti:"attention is all you need" AND cat:cs.CL`},
			`ti:"attention is all you need" AND cat:cs.CL`,
		},
		{
			"a grammar positional is parenthesised before a flag is ANDed onto it",
			SearchOptions{Query: "ti:transformer OR ti:attention", Categories: []string{"cs.CL"}},
			"(ti:transformer OR ti:attention) AND cat:cs.CL",
		},
		{
			"a bare operator is grammar too",
			SearchOptions{Query: "electron ANDNOT positron"},
			"electron ANDNOT positron",
		},
		{
			"a date bound becomes a range clause at minute resolution",
			SearchOptions{Categories: []string{"cs.CL"}, From: "2026-01", To: "2026-01"},
			"cat:cs.CL AND submittedDate:[202601010000 TO 202601312359]",
		},
		{
			"both date fields ride in the same query",
			SearchOptions{Categories: []string{"cs.CL"}, From: "2026", To: "2026", UpdatedFrom: "2026-08-01", UpdatedTo: "2026-08-10"},
			"cat:cs.CL AND lastUpdatedDate:[202608010000 TO 202608102359] AND submittedDate:[202601010000 TO 202612312359]",
		},
	}
	for _, tc := range cases {
		if got := query(t, tc.opts); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// TestGrammarPositionalSurvives is the second escaping bug, found by working
// through the acceptance list. A query typed in arXiv's own grammar was cut on
// spaces and every word prefixed, so ti:"attention is all you need" AND
// cat:cs.CL went out as all:ti:"attention AND all:is AND ... AND all:AND AND
// all:cat:cs.CL and arXiv answered "Invalid query string".
func TestGrammarPositionalSurvives(t *testing.T) {
	const typed = `ti:"attention is all you need" AND cat:cs.CL`
	got := query(t, SearchOptions{Query: typed})
	if got != typed {
		t.Errorf("query:\n got %q\nwant %q", got, typed)
	}
}

func TestIsGrammar(t *testing.T) {
	grammar := []string{
		`ti:"attention is all you need" AND cat:cs.CL`,
		"cat:cs.CL",
		"electron ANDNOT positron",
		"(ti:transformer OR ti:attention)",
		"au:vaswani",
		"id:1706.03762",
	}
	words := []string{
		"attention",
		"attention transformer",
		`"attention is all you need"`,
		// Not a prefix arXiv has, so it is a word with a colon in it and not a
		// clause. Sending it as grammar would be sending arXiv something it
		// answers with an error.
		"title:attention",
		"schrodinger and heisenberg",
	}
	for _, s := range grammar {
		if !isGrammar(s) {
			t.Errorf("isGrammar(%q) = false, want true", s)
		}
	}
	for _, s := range words {
		if isGrammar(s) {
			t.Errorf("isGrammar(%q) = true, want false", s)
		}
	}
}

func TestSplitTerms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"attention transformer", []string{"attention", "transformer"}},
		{`"attention is all you need"`, []string{`"attention is all you need"`}},
		{`ti:"a b" AND cat:cs.CL`, []string{`ti:"a b"`, "AND", "cat:cs.CL"}},
		{"  spaced   out\tby\ttabs ", []string{"spaced", "out", "by", "tabs"}},
		{`"unclosed quote`, []string{`"unclosed quote"`}},
		{"", nil},
	}
	for _, tc := range cases {
		if got := splitTerms(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitTerms(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSearchURLIsEscapedOnce is the golden the whole rewrite exists for. The
// old tool wrote "+AND+" into the query and then encoded it, so the plus signs
// came out as %2B and arXiv was asked one nonsense question instead of two real
// ones.
func TestSearchURLIsEscapedOnce(t *testing.T) {
	p, err := buildSearch(SearchOptions{Title: "attention", Author: "vaswani"})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	u, err := Request{Query: p.full(), Max: 10, Sort: p.Sort, Order: p.Order}.URL()
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	want := "https://export.arxiv.org/api/query?" +
		"max_results=10&search_query=au%3Avaswani+AND+ti%3Aattention" +
		"&sortBy=relevance&sortOrder=descending"
	if u != want {
		t.Errorf("URL:\n got %s\nwant %s", u, want)
	}
	if strings.Contains(u, "%2B") {
		t.Error("the query was escaped twice, which is the bug this test exists for")
	}
}

// TestParseBound checks that a bound names a period and not an instant. --from
// 2026 is the first minute of the year and --to 2026 is the last one, which is
// what a person means and is not what parsing both to midnight would give them.
func TestParseBound(t *testing.T) {
	cases := []struct {
		in   string
		end  bool
		want string
	}{
		{"2026", false, "2026-01-01T00:00:00Z"},
		{"2026", true, "2026-12-31T23:59:00Z"},
		{"2026-02", false, "2026-02-01T00:00:00Z"},
		{"2026-02", true, "2026-02-28T23:59:00Z"},
		{"2024-02", true, "2024-02-29T23:59:00Z"},
		{"2026-01-15", false, "2026-01-15T00:00:00Z"},
		{"2026-01-15", true, "2026-01-15T23:59:00Z"},
	}
	for _, tc := range cases {
		got, err := ParseBound(tc.in, tc.end)
		if err != nil {
			t.Errorf("ParseBound(%q, %v): %v", tc.in, tc.end, err)
			continue
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Errorf("ParseBound(%q, %v) = %s, want %s", tc.in, tc.end, got.Format(time.RFC3339), tc.want)
		}
		if got.Location() != time.UTC {
			t.Errorf("ParseBound(%q) is in %s, want UTC", tc.in, got.Location())
		}
	}
	for _, bad := range []string{"", "yesterday", "01-2026", "2026/01/15", "20260115"} {
		if _, err := ParseBound(bad, false); err == nil {
			t.Errorf("ParseBound(%q) accepted something that is not a date", bad)
		}
	}
}

// TestParseBoundErrorNamesTheForms checks the message tells the user what to
// type instead, which is the only thing a usage error is for.
func TestParseBoundErrorNamesTheForms(t *testing.T) {
	_, err := ParseBound("last tuesday", false)
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"2026", "2026-01", "2026-01-01"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestSearchUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		opts SearchOptions
		want string
	}{
		{
			"nothing to search for",
			SearchOptions{},
			"nothing to search for",
		},
		{
			"raw cannot be combined",
			SearchOptions{Raw: "cat:cs.CL", Title: "attention"},
			"untouched",
		},
		{
			"a date that is not a date",
			SearchOptions{Categories: []string{"cs.CL"}, From: "last tuesday"},
			"--from",
		},
		{
			"a range that ends before it starts",
			SearchOptions{Categories: []string{"cs.CL"}, From: "2026", To: "2020"},
			"ends before it starts",
		},
		{
			"a sort arXiv would reject",
			SearchOptions{Query: "attention", Sort: "citations"},
			"relevance",
		},
		{
			"a direction that is neither",
			SearchOptions{Query: "attention", Order: "sideways"},
			"desc or asc",
		},
		{
			"all cannot sort by relevance when it is asked for out loud",
			SearchOptions{Categories: []string{"cs.CL"}, Sort: "relevance", All: true},
			"recomputed",
		},
		{
			"all needs a bound",
			SearchOptions{Query: "attention", Sort: "submitted", All: true},
			"needs a bound",
		},
	}
	for _, tc := range cases {
		_, err := buildSearch(tc.opts)
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
	}
}

// TestSearchErrorsStartWithAWord guards a rule the surface imposes: fang
// sentence-cases the first word of an error, so a message that opens with a
// flag name or a quoted value comes out mangled.
func TestSearchErrorsStartWithAWord(t *testing.T) {
	for _, o := range []SearchOptions{
		{},
		{Categories: []string{"cs.CL"}, Sort: "relevance", All: true},
		{Query: "attention", Sort: "citations"},
	} {
		_, err := buildSearch(o)
		if err == nil {
			t.Fatalf("%#v produced no error", o)
		}
		first := strings.Fields(err.Error())[0]
		if strings.HasPrefix(first, `"`) || strings.HasPrefix(first, "-") {
			t.Errorf("error starts with %q, which the surface will capitalise into nonsense: %v", first, err)
		}
	}
}

// TestAllAcceptsABound checks the three things that count as a bound, because
// the guard exists to stop an accidental days-long walk and not to stop work.
func TestAllAcceptsABound(t *testing.T) {
	for _, o := range []SearchOptions{
		{Categories: []string{"cs.CL"}, Sort: "submitted", All: true},
		{Query: "attention", From: "2026", Sort: "submitted", All: true},
		{Query: "attention", Limit: 500, Sort: "submitted", All: true},
	} {
		if _, err := buildSearch(o); err != nil {
			t.Errorf("%#v was refused: %v", o, err)
		}
	}
}

// TestAllDefaultsToSubmittedOrder checks what an unset --sort means, which
// depends on what is being run. A search wants the best match first, a walk
// cannot have that at all, and a user who typed neither should not have to
// learn the difference before their first --all works.
func TestAllDefaultsToSubmittedOrder(t *testing.T) {
	walk, err := buildSearch(SearchOptions{Categories: []string{"cs.CL"}, All: true})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	if walk.Sort != SortSubmitted {
		t.Errorf("an unset sort under --all is %q, want %q", walk.Sort, SortSubmitted)
	}

	one, err := buildSearch(SearchOptions{Query: "attention"})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	if one.Sort != SortRelevance {
		t.Errorf("an unset sort on a plain search is %q, want %q", one.Sort, SortRelevance)
	}
}

// TestAllSlicesOnSubmittedDate checks which timestamp a walk cuts on. The
// submission date never moves, so it is the safe one, and lastUpdatedDate is
// only used when that is the range the caller asked for.
func TestAllSlicesOnSubmittedDate(t *testing.T) {
	p, err := buildSearch(SearchOptions{
		Categories: []string{"cs.CL"}, From: "2026", UpdatedFrom: "2026-08", Sort: "submitted", All: true,
	})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	if p.Field != SubmittedDate {
		t.Errorf("Field: got %q, want %q", p.Field, SubmittedDate)
	}
	if p.Extra.Empty() {
		t.Error("the lastUpdatedDate clause was dropped instead of riding along")
	}
	// The sliced field must not already be in the query, or the slicer would
	// add a second range clause and the two would fight.
	if strings.Contains(p.Query.String(), string(SubmittedDate)) {
		t.Errorf("the query carries its own date clause: %q", p.Query)
	}

	updatedOnly, err := buildSearch(SearchOptions{
		Categories: []string{"cs.CL"}, UpdatedFrom: "2026-08", Sort: "updated", All: true,
	})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	if updatedOnly.Field != LastUpdatedDate {
		t.Errorf("Field: got %q, want %q", updatedOnly.Field, LastUpdatedDate)
	}
}

func TestSearchLimits(t *testing.T) {
	quiet, err := buildSearch(SearchOptions{Query: "attention"})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	if quiet.limit() != DefaultLimit {
		t.Errorf("an unbounded search returns %d, want the default %d", quiet.limit(), DefaultLimit)
	}
	walk, err := buildSearch(SearchOptions{Categories: []string{"cs.CL"}, Sort: "submitted", All: true})
	if err != nil {
		t.Fatalf("buildSearch: %v", err)
	}
	if walk.limit() != 0 {
		t.Errorf("a walk stops at %d, want no stop", walk.limit())
	}
}

func TestPlanLine(t *testing.T) {
	one := Plan{Total: 42, Slices: []Slice{{Total: 42}}, Counts: 1}
	if got := planLine(one); got != "42 results in 1 slice, 1 count request to plan and about 1 to walk" {
		t.Errorf("one slice: %q", got)
	}
	many := Plan{
		Total:  25000,
		Counts: 3,
		Slices: []Slice{{Total: 9000}, {Total: 9000}, {Total: 7000}},
	}
	got := planLine(many)
	for _, want := range []string{"25000 results", "3 slices", "3 count requests", "250 to walk"} {
		if !strings.Contains(got, want) {
			t.Errorf("plan line %q does not mention %q", got, want)
		}
	}
	cut := Plan{Total: 20000, Counts: 1, Slices: []Slice{{Total: 20000, Truncated: true}}}
	if !strings.Contains(planLine(cut), "cannot be reached") {
		t.Errorf("a truncated plan said nothing about it: %q", planLine(cut))
	}
}

func TestSliceLine(t *testing.T) {
	// The two leaves are the first two the slicer cut for cat:cs.CL on
	// 2026-08-15, and the counts are what arXiv answered for them.
	all := []Slice{
		{Range: NewRange(
			time.Date(1991, 8, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2009, 2, 6, 11, 59, 0, 0, time.UTC)), Total: 1585},
		{Range: NewRange(
			time.Date(2009, 2, 6, 12, 0, 0, 0, time.UTC),
			time.Date(2017, 11, 11, 5, 59, 0, 0, time.UTC)), Total: 6000},
	}
	got := sliceLine(0, all)
	want := "slice 1 of 2, 199108010000 to 200902061159, 1585 results"
	if got != want {
		t.Errorf("first slice:\n got %q\nwant %q", got, want)
	}
	want = "slice 2 of 2, 200902061200 to 201711110559, 6000 results"
	if got := sliceLine(1, all); got != want {
		t.Errorf("second slice:\n got %q\nwant %q", got, want)
	}
	cut := []Slice{{Total: 20000, Truncated: true}}
	if got := sliceLine(0, cut); !strings.Contains(got, "only the first 10000 can be reached") {
		t.Errorf("a truncated slice said nothing about it: %q", got)
	}
}

// TestSearchAndCountTakeTheSameQueryFlags is the guard on the one duplication
// in the command layer. kit binds the fields a struct declares and not the ones
// it promotes, so the query flags are written out twice, and this is what stops
// the two copies drifting.
func TestSearchAndCountTakeTheSameQueryFlags(t *testing.T) {
	// The flags `count` has no use for, because a count has no order and
	// returns one number.
	skip := map[string]bool{"Sort": true, "Order": true, "All": true, "Limit": true, "Client": true}

	want := map[string]string{}
	st := reflect.TypeOf(searchIn{})
	for i := range st.NumField() {
		f := st.Field(i)
		if skip[f.Name] {
			continue
		}
		want[f.Name] = string(f.Tag)
	}

	got := map[string]string{}
	ct := reflect.TypeOf(countIn{})
	for i := range ct.NumField() {
		f := ct.Field(i)
		if skip[f.Name] {
			continue
		}
		got[f.Name] = string(f.Tag)
	}

	for name, tag := range want {
		switch g, ok := got[name]; {
		case !ok:
			t.Errorf("count is missing the %s flag that search has", name)
		case g != tag:
			t.Errorf("%s differs:\n search %s\n count  %s", name, tag, g)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("count has a %s flag search does not", name)
		}
	}
}
