package arxiv

import (
	"strings"
	"testing"
	"time"
)

// The two fixtures are real pages saved on 2026-08-13:
// search_all_attention.html is fifty results for "attention is all you need",
// which is the shape a plain search returns, and search_msc_18D10.html is
// twenty five results for msc_class:18D10, which is the route this file exists
// for.

func TestParseS5Total(t *testing.T) {
	for _, tc := range []struct {
		file    string
		total   int
		results int
	}{
		{"search_all_attention.html", 130, 50},
		{"search_msc_18D10.html", 723, 25},
	} {
		page, err := parseS5(fixture(t, tc.file))
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if page.Total != tc.total {
			t.Errorf("%s: total = %d, want %d", tc.file, page.Total, tc.total)
		}
		if len(page.Results) != tc.results {
			t.Errorf("%s: %d results, want %d", tc.file, len(page.Results), tc.results)
		}
	}
}

// TestParseS5Result reads one whole result and checks every field the page
// carries, because the point of this parser is that nothing on the page is
// left on the floor.
func TestParseS5Result(t *testing.T) {
	page, err := parseS5(fixture(t, "search_all_attention.html"))
	if err != nil {
		t.Fatal(err)
	}
	r := page.Results[0]

	if r.ID != "2607.23678" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Version != 1 {
		t.Errorf("Version = %d, want 1; the version is only in the abstract span id", r.Version)
	}
	if r.AbsURL != "https://arxiv.org/abs/2607.23678" {
		t.Errorf("AbsURL = %q", r.AbsURL)
	}
	if r.PDFURL != "https://arxiv.org/pdf/2607.23678" {
		t.Errorf("PDFURL = %q", r.PDFURL)
	}

	// The title has three highlight spans in it and none of them belong in the
	// title itself.
	want := "Focus Is All You Need: Adaptive Goal-aware Attention Orchestration for Multi-Agent Graph Systems"
	if r.Title != want {
		t.Errorf("Title = %q, want %q", r.Title, want)
	}
	if strings.Contains(r.Abstract, "More") || strings.HasPrefix(r.Abstract, "…") {
		t.Errorf("Abstract kept the toggle or the short form: %q", r.Abstract[:80])
	}
	if !strings.HasPrefix(r.Abstract, "Large language models (LLMs) enable autonomous agents") {
		t.Errorf("Abstract starts %q, want the full one", r.Abstract[:60])
	}
	// LaTeX is what the author wrote, so it stays.
	if !strings.Contains(r.Abstract, `\textbf{attention allocation}`) {
		t.Error("the LaTeX in the abstract was normalised away")
	}

	if len(r.Authors) != 3 || r.Authors[0] != "Mingzhou Fan" {
		t.Errorf("Authors = %v", r.Authors)
	}
	if len(r.Categories) != 1 || r.Categories[0] != "cs.AI" {
		t.Errorf("Categories = %v", r.Categories)
	}
	if r.SubjectNames["cs.AI"] != "Artificial Intelligence" {
		t.Errorf("SubjectNames = %v, want the tooltip name", r.SubjectNames)
	}
	if got, want := r.FirstSubmitted, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("FirstSubmitted = %s, want %s", got, want)
	}
	if !r.Submitted.Equal(r.FirstSubmitted) {
		t.Errorf("a one version paper has Submitted %s and FirstSubmitted %s", r.Submitted, r.FirstSubmitted)
	}
	if r.AnnouncedMonth != "July 2026" {
		t.Errorf("AnnouncedMonth = %q", r.AnnouncedMonth)
	}
	if len(r.Hits) == 0 {
		t.Error("no highlighted terms were recorded")
	}
}

// TestParseS5TwoDates checks the stamp line on a paper with more than one
// version, where the page prints the current date and the v1 date and the two
// are not the same thing.
func TestParseS5TwoDates(t *testing.T) {
	page, err := parseS5(fixture(t, "search_all_attention.html"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range page.Results {
		if r.Submitted.Equal(r.FirstSubmitted) {
			continue
		}
		found = true
		if r.Submitted.Before(r.FirstSubmitted) {
			t.Errorf("%s: the current version %s is older than v1 %s", r.ID, r.Submitted, r.FirstSubmitted)
		}
	}
	if !found {
		t.Fatal("no result on the page had a v1 line, so the two date case went unchecked")
	}
}

// TestParseS5Metadata checks the labelled rows, which all share one class and
// are told apart by the label alone.
func TestParseS5Metadata(t *testing.T) {
	page, err := parseS5(fixture(t, "search_msc_18D10.html"))
	if err != nil {
		t.Fatal(err)
	}
	var msc, comments, jref int
	for _, r := range page.Results {
		if len(r.MSCClass) > 0 {
			msc++
		}
		if r.Comment != "" {
			comments++
		}
		if r.JournalRef != "" {
			jref++
		}
	}
	// Counted in the saved page: every result of an msc_class search carries
	// one, twenty carry a comment and one carries a journal reference.
	if msc != 25 {
		t.Errorf("%d results carry an MSC class, want 25", msc)
	}
	if comments != 20 {
		t.Errorf("%d results carry a comment, want 20", comments)
	}
	if jref != 1 {
		t.Errorf("%d results carry a journal reference, want 1", jref)
	}
	for _, r := range page.Results {
		for _, class := range r.MSCClass {
			if strings.HasPrefix(class, "MSC") {
				t.Errorf("%s kept its label in the value: %q", r.ID, class)
			}
			// A value that opens a bracket and never closes it is a class that
			// was split through its own secondary list.
			if strings.Count(class, "(") != strings.Count(class, ")") {
				t.Errorf("%s has a half bracketed class: %q", r.ID, class)
			}
		}
	}
	// 2606.27343 is the record that found the bug: one primary class with a
	// bracketed secondary list, which is one class and not three.
	for _, r := range page.Results {
		if r.ID != "2606.27343" {
			continue
		}
		if len(r.MSCClass) != 1 || r.MSCClass[0] != "18D10 (16T05; 16T15; 18D10)" {
			t.Errorf("2606.27343 MSC = %q", r.MSCClass)
		}
	}
}

func TestS5ToPaper(t *testing.T) {
	page, err := parseS5(fixture(t, "search_all_attention.html"))
	if err != nil {
		t.Fatal(err)
	}
	const source = "https://arxiv.org/search/?searchtype=all&query=attention"
	p := s5ToPaper(page.Results[0], source, testTime)

	if p.Kind != "paper" {
		t.Errorf("Kind = %q", p.Kind)
	}
	if len(p.Surfaces) != 1 || p.Surfaces[0] != SurfaceSearch {
		t.Errorf("Surfaces = %v, want just s5", p.Surfaces)
	}
	if len(p.Sources) != 1 || p.Sources[0] != source {
		t.Errorf("Sources = %v", p.Sources)
	}
	if p.VersionedID != "2607.23678v1" {
		t.Errorf("VersionedID = %q", p.VersionedID)
	}
	if p.DOI != "10.48550/arXiv.2607.23678" {
		t.Errorf("DOI = %q", p.DOI)
	}
	// The page links to the bare id and the record describes a version, so the
	// links carry the version the rest of the record is about.
	if p.URL != "https://arxiv.org/abs/2607.23678v1" {
		t.Errorf("URL = %q", p.URL)
	}
	if p.PDFURL != "https://arxiv.org/pdf/2607.23678v1" {
		t.Errorf("PDFURL = %q", p.PDFURL)
	}
	if p.PrimaryCategory != "cs.AI" {
		t.Errorf("PrimaryCategory = %q", p.PrimaryCategory)
	}
	if p.Via["first_submitted"] != SurfaceSearch {
		t.Errorf("via first_submitted = %q, want s5", p.Via["first_submitted"])
	}
	if p.AuthorLine == "" {
		t.Error("AuthorLine is empty, so the table would print a bare count")
	}
	if len(p.Missed) == 0 {
		t.Error("a record that read one surface should say what it did not read")
	}
	// The record must not claim a precision the page does not have.
	if h, m := p.FirstSubmitted.Hour(), p.FirstSubmitted.Minute(); h != 0 || m != 0 {
		t.Errorf("FirstSubmitted = %s, but the page gives a day and no time", p.FirstSubmitted)
	}
}

func TestS5URL(t *testing.T) {
	cases := []struct {
		name string
		opts SearchOptions
		want []string
		gone []string
	}{
		{
			name: "one s5 only field goes to the advanced form",
			opts: SearchOptions{ORCID: "0000-0002-0609-9836"},
			want: []string{s5Advanced, "terms-0-field=orcid", "terms-0-term=0000-0002-0609-9836", "terms-0-operator=AND"},
		},
		{
			name: "the shared fields ride along in their own rows",
			opts: SearchOptions{Title: "categories", MSCClass: "18D10"},
			want: []string{"terms-0-field=title", "terms-1-field=msc_class"},
		},
		{
			name: "a licence goes through the single field form",
			opts: SearchOptions{License: "http://creativecommons.org/licenses/by/4.0/"},
			want: []string{s5Base + "?", "searchtype=license"},
			gone: []string{"terms-0-field"},
		},
		{
			name: "the to bound is pushed on a day because arXiv excludes it",
			opts: SearchOptions{DOI: "10.1000/x", From: "2026-01", To: "2026-01"},
			want: []string{"date-from_date=2026-01-01", "date-to_date=2026-02-01", "date-filter_by=date_range"},
		},
		{
			name: "submitted ascending is the form's own value",
			opts: SearchOptions{ORCID: "0000-0002-0609-9836", Sort: "submitted", Order: "asc"},
			want: []string{"order=submitted_date"},
		},
		{
			name: "relevance is the form's empty order, so it is not sent",
			opts: SearchOptions{ORCID: "0000-0002-0609-9836", Sort: "relevance"},
			gone: []string{"order="},
		},
	}
	for _, tc := range cases {
		p, err := buildSearch(tc.opts)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if p.HTML == nil {
			t.Errorf("%s: the query did not route to the search UI", tc.name)
			continue
		}
		u := p.HTML.URL(0, 50)
		for _, want := range tc.want {
			if !strings.Contains(u, want) {
				t.Errorf("%s: %s does not contain %s", tc.name, u, want)
			}
		}
		for _, gone := range tc.gone {
			if strings.Contains(u, gone) {
				t.Errorf("%s: %s still contains %s", tc.name, u, gone)
			}
		}
	}
}

// TestS5Routing checks which queries end up on which plane. A query that could
// be answered by the API must never be sent here, because a read here costs
// fifteen seconds and a quarter of a megabyte.
func TestS5Routing(t *testing.T) {
	for _, tc := range []struct {
		opts SearchOptions
		html bool
	}{
		{SearchOptions{Query: "attention"}, false},
		{SearchOptions{Categories: []string{"cs.CL"}}, false},
		{SearchOptions{Report: "CERN-PH-EP-2012-218"}, false},
		{SearchOptions{ACMClass: "I.2.7"}, true},
		{SearchOptions{MSCClass: "18D10"}, true},
		{SearchOptions{DOI: "10.1016/j.physletb.2012.08.020"}, true},
		{SearchOptions{ORCID: "0000-0002-0609-9836"}, true},
		{SearchOptions{AuthorID: "baez_j_1"}, true},
		{SearchOptions{License: "http://creativecommons.org/licenses/by/4.0/"}, true},
		{SearchOptions{Query: "attention", ORCID: "0000-0002-0609-9836"}, true},
	} {
		p, err := buildSearch(tc.opts)
		if err != nil {
			t.Errorf("%#v: %v", tc.opts, err)
			continue
		}
		if got := p.HTML != nil; got != tc.html {
			t.Errorf("%#v: routed to the search UI = %v, want %v", tc.opts, got, tc.html)
		}
	}
}

// TestS5UsageErrors is the list of things arXiv's own UI cannot do either. Each
// message has to say which, because a refusal with no reason gets retried.
func TestS5UsageErrors(t *testing.T) {
	cases := []struct {
		name string
		opts SearchOptions
		want string
	}{
		{
			"full text moved to a host that says no",
			SearchOptions{FullText: "quantum annealing"},
			"robots.txt",
		},
		{
			"a category means something narrower here",
			SearchOptions{ORCID: "0000-0002-0609-9836", Categories: []string{"cs.CL"}},
			"cross listed",
		},
		{
			"raw is the other plane's grammar",
			SearchOptions{ORCID: "0000-0002-0609-9836", Raw: "all:attention"},
			"cannot be combined",
		},
		{
			"there is no last updated filter",
			SearchOptions{ORCID: "0000-0002-0609-9836", UpdatedFrom: "2026-01"},
			"submission and announcement dates only",
		},
		{
			"there is no last updated ordering",
			SearchOptions{ORCID: "0000-0002-0609-9836", Sort: "updated"},
			"no last updated ordering",
		},
		{
			"the licence field is on its own form",
			SearchOptions{License: "http://creativecommons.org/licenses/by/4.0/", Title: "attention"},
			"single field form",
		},
		{
			"a walk here still needs a bound",
			SearchOptions{ORCID: "0000-0002-0609-9836", Sort: "submitted", All: true},
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
		// The same rule the API side has: the surface capitalises the first
		// word, so it has to be a word.
		if first := strings.Fields(err.Error())[0]; strings.HasPrefix(first, "-") || strings.HasPrefix(first, `"`) {
			t.Errorf("%s: error starts with %q", tc.name, first)
		}
	}
}

// TestS5AllTakesADateBound checks the other half of the bound rule: --cat is
// refused on this plane, so a date range or a limit has to be enough.
func TestS5AllTakesADateBound(t *testing.T) {
	for _, o := range []SearchOptions{
		{ORCID: "0000-0002-0609-9836", From: "2026", To: "2026", All: true},
		{ORCID: "0000-0002-0609-9836", Limit: 300, All: true},
	} {
		if _, err := buildSearch(o); err != nil {
			t.Errorf("%#v was refused: %v", o, err)
		}
	}
}

func TestS5Size(t *testing.T) {
	for _, tc := range []struct{ limit, want int }{
		{0, 200},
		{1, 25},
		{10, 25},
		{25, 25},
		{26, 50},
		{150, 200},
		{5000, 200},
	} {
		if got := s5Size(tc.limit); got != tc.want {
			t.Errorf("s5Size(%d) = %d, want %d", tc.limit, got, tc.want)
		}
	}
}

func TestS5PlanLine(t *testing.T) {
	got := s5PlanLine(130, 50)
	for _, want := range []string{"130 results", "3 requests", "45s"} {
		if !strings.Contains(got, want) {
			t.Errorf("plan line %q does not mention %q", got, want)
		}
	}
	// A result set past the window has to say what it cannot reach.
	big := s5PlanLine(565813, 200)
	if !strings.Contains(big, "cannot be reached") {
		t.Errorf("plan line for a set past the window says nothing about it: %q", big)
	}
}

func TestS5QueryString(t *testing.T) {
	p, err := buildSearch(SearchOptions{MSCClass: "18D10", Title: "categories", From: "2026-01", To: "2026-01"})
	if err != nil {
		t.Fatal(err)
	}
	got := p.HTML.String()
	for _, want := range []string{"title:categories", "msc_class:18D10", "submitted_date:[2026-01-01 TO 2026-02-01)"} {
		if !strings.Contains(got, want) {
			t.Errorf("query line %q does not mention %q", got, want)
		}
	}
}
