package latexml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two fixtures are real renderings, saved on 2026-08-14.
//
// 2401.00001v1 is the plain case: two authors, one affiliation each, an
// abstract typed as an unnumbered section, six sections and a References
// section written as prose. 2601.00086v3 is everything else: a title broken
// over two lines, eight authors packed four to a creator block, a real
// abstract element, an appendix, displayed maths and a bibliography of 46
// entries. Between them they cover every shape this parser claims to read.
const (
	small = "html_2401.00001v1.html"
	big   = "html_2601.00086v3.html"
)

func load(t *testing.T, name string) *Document {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc, err := Parse(body)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func TestParseSmallPaper(t *testing.T) {
	doc := load(t, small)

	if doc.Title != "Sector Rotation by Factor Model and Fundamental Analysis" {
		t.Errorf("title: got %q", doc.Title)
	}
	if doc.Dates != "Sept 2023" {
		t.Errorf("dates: got %q", doc.Dates)
	}
	if doc.License != "CC Zero" {
		t.Errorf("license: got %q", doc.License)
	}
	if doc.Stamp != "arXiv:2401.00001v1 [q-fin.PM] 18 Nov 2023" {
		t.Errorf("stamp: got %q", doc.Stamp)
	}
	if !strings.HasPrefix(doc.Abstract, "This study presents an analytical approach to sector rotation") {
		t.Errorf("abstract: got %.60q", doc.Abstract)
	}
	if len(doc.Sections) != 6 {
		t.Fatalf("sections: got %d, the page has six", len(doc.Sections))
	}
	if doc.Sections[0].ID != "S1" || doc.Sections[0].Title != "Introduction" {
		t.Errorf("first section: got %q %q", doc.Sections[0].ID, doc.Sections[0].Title)
	}
}

func TestAffiliationsComeOffThePage(t *testing.T) {
	doc := load(t, small)

	want := []Author{
		{Name: "Runjia Yang", Affiliation: "University of California, Davis"},
		{Name: "Beining Shi", Affiliation: "University of California, Davis"},
	}
	if len(doc.Authors) != len(want) {
		t.Fatalf("authors: got %d, want %d", len(doc.Authors), len(want))
	}
	for i, a := range doc.Authors {
		if a != want[i] {
			t.Errorf("author %d: got %+v, want %+v", i, a, want[i])
		}
	}
}

func TestEightAuthorsPackedIntoTwoCreatorBlocks(t *testing.T) {
	doc := load(t, big)

	// The page puts four names in one ltx_personname, separated by an em space
	// and a hair space, with four affiliations alongside. Reading that block as
	// one author called "Xiang Gao Yuguang Yao Qi Zhang Kaiwen Dong" is what a
	// naive text read gives, and it is wrong about four people at once.
	names := make([]string, 0, len(doc.Authors))
	for _, a := range doc.Authors {
		names = append(names, a.Name)
	}
	want := []string{
		"Xiang Gao", "Yuguang Yao", "Qi Zhang", "Kaiwen Dong",
		"Avinash Baidya", "Ruocheng Guo", "Hilaf Hasson", "Kamalika Das",
	}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names: got %v", names)
	}
	for i, a := range doc.Authors {
		if !strings.HasPrefix(a.Affiliation, "Intuit AI Research") && !strings.HasPrefix(a.Affiliation, "Temple University") {
			t.Errorf("author %d %q: affiliation %q", i, a.Name, a.Affiliation)
		}
	}
	// The fourth affiliation has the contact address run onto the end of it,
	// which is how the page renders it, and inventing a split here would mean
	// inventing where an institution's name stops.
	if doc.Authors[3].Affiliation != "Temple University{xiang_gao, kamalika_das}@intuit.com" {
		t.Errorf("fourth affiliation: got %q", doc.Authors[3].Affiliation)
	}
}

func TestTitleBrokenOverTwoLinesComesBackAsOne(t *testing.T) {
	doc := load(t, big)

	want := "RIMRULE: Improving Tool-Using Language Agents via MDL-Guided Rule Learning"
	if doc.Title != want {
		t.Errorf("title: got %q", doc.Title)
	}
	if strings.Contains(doc.Title, "\n") {
		t.Error("title has a newline in it")
	}
}

func TestAbstractIsFoundInBothShapes(t *testing.T) {
	// 2601.00086v3 has \begin{abstract}, which LaTeXML renders as an
	// ltx_abstract div.
	if a := load(t, big).Abstract; !strings.HasPrefix(a, "Large language models (LLMs) often struggle") {
		t.Errorf("ltx_abstract: got %.60q", a)
	}

	// 2401.00001v1 typed its abstract as an unnumbered section, so it arrives
	// as an ordinary section that happens to be titled Abstract. It belongs in
	// Abstract either way, and it must not be left in the tree pretending to be
	// the first chapter.
	doc := load(t, small)
	if !strings.HasPrefix(doc.Abstract, "This study presents") {
		t.Errorf("abstract section: got %.60q", doc.Abstract)
	}
	for _, s := range doc.Sections {
		if strings.EqualFold(s.Title, "Abstract") {
			t.Errorf("the abstract is still in the section tree as %s", s.ID)
		}
	}
}

func TestSectionsNest(t *testing.T) {
	doc := load(t, small)

	factors := doc.Sections[2]
	if factors.ID != "S3" || factors.Title != "Factor Analysis" || factors.Level != 1 {
		t.Fatalf("S3: got %+v", factors)
	}
	if factors.Text != "" {
		t.Errorf("S3 has no prose of its own, got %.40q", factors.Text)
	}
	if len(factors.Sections) != 2 {
		t.Fatalf("S3 subsections: got %d, want 2", len(factors.Sections))
	}
	sub := factors.Sections[0]
	if sub.ID != "S3.SS1" || sub.Level != 2 || sub.Kind != "subsection" {
		t.Errorf("S3.SS1: got id %q level %d kind %q", sub.ID, sub.Level, sub.Kind)
	}
	if len(sub.Sections) != 2 || sub.Sections[0].Level != 3 || sub.Sections[0].Kind != "subsubsection" {
		t.Errorf("S3.SS1 children: got %+v", sub.Sections)
	}
	if sub.Sections[0].Title != "Factor Construction" {
		t.Errorf("S3.SS1.SSS1 title: got %q", sub.Sections[0].Title)
	}
}

func TestAppendixAndParagraphLevels(t *testing.T) {
	doc := load(t, big)

	last := doc.Sections[len(doc.Sections)-1]
	if last.ID != "A1" || last.Kind != "appendix" || last.Level != 1 {
		t.Errorf("appendix: got id %q kind %q level %d", last.ID, last.Kind, last.Level)
	}
	if len(last.Sections) != 3 {
		t.Errorf("appendix subsections: got %d, want 3", len(last.Sections))
	}

	// LaTeX \paragraph is a heading too and it is the fourth level.
	para, ok := doc.Section("S2.SS2.SSS2.Px1")
	if !ok {
		t.Fatal("S2.SS2.SSS2.Px1 is missing")
	}
	if para.Level != 4 || para.Kind != "paragraph" || para.Title != "Objective." {
		t.Errorf("Px1: got level %d kind %q title %q", para.Level, para.Kind, para.Title)
	}
}

func TestNumberingIsStrippedFromTitles(t *testing.T) {
	doc := load(t, big)

	var titles []string
	var walk func([]Section)
	walk = func(sections []Section) {
		for _, s := range sections {
			titles = append(titles, s.Title)
			walk(s.Sections)
		}
	}
	walk(doc.Sections)

	if len(titles) == 0 {
		t.Fatal("no sections")
	}
	for _, title := range titles {
		if title == "" {
			t.Error("a section has no title")
			continue
		}
		if c := title[0]; c >= '0' && c <= '9' {
			t.Errorf("title %q still carries its number", title)
		}
	}
}

func TestMathsIsTheLaTeXTheAuthorWrote(t *testing.T) {
	doc := load(t, big)

	para, ok := doc.Section("S2.SS2.SSS2.Px1")
	if !ok {
		t.Fatal("S2.SS2.SSS2.Px1 is missing")
	}
	// Inline maths keeps its dollars and displayed maths gets \[ \], so a
	// reader can tell the two apart and a LaTeX parser can take either.
	if !strings.Contains(para.Text, `Let $H\subseteq\mathcal{R}^{\text{sym}}$ denote a candidate rule library`) {
		t.Errorf("inline maths: got %.120q", para.Text)
	}
	if !strings.Contains(para.Text, `\[\mathrm{MDL}(H)=L(H)+L(D\mid H),\]`) {
		t.Errorf("displayed maths: got %q", para.Text)
	}
	// MathML is markup for a browser and none of it should survive.
	for _, leak := range []string{"<mi>", "semantics", "annotation", "≔"} {
		if strings.Contains(para.Text, leak) {
			t.Errorf("mathml leaked %q into the text", leak)
		}
	}

	// A heading can hold maths too, and it comes out the same way.
	if title := load(t, small).Sections[3].Sections[3].Title; title != `EV/EBIT $\&$ EV/EBITDA` {
		t.Errorf("maths in a heading: got %q", title)
	}
}

func TestAParagraphComesBackAsOneLine(t *testing.T) {
	doc := load(t, big)

	// The source wraps this paragraph after "heuristics", because that is what
	// a LaTeX file looks like. Those newlines are the author's line wrapping
	// and not the author's meaning, and a sentence cut in half by one is
	// useless to anything that reads sentences.
	text := doc.Sections[0].Text
	if !strings.Contains(text, "reusable heuristics (16; 20; 10). Such compact representation") {
		t.Errorf("the paragraph came back wrapped: %.200q", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, ",") {
			t.Errorf("a line ends mid sentence: %q", line)
		}
	}
	// Paragraphs are still kept apart.
	if !strings.Contains(text, "\n\n") {
		t.Error("the paragraphs ran together")
	}
}

func TestFootnotesStayOutOfTheSentence(t *testing.T) {
	doc := load(t, big)

	// The abstract's last sentence carries footnote 1, whose body is the
	// conference notice. Neither the notice nor the mark belongs in the
	// abstract.
	if strings.Contains(doc.Abstract, "Proceedings of the 64th Annual Meeting") {
		t.Error("the footnote body landed in the abstract")
	}
	if !strings.HasSuffix(doc.Abstract, "portability of symbolic knowledge across architectures.") {
		t.Errorf("abstract ends %q", doc.Abstract[len(doc.Abstract)-60:])
	}
}

func TestCaptionsKeepTheirLabel(t *testing.T) {
	doc := load(t, big)

	// A caption without its "Figure 1: " reads as a stray sentence dropped into
	// the prose, so the numbering tag is kept here even though it is stripped
	// from headings.
	if !strings.Contains(doc.Sections[0].Text, "Figure 1: Learning reusable and interpretable rules from experience") {
		t.Error("the figure caption lost its label")
	}
}

func TestReferences(t *testing.T) {
	doc := load(t, big)

	if len(doc.References) != 46 {
		t.Fatalf("references: got %d, the bibliography has 46", len(doc.References))
	}
	first := doc.References[0]
	if first.ID != "bib.bib38" || first.Kind != "article" {
		t.Errorf("first reference: got id %q kind %q", first.ID, first.Kind)
	}
	if first.Label != "Bai et al. (2023)" {
		t.Errorf("label: got %q", first.Label)
	}
	if first.Title != "Qwen technical report" {
		t.Errorf("title: got %q", first.Title)
	}
	if !strings.HasPrefix(first.Authors, "J. Bai, S. Bai, Y. Chu") {
		t.Errorf("authors: got %q", first.Authors)
	}
	if first.Journal != "arXiv preprint arXiv:2309.16609" {
		t.Errorf("journal: got %q", first.Journal)
	}
	if len(first.Links) != 1 || first.Links[0] != "https://arxiv.org/abs/2309.16609" {
		t.Errorf("links: got %v", first.Links)
	}
	if len(first.CitedIn) != 1 || first.CitedIn[0] != "§1" {
		t.Errorf("cited in: got %v", first.CitedIn)
	}
	// The links line and the cited-by line are fields of their own, so they do
	// not belong in the sentence as well.
	if strings.Contains(first.Text, "External Links") || strings.Contains(first.Text, "Cited by") {
		t.Errorf("text carries the apparatus: %q", first.Text)
	}

	kinds := map[string]int{}
	for _, r := range doc.References {
		if r.ID == "" || r.Text == "" {
			t.Errorf("reference with no id or no text: %+v", r)
		}
		kinds[r.Kind]++
	}
	// LaTeXML keeps the BibTeX entry type, which is worth having: an
	// inproceedings and a misc are not the same claim about where something
	// was published.
	for kind, want := range map[string]int{"article": 34, "inproceedings": 7, "book": 2, "misc": 2, "incollection": 1} {
		if kinds[kind] != want {
			t.Errorf("%s entries: got %d, want %d", kind, kinds[kind], want)
		}
	}

	// A paged entry keeps its volume and pages apart, which a citation string
	// does not.
	var brown Reference
	for _, r := range doc.References {
		if r.ID == "bib.bib1" {
			brown = r
		}
	}
	if brown.Volume != "33" || brown.Pages != "pp. 1877–1901" {
		t.Errorf("bib.bib1: got volume %q pages %q", brown.Volume, brown.Pages)
	}
}

func TestAProseBibliographyIsNotAReferenceList(t *testing.T) {
	doc := load(t, small)

	// This paper typed its references as two numbered lines of prose rather
	// than as \bibitem entries, so there is nothing structured to read and the
	// list is empty rather than wrong.
	if len(doc.References) != 0 {
		t.Errorf("references: got %d, the page has no bibitems", len(doc.References))
	}
	last := doc.Sections[len(doc.Sections)-1]
	if last.Title != "References" || !strings.Contains(last.Text, "Returns to Buying Winners and Selling Losers") {
		t.Errorf("the prose references section: got %q %.60q", last.Title, last.Text)
	}
}

func TestSectionLookup(t *testing.T) {
	doc := load(t, small)

	got, ok := doc.Section("S3.SS1.SSS2")
	if !ok {
		t.Fatal("S3.SS1.SSS2 is missing")
	}
	if got.Title != "Calculate Factor Returns" {
		t.Errorf("title: got %q", got.Title)
	}
	if _, ok := doc.Section("S99"); ok {
		t.Error("S99 was found and it does not exist")
	}
}

func TestParseRejectsAPageThatIsNotARendering(t *testing.T) {
	if _, err := Parse([]byte("<html><body><p>not a paper</p></body></html>")); err == nil {
		t.Error("a page with no ltx_document parsed anyway")
	}
}

func TestAffiliationPairing(t *testing.T) {
	cases := []struct {
		name         string
		affiliations []string
		i, names     int
		want         string
	}{
		{"one shared by the block", []string{"MIT"}, 2, 3, "MIT"},
		{"one each, in order", []string{"MIT", "Caltech"}, 1, 2, "Caltech"},
		{"none at all", nil, 0, 2, ""},
		{"a count that pairs with nothing", []string{"MIT", "Caltech"}, 0, 3, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := affiliationFor(c.affiliations, c.i, c.names); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSplitNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Runjia Yang\n", []string{"Runjia Yang"}},
		{"Xiang Gao   Yuguang Yao   Qi Zhang", []string{"Xiang Gao", "Yuguang Yao", "Qi Zhang"}},
		// An em space and a hair space, which is what \quad renders to and what
		// 2601.00086v3 puts between its names.
		{"Ada Lovelace   Alan Turing", []string{"Ada Lovelace", "Alan Turing"}},
		{"The ATLAS Collaboration", []string{"The ATLAS Collaboration"}},
		{"", nil},
	}
	for _, c := range cases {
		got := splitNames(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitNames(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}
