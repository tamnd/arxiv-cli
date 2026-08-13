package arxiv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/latexml"
)

// renderingFixture reads a saved LaTeXML page.
//
// The pages live under pkg/latexml/testdata because that is the package that
// reads them, and one of them is three quarters of a megabyte, which is not
// worth keeping two copies of.
func renderingFixture(t *testing.T, name string) *latexml.Document {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "pkg", "latexml", "testdata", name))
	if err != nil {
		t.Fatalf("read rendering: %v", err)
	}
	doc, err := latexml.Parse(body)
	if err != nil {
		t.Fatalf("parse rendering: %v", err)
	}
	return doc
}

// sectorPaper is the metadata record the rendering of 2401.00001v1 folds into,
// with the two fields the abstract page answers for.
func sectorPaper() Paper {
	p := Paper{
		ID:      "2401.00001",
		Version: 1,
		Title:   "Sector Rotation by Factor Model and Fundamental Analysis",
		Authors: []Author{
			{Name: "Runjia Yang", Via: SurfaceAPI},
			{Name: "Beining Shi", Via: SurfaceAPI},
		},
		HasHTML: true,
		HTMLURL: "https://arxiv.org/html/2401.00001v1",
	}
	p.addSurface(SurfaceAPI, "https://export.arxiv.org/api/query")
	p.addSurface(SurfaceAbs, "https://arxiv.org/abs/2401.00001")
	return p
}

func TestFullTextFromRendering(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)

	if full.Kind != "fulltext" {
		t.Errorf("kind: got %q", full.Kind)
	}
	if full.PaperID != "2401.00001" || full.Version != 1 {
		t.Errorf("identity: got %s v%d", full.PaperID, full.Version)
	}
	if full.Title != "Sector Rotation by Factor Model and Fundamental Analysis" {
		t.Errorf("title: got %q", full.Title)
	}
	if full.LicenseName != "CC Zero" {
		t.Errorf("license name: got %q", full.LicenseName)
	}
	if full.Stamp != "arXiv:2401.00001v1 [q-fin.PM] 18 Nov 2023" {
		t.Errorf("stamp: got %q", full.Stamp)
	}
	if full.Dates != "Sept 2023" {
		t.Errorf("dates: got %q", full.Dates)
	}
	if !strings.HasPrefix(full.Abstract, "This study presents an analytical approach") {
		t.Errorf("abstract: got %.50q", full.Abstract)
	}
	// Six top level sections, and twenty five headings once the subsections and
	// subsubsections are counted.
	if len(full.Sections) != 6 || full.SectionCount != 25 {
		t.Errorf("sections: got %d top level, %d in all", len(full.Sections), full.SectionCount)
	}
	if full.Words < 3000 {
		t.Errorf("words: got %d, the paper is longer than that", full.Words)
	}
}

func TestFullTextSaysWhereItCameFrom(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	source := "https://arxiv.org/html/2401.00001v1"
	full := fullTextFrom(doc, sectorPaper(), source, testTime)

	// The record keeps the reads that got it here, because the rendering alone
	// does not say whether the paper exists and the metadata reads do.
	want := []string{SurfaceAPI, SurfaceAbs, SurfaceFullText}
	if strings.Join(full.Surfaces, ",") != strings.Join(want, ",") {
		t.Errorf("surfaces: got %v, want %v", full.Surfaces, want)
	}
	if !contains(full.Sources, source) {
		t.Errorf("sources: %v does not have the rendering in it", full.Sources)
	}
	for _, field := range []string{"sections", "affiliations", "license_name", "stamp"} {
		if full.Via[field] != SurfaceFullText {
			t.Errorf("via[%s]: got %q, want %s", field, full.Via[field], SurfaceFullText)
		}
	}
}

func TestAffiliationsAreOnTheRecord(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)

	if len(full.Authors) != 2 {
		t.Fatalf("authors: got %d", len(full.Authors))
	}
	for _, a := range full.Authors {
		if a.Affiliation != "University of California, Davis" {
			t.Errorf("%s: affiliation %q", a.Name, a.Affiliation)
		}
		if a.Via != SurfaceFullText {
			t.Errorf("%s: via %q", a.Name, a.Via)
		}
	}
}

func TestAProseBibliographySaysSoInMissed(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)

	if len(full.References) != 0 {
		t.Fatalf("references: got %d, this rendering has none", len(full.References))
	}
	if len(full.Missed) != 1 || !strings.Contains(full.Missed[0], "references as prose") {
		t.Errorf("missed: got %v", full.Missed)
	}
}

func TestReferencesBecomeSomethingToFollow(t *testing.T) {
	doc := renderingFixture(t, "html_2601.00086v3.html")
	p := Paper{ID: "2601.00086", Version: 3, HasHTML: true}
	full := fullTextFrom(doc, p, "https://arxiv.org/html/2601.00086v3", testTime)

	if len(full.References) != 46 {
		t.Fatalf("references: got %d, want 46", len(full.References))
	}
	first := full.References[0]
	if first.ID != "bib.bib38" || first.Title != "Qwen technical report" {
		t.Errorf("first reference: got %+v", first)
	}
	// A link to an abs page is a citation this tool can go and read, so the id
	// comes out of the URL rather than being left in it.
	if first.ArxivID != "2309.16609" {
		t.Errorf("arxiv id: got %q", first.ArxivID)
	}
	cited := 0
	for _, r := range full.References {
		if r.ArxivID != "" {
			cited++
		}
	}
	if cited != 21 {
		t.Errorf("references with an arXiv id: got %d, want 21", cited)
	}
	if len(full.Missed) != 0 {
		t.Errorf("missed: got %v", full.Missed)
	}
}

func TestIdentifiersIn(t *testing.T) {
	cases := []struct {
		name       string
		links      []string
		arxiv, doi string
	}{
		{"an abs link", []string{"https://arxiv.org/abs/2309.16609"}, "2309.16609", ""},
		{"a versioned abs link", []string{"https://arxiv.org/abs/2312.11805v2"}, "2312.11805", ""},
		{"an old style id", []string{"https://arxiv.org/abs/hep-th/9711200"}, "hep-th/9711200", ""},
		{"a doi", []string{"https://doi.org/10.1038/nature14539"}, "", "10.1038/nature14539"},
		{"both", []string{"https://doi.org/10.1000/x", "https://arxiv.org/abs/2401.00001"}, "2401.00001", "10.1000/x"},
		{"neither", []string{"https://example.com/paper.pdf"}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotID, gotDOI := identifiersIn(c.links)
			if gotID != c.arxiv || gotDOI != c.doi {
				t.Errorf("got %q %q, want %q %q", gotID, gotDOI, c.arxiv, c.doi)
			}
		})
	}
}

func TestNarrowToSections(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)
	if err := narrow(&full, FullTextOptions{Sections: true}); err != nil {
		t.Fatalf("narrow: %v", err)
	}

	// A table of contents keeps every heading and none of the prose.
	if full.SectionCount != 25 {
		t.Errorf("sections: got %d", full.SectionCount)
	}
	var walk func([]Section)
	walk = func(sections []Section) {
		for _, s := range sections {
			if s.Text != "" {
				t.Errorf("%s still has its text", s.ID)
			}
			if s.Title == "" {
				t.Errorf("%s has no title", s.ID)
			}
			walk(s.Sections)
		}
	}
	walk(full.Sections)
}

func TestNarrowToOneSection(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)
	if err := narrow(&full, FullTextOptions{Section: "S3.SS1"}); err != nil {
		t.Fatalf("narrow: %v", err)
	}

	if len(full.Sections) != 1 || full.Sections[0].ID != "S3.SS1" {
		t.Fatalf("sections: got %+v", full.Sections)
	}
	// The children come with it, and the counts are recomputed so the record
	// describes what it now holds rather than what it held before.
	if full.SectionCount != 3 {
		t.Errorf("section count: got %d, want the subsection and its two children", full.SectionCount)
	}
	if full.Words == 0 {
		t.Error("words: got 0")
	}
}

func TestNarrowToASectionThatIsNotThere(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)

	err := narrow(&full, FullTextOptions{Section: "S99"})
	if err == nil {
		t.Fatal("S99 was accepted and it does not exist")
	}
	if errs.KindOf(err) != errs.KindNotFound {
		t.Errorf("kind: got %v", errs.KindOf(err))
	}
	if !strings.Contains(err.Error(), "--sections lists the ids") {
		t.Errorf("message: got %q", err.Error())
	}
}

func TestNarrowToReferences(t *testing.T) {
	doc := renderingFixture(t, "html_2601.00086v3.html")
	full := fullTextFrom(doc, Paper{ID: "2601.00086", Version: 3}, "https://arxiv.org/html/2601.00086v3", testTime)
	if err := narrow(&full, FullTextOptions{Refs: true}); err != nil {
		t.Fatalf("narrow: %v", err)
	}

	if len(full.References) != 46 {
		t.Errorf("references: got %d", len(full.References))
	}
	if full.Sections != nil || full.Abstract != "" {
		t.Error("the body came along with the bibliography")
	}
}

func TestPlainTextReadsInOrder(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)
	text := full.PlainText()

	order := []string{
		"Sector Rotation by Factor Model and Fundamental Analysis",
		"Runjia Yang, University of California, Davis",
		"Beining Shi, University of California, Davis",
		"Abstract",
		"This study presents an analytical approach",
		"Introduction",
		"Factor Analysis",
		"Momentum Factor Exploration",
		"Factor Construction",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(text[at:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order", want)
		}
		at += i
	}

	// A sentence never comes out cut in half, whatever the LaTeX source did
	// with its line lengths.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, ",") || strings.HasSuffix(line, " and") {
			t.Errorf("a line ends mid sentence: %q", line)
		}
	}
}

func TestNoRenderingIsUnsupported(t *testing.T) {
	err := noRendering("hep-th/9711200")

	// Exit 7, not 6: the paper is real and the answer is that arXiv never
	// rendered it, which no amount of asking again will change.
	if errs.KindOf(err) != errs.KindUnsupported {
		t.Errorf("kind: got %v", errs.KindOf(err))
	}
	want := "no LaTeXML HTML for hep-th/9711200; arXiv renders HTML for papers submitted since December 2023 and for some earlier ones"
	if err.Error() != want {
		t.Errorf("message:\n got %q\nwant %q", err.Error(), want)
	}
}

func TestHTMLURL(t *testing.T) {
	cases := []struct {
		id      string
		version int
		want    string
	}{
		{"2401.00001", 1, "https://arxiv.org/html/2401.00001v1"},
		{"2601.00086", 3, "https://arxiv.org/html/2601.00086v3"},
		// No version is a URL arXiv redirects rather than one it serves, and it
		// is only reachable when a record arrived without a version at all.
		{"2401.00001", 0, "https://arxiv.org/html/2401.00001"},
	}
	for _, c := range cases {
		if got := htmlURL(c.id, c.version); got != c.want {
			t.Errorf("htmlURL(%q, %d): got %q", c.id, c.version, got)
		}
	}
}

func TestMergeFullTextMatchesAffiliationsByName(t *testing.T) {
	doc := renderingFixture(t, "html_2601.00086v3.html")
	// The metadata surfaces give these names in a different order from the
	// rendering, which is the case that a positional merge gets wrong and
	// nobody notices.
	p := Paper{
		ID: "2601.00086",
		Authors: []Author{
			{Name: "Kamalika Das", Via: SurfaceAPI},
			{Name: "Xiang Gao", Via: SurfaceAPI},
			{Name: "Kaiwen Dong", Via: SurfaceAPI},
		},
	}
	mergeFullText(&p, doc, "https://arxiv.org/html/2601.00086v3")

	byName := map[string]Author{}
	for _, a := range p.Authors {
		byName[a.Name] = a
	}
	if got := byName["Xiang Gao"].Affiliation; got != "Intuit AI Research" {
		t.Errorf("Xiang Gao: got %q", got)
	}
	if got := byName["Kamalika Das"].Affiliation; got != "Intuit AI Research" {
		t.Errorf("Kamalika Das: got %q", got)
	}
	if got := byName["Kaiwen Dong"].Affiliation; !strings.HasPrefix(got, "Temple University") {
		t.Errorf("Kaiwen Dong: got %q", got)
	}
	// The five names the metadata did not have are on the paper too, with the
	// rendering against them, because the rendering is the paper itself.
	if len(p.Authors) != 8 {
		t.Fatalf("authors: got %d, want 8", len(p.Authors))
	}
	for _, a := range p.Authors[3:] {
		if a.Via != SurfaceFullText {
			t.Errorf("%s: via %q", a.Name, a.Via)
		}
	}
	if p.Via["affiliations"] != SurfaceFullText || p.Via["sections"] != SurfaceFullText {
		t.Errorf("via: got %v", p.Via)
	}
	if len(p.Sections) != 8 {
		t.Errorf("sections: got %d", len(p.Sections))
	}
	if p.LicenseName != "CC BY 4.0" {
		t.Errorf("license name: got %q", p.LicenseName)
	}
}
