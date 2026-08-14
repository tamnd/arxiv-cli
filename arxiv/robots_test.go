package arxiv

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// robots_test.go reads arxiv.org's robots.txt as committed and checks this
// tool's routes against it.
//
// The file is here rather than fetched because a test that fetches robots.txt
// to decide whether it is allowed to fetch has already made a request, and
// because the interesting failure is arXiv changing the file. When that
// happens, replacing testdata/robots.txt is a diff somebody reads, and this test
// says which routes the change affects.
//
// The answer is not "everything is allowed". Four of the routes below are on
// paths robots.txt disallows, and the tool reads them anyway, but only when
// somebody asks for one by name. The distinction the test enforces is between a
// route the tool follows on its own, which has to be allowed, and a route it
// only ever fetches because a person typed it, which is a browser request made
// by a command line rather than a crawl.

// robotsGroup is the Allow and Disallow lines for one user agent.
type robotsGroup struct {
	Allow    []string
	Disallow []string
	Delay    time.Duration
}

// robotsForEveryone parses the User-agent: * group out of the committed file.
//
// It is the smallest parser that answers the question asked here: the group
// header, the two rules, and Crawl-delay. Anything else in the file belongs to
// Googlebot and the rest, and none of it applies to this tool.
func robotsForEveryone(t *testing.T) robotsGroup {
	t.Helper()
	var g robotsGroup
	inGroup := false
	for _, line := range strings.Split(string(fixture(t, "robots.txt")), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		field = strings.ToLower(strings.TrimSpace(field))
		value = strings.TrimSpace(value)
		if field == "user-agent" {
			inGroup = value == "*"
			continue
		}
		if !inGroup {
			continue
		}
		switch field {
		case "allow":
			g.Allow = append(g.Allow, value)
		case "disallow":
			g.Disallow = append(g.Disallow, value)
		case "crawl-delay":
			n, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("crawl-delay %q is not a number: %v", value, err)
			}
			g.Delay = time.Duration(n) * time.Second
		}
	}
	if len(g.Allow) == 0 || len(g.Disallow) == 0 {
		t.Fatal("the committed robots.txt has no rules for everyone, so it is not the file arXiv serves")
	}
	return g
}

// allows applies the longest-match rule: the most specific line wins, and Allow
// wins a tie. That is what every crawler does and what arXiv is relying on when
// it writes Allow: /abs above Disallow: /api.
func (g robotsGroup) allows(path string) bool {
	longest, allowed := -1, true
	for _, p := range g.Allow {
		if strings.HasPrefix(path, p) && len(p) > longest {
			longest, allowed = len(p), true
		}
	}
	for _, p := range g.Disallow {
		if strings.HasPrefix(path, p) && len(p) > longest {
			longest, allowed = len(p), false
		}
	}
	return allowed
}

// The routes this tool reads on arxiv.org, each with what it is for. The API
// hosts are not here: robots.txt on arxiv.org says nothing about
// export.arxiv.org, which serves its own.
var htmlRoutes = []struct {
	surface string
	base    string
	// asked is set on a route the tool never follows by itself. Every one of
	// these is disallowed, and the note says what has to happen before a
	// request is made.
	asked string
}{
	{surface: SurfaceAbs, base: absBase},
	{surface: SurfaceList, base: listBase},
	{surface: SurfaceTaxonomy, base: taxonomyURL},
	{surface: SurfaceAuthorID, base: authorBase},
	{surface: SurfaceBibTeX, base: bibtexBase},
	{surface: SurfaceFullText, base: htmlBase},
	{surface: SurfaceFiles, base: pdfBase},

	{surface: SurfaceSearch, base: s5Base,
		asked: "a search for one of the seven fields the export API has no prefix for"},
	{surface: SurfaceTrackback, base: trackbackBase,
		asked: "arxiv trackbacks, or a crawl run with --trackbacks"},
	{surface: SurfaceTrackback, base: trackbackRecent,
		asked: "arxiv trackbacks --recent"},
	{surface: SurfaceFiles, base: srcBase,
		asked: "arxiv download --kind source, one paper at a time"},
}

// pathOf is the path robots.txt matches on.
func pathOf(t *testing.T, base string) string {
	t.Helper()
	const prefix = "https://" + Host
	if !strings.HasPrefix(base, prefix) {
		t.Fatalf("%s is not on %s, so robots.txt for %s does not govern it", base, Host, Host)
	}
	return strings.TrimPrefix(base, prefix)
}

// Every route the tool follows on its own is allowed, and every route that is
// disallowed is one a person has to ask for. Both halves matter: the first is
// the promise, and the second is the list of exceptions, which stays short only
// if something counts it.
func TestEveryRouteIsAllowedOrIsAskedFor(t *testing.T) {
	g := robotsForEveryone(t)
	for _, r := range htmlRoutes {
		path := pathOf(t, r.base)
		allowed := g.allows(path)
		switch {
		case r.asked == "" && !allowed:
			t.Errorf("%s reads %s on its own and robots.txt disallows it", r.surface, path)
		case r.asked != "" && allowed:
			t.Errorf("%s reads %s only when asked, and robots.txt allows it, so the gate is no longer needed", r.surface, path)
		}
	}
}

// The gated routes are these four and no others. A fifth appearing means a read
// went onto a disallowed path, and it should be a decision rather than a diff
// nobody read.
func TestTheGatedRoutesAreTheOnesTheSpecNames(t *testing.T) {
	want := map[string]bool{"/search/": true, "/tb/": true, "/tb/recent": true, "/src/": true}
	got := map[string]bool{}
	for _, r := range htmlRoutes {
		if r.asked != "" {
			got[pathOf(t, r.base)] = true
		}
	}
	for p := range want {
		if !got[p] {
			t.Errorf("%s is no longer gated", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("%s is gated and is not one of the four the spec names", p)
		}
	}
}

// The pace comes from the file. This is the same assertion as the one in
// planes_test.go, made against the bytes rather than against the number: 15 is
// in the code because 15 is in robots.txt, and if arXiv changes it the fixture
// changes and this fails.
func TestTheHTMLFloorIsTheCrawlDelayInTheFile(t *testing.T) {
	g := robotsForEveryone(t)
	if g.Delay == 0 {
		t.Fatal("the committed robots.txt has no Crawl-delay for everyone")
	}
	if HTMLPlane.Floor != g.Delay {
		t.Errorf("the html floor is %s and robots.txt says %s", HTMLPlane.Floor, g.Delay)
	}
	if HTMLPlane.Pace < g.Delay {
		t.Errorf("the html pace %s is faster than the crawl delay %s", HTMLPlane.Pace, g.Delay)
	}
}

// The longest match rule, on the lines that need it. /abs is allowed under a
// file that also says Disallow: /archive, and /api is disallowed on arxiv.org
// while the export host serves the API this tool actually uses.
func TestTheLongestRuleWins(t *testing.T) {
	g := robotsForEveryone(t)
	for _, c := range []struct {
		path string
		want bool
	}{
		{"/abs/1706.03762", true},
		{"/list/cs.CL/2026-01", true},
		{"/pdf/1706.03762v7", true},
		{"/html/1706.03762v7", true},
		{"/category_taxonomy", true},
		{"/a/baez_j_1", true},
		{"/bibtex/1706.03762", true},
		{"/search/", false},
		{"/tb/1706.03762", false},
		{"/src/1706.03762", false},
		{"/api/query", false},
		{"/format/1706.03762", false},
	} {
		if got := g.allows(c.path); got != c.want {
			t.Errorf("robots.txt allows %s = %v, want %v", c.path, got, c.want)
		}
	}
}
