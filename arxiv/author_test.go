package arxiv

import (
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit/errs"
)

// The fixture is the real identifier page for baez_j_1, saved 2026-08-14:
// 147,012 bytes, 125 papers, 42 of them under old style ids, and the ORCID that
// is the reason the page is worth a request.

func authorFixture(t *testing.T) *authorPage {
	t.Helper()
	page, err := parseAuthorPage(fixture(t, "a_baez_j_1.html"))
	if err != nil {
		t.Fatalf("parse author page: %v", err)
	}
	return page
}

func TestParseAuthorPage(t *testing.T) {
	page := authorFixture(t)
	if page.Name != "John Baez" {
		t.Errorf("Name: got %q, want the name without the page's own wording round it", page.Name)
	}
	if page.ORCID != "0000-0002-0609-9836" {
		t.Errorf("ORCID: got %q", page.ORCID)
	}
	if len(page.Rows) != 125 {
		t.Fatalf("%d rows, want 125", len(page.Rows))
	}

	first := page.Rows[0]
	if first.ID != "2608.06271" || first.Title != "Three Generations in E7" {
		t.Errorf("first row: got %q %q", first.ID, first.Title)
	}
	if first.Comment != "17 pages LaTeX with TikZ figures" {
		t.Errorf("Comment: got %q", first.Comment)
	}
	if len(first.Authors) != 1 || first.Authors[0] != "John C. Baez" {
		t.Errorf("Authors: got %v", first.Authors)
	}
	if first.PrimaryCategory != "math-ph" || first.SubjectNames["math-ph"] != "Mathematical Physics" {
		t.Errorf("subjects: got %q %v", first.PrimaryCategory, first.SubjectNames)
	}
	// The author list on this page carries a descriptor and the category
	// listing's does not, and the same parser reads both, so the label must not
	// end up in the names.
	for _, name := range first.Authors {
		if strings.Contains(name, "Authors") {
			t.Errorf("the descriptor got into the author list: %q", name)
		}
	}
}

// TestAuthorPageRowsAreListingRows is the reason there is no second row parser:
// the identifier page's rows are the category listing's rows, wrapped in a div.
func TestAuthorPageRowsAreListingRows(t *testing.T) {
	page := authorFixture(t)

	second := page.Rows[1]
	if second.JournalRef != "Appl. Categor. Struct., 34 (2026), 46" {
		t.Errorf("Journal-ref: got %q", second.JournalRef)
	}

	third := page.Rows[2]
	if len(third.Authors) != 3 || third.Authors[2] != "Latham Boyle" {
		t.Errorf("three authors: got %v", third.Authors)
	}
	if len(third.Categories) != 4 || third.PrimaryCategory != "math-ph" {
		t.Errorf("subjects: got %v primary %q", third.Categories, third.PrimaryCategory)
	}
	if got := crossLists(third.Categories, third.PrimaryCategory); len(got) != 3 {
		t.Errorf("cross lists: got %v", got)
	}
}

// TestAuthorPageOldStyleIDs: a career that started in 1992 is half old style
// ids, and hep-th/9205007 has to come out as an id and not as a broken one.
func TestAuthorPageOldStyleIDs(t *testing.T) {
	page := authorFixture(t)
	var old int
	for _, row := range page.Rows {
		if !strings.Contains(row.ID, "/") {
			continue
		}
		old++
		p := listToPaper(row, SurfaceAuthorID, "https://arxiv.org/a/baez_j_1.html", testTime)
		if p.Style != "old" {
			t.Errorf("%s: Style %q", row.ID, p.Style)
		}
		if p.OAIID != "oai:arXiv.org:"+row.ID {
			t.Errorf("%s: OAIID %q", row.ID, p.OAIID)
		}
	}
	if old != 42 {
		t.Errorf("%d old style ids, want 42", old)
	}

	last := page.Rows[len(page.Rows)-1]
	if last.ID != "hep-th/9205007" || last.JournalRef != "Class.Quant.Grav.10:673-694,1993" {
		t.Errorf("last row: got %q %q", last.ID, last.JournalRef)
	}
}

func TestEveryAuthorRowParses(t *testing.T) {
	for _, row := range authorFixture(t).Rows {
		if row.ID == "" || row.Title == "" || len(row.Authors) == 0 || row.PrimaryCategory == "" {
			t.Errorf("%+v is missing something every row has", row)
		}
		// No row on this page offers an html rendering, and the html link is
		// the only element that carries a version, so no version is claimed.
		if row.Version != 0 {
			t.Errorf("%s claims version %d with no html link to read it off", row.ID, row.Version)
		}
	}
}

// TestNameAndPersonAreDifferentSpaces is the rule doc 04 section 2 is built on.
// Merging them would put two physicists named Wang in one node, and it would
// not be undoable.
func TestNameAndPersonAreDifferentSpaces(t *testing.T) {
	name := NameURI("John Baez")
	person := AuthorURI("baez_j_1")
	if name == person {
		t.Fatal("a name and a person landed on one node")
	}
	if name != "ax://name/john-baez" {
		t.Errorf("NameURI: got %q", name)
	}
	if person != "ax://author/baez_j_1" {
		t.Errorf("AuthorURI: got %q", person)
	}
	if got := ORCIDURI("0000-0002-0609-9836"); got != "ax://orcid/0000-0002-0609-9836" {
		t.Errorf("ORCIDURI: got %q", got)
	}
}

func TestNameSlug(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Aidan N. Gomez", "aidan-n-gomez"},
		{"  John   Baez  ", "john-baez"},
		{"John C. Baez", "john-c-baez"},
		// The whole value of a name node is that two spellings of one name land
		// together, so the accent goes and the letter stays.
		{"Paul Erdős", "paul-erdos"},
		{"Erdos, Paul", "erdos-paul"},
		{"Jean-Pierre Serre", "jean-pierre-serre"},
		{"", ""},
	} {
		if got := nameSlug(tc.in); got != tc.want {
			t.Errorf("nameSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormaliseAuthorID(t *testing.T) {
	for _, in := range []string{
		"baez_j_1",
		"BAEZ_J_1",
		" baez_j_1 ",
		"baez_j_1.html",
		"https://arxiv.org/a/baez_j_1",
		"https://arxiv.org/a/baez_j_1.html",
	} {
		got, err := normaliseAuthorID(in)
		if err != nil || got != "baez_j_1" {
			t.Errorf("normaliseAuthorID(%q) = %q, %v", in, got, err)
		}
	}
	// A name is not an identifier and there is no way to turn one into the
	// other, so this is refused rather than guessed at.
	for _, in := range []string{"John Baez", "", "baez", "1706.03762"} {
		if _, err := normaliseAuthorID(in); err == nil {
			t.Errorf("normaliseAuthorID(%q) was accepted", in)
		}
	}
}

// TestAuthorNotFoundSaysWhatA404Means: the identifier page is opt-in, so its
// absence is not a statement about the author.
func TestAuthorNotFoundSaysWhatA404Means(t *testing.T) {
	err := authorNotFound("baez_j_9")
	if errs.KindOf(err) != errs.KindNotFound {
		t.Errorf("kind: got %v", errs.KindOf(err))
	}
	msg := err.Error()
	if !strings.Contains(msg, "opt-in") || !strings.Contains(msg, "baez_j_9") {
		t.Errorf("message: got %q", msg)
	}
	if strings.Contains(msg, "no such author") {
		t.Errorf("the message claims the author does not exist: %q", msg)
	}
}

func TestAuthorURL(t *testing.T) {
	if got := authorURL("baez_j_1"); got != "https://arxiv.org/a/baez_j_1.html" {
		t.Errorf("got %s", got)
	}
	// The page is on arxiv.org, so it is a fifteen second read and not a three
	// second one.
	plane, ok := PlaneFor(Host)
	if !ok || plane.Name != HTMLPlane.Name {
		t.Errorf("the identifier page is on the %q plane", plane.Name)
	}
}

// TestAuthorPagePapersSayWhereTheyCameFrom: the row shape is shared with the
// category listing and the record has to name the page it was actually read
// from, or the provenance points at a page that never had the row on it.
func TestAuthorPagePapersSayWhereTheyCameFrom(t *testing.T) {
	page := authorFixture(t)
	u := authorURL("baez_j_1")
	p := listToPaper(page.Rows[1], SurfaceAuthorID, u, testTime)

	if len(p.Surfaces) != 1 || p.Surfaces[0] != SurfaceAuthorID {
		t.Errorf("Surfaces: got %v", p.Surfaces)
	}
	if len(p.Sources) != 1 || p.Sources[0] != u {
		t.Errorf("Sources: got %v", p.Sources)
	}
	for field, surface := range p.Via {
		if surface != SurfaceAuthorID {
			t.Errorf("via[%s] = %q", field, surface)
		}
	}
	for _, a := range p.Authors {
		if a.Via != SurfaceAuthorID {
			t.Errorf("author %q came via %q", a.Name, a.Via)
		}
	}
}
