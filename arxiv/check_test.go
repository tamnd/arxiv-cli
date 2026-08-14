package arxiv

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
	"github.com/tamnd/arxiv-cli/pkg/rdf"
)

// The check is tested against saved copies of the two surfaces it reads. The
// live version of the date test is in live_test.go, because the assertion worth
// making about arXiv's dates is about what arXiv publishes today, and a fixture
// cannot notice OAI changing its mind.

// dcFixture decodes a saved oai_dc record.
func dcFixture(t *testing.T, name string) oaiDC {
	t.Helper()
	var resp oaiResponse
	if err := xml.Unmarshal(fixture(t, name), &resp); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if code := resp.Error.Code; code != "" {
		t.Fatalf("%s carries an oai error: %s", name, code)
	}
	return resp.GetRecord.Record.Metadata.DC
}

// tagsFixture reads the citation tags off a saved abstract page, through the
// same parser the command uses.
func tagsFixture(t *testing.T, name string) map[string][]string {
	t.Helper()
	tags, err := citationTagsFrom(fixture(t, name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return tags
}

// higgsRecord is the ATLAS Higgs paper read to depth full. It is the second
// full record in the suite and it is a different shape to the first: one author
// and that author a collaboration, a journal reference, a publisher DOI, and two
// dates a month apart.
func higgsRecord(t *testing.T) Paper {
	t.Helper()
	p := paperFixture(t, "api_1207.7214.xml")
	mergeOAIArxiv(&p, oaiFixture(t, "oai_arxiv_1207.7214.xml"),
		"https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3A1207.7214&metadataPrefix=arXiv")
	mergeAbs(&p, absFixture(t, "abs_1207.7214.html"), "https://arxiv.org/abs/1207.7214")
	annotateDepth(&p, DepthFull)
	return p
}

// docOf is a record and its claims as one document, which is what the writer
// gets and so what the check compares against.
func docOf(p Paper) *rdf.Doc {
	d := rdf.New()
	AddPaper(d, p)
	AddClaims(d, EdgesOfPaper(p))
	return d
}

// paperIRI is the subject every row is about.
func paperIRI(p Paper) rdf.IRI { return rdf.NodeIRI(graph.Paper(p.ID)) }

// checkOf runs the comparison over saved bytes, by the row's name.
func checkOf(t *testing.T, p Paper, dcFile, absFile string) map[string]CheckRow {
	t.Helper()
	rows := compare(docOf(p), paperIRI(p), dcFixture(t, dcFile), tagsFixture(t, absFile))
	out := map[string]CheckRow{}
	for _, r := range rows {
		out[r.Predicate] = r
	}
	if len(out) != len(rows) {
		t.Fatalf("two rows share a name in a table of %d", len(rows))
	}
	return out
}

func TestTheThreeSurfacesAgreeOnATitle(t *testing.T) {
	for _, c := range []struct {
		paper Paper
		dc    string
		abs   string
	}{
		{fullRecord(t), "oai_dc_1706.03762.xml", "abs_1706.03762.html"},
		{higgsRecord(t), "oai_dc_1207.7214.xml", "abs_1207.7214.html"},
	} {
		rows := checkOf(t, c.paper, c.dc, c.abs)
		if got := rows["dc:title"].Agree; got != AgreeYes {
			t.Errorf("%s: the title reads %q, not %q", c.dc, got, AgreeYes)
		}
	}
}

// The one row the command exists for. oai_dc publishes the submission date and
// the last update in two dc:date elements and says nothing about which is which,
// this tool writes the submission, and that is a disagreement rather than a bug
// in either of them.
func TestTheDateRowStillDisagrees(t *testing.T) {
	rows := checkOf(t, higgsRecord(t), "oai_dc_1207.7214.xml", "abs_1207.7214.html")
	date := rows["dc:date"]
	if date.Agree == AgreeYes || date.Agree == AgreeNormalised {
		t.Fatalf("dc:date reads %q, so the surfaces have started agreeing: %v vs %v", date.Agree, date.OursValues, date.DCValues)
	}
	if len(date.OursValues) != 1 || date.OursValues[0] != "2012-07-31" {
		t.Errorf("this tool writes %v, want the submission date on its own", date.OursValues)
	}
	if !contains(date.DCValues, "2012-08-31") {
		t.Errorf("oai_dc no longer publishes the second date: %v", date.DCValues)
	}
	if date.Note == "" {
		t.Error("a row that disagrees says nothing about why")
	}
}

// Three surfaces, three name formats. Two of them write the surname first and
// this tool writes the name as it reads, so the row only agrees after
// normalising, and saying so is the point of having a fourth verdict.
func TestNamesAgreeOnlyAfterNormalising(t *testing.T) {
	rows := checkOf(t, fullRecord(t), "oai_dc_1706.03762.xml", "abs_1706.03762.html")
	creator := rows["dc:creator"]
	if creator.Agree != AgreeNormalised {
		t.Fatalf("dc:creator reads %q, want %q: %v vs %v", creator.Agree, AgreeNormalised, creator.OursValues, creator.DCValues)
	}
	if len(creator.OursValues) != 8 || len(creator.DCValues) != 8 {
		t.Errorf("%d names here and %d there, want eight each", len(creator.OursValues), len(creator.DCValues))
	}
	if !contains(creator.OursValues, "Ashish Vaswani") {
		t.Errorf("this tool writes %v, want the names as arXiv spells them in the API", creator.OursValues)
	}
	if !contains(creator.DCValues, "Vaswani, Ashish") {
		t.Errorf("oai_dc writes %v, want surname first", creator.DCValues)
	}

	// A collaboration has no comma to split on and must survive the normaliser
	// whole, because "The ATLAS Collaboration" is not a surname.
	higgs := checkOf(t, higgsRecord(t), "oai_dc_1207.7214.xml", "abs_1207.7214.html")
	if got := higgs["dc:creator"].Agree; got != AgreeYes {
		t.Errorf("a collaboration reads %q, and it is the same string on all three: %v", got, higgs["dc:creator"].OursValues)
	}
}

// A category is a code here and a name there, and the row compares the names,
// because the code is nowhere in oai_dc at all.
func TestTheSubjectRowComparesNames(t *testing.T) {
	rows := checkOf(t, fullRecord(t), "oai_dc_1706.03762.xml", "abs_1706.03762.html")
	subject := rows["dc:subject"]
	if subject.Agree != AgreeYes {
		t.Fatalf("dc:subject reads %q: %v vs %v", subject.Agree, subject.OursValues, subject.DCValues)
	}
	if !contains(subject.OursValues, "Computation and Language") {
		t.Errorf("the category node has no name on it: %v", subject.OursValues)
	}
	if strings.Contains(subject.Ours, "cs.CL") {
		t.Errorf("the row compares a code against a name: %q", subject.Ours)
	}
}

// A surface that does not carry a fact has not contradicted anybody, and a term
// this tool writes alone is not a disagreement. The licence is the case: it is
// in the arXiv format's own license element, and oai_dc has no dc:rights.
func TestATermNobodyElseWritesIsNotADisagreement(t *testing.T) {
	rows := checkOf(t, fullRecord(t), "oai_dc_1706.03762.xml", "abs_1706.03762.html")
	rights := rows["dc:rights"]
	if rights.Agree != AgreeAlone {
		t.Fatalf("dc:rights reads %q, want %q: oai_dc has %v", rights.Agree, AgreeAlone, rights.DCValues)
	}
	if rights.OAIDC != "absent" || rights.Citation != "absent" {
		t.Errorf("a silent surface prints %q and %q, want absent", rights.OAIDC, rights.Citation)
	}
	if len(rights.OursValues) != 1 || !strings.Contains(rights.OursValues[0], "arxiv.org/licenses/") {
		t.Errorf("the licence is not in the row: %v", rights.OursValues)
	}
}

// The type row is the one that is meant to read no. arXiv says text, which is a
// DCMI type, and the classes this tool writes are its own reading of what a
// preprint is, which is why they carry no provenance.
func TestTheTypeRowSaysWhatArxivSaidAndWhatWeSaid(t *testing.T) {
	rows := checkOf(t, fullRecord(t), "oai_dc_1706.03762.xml", "abs_1706.03762.html")
	kind := rows["dc:type"]
	if kind.Agree != AgreeNo {
		t.Fatalf("dc:type reads %q, want %q", kind.Agree, AgreeNo)
	}
	if !contains(kind.DCValues, "text") {
		t.Errorf("oai_dc no longer says text: %v", kind.DCValues)
	}
	if !contains(kind.OursValues, "https://schema.org/ScholarlyArticle") {
		t.Errorf("the classes this tool writes are not in the row: %v", kind.OursValues)
	}
}

// Every row is filled in, including the sides that said nothing, because a table
// with a hole in it reads as a fact nobody looked for.
func TestEveryRowIsAnswered(t *testing.T) {
	rows := compare(docOf(fullRecord(t)), paperIRI(fullRecord(t)),
		dcFixture(t, "oai_dc_1706.03762.xml"), tagsFixture(t, "abs_1706.03762.html"))
	if len(rows) != len(elements) {
		t.Fatalf("%d rows for %d terms", len(rows), len(elements))
	}
	for _, r := range rows {
		if r.Agree == "" {
			t.Errorf("%s has no verdict", r.Predicate)
		}
		if r.Ours == "" || r.OAIDC == "" || r.Citation == "" {
			t.Errorf("%s has an empty cell: %q %q %q", r.Predicate, r.Ours, r.OAIDC, r.Citation)
		}
		// A cell is elided to keep the table readable and the values are not,
		// because the whole abstract is the thing being compared.
		if len([]rune(r.Ours)) > 56 {
			t.Errorf("%s prints a cell %d runes wide", r.Predicate, len([]rune(r.Ours)))
		}
		if r.Predicate == "dc:description" && len(r.OursValues[0]) < 500 {
			t.Errorf("the abstract was elided in the values as well: %d characters", len(r.OursValues[0]))
		}
	}
}

// The normalisers, on the spellings the three surfaces actually use.
func TestTheNormalisersFoldWhatTheyShould(t *testing.T) {
	for _, c := range []struct {
		what string
		norm func(string) string
		in   string
		want string
	}{
		{"a date with slashes", normDate, "2012/07/31", "2012-07-31"},
		{"a timestamp", normDate, "2023-08-02T00:41:18Z", "2023-08-02"},
		{"a name surname first", normName, "Vaswani, Ashish", "ashish vaswani"},
		{"a name with a particle", normName, "van den Berg, Rianne", "rianne van den berg"},
		{"a collaboration", normName, "The ATLAS Collaboration", "the atlas collaboration"},
		{"a doi as a url", normIdent, "https://doi.org/10.1016/j.physletb.2012.08.020", "10.1016/j.physletb.2012.08.020"},
		{"a doi with the scheme", normIdent, "doi:10.1016/j.physletb.2012.08.020", "10.1016/j.physletb.2012.08.020"},
		{"an abs url over http", normIdent, "http://arxiv.org/abs/1207.7214", "arxiv.org/abs/1207.7214"},
		{"a class", normType, "https://schema.org/ScholarlyArticle", "scholarlyarticle"},
		{"a title with a line break in it", normText, "Attention Is\n  All You Need", "attention is all you need"},
	} {
		if got := c.norm(c.in); got != c.want {
			t.Errorf("%s: %q became %q, want %q", c.what, c.in, got, c.want)
		}
	}
}
