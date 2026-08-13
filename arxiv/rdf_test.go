package arxiv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
	"github.com/tamnd/arxiv-cli/pkg/rdf"
)

// The mapping is tested against the record a full read of 1706.03762 actually
// produces, merged from the four saved surfaces. pkg/rdf checks the shape of
// the output on claims written by hand; this checks that a real record turns
// into the right claims in the first place, which is the half that breaks when
// a parser changes.

// paperDoc is the full record as statements, claims and literals together.
func paperDoc(t *testing.T) (*rdf.Doc, Paper) {
	t.Helper()
	p := fullRecord(t)
	d := rdf.New()
	AddPaper(d, p)
	AddClaims(d, EdgesOfPaper(p))
	return d, p
}

// nt is the document as n-triples, which is the format a test can grep.
func nt(t *testing.T, d *rdf.Doc, prov bool) string {
	t.Helper()
	var b bytes.Buffer
	if err := rdf.Write(&b, d, rdf.Options{Provenance: prov}); err != nil {
		t.Fatalf("write: %v", err)
	}
	return b.String()
}

func TestARealPaperWritesTheTermsArxivPublishes(t *testing.T) {
	d, p := paperDoc(t)
	out := nt(t, d, false)
	paper := "<https://arxiv.org/abs/1706.03762>"

	for _, want := range []string{
		paper + " <" + string(rdf.DCTitle) + `> "Attention Is All You Need" .`,
		paper + " <" + string(rdf.SchemaName) + `> "Attention Is All You Need" .`,
		paper + " <" + string(rdf.DCDate) + `> "2017-06-12"^^<` + string(rdf.XSDDate) + "> .",
		paper + " <" + string(rdf.RDFType) + "> <" + string(rdf.SchemaScholarlyArticle) + "> .",
		paper + " <" + string(rdf.RDFType) + "> <" + string(rdf.FabioPreprint) + "> .",
		paper + " <" + string(rdf.DCSubject) + "> <https://tamnd.github.io/arxiv-cli/id/category/cs.CL> .",
		paper + " <" + string(rdf.SchemaLicense) + "> <http://arxiv.org/licenses/nonexclusive-distrib/1.0/> .",
		paper + " <" + string(rdf.SchemaIdentifier) + "> <https://doi.org/10.48550/arxiv.1706.03762> .",
		// The category's name came off a claim's note rather than off a
		// category record, so it is a label and not a preferred label: the
		// paper page named it in passing, the taxonomy is what names it
		// properly.
		"<https://tamnd.github.io/arxiv-cli/id/category/cs.CL> <" + string(rdf.RDFSLabel) + `> "Computation and Language" .`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing:\n  %s", want)
		}
	}

	// Eight authors, each written under both terms, each pointing at a name
	// node and none of them pointing back.
	if n := strings.Count(out, "<"+string(rdf.SchemaAuthor)+">"); n != len(p.Authors) {
		t.Errorf("%d schema:author statements for %d authors", n, len(p.Authors))
	}
	if n := strings.Count(out, "<"+string(rdf.DCCreator)+">"); n != len(p.Authors) {
		t.Errorf("%d dc:creator statements for %d authors", n, len(p.Authors))
	}
	if !strings.Contains(out, "<https://tamnd.github.io/arxiv-cli/id/name/aidan-n-gomez> <"+string(rdf.SchemaName)+">") &&
		!strings.Contains(out, "<https://tamnd.github.io/arxiv-cli/id/name/aidan-n-gomez> <"+string(rdf.RDFSLabel)+`> "Aidan N. Gomez"`) {
		t.Error("the spelling behind the name slug is not in the output")
	}

	// The version history is prov, and the versions are versioned URLs rather
	// than nodes of their own.
	if !strings.Contains(out, paper+" <"+string(rdf.PROVHadRevision)+"> <https://arxiv.org/abs/1706.03762v1> .") {
		t.Error("v1 is not written as a revision of the paper")
	}
	if !strings.Contains(out, "<https://arxiv.org/abs/1706.03762v2> <"+string(rdf.PROVWasRevisionOf)+"> <https://arxiv.org/abs/1706.03762v1> .") {
		t.Error("the version order is not written")
	}
}

// A literal with no page behind it is a fact from nowhere. Every one of them
// names the URL that answered, which is what the record's via map is for.
func TestEveryLiteralNamesThePageThatAnsweredForIt(t *testing.T) {
	d, _ := paperDoc(t)
	for _, s := range d.Statements() {
		if _, ok := s.Object.(rdf.Literal); !ok {
			continue
		}
		if s.Predicate == rdf.SchemaEncodingFmt {
			// The one literal nobody read. A PDF served from /pdf/ is a PDF
			// because of where it is, not because a page said so, and it is
			// sourceless for the same reason an inferred type is.
			continue
		}
		if len(s.Sources) == 0 {
			t.Errorf("%s %s has no source", s.Subject, s.Predicate)
		}
	}

	// And the title's source is the surface the record says answered for it,
	// not whichever page happened to be read last.
	out := nt(t, d, true)
	want := "<" + string(rdf.DCTitle) + `> "Attention Is All You Need" >> <` + string(rdf.PROVDerivedFrom) + "> <https://export.arxiv.org/api/query>"
	if !strings.Contains(out, want) {
		t.Errorf("the title's provenance is not the export API:\n%s", firstLines(out, 12))
	}
}

// The PDF is a thing with an address rather than a string on the paper, and it
// is the same node the has_file claim points at, so the two join instead of
// being two facts about two different things.
func TestThePDFIsOneNodeAndNotTwo(t *testing.T) {
	d, _ := paperDoc(t)
	out := nt(t, d, false)
	pdf := "<https://arxiv.org/pdf/1706.03762v7>"
	if !strings.Contains(out, "<https://arxiv.org/abs/1706.03762> <"+string(rdf.SchemaEncoding)+"> "+pdf+" .") {
		t.Errorf("the paper does not encode the pdf:\n%s", out)
	}
	if !strings.Contains(out, pdf+" <"+string(rdf.RDFType)+"> <"+string(rdf.SchemaMediaObject)+"> .") {
		t.Error("the pdf is not a media object")
	}
	if n := strings.Count(out, "<"+string(rdf.SchemaEncoding)+"> <https://arxiv.org/pdf/"); n != 1 {
		t.Errorf("%d encodings point at a pdf, want one node and not two spellings of it", n)
	}
}

// A category is a concept in the taxonomy scheme, which is what makes the tree
// walkable by something that has never heard of arXiv.
func TestACategoryRecordIsAConceptInTheScheme(t *testing.T) {
	d := rdf.New()
	c := Category{
		Envelope: Envelope{Kind: "category", Surfaces: []string{SurfaceTaxonomy}, Sources: []string{taxonomyURL}},
		Code:     "cs.CL", Name: "Computation and Language", Archive: "cs", Group: "Computer Science",
		Description: "Covers natural language processing.",
	}
	AddCategory(d, c)
	out := nt(t, d, false)
	code := "<https://tamnd.github.io/arxiv-cli/id/category/cs.CL>"
	for _, want := range []string{
		code + " <" + string(rdf.RDFType) + "> <" + string(rdf.SKOSConcept) + "> .",
		code + " <" + string(rdf.SKOSInScheme) + "> <" + string(rdf.Scheme) + "> .",
		code + " <" + string(rdf.SKOSPrefLabel) + `> "Computation and Language" .`,
		code + " <" + string(rdf.SKOSDefinition) + `> "Covers natural language processing." .`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing:\n  %s\ngot:\n%s", want, out)
		}
	}
}

// A name search matched a string. Writing schema:name on that node would say
// this is a person called that, and doc 04 section 2 is one long argument for
// why it is not.
func TestANameSearchGetsNoPersonsName(t *testing.T) {
	search := "https://arxiv.org/a/search?query=John+Baez"
	d := rdf.New()
	AddPerson(d, Person{
		Envelope: Envelope{Kind: "author", Surfaces: []string{SurfaceSearch}, Sources: []string{search}},
		Name:     "John Baez", Mode: "name search", URI: graph.Name("John Baez"),
	})
	out := nt(t, d, false)
	if strings.Contains(out, string(rdf.SchemaName)) {
		t.Errorf("a matched string was given a person's name:\n%s", out)
	}
	if !strings.Contains(out, `<`+string(rdf.RDFSLabel)+`> "John Baez"`) {
		t.Errorf("the spelling searched for is gone:\n%s", out)
	}

	// An identifier page is arXiv saying a person exists, so that one does get
	// a name.
	d = rdf.New()
	page := "https://arxiv.org/a/baez_j_1"
	AddPerson(d, Person{
		Envelope: Envelope{Kind: "author", Surfaces: []string{SurfaceAuthorID}, Sources: []string{page}},
		Name:     "John Baez", Mode: "identifier page", Identified: true, ArxivID: "baez_j_1", URI: graph.Author("baez_j_1"),
	})
	out = nt(t, d, false)
	if !strings.Contains(out, "<https://arxiv.org/a/baez_j_1> <"+string(rdf.SchemaName)+`> "John Baez" .`) {
		t.Errorf("an identified person has no name:\n%s", out)
	}
}

// Nothing in a real record should hit the refusal path, and if something does
// it is a node kind nobody taught this package rather than bad data.
func TestARealRecordLosesNothingInTranslation(t *testing.T) {
	d, _ := paperDoc(t)
	if n := d.Refused(); n != 0 {
		t.Errorf("%d claims off a real paper could not be named", n)
	}
	edges := EdgesOfPaper(fullRecord(t))
	written := map[string]bool{}
	for _, s := range d.Statements() {
		written[string(s.Predicate)] = true
	}
	for _, e := range edges {
		row, ok := rdf.Predicate(e.Predicate)
		if !ok {
			t.Errorf("%s is a predicate the mapping has never heard of", e.Predicate)
			continue
		}
		for _, term := range row.Terms {
			if !written[string(term)] {
				t.Errorf("%s should have written %s and did not", e.Predicate, term)
			}
		}
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
