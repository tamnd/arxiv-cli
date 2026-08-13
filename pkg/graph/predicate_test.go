package graph

import (
	"strings"
	"testing"
)

// Twenty predicates, and the count is asserted so that adding a twenty first
// has to be a decision rather than an accident.
func TestTheTableIsTwenty(t *testing.T) {
	if len(Predicates) != 20 {
		t.Errorf("the table has %d predicates, want 20", len(Predicates))
	}
	seen := map[string]bool{}
	for _, p := range Predicates {
		if seen[p.Name] {
			t.Errorf("%q is in the table twice", p.Name)
		}
		seen[p.Name] = true
	}
}

// Every row needs both ends, at least one surface allowed to assert it, and a
// sentence saying what it means. A predicate with no domain is a predicate
// nothing can check.
func TestEveryPredicateIsComplete(t *testing.T) {
	for _, p := range Predicates {
		if len(p.From) == 0 || len(p.To) == 0 {
			t.Errorf("%s has no domain or no range", p.Name)
		}
		if len(p.Surfaces) == 0 {
			t.Errorf("%s says no surface may assert it, so nothing can write it", p.Name)
		}
		if p.Help == "" {
			t.Errorf("%s has no help", p.Name)
		}
		for _, kind := range append(append([]string{}, p.From...), p.To...) {
			if !allows(Kinds, kind) {
				t.Errorf("%s names %q, which is not a node kind", p.Name, kind)
			}
		}
		for _, s := range p.Surfaces {
			if !strings.HasPrefix(s, "s") {
				t.Errorf("%s names surface %q", p.Name, s)
			}
		}
	}
}

// A predicate not in the table cannot be written, and this is where that is
// enforced rather than in a comment.
func TestValidateRefusesAPredicateOutsideTheTable(t *testing.T) {
	e := Edge{
		From:      Paper("1706.03762"),
		Predicate: "cited_by_count",
		To:        Paper("2401.00001"),
		Source:    "https://export.arxiv.org/api/query",
		Surface:   "s1",
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("a predicate nobody defined was accepted")
	}
	if !strings.Contains(err.Error(), "is not a predicate") {
		t.Errorf("%v does not say why", err)
	}
}

func TestValidateChecksBothEnds(t *testing.T) {
	cases := []struct {
		why  string
		edge Edge
		want string
	}{
		{
			"authored runs from a name, not from a paper",
			Edge{From: Paper("1706.03762"), Predicate: Authored, To: Name("Ashish Vaswani"), Surface: "s1", Source: "u"},
			"runs from",
		},
		{
			"a paper is licensed under a license and not under a category",
			Edge{From: Paper("1706.03762"), Predicate: LicensedUnder, To: Category("cs.CL"), Surface: "s2", Source: "u"},
			"runs to",
		},
		{
			"neither end may be something that is not a uri at all",
			Edge{From: "1706.03762", Predicate: HasDOI, To: DOI("10.1038/x"), Surface: "s1", Source: "u"},
			"not an ax:// uri",
		},
		{
			"a claim nobody made is not a claim",
			Edge{From: Paper("1706.03762"), Predicate: HasDOI, To: DOI("10.1038/x"), Surface: "s1"},
			"no source",
		},
	}
	for _, c := range cases {
		err := c.edge.Validate()
		if err == nil {
			t.Errorf("%s: accepted", c.why)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v does not mention %q", c.why, err, c.want)
		}
	}
}

// cites is the one that has to be locked to a surface. arXiv publishes no
// citation graph, so a cites row from anywhere but the rendered bibliography
// would be something this tool made up.
func TestCitesOnlyComesFromTheRenderedBibliography(t *testing.T) {
	e := Edge{
		From:      Paper("1706.03762"),
		Predicate: Cites,
		To:        Paper("1409.0473"),
		Source:    "https://arxiv.org/html/1706.03762v7",
		Surface:   "s10",
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("the real case was refused: %v", err)
	}
	e.Surface = "s1"
	e.Source = "https://export.arxiv.org/api/query"
	if err := e.Validate(); err == nil {
		t.Error("the export API was allowed to assert a citation")
	}
}

// A trackback is somebody else's page linking to the paper, so the external
// page is the subject. Writing it the other way round would say the paper cites
// the blog, which is backwards and would corrupt any merge with a real citation
// set.
func TestLinkedByPointsInward(t *testing.T) {
	ok := Edge{
		From:      External("https://example.org/a-post"),
		Predicate: LinkedBy,
		To:        Paper("1706.03762"),
		Source:    "https://arxiv.org/tb/1706.03762",
		Surface:   "s11",
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("the right direction was refused: %v", err)
	}
	backwards := Edge{From: ok.To, Predicate: LinkedBy, To: ok.From, Source: ok.Source, Surface: ok.Surface}
	if err := backwards.Validate(); err == nil {
		t.Error("the backwards direction was accepted")
	}
}

// authored reads backwards compared to the record, where authors are a list on
// the paper. It is that way round because the query worth answering is
// everything this name touched, and a claim indexed from the name answers it
// without a scan.
func TestAuthoredRunsFromTheName(t *testing.T) {
	e := Edge{
		From:      Name("Ashish Vaswani"),
		Predicate: Authored,
		To:        Paper("1706.03762"),
		Source:    "https://export.arxiv.org/api/query?id_list=1706.03762",
		Surface:   "s1",
		Position:  1,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("%v", err)
	}
	// A registered person may also be the subject, because s8 gives one.
	e.From = Author("baez_j_1")
	e.Surface = "s8"
	if err := e.Validate(); err != nil {
		t.Errorf("an author identifier was refused as an author: %v", err)
	}
}

// A version edge stays inside the paper space, which is what makes has_version
// a fragment relation rather than a join to somewhere else.
func TestVersionEdges(t *testing.T) {
	src := "https://export.arxiv.org/oai2"
	has := Edge{From: Paper("1706.03762"), Predicate: HasVersion, To: Version("1706.03762", 7), Source: src, Surface: "s2"}
	if err := has.Validate(); err != nil {
		t.Errorf("has_version: %v", err)
	}
	sup := Edge{From: Version("1706.03762", 7), Predicate: Supersedes, To: Version("1706.03762", 6), Source: src, Surface: "s2"}
	if err := sup.Validate(); err != nil {
		t.Errorf("supersedes: %v", err)
	}
}

// Note and Position are labels rather than assertions, so two sightings of one
// claim are one row and a later one carrying a label can fill in an earlier one
// that did not.
func TestKeyLeavesOutTheLabels(t *testing.T) {
	a := Edge{From: Name("A B"), Predicate: Authored, To: Paper("1706.03762"), Source: "u", Surface: "s1"}
	b := a
	b.Note = "Computation and Language"
	b.Position = 3
	if a.Key() != b.Key() {
		t.Error("a label changed the identity of the claim")
	}
	// The source is part of the identity, so two surfaces asserting one fact
	// stay two rows and a disagreement stays queryable.
	c := a
	c.Source = "https://arxiv.org/abs/1706.03762"
	if a.Key() == c.Key() {
		t.Error("two sources collapsed into one claim")
	}
}

func TestLookupAndNames(t *testing.T) {
	if _, ok := Lookup(Authored); !ok {
		t.Error("authored is not in the index")
	}
	if _, ok := Lookup("nonsense"); ok {
		t.Error("a predicate nobody defined was found")
	}
	names := Names()
	if len(names) != len(Predicates) {
		t.Errorf("Names has %d entries, the table has %d", len(names), len(Predicates))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("Names is not sorted: %q before %q", names[i-1], names[i])
		}
	}
}
