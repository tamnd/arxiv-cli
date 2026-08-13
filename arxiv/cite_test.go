package arxiv

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit/errs"
)

func TestParseStyle(t *testing.T) {
	cases := map[string]Style{
		"":         StyleBibTeX,
		"bibtex":   StyleBibTeX,
		"bib":      StyleBibTeX,
		"BibTeX":   StyleBibTeX,
		" apa ":    StyleAPA,
		"mla":      StyleMLA,
		"chicago":  StyleChicago,
		"ris":      StyleRIS,
		"csl-json": StyleCSLJSON,
		"csl":      StyleCSLJSON,
		"json":     StyleCSLJSON,
		"text":     StyleText,
		"plain":    StyleText,
	}
	for in, want := range cases {
		got, err := ParseStyle(in)
		if err != nil {
			t.Errorf("ParseStyle(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseStyle(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseStyleRejectsTheRest checks the error is a usage error, because a
// misspelled style is the user's typo and exits 2 rather than 1.
func TestParseStyleRejectsTheRest(t *testing.T) {
	_, err := ParseStyle("harvard")
	if err == nil {
		t.Fatal("harvard was accepted")
	}
	if errs.KindOf(err) != errs.KindUsage {
		t.Errorf("kind is %v, want usage", errs.KindOf(err))
	}
	// The message lists what is on offer, so nobody has to go and read --help.
	for _, style := range styleNames() {
		if !strings.Contains(err.Error(), style) {
			t.Errorf("the error does not name %q: %v", style, err)
		}
	}
}

func TestRenderAPA(t *testing.T) {
	got := renderAPA(fullRecord(t))
	want := "Vaswani, A., Shazeer, N., Parmar, N., Uszkoreit, J., Jones, L., Gomez, A. N., Kaiser, L., & Polosukhin, I. (2017). Attention Is All You Need. arXiv. https://doi.org/10.48550/arXiv.1706.03762"
	if got != want {
		t.Errorf("APA:\n%s\nwant:\n%s", got, want)
	}
}

// TestRenderAPAOfAPublishedPaper checks the journal replaces "arXiv" and the
// publisher DOI replaces the arXiv one. A published paper cited as a preprint
// is the mistake this style exists to avoid.
func TestRenderAPAOfAPublishedPaper(t *testing.T) {
	got := renderAPA(publishedRecord(t))
	want := "The ATLAS Collaboration. (2012). Observation of a new particle in the search for the Standard Model Higgs boson with the ATLAS detector at the LHC. Phys.Lett. B716 (2012) 1-29. https://doi.org/10.1016/j.physletb.2012.08.020"
	if got != want {
		t.Errorf("APA:\n%s\nwant:\n%s", got, want)
	}
}

func TestAPAList(t *testing.T) {
	many := make([]string, 0, 21)
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U"} {
		many = append(many, n+", X.")
	}
	cases := []struct {
		name  string
		names []string
		want  string
	}{
		{"nobody", nil, ""},
		{"one", []string{"Vaswani, A."}, "Vaswani, A."},
		{"two get an ampersand", []string{"Vaswani, A.", "Shazeer, N."}, "Vaswani, A., & Shazeer, N."},
		{"three keep the serial comma", []string{"Vaswani, A.", "Shazeer, N.", "Parmar, N."}, "Vaswani, A., Shazeer, N., & Parmar, N."},
		{
			"twenty one is nineteen, an ellipsis and the last",
			many,
			"A, X., B, X., C, X., D, X., E, X., F, X., G, X., H, X., I, X., J, X., K, X., L, X., M, X., N, X., O, X., P, X., Q, X., R, X., S, X., ... U, X.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := apaList(tc.names); got != tc.want {
				t.Errorf("apaList = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAPAListStopsAtTwenty checks the boundary APA actually draws: twenty
// authors are all printed and twenty one are not.
func TestAPAListStopsAtTwenty(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = "A" + string(rune('a'+i))
	}
	if got := apaList(names); strings.Contains(got, "...") {
		t.Errorf("twenty authors were truncated: %s", got)
	}
	if got := apaList(append(names, "Last")); !strings.Contains(got, ", ... Last.") {
		t.Errorf("twenty one authors were not truncated: %s", got)
	}
}

func TestInitials(t *testing.T) {
	cases := map[string]string{
		"Ashish":        "A.",
		"Aidan N.":      "A. N.",
		"Jean-Pierre":   "J. P.",
		"  Illia ":      "I.",
		"":              "",
		"Ludwig van":    "L. v.",
		"Ashish Kumar ": "A. K.",
	}
	for in, want := range cases {
		if got := initials(in); got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAPANameKeepsWhatArxivGave is the rule from doc 03 section 2.3. A name
// that only ever arrived as a display string is printed as that string, because
// splitting it is a guess and the guess is wrong on the names that matter.
func TestAPANameKeepsWhatArxivGave(t *testing.T) {
	cases := []struct {
		author Author
		want   string
	}{
		{Author{Name: "Ashish Vaswani", Keyname: "Vaswani", Forenames: "Ashish"}, "Vaswani, A."},
		{Author{Name: "The ATLAS Collaboration"}, "The ATLAS Collaboration"},
		{Author{Name: "Plato", Keyname: "Plato"}, "Plato"},
	}
	for _, tc := range cases {
		if got := apaName(tc.author); got != tc.want {
			t.Errorf("apaName(%q) = %q, want %q", tc.author.Name, got, tc.want)
		}
	}
}

func TestRenderMLA(t *testing.T) {
	got := renderMLA(fullRecord(t))
	want := `Vaswani, Ashish, et al. "Attention Is All You Need." arXiv, 12 June 2017, arxiv.org/abs/1706.03762v7.`
	if got != want {
		t.Errorf("MLA:\n%s\nwant:\n%s", got, want)
	}
}

// TestMLACountsAuthors pins MLA 9's rule: one inverted, two joined with "and",
// three or more cut to "et al.".
func TestMLACountsAuthors(t *testing.T) {
	p := fullRecord(t)
	cases := []struct {
		n    int
		want string
	}{
		{1, "Vaswani, Ashish. \""},
		{2, "Vaswani, Ashish, and Noam Shazeer. \""},
		{3, "Vaswani, Ashish, et al. \""},
	}
	for _, tc := range cases {
		q := p
		q.Authors = p.Authors[:tc.n]
		if got := renderMLA(q); !strings.HasPrefix(got, tc.want) {
			t.Errorf("%d authors: %s\nwant the prefix %q", tc.n, got, tc.want)
		}
	}
}

func TestRenderChicago(t *testing.T) {
	got := renderChicago(fullRecord(t))
	want := `Vaswani, Ashish, Noam Shazeer, Niki Parmar, Jakob Uszkoreit, Llion Jones, Aidan N. Gomez, Lukasz Kaiser, and Illia Polosukhin. 2017. "Attention Is All You Need." arXiv. https://arxiv.org/abs/1706.03762v7.`
	if got != want {
		t.Errorf("Chicago:\n%s\nwant:\n%s", got, want)
	}
}

// TestChicagoCutsPastTen is Chicago's own boundary: ten authors are all listed,
// eleven become the first seven and "et al.".
func TestChicagoCutsPastTen(t *testing.T) {
	base := fullRecord(t)
	authors := make([]Author, 0, 11)
	for i := 0; i < 11; i++ {
		authors = append(authors, Author{Name: "Author " + string(rune('A'+i))})
	}

	ten := base
	ten.Authors = authors[:10]
	if got := renderChicago(ten); strings.Contains(got, "et al.") {
		t.Errorf("ten authors were cut: %s", got)
	}

	eleven := base
	eleven.Authors = authors
	got := renderChicago(eleven)
	if !strings.HasPrefix(got, "Author A, Author B, Author C, Author D, Author E, Author F, Author G, et al. ") {
		t.Errorf("eleven authors: %s", got)
	}
}

func TestInvertedName(t *testing.T) {
	cases := []struct {
		author Author
		want   string
	}{
		{Author{Name: "Ashish Vaswani", Keyname: "Vaswani", Forenames: "Ashish"}, "Vaswani, Ashish"},
		{Author{Name: "The ATLAS Collaboration"}, "The ATLAS Collaboration"},
		{Author{Name: "Plato", Keyname: "Plato"}, "Plato"},
	}
	for _, tc := range cases {
		if got := invertedName(tc.author); got != tc.want {
			t.Errorf("invertedName(%q) = %q, want %q", tc.author.Name, got, tc.want)
		}
	}
}

// TestRISTypeTellsTheTruth is the one thing in RIS that would be a lie. A
// preprint is GEN, and only a paper with a journal reference is JOUR.
func TestRISTypeTellsTheTruth(t *testing.T) {
	if got := renderRIS(fullRecord(t)); !strings.HasPrefix(got, "TY  - GEN\n") {
		t.Errorf("a preprint came out as %.12q, want GEN", got)
	}
	published := renderRIS(publishedRecord(t))
	if !strings.HasPrefix(published, "TY  - JOUR\n") {
		t.Errorf("a published paper came out as %.13q, want JOUR", published)
	}
	if !strings.Contains(published, "JO  - Phys.Lett. B716 (2012) 1-29\n") {
		t.Errorf("the journal line is missing:\n%s", published)
	}
}

func TestRenderRIS(t *testing.T) {
	got := renderRIS(fullRecord(t))
	if !strings.HasSuffix(got, "\nER  -") {
		t.Errorf("the record does not end with ER:\n%s", got)
	}
	for _, want := range []string{
		"AU  - Vaswani, Ashish\n",
		"AU  - Polosukhin, Illia\n",
		"TI  - Attention Is All You Need\n",
		"PY  - 2017\n",
		"DA  - 2017/06/12\n",
		"KW  - cs.CL\n",
		"KW  - cs.LG\n",
		"PB  - arXiv\n",
		"DO  - 10.48550/arXiv.1706.03762\n",
		"UR  - https://arxiv.org/abs/1706.03762v7\n",
		"N1  - arXiv:1706.03762v7\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the record is missing %q:\n%s", want, got)
		}
	}
	// Every author gets a line of their own, which is what a reference manager
	// reads back as eight authors rather than one long name.
	if n := strings.Count(got, "AU  - "); n != 8 {
		t.Errorf("%d author lines, want 8", n)
	}
}

func TestRenderPlain(t *testing.T) {
	got := renderPlain(fullRecord(t))
	want := `Ashish Vaswani et al., "Attention Is All You Need", arXiv:1706.03762v7 (2017).`
	if got != want {
		t.Errorf("text:\n%s\nwant:\n%s", got, want)
	}
	published := renderPlain(publishedRecord(t))
	if !strings.Contains(published, `, Phys.Lett. B716 (2012) 1-29 (2012).`) {
		t.Errorf("the journal is missing:\n%s", published)
	}
}

// TestPlainNamesUpToThree keeps the short line short. Three authors fit on one
// line and four do not.
func TestPlainNamesUpToThree(t *testing.T) {
	p := fullRecord(t)
	three := p
	three.Authors = p.Authors[:3]
	if got := renderPlain(three); !strings.HasPrefix(got, "Ashish Vaswani, Noam Shazeer, Niki Parmar, \"") {
		t.Errorf("three authors: %s", got)
	}
	four := p
	four.Authors = p.Authors[:4]
	if got := renderPlain(four); !strings.HasPrefix(got, "Ashish Vaswani et al., \"") {
		t.Errorf("four authors: %s", got)
	}
}

func TestRenderCSLJSON(t *testing.T) {
	out, err := renderCSLJSON([]Paper{fullRecord(t), publishedRecord(t)})
	if err != nil {
		t.Fatal(err)
	}
	var items []cslItem
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("the output is not valid CSL-JSON: %v\n%s", err, out)
	}
	// Two papers are one array. A reference manager reads the file whole, so
	// two documents concatenated would be a parse error.
	if len(items) != 2 {
		t.Fatalf("%d items, want 2", len(items))
	}

	preprint := items[0]
	if preprint.Type != "article" {
		t.Errorf("a preprint is type %q, want article", preprint.Type)
	}
	if preprint.ID != "arxiv-1706.03762" {
		t.Errorf("id is %q", preprint.ID)
	}
	if preprint.Number != "arXiv:1706.03762v7" {
		t.Errorf("number is %q, want the versioned id", preprint.Number)
	}
	if len(preprint.Author) != 8 {
		t.Fatalf("%d authors, want 8", len(preprint.Author))
	}
	if got := preprint.Author[0]; got.Family != "Vaswani" || got.Given != "Ashish" {
		t.Errorf("first author is %+v", got)
	}
	want := [][]int{{2017, 6, 12}}
	if preprint.Issued == nil || len(preprint.Issued.DateParts) != 1 || len(preprint.Issued.DateParts[0]) != 3 {
		t.Fatalf("issued is %+v, want %v", preprint.Issued, want)
	}
	for i, part := range want[0] {
		if preprint.Issued.DateParts[0][i] != part {
			t.Errorf("issued is %v, want %v", preprint.Issued.DateParts, want)
			break
		}
	}
	// The abstract and the categories are the reason this style is generated
	// from the record and not from the BibTeX, which carries neither.
	if preprint.Abstract == "" {
		t.Error("the abstract is missing")
	}
	if len(preprint.Categories) != 2 {
		t.Errorf("categories are %v, want both", preprint.Categories)
	}

	published := items[1]
	if published.Type != "article-journal" {
		t.Errorf("a published paper is type %q, want article-journal", published.Type)
	}
	if published.ContainerTitle != "Phys.Lett. B716 (2012) 1-29" {
		t.Errorf("container-title is %q", published.ContainerTitle)
	}
	if published.DOI != "10.1016/j.physletb.2012.08.020" {
		t.Errorf("DOI is %q, want the publisher's", published.DOI)
	}
	// A collaboration is a literal name. Splitting "The ATLAS Collaboration"
	// into a family and a given name would put "Collaboration" in a
	// bibliography's author column.
	if len(published.Author) != 1 || published.Author[0].Literal != "The ATLAS Collaboration" {
		t.Errorf("author is %+v, want one literal name", published.Author)
	}
}

func TestCiteOneCoversEveryStyle(t *testing.T) {
	p := fullRecord(t)
	for _, style := range Styles {
		if style == StyleCSLJSON {
			continue
		}
		got := citeOne(p, style)
		if got == "" {
			t.Errorf("%s produced nothing", style)
			continue
		}
		if !strings.Contains(got, "1706.03762") {
			t.Errorf("%s does not name the paper:\n%s", style, got)
		}
	}
}

// TestVersionedID checks the string the styles print. A citation of v1 that
// says v7 points at a different paper.
func TestVersionedID(t *testing.T) {
	cases := []struct {
		name  string
		paper Paper
		want  string
	}{
		{"the record's own string wins", Paper{ID: "1706.03762", Version: 7, VersionedID: "1706.03762v7"}, "1706.03762v7"},
		{"a version with no string is built", Paper{ID: "1706.03762", Version: 1}, "1706.03762v1"},
		{"no version at all is the bare id", Paper{ID: "hep-th/9711200"}, "hep-th/9711200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionedID(tc.paper); got != tc.want {
				t.Errorf("versionedID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrimDot(t *testing.T) {
	cases := map[string]string{
		"Attention Is All You Need.": "Attention Is All You Need",
		" Vaswani, A. ":              "Vaswani, A",
		"no dot":                     "no dot",
		"":                           "",
	}
	for in, want := range cases {
		if got := trimDot(in); got != want {
			t.Errorf("trimDot(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStylesAndParseStyleAgree keeps the enum on the flag honest. A style added
// to the list and not to the parser would be advertised and then rejected.
func TestStylesAndParseStyleAgree(t *testing.T) {
	for _, style := range Styles {
		got, err := ParseStyle(string(style))
		if err != nil {
			t.Errorf("ParseStyle(%q): %v", style, err)
			continue
		}
		if got != style {
			t.Errorf("ParseStyle(%q) = %q", style, got)
		}
	}
	if len(styleNames()) != len(Styles) {
		t.Errorf("styleNames has %d entries, Styles has %d", len(styleNames()), len(Styles))
	}
}
