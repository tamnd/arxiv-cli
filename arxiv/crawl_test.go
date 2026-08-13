package arxiv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// The crawl tests run against a real store and the same saved surfaces as
// everything else. None of them make a request: a crawl decides what to read
// before it reads it, and every decision worth testing happens on this side of
// the network.

// testCrawler is a crawler over an empty store, with whatever options the test
// is about.
func testCrawler(t *testing.T, o CrawlOptions) (*Crawler, *Store) {
	t.Helper()
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.log = nil
	st := testStore(t)
	cr, err := NewCrawler(c, st, o)
	if err != nil {
		t.Fatalf("NewCrawler: %v", err)
	}
	return cr, st
}

func TestASeedIsAPaperACategoryOrAURI(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1706.03762", graph.Paper("1706.03762")},
		{"arXiv:1706.03762v5", graph.Paper("1706.03762")},
		{"https://arxiv.org/abs/1706.03762", graph.Paper("1706.03762")},
		{"math/0309136", graph.Paper("math/0309136")},
		{"cs.CL", graph.Category("cs.CL")},
		{"hep-th", graph.Category("hep-th")},
		{"q-bio.NC", graph.Category("q-bio.NC")},
		{graph.Author("vaswani_a_1"), graph.Author("vaswani_a_1")},
	}
	for _, tc := range cases {
		got, err := ResolveSeed(tc.in)
		if err != nil {
			t.Errorf("ResolveSeed(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolveSeed(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "   ", "not a seed!", "ax://nonsense/1"} {
		if got, err := ResolveSeed(bad); err == nil {
			t.Errorf("ResolveSeed(%q) = %s, want an error", bad, got)
		}
	}
}

// A crawl needs somewhere to start. Refusing at the door beats opening a store
// and reading nothing.
func TestACrawlWithNoSeedIsRefused(t *testing.T) {
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCrawler(c, testStore(t), CrawlOptions{}); err == nil {
		t.Fatal("a crawl with no seed, no search and no resume was accepted")
	}
}

func TestThePlanCostsTheRunBeforeItStarts(t *testing.T) {
	cr, _ := testCrawler(t, CrawlOptions{
		Seeds:      []string{"1706.03762"},
		Depth:      DepthFull,
		Budget:     10,
		HTMLBudget: 4,
	})
	plan, err := cr.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// Ten API requests at three seconds and four arxiv.org requests at fifteen.
	if want := 10*APIPlane.Pace + 4*HTMLPlane.Pace; plan.Wall != want {
		t.Errorf("wall is %s, want %s", plan.Wall, want)
	}
	if len(plan.Seeds) != 1 || plan.Seeds[0] != graph.Paper("1706.03762") {
		t.Errorf("seeds are %v", plan.Seeds)
	}
	if !strings.Contains(plan.String(), "arxiv.org paces at 15s") {
		t.Errorf("the plan does not say what the html plane costs:\n%s", plan)
	}
}

// --api-only is the flag that has to be true whatever else was asked for, so it
// is checked on both: the budget it leaves and the depth it allows.
func TestAPIOnlyLeavesNothingForTheHTMLPlane(t *testing.T) {
	cr, _ := testCrawler(t, CrawlOptions{
		Seeds:      []string{"1706.03762"},
		Depth:      DepthText,
		HTMLBudget: 50,
		APIOnly:    true,
		Trackbacks: true,
	})
	plan, err := cr.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.HTML != 0 {
		t.Errorf("the html budget is %d, want 0", plan.HTML)
	}
	if got := cr.depth(); got != DepthMeta {
		t.Errorf("depth text under --api-only reads at %s, want %s", got, DepthMeta)
	}
	joined := strings.Join(plan.Notes, "\n")
	for _, want := range []string{"nothing is queued on arxiv.org", "depth meta instead", "trackbacks"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the plan does not mention %q:\n%s", want, joined)
		}
	}
}

// A category is on the HTML plane, so --api-only refuses it rather than quietly
// reading the taxonomy anyway. Nothing here makes a request.
func TestAPIOnlyRefusesTheTaxonomyRatherThanReadingIt(t *testing.T) {
	cr, st := testCrawler(t, CrawlOptions{Seeds: []string{"cs.CL"}, APIOnly: true})
	m, err := cr.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m.Requests != 0 {
		t.Fatalf("an api-only crawl of a category made %d requests", m.Requests)
	}
	if len(m.Refusals) != 1 || m.Refusals[0].Plane != HTMLPlane.Name {
		t.Fatalf("refusals are %+v", m.Refusals)
	}
	// The category is still in the store, unread, which is what makes the
	// refusal recoverable: run it again without --api-only and it is the
	// frontier.
	front, err := st.Frontier(graph.KindCategory, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(front) != 1 || front[0] != graph.Category("cs.CL") {
		t.Errorf("the frontier is %v", front)
	}
}

// A budget too small for the read that was asked for stops before the request,
// not after it.
func TestABudgetTooSmallStopsBeforeItSpends(t *testing.T) {
	cr, _ := testCrawler(t, CrawlOptions{
		Seeds:  []string{"1706.03762"},
		Depth:  DepthFull, // three api requests for one paper
		Budget: 1,
	})
	m, err := cr.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m.Requests != 0 || m.Papers != 0 {
		t.Fatalf("the crawl spent %d requests and read %d papers", m.Requests, m.Papers)
	}
	if len(m.Refusals) != 1 {
		t.Fatalf("refusals are %+v", m.Refusals)
	}
	r := m.Refusals[0]
	if r.Plane != APIPlane.Name || r.Need != 3 || r.Left != 1 {
		t.Errorf("the refusal is %+v, want 3 needed and 1 left on the api plane", r)
	}
	if !strings.Contains(m.String(), "1 refused") {
		t.Errorf("the manifest does not say what it refused: %s", m)
	}
}

// A paper the store has already read is not read again, so a crawl of it costs
// nothing at all.
func TestAPaperAlreadyReadIsNotReadAgain(t *testing.T) {
	st, p, _ := filedPaper(t)
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	c.log = nil
	cr, err := NewCrawler(c, st, CrawlOptions{Seeds: []string{p.ID}, Hops: 3})
	if err != nil {
		t.Fatal(err)
	}
	m, err := cr.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m.Requests != 0 || m.Papers != 0 {
		t.Errorf("re reading a stored paper cost %d requests", m.Requests)
	}
	if len(m.Refusals) != 0 {
		t.Errorf("nothing should have been refused: %+v", m.Refusals)
	}
}

// Author names are the expansion that explodes, so they wait for --names even
// when they are sitting on the frontier.
func TestNamesStayOffTheFrontierUntilAskedFor(t *testing.T) {
	st, p, _ := filedPaper(t)
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	c.log = nil
	cr, err := NewCrawler(c, st, CrawlOptions{Seeds: []string{p.ID}, Hops: 2, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	// The paper is read, so what is left waiting is its authors.
	front := cr.frontier(graph.KindName)
	if len(front) == 0 {
		t.Fatal("the stored paper named no authors")
	}
	if n := cr.readNames(context.Background(), front); n != 0 {
		t.Errorf("%d names were followed without --names", n)
	}
}

// The spelling to search for is on the claims, because a name node's uri is a
// slug and nothing on arXiv answers for a slug.
func TestANameKeepsTheSpellingArxivPrinted(t *testing.T) {
	st, _, _ := filedPaper(t)
	uri := graph.Name("Ashish Vaswani")
	label, err := st.Label(uri)
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if label != "Ashish Vaswani" {
		t.Errorf("the label of %s is %q, want %q", uri, label, "Ashish Vaswani")
	}
	if label, err := st.Label(graph.Name("Nobody At All")); err != nil || label != "" {
		t.Errorf("a name nothing claimed has label %q, %v", label, err)
	}
}

// The manifest is the record of a run that did not finish as much as of one
// that did, so it is written either way and it round trips.
func TestTheManifestIsWrittenAndReadsBack(t *testing.T) {
	cr, _ := testCrawler(t, CrawlOptions{Seeds: []string{"1706.03762"}, Depth: DepthFull, Budget: 1})
	m, err := cr.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "crawls")
	path, err := m.Save(dir)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var back Manifest
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(back.Seeds) != 1 || back.Seeds[0] != "1706.03762" {
		t.Errorf("the manifest seeds are %v", back.Seeds)
	}
	if back.Budget[APIPlane.Name] != 1 || back.Spent[APIPlane.Name] != 0 {
		t.Errorf("budget %v, spent %v", back.Budget, back.Spent)
	}
	if len(back.Refusals) != 1 {
		t.Errorf("the refusal did not survive the round trip: %+v", back.Refusals)
	}
	if back.Elapsed == "" || back.Depth != string(DepthFull) {
		t.Errorf("the manifest is missing its own shape: %+v", back)
	}
}

// Every request that leaves the machine lands in the read log, with the plane
// it went to and the bytes that came back. This is the only test that makes a
// request, and it makes it to a server on this machine.
func TestEveryRequestLandsInTheReadLog(t *testing.T) {
	body := []byte("<feed></feed>")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	st := testStore(t)
	c.Watch(func(r Read) {
		if err := st.PutRead(r); err != nil {
			t.Errorf("PutRead: %v", err)
		}
	})

	if _, err := c.fetchLive(context.Background(), ts.URL+"/api/query"); err != nil {
		t.Fatalf("fetchLive: %v", err)
	}
	if _, err := c.fetchLive(context.Background(), ts.URL+"/missing"); err == nil {
		t.Fatal("the missing page came back without an error")
	}

	reads, err := st.Reads(10)
	if err != nil {
		t.Fatalf("Reads: %v", err)
	}
	if len(reads) != 2 {
		t.Fatalf("the log has %d rows, want 2", len(reads))
	}
	byStatus := map[int]Read{}
	for _, r := range reads {
		byStatus[r.Status] = r
	}
	ok, found := byStatus[200], byStatus[404]
	if ok.Bytes != int64(len(body)) {
		t.Errorf("the 200 logged %d bytes, want %d", ok.Bytes, len(body))
	}
	if ok.Plane != "test" {
		t.Errorf("the 200 logged plane %q, want the client's own plane", ok.Plane)
	}
	if found.Error == "" {
		t.Errorf("the 404 logged no error")
	}
	// A cache hit is not a request and must not appear here, which is what the
	// two rows above already say: fetchLive never touches the cache.
}

// The watcher is taken off again when the crawl ends, so a later read does not
// land in a store the crawl has closed.
func TestTheReadHookComesOffWhenTheCrawlEnds(t *testing.T) {
	cr, _ := testCrawler(t, CrawlOptions{Seeds: []string{"1706.03762"}, Depth: DepthFull, Budget: 1})
	if _, err := cr.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cr.c.watchMu.Lock()
	defer cr.c.watchMu.Unlock()
	if cr.c.watch != nil {
		t.Error("the crawl left its read hook on the client")
	}
}

// A cancelled crawl still returns its manifest, and the manifest says it was
// cancelled. Anything else and an interrupted run looks like a clean one.
func TestACancelledCrawlStillHasAManifest(t *testing.T) {
	cr, _ := testCrawler(t, CrawlOptions{Seeds: []string{"1706.03762"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m, err := cr.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m == nil || !m.Cancelled {
		t.Fatalf("the manifest is %+v", m)
	}
	if m.Elapsed == "" {
		t.Error("the manifest was not stamped on the way out")
	}
}

// depth splits across the planes, and the split is what the budget is counted
// against.
func TestADepthKnowsWhichPlaneItSpendsOn(t *testing.T) {
	cases := []struct {
		depth      Depth
		api, html  int
		crossesWeb bool
	}{
		{DepthQuick, 1, 0, false},
		{DepthMeta, 2, 0, false},
		{DepthFull, 3, 1, true},
		{DepthText, 3, 2, true},
	}
	for _, tc := range cases {
		api, html := tc.depth.PlaneRequests()
		if api != tc.api || html != tc.html {
			t.Errorf("%s costs %d api and %d html, want %d and %d", tc.depth, api, html, tc.api, tc.html)
		}
		if api+html != tc.depth.Requests() {
			t.Errorf("%s: the split is %d and the total is %d", tc.depth, api+html, tc.depth.Requests())
		}
		if got := tc.depth.CrossesHTMLPlane(); got != tc.crossesWeb {
			t.Errorf("%s crosses the html plane: %v", tc.depth, got)
		}
	}
}
