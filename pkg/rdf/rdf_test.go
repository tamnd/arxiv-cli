package rdf

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// The claims used here are real ones off 1706.03762, written by hand so the
// test says what it is testing. What a live read produces is checked in the
// arxiv package against the saved surfaces.

const source = "https://export.arxiv.org/api/query?id_list=1706.03762"
const abs = "https://arxiv.org/abs/1706.03762"

func authored() graph.Edge {
	return graph.Edge{
		From: graph.Name("Ashish Vaswani"), Predicate: graph.Authored, To: graph.Paper("1706.03762"),
		Source: source, Surface: "s1", Note: "Ashish Vaswani", Position: 1,
	}
}

// ─── naming ───

// Where arXiv has an address for something, that address is its name. Minting
// our own for a paper would be inventing a second name for a thing the whole
// world already agrees on.
func TestANodeWithAnAddressIsNamedByIt(t *testing.T) {
	cases := []struct{ uri, want string }{
		{graph.Paper("1706.03762"), abs},
		{graph.Paper("hep-th/9711200"), "https://arxiv.org/abs/hep-th/9711200"},
		{graph.Version("1706.03762", 7), abs + "v7"},
		{graph.Author("baez_j_1"), "https://arxiv.org/a/baez_j_1"},
		{graph.ORCID("0000-0002-1825-0097"), "https://orcid.org/0000-0002-1825-0097"},
		{graph.DOI("10.1038/nature14539"), "https://doi.org/10.1038/nature14539"},
		{graph.File("1706.03762", 7, "pdf"), "https://arxiv.org/pdf/1706.03762v7"},
		{graph.File("1706.03762", 7, "source"), "https://arxiv.org/src/1706.03762v7"},
		{graph.File("1706.03762", 0, "html"), "https://arxiv.org/html/1706.03762"},
	}
	for _, tc := range cases {
		if got := NodeIRI(tc.uri); string(got) != tc.want {
			t.Errorf("NodeIRI(%s) = %s, want %s", tc.uri, got, tc.want)
		}
	}
}

// A name is a normalised author string and there is no page for it anywhere, so
// it is minted here where it is obvious whose claim it is.
func TestANodeWithNoAddressIsMinted(t *testing.T) {
	name := NodeIRI(graph.Name("Aidan N. Gomez"))
	if want := IRI(NSID + "name/aidan-n-gomez"); name != want {
		t.Errorf("a name is %s, want %s", name, want)
	}
	if !Minted(name) {
		t.Error("a minted name does not say it was minted")
	}
	if Minted(NodeIRI(graph.Paper("1706.03762"))) {
		t.Error("a paper claims to have been minted here")
	}
	if got := NodeIRI(graph.Category("cs.CL")); got != IRI(NSID+"category/cs.CL") {
		t.Errorf("a category is %s", got)
	}
	if got := NodeIRI("ax://nonsense/thing"); got != "" {
		t.Errorf("a uri that is not ours came back as %s", got)
	}
}

// The slug exists so a query can say cc-by-4.0 without a lookup table, and the
// URL is what dc:rights holds on the OAI record. Both have to be reachable from
// the other or the two halves of the store stop joining.
func TestALicenseSlugBecomesItsURLAgain(t *testing.T) {
	urls := []string{
		"http://creativecommons.org/licenses/by/4.0/",
		"http://creativecommons.org/licenses/by-nc-sa/4.0/",
		"http://creativecommons.org/publicdomain/zero/1.0/",
		"http://arxiv.org/licenses/nonexclusive-distrib/1.0/",
	}
	bare := func(u string) string { return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://") }
	for _, u := range urls {
		got := string(NodeIRI(graph.License(u)))
		if bare(got) != bare(u) {
			t.Errorf("%s came back as %s", u, got)
		}
	}
	// arXiv's own licence URLs are http on the record, so writing https would
	// be a second IRI for one licence.
	if got := NodeIRI(graph.License("http://arxiv.org/licenses/nonexclusive-distrib/1.0/")); !strings.HasPrefix(string(got), "http://") {
		t.Errorf("arXiv's licence is %s", got)
	}
	// Anything the two families do not cover is minted rather than guessed at.
	odd := NodeIRI(graph.License("https://example.org/some/licence"))
	if !Minted(odd) {
		t.Errorf("an unknown licence became %s", odd)
	}
}

// ─── claims ───

// schema:author runs from the work to its author. arxiv writes it the other way
// because the frontier reads from the name, and getting this backwards produces
// a file that loads without complaint and says a paper wrote Ashish Vaswani.
func TestAuthoredTurnsRoundOnTheWayOut(t *testing.T) {
	d := New()
	d.AddEdge(authored())
	out := text(t, d, Options{})
	if !strings.Contains(out, "<"+abs+"> <"+string(SchemaAuthor)+"> <"+NSID+"name/ashish-vaswani>") {
		t.Errorf("schema:author runs the wrong way:\n%s", out)
	}
	if !strings.Contains(out, string(DCCreator)) {
		t.Error("dc:creator is not written, and dc:creator is the term arXiv itself publishes")
	}
	if strings.Contains(out, "<"+NSID+"name/ashish-vaswani> <"+string(SchemaAuthor)+">") {
		t.Error("a name authored a paper, which is backwards")
	}
}

// Three surfaces agree about the title and RDF cannot say the same thing twice,
// so the statement is written once and all three sources hang off it.
func TestAClaimTwoSurfacesAgreeOnIsWrittenOnce(t *testing.T) {
	d := New()
	d.Add(IRI(abs), DCTitle, Text("Attention Is All You Need"), source)
	d.Add(IRI(abs), DCTitle, Text("Attention Is All You Need"), abs)
	if d.Len() != 1 {
		t.Fatalf("%d statements, want one", d.Len())
	}
	st := d.Statements()[0]
	if len(st.Sources) != 2 || st.Sources[0] != abs || st.Sources[1] != source {
		t.Errorf("the sources are %v, sorted and both expected", st.Sources)
	}
}

// A claim this file has no translation for is written under its own name rather
// than dropped, because a claim lost in translation is lost silently and the
// output still looks complete.
func TestAPredicateNobodyTaughtIsStillWritten(t *testing.T) {
	d := New()
	d.AddEdge(graph.Edge{
		From: graph.Paper("1706.03762"), Predicate: "reviewed_by", To: graph.Paper("1607.06450"), Source: source,
	})
	out := text(t, d, Options{})
	if !strings.Contains(out, "<"+NSAX+"reviewedBy>") {
		t.Errorf("an untaught predicate went missing:\n%s", out)
	}
}

// The note names one end and which end is a per predicate fact, not a rule. On
// announced_as the note is "new", and a label saying cs.CL is called new would
// be worse than no label at all.
func TestOnlyTheEndTheTableNamesGetsTheLabel(t *testing.T) {
	d := New()
	d.AddEdge(authored())
	d.AddEdge(graph.Edge{
		From: graph.Paper("1706.03762"), Predicate: graph.AnnouncedAs, To: graph.Category("cs.CL"),
		Source: "https://rss.arxiv.org/rss/cs.CL", Surface: "s6", Note: "new",
	})
	d.AddEdge(graph.Edge{
		From: graph.Paper("1706.03762"), Predicate: graph.HasDOI, To: graph.DOI("10.1038/nature14539"),
		Source: source, Surface: "s1", Note: "the publisher's",
	})
	out := text(t, d, Options{})
	if !strings.Contains(out, `"Ashish Vaswani"`) {
		t.Errorf("the spelling the slug threw away is gone:\n%s", out)
	}
	if strings.Contains(out, `"new"`) {
		t.Error("a category was labelled with an announcement type")
	}
	if strings.Contains(out, `"the publisher's"`) {
		t.Error("a DOI somebody else minted was given a name by this tool")
	}
}

// owl:sameAs is arXiv's own assertion and it is used exactly twice. A name is
// not a person, which is the whole reason the two are different node spaces.
func TestSameAsIsOnlyWhereArxivAssertsIt(t *testing.T) {
	d := New()
	d.AddEdge(graph.Edge{
		From: graph.Name("john baez"), Predicate: graph.IdentifiedAs, To: graph.Author("baez_j_1"),
		Source: "https://arxiv.org/a/baez_j_1", Surface: "s8", Note: "John Baez",
	})
	d.AddEdge(authored())
	for _, s := range d.Statements() {
		if s.Predicate != OWLSameAs {
			continue
		}
		if !strings.HasPrefix(string(s.Subject), NSID+"name/") {
			continue
		}
		// A name may be sameAs a person only because arXiv's own author page
		// said so, and that page is the source on the claim.
		if len(s.Sources) == 0 || !strings.Contains(s.Sources[0], "/a/") {
			t.Errorf("a name is sameAs something on no authority: %+v", s)
		}
	}
}

// arXiv said a paper's dc:type is text. It never said schema:ScholarlyArticle,
// so that claim cites nothing rather than putting words in the endpoint's mouth.
func TestAnInferredTypeCitesNobody(t *testing.T) {
	d := New()
	d.AddEdge(authored())
	found := false
	for _, s := range d.Statements() {
		if s.Predicate != RDFType {
			continue
		}
		found = true
		if len(s.Sources) != 0 {
			t.Errorf("%s is typed on the authority of %v", s.Subject, s.Sources)
		}
	}
	if !found {
		t.Error("nothing was typed at all")
	}
}

// ─── the formats ───

// A dump that reorders itself cannot be diffed, and a diff is how somebody
// notices that arXiv started saying something different.
func TestTheSameInputGivesTheSameBytes(t *testing.T) {
	for _, format := range Formats {
		o := Options{Format: format, Provenance: true}
		first := text(t, sample(), o)
		second := text(t, sample(), o)
		if first != second {
			t.Errorf("%s is not stable between two runs", format)
		}
		if first == "" {
			t.Errorf("%s wrote nothing", format)
		}
	}
}

func TestNTriplesCarriesProvenanceAsAQuotedTriple(t *testing.T) {
	with := text(t, sample(), Options{Provenance: true})
	line := "<< <" + abs + "> <" + string(DCTitle) + "> \"Attention Is All You Need\" >> <" + string(PROVDerivedFrom) + "> <" + source + "> ."
	if !strings.Contains(with, line) {
		t.Errorf("the quoted triple is not there:\n%s", with)
	}
	without := text(t, sample(), Options{})
	if strings.Contains(without, "<<") || strings.Contains(without, string(PROVDerivedFrom)) {
		t.Errorf("--no-provenance still wrote provenance:\n%s", without)
	}
	// Every line is a statement, so dropping the provenance drops lines and
	// nothing else.
	if len(lines(without)) >= len(lines(with)) {
		t.Error("provenance cost nothing, which means it was not written")
	}
	for _, l := range lines(without) {
		if !strings.HasSuffix(l, " .") {
			t.Errorf("a line does not end a statement: %q", l)
		}
	}
}

func TestTurtleDeclaresWhatItUsesAndNothingElse(t *testing.T) {
	out := text(t, sample(), Options{Format: FormatTurtle, Provenance: true})
	for _, want := range []string{"@prefix dc: <" + NSDC + "> .", "@prefix schema: <" + NSSchema + "> .", "    a fabio:Preprint, schema:ScholarlyArticle ;"} {
		if !strings.Contains(out, want) {
			t.Errorf("turtle is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "@prefix cito:") {
		t.Error("cito is declared and there are no citations in this document")
	}
	// One subject, one block: the only reason to prefer turtle to n-triples.
	if n := strings.Count(out, "<"+abs+">\n"); n != 1 {
		t.Errorf("the paper is the subject of %d blocks, want one:\n%s", n, out)
	}
}

func TestJSONLDPutsEachSourceInItsOwnGraph(t *testing.T) {
	var doc struct {
		Context map[string]any `json:"@context"`
		Graph   []struct {
			ID    string           `json:"@id"`
			Graph []map[string]any `json:"@graph"`
		} `json:"@graph"`
	}
	raw := text(t, sample(), Options{Format: FormatJSONLD, Provenance: true})
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("the json-ld does not parse: %v\n%s", err, raw)
	}
	if doc.Context["dc"] != NSDC {
		t.Errorf("the context is %v, and it has to be inline so nothing is fetched to read the file", doc.Context)
	}
	named := 0
	for _, g := range doc.Graph {
		if g.ID == source+"#claims" {
			named++
		}
		if g.ID == source {
			t.Error("the graph borrowed the page's own address, so arXiv's html now has an author")
		}
	}
	if named != 1 {
		t.Errorf("%d graphs for the api read, want one", named)
	}
	// A term whose objects are IRIs has to say so or a consumer reads the
	// author as the string https://...
	author, ok := doc.Context["schema:author"].(map[string]any)
	if !ok || author["@type"] != "@id" {
		t.Errorf("schema:author is declared as %v", doc.Context["schema:author"])
	}
}

// An abstract is LaTeX, so quotes, backslashes and newlines arrive on every
// paper rather than in the odd corner case.
func TestALiteralKeepsWhatItSays(t *testing.T) {
	d := New()
	d.Add(IRI(abs), DCDescription, Text("a \"quoted\" \\alpha\nover two lines"), source)
	out := text(t, d, Options{})
	if !strings.Contains(out, `"a \"quoted\" \\alpha\nover two lines"`) {
		t.Errorf("the literal came out as:\n%s", out)
	}
	if len(lines(out)) != 1 {
		t.Errorf("a newline in a literal became a newline in the file:\n%s", out)
	}
	var doc map[string]any
	raw := text(t, d, Options{Format: FormatJSONLD})
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("json-ld with a literal in it does not parse: %v", err)
	}
}

func TestAFormatNobodyHasIsAnError(t *testing.T) {
	if err := Write(&bytes.Buffer{}, sample(), Options{Format: "rdfxml"}); err == nil {
		t.Fatal("rdfxml was written")
	}
}

// ─── helpers ───

// sample is a small document with a literal, a claim and a type in it.
func sample() *Doc {
	d := New()
	d.Add(IRI(abs), DCTitle, Text("Attention Is All You Need"), source)
	d.Add(IRI(abs), SchemaName, Text("Attention Is All You Need"), source, "https://arxiv.org/abs/1706.03762")
	d.Type(IRI(abs), Classes(graph.KindPaper)...)
	d.AddEdge(authored())
	return d
}

func text(t *testing.T, d *Doc, o Options) string {
	t.Helper()
	var b bytes.Buffer
	if err := Write(&b, d, o); err != nil {
		t.Fatalf("write %s: %v", o.Format, err)
	}
	return b.String()
}

func lines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
