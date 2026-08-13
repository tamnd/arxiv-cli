package arxiv

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// The store is tested against the same saved surfaces every other test uses, so
// what goes in is a real record with a real claim set behind it rather than a
// row somebody wrote to make an assertion pass.

// testStore is an empty store in a directory the test framework throws away.
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "arxiv.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// filedPaper is the full record and its claims, written the way a read writes
// them: the record first, then everything it asserted.
func filedPaper(t *testing.T) (*Store, Paper, []graph.Edge) {
	t.Helper()
	s := testStore(t)
	p := fullRecord(t)
	if _, err := s.PutRecord(p); err != nil {
		t.Fatalf("PutRecord: %v", err)
	}
	edges := EdgesOfPaper(p)
	if _, err := s.PutClaims(edges); err != nil {
		t.Fatalf("PutClaims: %v", err)
	}
	return s, p, edges
}

func TestARecordGoesUnderTheURIItNames(t *testing.T) {
	s, p, _ := filedPaper(t)

	uri, err := s.PutRecord(p)
	if err != nil {
		t.Fatalf("PutRecord: %v", err)
	}
	if want := graph.Paper("1706.03762"); uri != want {
		t.Errorf("the paper went to %s, want %s", uri, want)
	}
	node, err := s.Node(uri)
	if err != nil || node == nil {
		t.Fatalf("Node: %v, %v", node, err)
	}
	if node.Kind != graph.KindPaper {
		t.Errorf("kind is %q", node.Kind)
	}
	if !node.Read() {
		t.Fatal("the node has no record, and something did read it")
	}
	var back Paper
	if err := json.Unmarshal(node.Record, &back); err != nil {
		t.Fatalf("the record does not decode: %v", err)
	}
	if back.Title != p.Title || len(back.Authors) != len(p.Authors) {
		t.Errorf("the record came back as %q with %d authors", back.Title, len(back.Authors))
	}
}

// An announcement and a full text both name a paper. Filing either under the
// paper's URI would put one view of a paper where another view lives, and the
// two would take turns overwriting each other.
func TestOnlyTheRecordsThatNameANodeGetOne(t *testing.T) {
	cases := []struct {
		record any
		want   string
	}{
		{fullRecord(t), graph.Paper("1706.03762")},
		{categoryFixture(t, "cs.CL"), graph.Category("cs.CL")},
		{Set{SetSpec: "cs:cs:CL", Name: "Computation and Language"}, graph.Set("cs:cs:CL")},
		{personFixture(t), graph.Author("baez_j_1")},
		{Announcement{}, ""},
		{FullText{}, ""},
		{Trackback{}, ""},
	}
	for _, c := range cases {
		got, _ := recordURI(c.record)
		if got != c.want {
			t.Errorf("%T went to %q, want %q", c.record, got, c.want)
		}
	}
}

// A name search matched strings. Filing it as a person would mark a name read
// on the strength of a string match, which is the one thing the whole name and
// person split exists to prevent.
func TestANameSearchIsNotAPerson(t *testing.T) {
	p := Person{Name: "Ashish Vaswani", Identified: false}
	if uri, _ := recordURI(p); uri != "" {
		t.Errorf("a name search was filed at %s", uri)
	}
	s := testStore(t)
	if _, err := s.PutRecord(p); err == nil {
		t.Error("a name search was stored, so the store now says somebody read that person")
	}
}

// A claim naming a paper is a paper the store now knows exists. Writing the
// claim without the node would leave the frontier empty on a store full of
// claims.
func TestClaimsBringTheirNodesWithThem(t *testing.T) {
	s, _, edges := filedPaper(t)

	for _, uri := range []string{
		graph.Name("Ashish Vaswani"),
		graph.Category("cs.CL"),
		graph.DOI("10.48550/arXiv.1706.03762"),
	} {
		node, err := s.Node(uri)
		if err != nil {
			t.Fatalf("Node %s: %v", uri, err)
		}
		if node == nil {
			t.Errorf("%s was named by a claim and is not in the store", uri)
			continue
		}
		if node.Read() {
			t.Errorf("%s carries a record, and nothing fetched it", uri)
		}
	}

	claims, err := s.Claims("", graph.Authored, "", 0)
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	if len(claims) != 8 {
		t.Errorf("got %d authored claims off %d, want the eight authors", len(claims), len(edges))
	}
	// The author order survives as the position on the claim rather than as the
	// row order, because rows come back in the order the query asked for and
	// nobody should have to know which query that was.
	byPosition := map[int]graph.Edge{}
	for _, c := range claims {
		byPosition[c.Position] = c
	}
	if first := byPosition[1]; !strings.Contains(first.Note, "Ashish Vaswani") {
		t.Errorf("the first author is %+v, want Ashish Vaswani", first)
	}
	if eighth := byPosition[8]; !strings.Contains(eighth.Note, "Illia Polosukhin") {
		t.Errorf("the eighth author is %+v, want Illia Polosukhin", eighth)
	}
}

// A version is a fragment on a paper rather than a node of its own, so nothing
// will ever fetch one and it must not sit on the frontier looking like work.
func TestAVersionIsNotANode(t *testing.T) {
	s, _, _ := filedPaper(t)
	uri := graph.Version("1706.03762", 3)

	node, err := s.Node(uri)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node != nil {
		t.Errorf("%s is a node of its own", uri)
	}
	if err := s.Sight(uri); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	if node, _ := s.Node(uri); node != nil {
		t.Errorf("%s was sighted into the store", uri)
	}
}

// The same claim seen twice is one row, because the key is what was said and
// who said it, and a store that grew a row per read would count a claim by how
// often it was fetched.
func TestTheSameClaimTwiceIsOneRowInTheStore(t *testing.T) {
	s, p, edges := filedPaper(t)

	again, err := s.PutClaims(EdgesOfPaper(p))
	if err != nil {
		t.Fatalf("PutClaims: %v", err)
	}
	if again != 0 {
		t.Errorf("%d claims were written a second time", again)
	}
	all, err := s.Claims("", "", "", 0)
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	if len(all) != len(edges) {
		t.Errorf("the store holds %d claims, want the %d that were asserted", len(all), len(edges))
	}
}

// note and position are labels rather than assertions, so they are outside the
// key: a later sighting that carries one fills in an earlier one that did not,
// instead of becoming a second row.
func TestALabelFillsInAndDoesNotSplit(t *testing.T) {
	s := testStore(t)
	bare := graph.Edge{
		From:      graph.Name("Ashish Vaswani"),
		Predicate: graph.Authored,
		To:        graph.Paper("1706.03762"),
		Source:    "https://export.arxiv.org/api/query",
		Surface:   SurfaceAPI,
	}
	labelled := bare
	labelled.Note = "Ashish Vaswani"
	labelled.Position = 1

	if _, err := s.PutClaims([]graph.Edge{bare}); err != nil {
		t.Fatalf("PutClaims: %v", err)
	}
	if _, err := s.PutClaims([]graph.Edge{labelled}); err != nil {
		t.Fatalf("PutClaims: %v", err)
	}
	got, err := s.Claims("", "", "", 0)
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the label made a second row: %+v", got)
	}
	if got[0].Note != "Ashish Vaswani" || got[0].Position != 1 {
		t.Errorf("the label did not fill in: %+v", got[0])
	}

	// And the other way round: a later sighting with no label does not blank
	// the one already there.
	if _, err := s.PutClaims([]graph.Edge{bare}); err != nil {
		t.Fatalf("PutClaims: %v", err)
	}
	got, _ = s.Claims("", "", "", 0)
	if got[0].Note == "" || got[0].Position == 0 {
		t.Errorf("an unlabelled sighting erased the label: %+v", got[0])
	}
}

// A claim that points the wrong way is a bug, and a store is where one would
// live longest, so the table is checked on the way in as well as on the way out.
func TestABackwardsClaimIsRefusedAtTheDoor(t *testing.T) {
	s := testStore(t)
	_, err := s.PutClaims([]graph.Edge{{
		From:      graph.Paper("1706.03762"),
		Predicate: graph.Authored,
		To:        graph.Name("Ashish Vaswani"),
		Source:    "https://export.arxiv.org/api/query",
	}})
	if err == nil {
		t.Fatal("a paper was stored as the author of a person")
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("the error does not say it was refused: %v", err)
	}
}

// The frontier is what a crawl reads next, and a leaf it can never fetch is not
// on it: a DOI, a licence and a file are named and never read.
func TestTheFrontierIsWhatIsWorthReading(t *testing.T) {
	s, _, _ := filedPaper(t)

	front, err := s.Frontier("", 0)
	if err != nil {
		t.Fatalf("Frontier: %v", err)
	}
	seen := map[string]bool{}
	for _, uri := range front {
		seen[uri] = true
		kind, _ := graph.KindOf(uri)
		switch kind {
		case graph.KindPaper, graph.KindCategory, graph.KindAuthor, graph.KindName:
		default:
			t.Errorf("%s is on the frontier and nothing can read it", uri)
		}
	}
	if seen[graph.Paper("1706.03762")] {
		t.Error("the paper that was read is on the frontier, and a crawl that revisits its own seed never ends")
	}
	if !seen[graph.Name("Ashish Vaswani")] {
		t.Error("an author name a claim gave is not on the frontier")
	}

	papers, err := s.Frontier(graph.KindPaper, 0)
	if err != nil {
		t.Fatalf("Frontier: %v", err)
	}
	if len(papers) != 0 {
		t.Errorf("papers on the frontier: %v, want none, since the versions are fragments", papers)
	}
}

// A later sighting with no record does not erase a record that is there, which
// is what stops a search result naming a paper the store read last week from
// blanking it.
func TestASightingDoesNotBlankARecord(t *testing.T) {
	s, p, _ := filedPaper(t)
	uri := graph.Paper(p.ID)

	before, _ := s.Node(uri)
	if err := s.Sight(uri); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	after, _ := s.Node(uri)
	if !after.Read() {
		t.Fatal("the record is gone")
	}
	if !after.FirstSeen.Equal(before.FirstSeen) {
		t.Errorf("first_seen moved from %s to %s", before.FirstSeen, after.FirstSeen)
	}
}

// Stats is what somebody runs to find out whether a crawl worked, so it counts
// the read log too: a crawl that spent its budget on 404s and a crawl that
// worked look identical in the other two sections.
func TestStatsCountsAllThreeTables(t *testing.T) {
	s, _, _ := filedPaper(t)
	at := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	reads := []Read{
		NewRead("https://export.arxiv.org/api/query?id_list=1706.03762", 200, 4096, at, nil),
		NewRead("https://arxiv.org/abs/1706.03762", 200, 51200, at, nil),
		NewRead("https://arxiv.org/abs/9999.99999", 404, 0, at, nil),
	}
	for _, r := range reads {
		if err := s.PutRead(r); err != nil {
			t.Fatalf("PutRead: %v", err)
		}
	}

	rows, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	byKey := map[string]StatRow{}
	tables := map[string]bool{}
	for _, r := range rows {
		byKey[r.Table+" "+r.Key] = r
		tables[r.Table] = true
	}
	for _, table := range []string{"nodes", "claims", "reads"} {
		if !tables[table] {
			t.Errorf("stats says nothing about %s", table)
		}
	}
	if got := byKey["claims "+graph.Authored].Rows; got != 8 {
		t.Errorf("authored claims counted as %d", got)
	}
	// Eight and not nine: the submitter of this paper is Llion Jones, who is
	// also its fifth author, and a name node is the name.
	if got := byKey["nodes name (not read)"].Rows; got != 8 {
		t.Errorf("unread names counted as %d, want the eight authors", got)
	}
	if got := byKey["reads html s3 200"]; got.Rows != 1 || got.Bytes != 51200 {
		t.Errorf("the abs page read came back as %+v", got)
	}
	if got := byKey["reads html s3 404"].Rows; got != 1 {
		t.Errorf("the 404 counted as %d, and a crawl that got nothing but these looks fine without it", got)
	}
}

// The plane on a read is worked out from the host and never from the caller,
// because the whole point of the column is to say how much of a run went to the
// slow one.
func TestAReadKnowsItsPlaneAndItsSurface(t *testing.T) {
	cases := []struct {
		url     string
		plane   string
		surface string
	}{
		{"https://export.arxiv.org/api/query?id_list=1706.03762", APIPlane.Name, SurfaceAPI},
		{"https://oaipmh.arxiv.org/oai?verb=GetRecord", APIPlane.Name, SurfaceOAI},
		{"https://arxiv.org/abs/1706.03762", HTMLPlane.Name, SurfaceAbs},
		{"https://arxiv.org/html/2601.00086v3", HTMLPlane.Name, SurfaceFullText},
	}
	for _, c := range cases {
		r := NewRead(c.url, 200, 10, time.Now(), nil)
		if r.Plane != c.plane || r.Surface != c.surface {
			t.Errorf("%s reads as plane %q surface %q, want %q and %q", c.url, r.Plane, r.Surface, c.plane, c.surface)
		}
	}
}

// Query hands the string to SQLite and gives back the query's own column names,
// so `select predicate, count(*) c` prints a predicate column and a c column.
func TestQueryAnswersInTheQuerysOwnColumns(t *testing.T) {
	s, _, _ := filedPaper(t)

	cols, rows, err := s.Query(`select predicate, count(*) c from claims group by 1 order by c desc, 1`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(cols) != 2 || cols[0] != "predicate" || cols[1] != "c" {
		t.Errorf("columns are %v", cols)
	}
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	if got := cell(rows[0][0]); got != graph.Authored {
		t.Errorf("the biggest predicate is %q, want authored", got)
	}

	// The frontier query from the help text has to work as written.
	_, unread, err := s.Query(`select uri from nodes where kind='name' and record is null`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(unread) != 8 {
		t.Errorf("the frontier query returned %d names, want the eight authors", len(unread))
	}
}

// cell is the CLI's own printer, repeated here because a store test should not
// import the command layer.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return ""
	}
}

// Read only means SQLite refuses the write, not that this tool checks the
// string. A check here would be one regular expression away from being wrong.
func TestAReadOnlyStoreRefusesAWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arxiv.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := s.PutRecord(fullRecord(t)); err != nil {
		t.Fatalf("PutRecord: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ro, err := OpenStoreReadOnly(path)
	if err != nil {
		t.Fatalf("OpenStoreReadOnly: %v", err)
	}
	defer func() { _ = ro.Close() }()

	if _, _, err := ro.Query(`select count(*) from nodes`); err != nil {
		t.Errorf("a read was refused on a read only store: %v", err)
	}
	if _, _, err := ro.Query(`delete from nodes`); err == nil {
		t.Error("delete went through on a store opened mode=ro")
	}
}

// A store that is not there is a not found rather than an empty one, because
// creating a store on the way to reading it would answer "nothing here" for a
// mistyped path.
func TestAMissingStoreIsNotAnEmptyOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nothing.db")
	if _, err := OpenStoreReadOnly(path); err == nil {
		t.Fatal("a store that does not exist opened")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("the error does not name the path: %v", err)
	}
}

// Papers is what an export walks, and it comes back as the record that went in.
func TestPapersComeBackOutOfTheStore(t *testing.T) {
	s, p, _ := filedPaper(t)

	papers, err := s.Papers(0)
	if err != nil {
		t.Fatalf("Papers: %v", err)
	}
	if len(papers) != 1 {
		t.Fatalf("got %d papers", len(papers))
	}
	if papers[0].ID != p.ID || papers[0].Title != p.Title {
		t.Errorf("got %s %q", papers[0].ID, papers[0].Title)
	}
	if len(papers[0].Surfaces) != len(p.Surfaces) {
		t.Errorf("the provenance did not survive the round trip: %v", papers[0].Surfaces)
	}
}

// Reset empties the store rather than deleting the file, because a path is
// something somebody else may have pointed a script at.
func TestResetKeepsTheFile(t *testing.T) {
	s, _, _ := filedPaper(t)
	if err := s.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	nodes, err := s.Nodes("", 0)
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("%d nodes survived the reset", len(nodes))
	}
	if _, err := s.PutRecord(fullRecord(t)); err != nil {
		t.Errorf("the store cannot be written after a reset: %v", err)
	}
}

// Every category and person a read produced files under its own URI, which is
// the case that would break if the record to node mapping were done by kind
// name rather than by type.
func TestTheOtherRecordKindsFileThemselves(t *testing.T) {
	s := testStore(t)
	for _, rec := range []any{categoryFixture(t, "cs.CL"), personFixture(t)} {
		uri, err := s.PutRecord(rec)
		if err != nil {
			t.Fatalf("PutRecord %T: %v", rec, err)
		}
		node, err := s.Node(uri)
		if err != nil || node == nil || !node.Read() {
			t.Fatalf("%T is not readable back at %s", rec, uri)
		}
	}
	cats, err := s.Nodes(graph.KindCategory, 0)
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(cats) != 1 || cats[0].URI != graph.Category("cs.CL") {
		t.Errorf("the categories in the store are %v", cats)
	}
}
