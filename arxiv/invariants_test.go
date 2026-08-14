package arxiv

import (
	"go/ast"
	"go/token"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
	"github.com/tamnd/arxiv-cli/pkg/rdf"
)

// invariants_test.go holds the counting arguments. Doc 01 says there are twelve
// surfaces and doc 02 says there are two planes, and both numbers appear in
// help text, in the printed tables and in every explanation of what this tool
// does. A number that is stated that often has to be checked somewhere, because
// the failure mode is not a crash: it is a thirteenth surface read by one
// function that nothing else knows about, and a record whose provenance column
// says a surface the taxonomy has never heard of.
//
// These read the package as source where a value cannot answer the question.
// Whether a surface is spelled as a constant is not something the running
// program can be asked, and it is exactly the thing that rots, so the test
// parses the files instead.
//
// The field census is not here. It belongs with `arxiv fields`, which does not
// exist yet, and a census against a command that has not been written would be
// a list maintained by hand pretending to be generated.

// surfaceConstants parses model.go and returns every Surface constant as name
// to id. Reading the declaration rather than listing the names here is what
// makes the count below a count of what is declared, instead of a count of what
// somebody remembered to add to a test.
func surfaceConstants(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	goFiles(t, func(path string, _ *token.FileSet, file *ast.File) {
		if path != "arxiv/model.go" {
			return
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				if !strings.HasPrefix(vs.Names[0].Name, "Surface") {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				id, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("surface %s is %s: %v", vs.Names[0].Name, lit.Value, err)
				}
				out[vs.Names[0].Name] = id
			}
		}
	})
	if len(out) == 0 {
		t.Fatal("no Surface constants found in model.go, so this file is checking nothing")
	}
	return out
}

// Twelve, numbered s1 to s12 with no gap and no repeat, and SurfaceNames holds
// exactly those ids. The numbering matters because the ids are printed in
// provenance and quoted in the docs, so s7 has to keep meaning the taxonomy.
func TestThereAreExactlyTwelveSurfaces(t *testing.T) {
	consts := surfaceConstants(t)
	if len(consts) != 12 {
		t.Errorf("%d surface constants, and every document in the spec says twelve", len(consts))
	}

	byID := map[string]string{}
	for name, id := range consts {
		if other, dup := byID[id]; dup {
			t.Errorf("%s and %s are both %s", name, other, id)
		}
		byID[id] = name
	}
	for i := 1; i <= 12; i++ {
		id := "s" + strconv.Itoa(i)
		if byID[id] == "" {
			t.Errorf("nothing is %s, so the numbering has a hole in it", id)
		}
	}

	for id, name := range byID {
		if SurfaceNames[id] == "" {
			t.Errorf("%s is %s and has no name, so `arxiv planes` prints a blank row", name, id)
		}
	}
	for id := range SurfaceNames {
		if byID[id] == "" {
			t.Errorf("SurfaceNames has %s, which is not a declared surface", id)
		}
	}
}

// Every surface is read by something. A constant nothing references is either a
// surface that was described and never implemented or one whose reader was
// deleted, and both leave a documented surface that returns nothing.
func TestEverySurfaceIsClaimedByARead(t *testing.T) {
	consts := surfaceConstants(t)
	used := map[string]bool{}
	goFiles(t, func(path string, _ *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(path, "arxiv/") || path == "arxiv/model.go" || strings.HasSuffix(path, "_test.go") {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if ok && consts[id.Name] != "" {
				used[id.Name] = true
			}
			return true
		})
	})

	var orphans []string
	for name := range consts {
		if !used[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		t.Errorf("%s is %s and nothing outside model.go reads it", name, consts[name])
	}
}

// A surface id is a constant everywhere in this package. The bare string is the
// way a typo survives: "s4" in a call site compiles, prints, and is wrong in a
// column nobody reads until they are chasing where a claim came from.
//
// The rule is this package only. pkg/graph and pkg/rdf hold the ids as data,
// and neither may import this one.
func TestNoSurfaceIsSpelledAsALiteral(t *testing.T) {
	consts := surfaceConstants(t)
	ids := map[string]bool{}
	for _, id := range consts {
		ids[id] = true
	}
	goFiles(t, func(path string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(path, "arxiv/") || path == "arxiv/model.go" {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil || !ids[text] {
				return true
			}
			t.Errorf("%s:%d writes %s as a literal, and there is a constant for it",
				path, fset.Position(lit.Pos()).Line, lit.Value)
			return true
		})
	})
}

// One URL per surface, built from the base the reader uses. These are the
// shapes surfaceOfURL sees in a provenance column, so the table doubles as a
// list of what a source URL looks like for each surface.
var surfaceURLs = map[string][]string{
	SurfaceAPI:       {apiBase + "?id_list=1706.03762"},
	SurfaceOAI:       {oaiBase + "?verb=ListSets"},
	SurfaceAbs:       {absBase + "1706.03762v7"},
	SurfaceList:      {listBase + "cs.CL/2026-01"},
	SurfaceSearch:    {s5Base + "?searchtype=all&query=attention"},
	SurfaceRSS:       {rssBase + "cs.CL"},
	SurfaceTaxonomy:  {taxonomyURL},
	SurfaceAuthorID:  {authorBase + "baez_j_1"},
	SurfaceBibTeX:    {bibtexBase + "1706.03762"},
	SurfaceFullText:  {htmlBase + "1706.03762v7"},
	SurfaceTrackback: {trackbackBase + "1706.03762", trackbackRecent},
	SurfaceFiles:     {pdfBase + "1706.03762v7", srcBase + "1706.03762"},
}

// surfaceOfURL answers for all twelve and for nothing else.
//
// The two halves are separate failures. Missing a surface means a claim read
// off a real page carries an empty provenance column. Answering for a foreign
// URL is worse, because it labels somebody else's page as arXiv's and the row
// looks fine.
func TestSurfaceOfURLCoversTheTwelveAndNothingElse(t *testing.T) {
	consts := surfaceConstants(t)
	for _, id := range consts {
		if len(surfaceURLs[id]) == 0 {
			t.Errorf("%s has no URL in the table, so nothing checks that surfaceOfURL knows it", id)
		}
	}
	for want, urls := range surfaceURLs {
		for _, u := range urls {
			if got := surfaceOfURL(u); got != want {
				t.Errorf("surfaceOfURL(%s) = %q, want %s", u, got, want)
			}
		}
	}
	for _, u := range []string{
		"https://example.org/abs/1706.03762",
		"https://arxiv.org/",
		"https://arxiv.org/format/1706.03762",
		"https://scholar.google.com/",
		"snapshot:2026-08-13",
		"",
	} {
		if got := surfaceOfURL(u); got != "" {
			t.Errorf("surfaceOfURL(%q) = %q, and that URL is not a surface this tool reads", u, got)
		}
	}
}

// Every URL this tool builds is on a host with a pace. A host that resolves to
// no plane is a request with no limiter in front of it, which is the one thing
// doc 02 promises cannot happen.
func TestEveryURLThisToolBuildsIsOnAPlane(t *testing.T) {
	for surface, urls := range surfaceURLs {
		for _, raw := range urls {
			u, err := url.Parse(raw)
			if err != nil {
				t.Errorf("%s reads %s, which does not parse: %v", surface, raw, err)
				continue
			}
			plane, ok := PlaneFor(u.Host)
			if !ok {
				t.Errorf("%s reads %s, and %s is not on either plane", surface, raw, u.Host)
				continue
			}
			if u.Scheme != "https" {
				t.Errorf("%s reads %s over %s", surface, raw, u.Scheme)
			}
			if plane.Pace < plane.Floor {
				t.Errorf("the %s plane paces at %s under a floor of %s", plane.Name, plane.Pace, plane.Floor)
			}
		}
	}
}

// Two planes, and a host belongs to one of them. The lookup walks the table and
// returns the first match, so a host listed twice would take whichever pace
// happened to be written first, which is a fifteen second promise decided by
// line order.
func TestTheTwoPlanesDoNotShareAHost(t *testing.T) {
	if len(Planes) != 2 {
		t.Errorf("%d planes, and the spec measured two", len(Planes))
	}
	seen := map[string]string{}
	for _, p := range Planes {
		if len(p.Hosts) == 0 {
			t.Errorf("the %s plane has no hosts", p.Name)
		}
		if p.Why == "" {
			t.Errorf("the %s plane paces at %s with no evidence for the number", p.Name, p.Pace)
		}
		for _, h := range p.Hosts {
			if other, dup := seen[h]; dup {
				t.Errorf("%s is on both the %s and the %s plane", h, other, p.Name)
			}
			seen[h] = p.Name
		}
	}
	if _, ok := PlaneFor("example.org"); ok {
		t.Error("a host nobody listed resolved to a plane")
	}
}

// The predicate table names real surfaces. Every row says which surfaces may
// assert it, and a row naming s13 would refuse every claim from the surface it
// meant to allow, quietly, at write time.
func TestEveryPredicateNamesRealSurfaces(t *testing.T) {
	for _, p := range graph.Predicates {
		if len(p.Surfaces) == 0 {
			t.Errorf("%s may be asserted by nothing, so it can never be written", p.Name)
		}
		for _, id := range p.Surfaces {
			if SurfaceNames[id] == "" {
				t.Errorf("%s allows %s, which is not one of the twelve", p.Name, id)
			}
		}
	}
}

// The RDF mapping cites its surfaces in prose, so this pulls the ids out of the
// sentence and checks them. The evidence column is what somebody reads to
// decide whether a term was chosen well, and a citation to a surface that does
// not exist makes the whole column worth less.
func TestEveryRDFRowCitesRealSurfaces(t *testing.T) {
	for _, row := range rdf.Mapping {
		for _, word := range strings.FieldsFunc(row.Evidence, func(r rune) bool {
			return r == ' ' || r == ',' || r == ';'
		}) {
			if !isSurfaceID(word) {
				continue
			}
			if SurfaceNames[word] == "" {
				t.Errorf("the %s row cites %s, which is not one of the twelve", row.What, word)
			}
		}
	}
}

// isSurfaceID is the shape of an id: an s and then digits. It is deliberately
// shape rather than membership, because a row citing s13 has to reach the check
// above rather than be skipped as prose.
func isSurfaceID(word string) bool {
	if len(word) < 2 || word[0] != 's' {
		return false
	}
	for _, r := range word[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
