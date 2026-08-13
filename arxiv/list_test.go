package arxiv

import (
	"strings"
	"testing"
	"time"
)

// The three listing fixtures are real pages saved on 2026-08-13: a month of
// cs.CL, the same month of math.AG for the cross listed subject lines, and the
// recent listing for the day headings. A hand written listing would only prove
// the parser handles the rows its author already knew about.

func listFixture(t *testing.T, name string) *listPage {
	t.Helper()
	page, err := parseList(fixture(t, name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return page
}

func TestParseListMonth(t *testing.T) {
	page := listFixture(t, "list_cs.CL_2026-01.html")

	if page.Name != "Computation and Language" {
		t.Errorf("Name: got %q", page.Name)
	}
	// The total is the whole month and the page is fifty rows of it, which is
	// the pair that drives every paging decision.
	if page.Total != 2168 {
		t.Errorf("Total: got %d, want 2168", page.Total)
	}
	if len(page.Rows) != 50 {
		t.Fatalf("%d rows, want 50", len(page.Rows))
	}

	r := page.Rows[0]
	if r.ID != "2601.00086" {
		t.Errorf("ID: got %q", r.ID)
	}
	// The version is on the html link and nowhere else on the row. The
	// abstract link is bare.
	if r.Version != 3 {
		t.Errorf("Version: got %d, want 3 off the html link", r.Version)
	}
	if !r.HasHTML || r.HTMLURL != "https://arxiv.org/html/2601.00086v3" {
		t.Errorf("html: got %v %q", r.HasHTML, r.HTMLURL)
	}
	if r.AbsURL != "https://arxiv.org/abs/2601.00086" || r.PDFURL != "https://arxiv.org/pdf/2601.00086" {
		t.Errorf("links: got %q and %q", r.AbsURL, r.PDFURL)
	}
	if r.Title != "RIMRULE: Improving Tool-Using Language Agents via MDL-Guided Rule Learning" {
		t.Errorf("Title: got %q", r.Title)
	}
	if len(r.Authors) != 8 || r.Authors[0] != "Xiang Gao" || r.Authors[7] != "Kamalika Das" {
		t.Errorf("Authors: got %v", r.Authors)
	}
	if r.Comment != "Published as a long paper in the main conference of ACL 2026" {
		t.Errorf("Comment: got %q", r.Comment)
	}
	if r.PrimaryCategory != "cs.CL" || len(r.Categories) != 1 {
		t.Errorf("subjects: got %q %v", r.PrimaryCategory, r.Categories)
	}
	if r.SubjectNames["cs.CL"] != "Computation and Language" {
		t.Errorf("SubjectNames: got %v", r.SubjectNames)
	}

	// The labels on this page are Title, Subjects, Comments and Journal-ref,
	// and only two of the four are on every row. A parser that assumed a fixed
	// set of divs would read the comment of one row as the journal reference
	// of another.
	var comments, journals int
	for _, row := range page.Rows {
		if row.ID == "" || row.Title == "" || len(row.Authors) == 0 {
			t.Errorf("%+v is missing an id, a title or its authors", row)
		}
		if row.Comment != "" {
			comments++
		}
		if row.JournalRef != "" {
			journals++
		}
		if len(row.Extra) != 0 {
			t.Errorf("%s put %v in extra, so this page has a label the parser should be reading", row.ID, row.Extra)
		}
	}
	if comments != 26 {
		t.Errorf("%d rows carry a comment, want 26", comments)
	}
	if journals != 1 {
		t.Errorf("%d rows carry a journal reference, want 1", journals)
	}
}

// TestListJournalRef is the one row on the page that has one, and it is here
// because the descriptor is spelled Journal-ref and not Journal ref.
func TestListJournalRef(t *testing.T) {
	for _, r := range listFixture(t, "list_cs.CL_2026-01.html").Rows {
		if r.ID != "2601.00348" {
			continue
		}
		want := "2025 International Joint Conference on Neural Networks (IJCNN), Rome, Italy, 2025, pp. 1-9"
		if r.JournalRef != want {
			t.Errorf("JournalRef: got %q, want %q", r.JournalRef, want)
		}
		return
	}
	t.Error("2601.00348 is not on the page")
}

// TestListRowWithoutHTML pins the row that has no html link, because the
// version is read off that link and a missing one is a version we do not know
// rather than version zero of something.
func TestListRowWithoutHTML(t *testing.T) {
	for _, r := range listFixture(t, "list_cs.CL_2026-01.html").Rows {
		if r.ID != "2601.00224" {
			continue
		}
		if r.HasHTML || r.HTMLURL != "" {
			t.Errorf("%s has no html link on the page: got %v %q", r.ID, r.HasHTML, r.HTMLURL)
		}
		if r.Version != 0 {
			t.Errorf("Version: got %d, want zero, because nothing on the row states one", r.Version)
		}
		return
	}
	t.Error("2601.00224 is not on the page")
}

// TestParseListCrossListedSubjects is the math.AG page, where 18 of 50 rows are
// cross listed and the subject line is a semicolon separated list.
func TestParseListCrossListedSubjects(t *testing.T) {
	page := listFixture(t, "list_math.AG_2026-01.html")
	if page.Total != 296 {
		t.Errorf("Total: got %d, want 296", page.Total)
	}

	var multi int
	for _, r := range page.Rows {
		if len(r.Categories) > 1 {
			multi++
		}
		if r.PrimaryCategory != "math.AG" {
			t.Errorf("%s has primary %q on the math.AG listing", r.ID, r.PrimaryCategory)
		}
		for _, code := range r.Categories {
			if r.SubjectNames[code] == "" {
				t.Errorf("%s has no name for %s", r.ID, code)
			}
		}
	}
	if multi != 18 {
		t.Errorf("%d rows are cross listed, want 18", multi)
	}

	r := page.Rows[0]
	if r.ID != "2601.00033" {
		t.Fatalf("first row is %s", r.ID)
	}
	if r.SubjectNames["math.AG"] != "Algebraic Geometry" {
		t.Errorf("SubjectNames: got %v", r.SubjectNames)
	}

	// A three subject row: the primary is marked up as its own span and the
	// rest follow it, so the primary is read from the markup rather than from
	// the position in the list.
	var three *listRow
	for i := range page.Rows {
		if len(page.Rows[i].Categories) == 3 {
			three = &page.Rows[i]
			break
		}
	}
	if three == nil {
		t.Fatal("no row on the page carries three subjects")
	}
	if three.Categories[0] != "math.AG" || len(crossLists(three.Categories, three.PrimaryCategory)) != 2 {
		t.Errorf("subjects: got %v", three.Categories)
	}
}

func TestParseListRecent(t *testing.T) {
	page := listFixture(t, "list_cs.CL_recent.html")
	if page.Total != 528 {
		t.Errorf("Total: got %d, want 528", page.Total)
	}
	if len(page.Rows) != 50 {
		t.Fatalf("%d rows, want 50", len(page.Rows))
	}

	// The recent listing groups its rows under day headings, and that day is
	// the announcement date, which no other part of a listing publishes.
	want := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	for _, r := range page.Rows {
		if !r.Announced.Equal(want) {
			t.Fatalf("%s is announced %s, want %s", r.ID, r.Announced, want)
		}
	}
}

// TestListDayHeading reads the heading with the page's own paging note on the
// end of it, which is not part of the date.
func TestListDayHeading(t *testing.T) {
	got := listDay("Thu, 13 Aug 2026 (showing first 50 of 92 entries )")
	if want := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
	if !listDay("not a heading").IsZero() {
		t.Error("a heading that is not a date parsed as one")
	}
}

func TestListToPaper(t *testing.T) {
	page := listFixture(t, "list_cs.CL_2026-01.html")
	source := "https://arxiv.org/list/cs.CL/2026-01?skip=0&show=50"
	p := listToPaper(page.Rows[0], source, testTime)

	if p.Kind != "paper" || p.ID != "2601.00086" || p.Version != 3 {
		t.Errorf("identity: got %+v", p)
	}
	if p.VersionedID != "2601.00086v3" {
		t.Errorf("VersionedID: got %q", p.VersionedID)
	}
	if p.OAIID != "oai:arXiv.org:2601.00086" || p.DOI != "10.48550/arXiv.2601.00086" {
		t.Errorf("derived ids: got %q and %q", p.OAIID, p.DOI)
	}
	if len(p.Surfaces) != 1 || p.Surfaces[0] != SurfaceList || p.Sources[0] != source {
		t.Errorf("envelope: got %v %v", p.Surfaces, p.Sources)
	}
	if p.Via["title"] != SurfaceList || p.Via["categories"] != SurfaceList {
		t.Errorf("via: got %v", p.Via)
	}
	if p.AuthorLine != "Xiang Gao, Yuguang Yao, Qi Zhang, and 5 more" {
		t.Errorf("AuthorLine: got %q", p.AuthorLine)
	}
	if p.Authors[0].Via != SurfaceList {
		t.Errorf("author via: got %q", p.Authors[0].Via)
	}
	// A listing row is not a paper read and the record says so, naming the
	// command that would fill the gap.
	if len(p.Missed) != 2 {
		t.Fatalf("Missed: got %v", p.Missed)
	}
	for _, s := range p.Missed {
		if !strings.Contains(s, "arxiv paper 2601.00086") {
			t.Errorf("a missed sentence names no way to fix it: %q", s)
		}
	}
	if p.Abstract != "" {
		t.Error("the listing has no abstract on it, so a record built from one must not carry it")
	}
	// The monthly listing is by the id's month, which is when the paper was
	// registered rather than when it was announced, so nothing is claimed.
	if !p.Announced.IsZero() {
		t.Errorf("Announced: got %s from a monthly listing, which does not publish one", p.Announced)
	}
}

// TestListToPaperCarriesTheAnnouncementDay is the recent listing's one extra
// fact.
func TestListToPaperCarriesTheAnnouncementDay(t *testing.T) {
	page := listFixture(t, "list_cs.CL_recent.html")
	p := listToPaper(page.Rows[0], "https://arxiv.org/list/cs.CL/recent?skip=0&show=50", testTime)
	if p.Announced.IsZero() || p.Via["announced"] != SurfaceList {
		t.Errorf("Announced: got %s via %q", p.Announced, p.Via["announced"])
	}
}

func TestListOptionsValidate(t *testing.T) {
	// The short month form is what older guides document and what the id
	// itself uses, and arXiv answers it with a 404, so the refusal says what
	// to type instead.
	o := ListOptions{Category: "cs.CL", Month: "2601"}
	err := o.validate()
	if err == nil || !strings.Contains(err.Error(), "2026-01") {
		t.Errorf("2601: got %v", err)
	}

	o = ListOptions{Category: "cs.CL", Month: "January"}
	if err := o.validate(); err == nil {
		t.Error("January was accepted as a month")
	}

	// arXiv publishes the sizes it takes in its own 400 body, so a size it
	// would refuse is refused here, one request earlier.
	o = ListOptions{Category: "cs.CL", Month: "2026-01", Show: 7}
	err = o.validate()
	if err == nil || !strings.Contains(err.Error(), "2000") {
		t.Errorf("--show 7: got %v", err)
	}

	o = ListOptions{Category: "cs.NOPE", Month: "2026-01"}
	if err := o.validate(); err == nil {
		t.Error("a category that does not exist was accepted")
	}

	o = ListOptions{Category: "cs.CL", Month: "2026-01", Recent: true}
	if err := o.validate(); err == nil {
		t.Error("a month and --recent were both accepted, and they are two different pages")
	}

	// The defaults: fifty rows the way arXiv's own page does it, and the
	// biggest page it offers for a walk, because a month of cs.CL is two
	// requests at 2000 and eighty seven at 25.
	o = ListOptions{Category: "cs.CL", Month: "2026-01"}
	if err := o.validate(); err != nil || o.Show != listDefaultShow {
		t.Errorf("default show: got %d, %v", o.Show, err)
	}
	o = ListOptions{Category: "cs.CL", Month: "2026-01", All: true}
	if err := o.validate(); err != nil || o.Show != listWalkShow {
		t.Errorf("walk show: got %d, %v", o.Show, err)
	}
}

func TestListURL(t *testing.T) {
	o := ListOptions{Category: "cs.CL", Month: "2026-01"}
	if got := o.URL(50, 25); got != "https://arxiv.org/list/cs.CL/2026-01?skip=50&show=25" {
		t.Errorf("got %s", got)
	}
	// No month is the recent listing, which is the same page arXiv links to
	// from the category header.
	o = ListOptions{Category: "hep-th"}
	if got := o.URL(0, 50); got != "https://arxiv.org/list/hep-th/recent?skip=0&show=50" {
		t.Errorf("got %s", got)
	}
}

func TestListPlanLine(t *testing.T) {
	// 2168 entries at 2000 a page is two requests, so half a minute rather
	// than the twenty minutes a fifty row page would cost.
	got := listPlanLine(2168, 2000)
	if !strings.Contains(got, "2168 entries") || !strings.Contains(got, "2 requests") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(listPlanLine(0, 2000), "1 request ") {
		t.Errorf("an empty listing still costs the request that found that out: %q", listPlanLine(0, 2000))
	}
}
