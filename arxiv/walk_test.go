package arxiv

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// loudClient is a client that keeps what it said, which is how these tests
// check that a walk reports what it left unread rather than leaving it to be
// inferred from a short answer.
func loudClient(t *testing.T) (*Client, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Log = &buf
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, &buf
}

// testWalk is a walk with the seed's claims already in it and nothing read.
func testWalk(t *testing.T, o WalkOptions) (*walk, *bytes.Buffer) {
	t.Helper()
	c, buf := loudClient(t)
	if o.Budget == 0 {
		o.Budget = 25
	}
	if o.Limit == 0 {
		o.Limit = 25
	}
	w := &walk{c: c, opts: o, expanded: map[string]bool{}, label: map[string]string{}}
	edges := EdgesOfPaper(fullRecord(t))
	w.set.addAll(edges)
	w.note(edges)
	w.expanded[graph.Paper("1706.03762")] = true
	return w, buf
}

func TestFilterEdges(t *testing.T) {
	edges := EdgesOfPaper(fullRecord(t))

	if got := filterEdges(edges, nil); len(got) != len(edges) {
		t.Errorf("no filter kept %d of %d claims", len(got), len(edges))
	}
	got := filterEdges(edges, []string{graph.Authored, graph.HasDOI})
	if len(got) != 9 {
		t.Errorf("got %d claims, want eight authors and one DOI", len(got))
	}
	for _, e := range got {
		if e.Predicate != graph.Authored && e.Predicate != graph.HasDOI {
			t.Errorf("the filter let %s through", e.Predicate)
		}
	}
	// A predicate nothing asserted is an empty answer rather than an error.
	// arxiv edges refuses a name that is not a predicate at all before it gets
	// this far, which is the case worth an error.
	if got := filterEdges(edges, []string{graph.Cites}); len(got) != 0 {
		t.Errorf("got %d cites claims off a metadata read", len(got))
	}
}

func TestKnownPredicatesRefusesATypo(t *testing.T) {
	if err := knownPredicates([]string{graph.Authored, graph.Cites}); err != nil {
		t.Errorf("two real predicates were refused: %v", err)
	}
	err := knownPredicates([]string{"authoerd"})
	if err == nil {
		t.Fatal("a typo was accepted, and it would come back as an empty result reading like arXiv says nothing about this")
	}
	if !strings.Contains(err.Error(), "authored") {
		t.Errorf("the error does not list the real predicates: %v", err)
	}
}

// The note on a claim names one end and not the other, and which end it is
// depends on the predicate. authored and submitted_by run from the name, so the
// spelling belongs to the subject; everything else names the object.
func TestNoteLabelsTheEndItDescribes(t *testing.T) {
	w, _ := testWalk(t, WalkOptions{})

	if got := w.label[graph.Name("Ashish Vaswani")]; got != "Ashish Vaswani" {
		t.Errorf("the first author's name node is labelled %q", got)
	}
	if got := w.label[graph.Name("Llion Jones")]; got != "Llion Jones" {
		t.Errorf("the submitter's name node is labelled %q", got)
	}
	if got := w.label[graph.Category("cs.CL")]; got != "Computation and Language" {
		t.Errorf("cs.CL is labelled %q", got)
	}
	// A label is what a name search is run on, and the paper is not something
	// to search a name for.
	if got := w.label[graph.Paper("1706.03762")]; got != "" {
		t.Errorf("the paper is labelled %q, which came off an authored note", got)
	}
}

func TestFrontierGroupsWhatIsLeft(t *testing.T) {
	w, _ := testWalk(t, WalkOptions{})
	front := w.frontier()

	if len(front[graph.KindPaper]) != 7 {
		t.Errorf("papers on the frontier: got %d, want the seven versions", len(front[graph.KindPaper]))
	}
	if len(front[graph.KindName]) != 8 {
		t.Errorf("names on the frontier: got %d, want the eight authors", len(front[graph.KindName]))
	}
	if len(front[graph.KindCategory]) != 2 {
		t.Errorf("categories on the frontier: got %d", len(front[graph.KindCategory]))
	}
	for _, uri := range front[graph.KindPaper] {
		if uri == graph.Paper("1706.03762") {
			t.Error("the seed is back on the frontier, and a walk that revisits its own seed never ends")
		}
	}
	// A node named twice is one entry, because reading it twice would cost a
	// request and change nothing.
	seen := map[string]bool{}
	for _, uris := range front {
		for _, uri := range uris {
			if seen[uri] {
				t.Errorf("%s is on the frontier twice", uri)
			}
			seen[uri] = true
		}
	}
}

// A DOI, a licence, a journal reference and a file are nodes this tool names
// and does not fetch. They are marked read so they are not counted as unreached
// at the end, which would read as a walk that ran out of budget.
func TestLeafNodesAreNotLeftLookingUnread(t *testing.T) {
	w, _ := testWalk(t, WalkOptions{Names: false, Budget: 1})
	w.expand(context.Background(), 2)

	for _, uri := range []string{
		graph.DOI("10.48550/arXiv.1706.03762"),
		graph.License("http://arxiv.org/licenses/nonexclusive-distrib/1.0/"),
		graph.File("1706.03762", 7, KindPDF),
	} {
		if !w.expanded[uri] {
			t.Errorf("%s is still on the frontier, and there is nothing behind it to read", uri)
		}
	}
}

// The name expansion is the one that explodes: eight authors is eight searches,
// and a name search matches strings, so most of what comes back is somebody
// else's work. It is opt in, and the walk says so rather than quietly skipping.
func TestNamesAreOffTheFrontierUnlessAskedFor(t *testing.T) {
	w, buf := testWalk(t, WalkOptions{})
	if w.readNames(context.Background(), []string{graph.Name("Ashish Vaswani")}) {
		t.Error("a name was followed without --names")
	}
	if !strings.Contains(buf.String(), "--names") {
		t.Errorf("the walk said %q, and nothing in it points at the flag", buf.String())
	}
	if w.spent != 0 {
		t.Errorf("spent %d requests on names that were never read", w.spent)
	}
}

// Only the slug is known for a name nothing labelled, and searching for a slug
// finds nothing, so it is dropped rather than spent a request on.
func TestAnUnlabelledNameIsNotSearchedFor(t *testing.T) {
	w, _ := testWalk(t, WalkOptions{Names: true})
	uri := graph.Name("Somebody Nothing Named")
	if w.readNames(context.Background(), []string{uri}) {
		t.Error("a name with no spelling was searched for")
	}
	if !w.expanded[uri] {
		t.Error("the name is still on the frontier, and it will be picked up again next hop")
	}
	if w.spent != 0 {
		t.Errorf("spent %d requests", w.spent)
	}
}

// The budget is in requests, because requests are the unit the rate limits are
// written in, and it is checked before a read rather than before a request, so
// a walk finishes what it started or does not start it.
func TestAffordChecksBeforeAReadAndSaysWhatWentUnread(t *testing.T) {
	w, buf := testWalk(t, WalkOptions{Budget: 10})
	w.spent = 8

	if !w.afford(2, "the taxonomy") {
		t.Error("a read that fits exactly was refused")
	}
	if w.afford(3, "the search for %s", "Illia Polosukhin") {
		t.Error("a read that goes over the budget was allowed")
	}
	said := buf.String()
	for _, want := range []string{"budget of 10", "8 used", "the search for Illia Polosukhin", "went unread"} {
		if !strings.Contains(said, want) {
			t.Errorf("the walk said %q, want %q in it", said, want)
		}
	}
}

// Nothing left to expand is a finished walk rather than a failure, and it says
// so instead of stopping in silence.
func TestExpandStopsWhenTheFrontierIsEmpty(t *testing.T) {
	c, buf := loudClient(t)
	w := &walk{c: c, opts: WalkOptions{Budget: 25}, expanded: map[string]bool{}, label: map[string]string{}}
	if w.expand(context.Background(), 2) {
		t.Error("an empty frontier expanded into something")
	}
	if !strings.Contains(buf.String(), "nothing left to expand") {
		t.Errorf("the walk said %q", buf.String())
	}
}

// A version is a fragment on a paper that is already in hand, so it costs
// nothing to expand and must not become a request.
func TestVersionsAreNotReadAgain(t *testing.T) {
	w, _ := testWalk(t, WalkOptions{})
	uris := []string{graph.Version("1706.03762", 3), graph.Version("1706.03762", 4)}
	if w.readPapers(context.Background(), uris) {
		t.Error("a version fragment was read as a paper")
	}
	if w.spent != 0 {
		t.Errorf("spent %d requests on versions of a paper already read", w.spent)
	}
	for _, uri := range uris {
		if !w.expanded[uri] {
			t.Errorf("%s is still on the frontier", uri)
		}
	}
}

// A refusal is a bug that would otherwise look like missing data, so it goes to
// stderr rather than behind -v.
func TestARefusalIsSaidOutLoud(t *testing.T) {
	c, buf := loudClient(t)
	c.report([]string{"cites cannot be asserted by s1, only by s10"})
	if !strings.Contains(buf.String(), "refused by the predicate table") {
		t.Errorf("the client said %q", buf.String())
	}
}
