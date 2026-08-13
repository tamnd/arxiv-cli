package arxiv

import (
	"strings"
	"testing"
	"time"
)

func TestParseDepth(t *testing.T) {
	cases := []struct {
		in   string
		want Depth
		ok   bool
	}{
		{"", DepthMeta, true},
		{"quick", DepthQuick, true},
		{" FULL ", DepthFull, true},
		{"text", DepthText, true},
		{"deep", "", false},
	}
	for _, tc := range cases {
		got, err := ParseDepth(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("ParseDepth(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDepth(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParseDepthErrorStartsWithAWord is the rule fang imposes. It uppercases
// the first character of an error, so a message that opens with quoted user
// input comes out as Deep rather than "deep".
func TestParseDepthErrorStartsWithAWord(t *testing.T) {
	_, err := ParseDepth("deep")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(err.Error(), "depth ") {
		t.Errorf("message starts with the user's own input: %q", err)
	}
}

func TestDepthOrder(t *testing.T) {
	if !DepthFull.AtLeast(DepthMeta) || !DepthMeta.AtLeast(DepthQuick) {
		t.Error("the depths are not ordered cheapest first")
	}
	if DepthQuick.AtLeast(DepthMeta) {
		t.Error("quick claims to read what meta reads")
	}
	if !DepthText.AtLeast(DepthText) {
		t.Error("a depth does not read itself")
	}
	if DepthMeta.CrossesHTMLPlane() {
		t.Error("meta claims to touch the fifteen second plane")
	}
	if !DepthFull.CrossesHTMLPlane() {
		t.Error("full reads the abstract page and that is on arxiv.org")
	}
}

func TestDepthCost(t *testing.T) {
	if got := DepthQuick.Cost(0); got != 0 {
		t.Errorf("no papers should cost nothing, got %s", got)
	}
	// One paper at quick is one request on the three second plane.
	if got := DepthQuick.Cost(1); got != APIPlane.Pace {
		t.Errorf("quick: got %s, want %s", got, APIPlane.Pace)
	}
	// Full is three API requests and one on the fifteen second plane, so it is
	// dominated by the last one.
	want := 3*APIPlane.Pace + HTMLPlane.Pace
	if got := DepthFull.Cost(1); got != want {
		t.Errorf("full: got %s, want %s", got, want)
	}
	if DepthFull.Cost(10) != 10*want {
		t.Errorf("cost does not scale with the number of papers")
	}
	if DepthQuick.Cost(100) >= DepthFull.Cost(100) {
		t.Error("a hundred quick reads should be cheaper than a hundred full ones")
	}
}

func TestDepthRequests(t *testing.T) {
	for i, d := range Depths {
		if i == 0 {
			continue
		}
		if d.Requests() <= Depths[i-1].Requests() {
			t.Errorf("%s costs %d requests, %s costs %d",
				d, d.Requests(), Depths[i-1], Depths[i-1].Requests())
		}
	}
}

// TestDepthMissed checks the sentences name a command that would fill the gap,
// because that is the only reason to print them.
func TestDepthMissed(t *testing.T) {
	quick := DepthQuick.Missed("1706.03762")
	if len(quick) != 3 {
		t.Fatalf("quick missed %d things, want three deeper depths", len(quick))
	}
	for _, s := range quick {
		if !strings.Contains(s, "arxiv paper 1706.03762 --depth ") {
			t.Errorf("a missed sentence names no way to fix it: %q", s)
		}
	}
	if len(DepthMeta.Missed("1706.03762")) != 2 {
		t.Errorf("meta: got %v", DepthMeta.Missed("1706.03762"))
	}
	if len(DepthFull.Missed("1706.03762")) != 1 {
		t.Errorf("full: got %v", DepthFull.Missed("1706.03762"))
	}
	if got := DepthText.Missed("1706.03762"); len(got) != 0 {
		t.Errorf("text is the deepest read and missed %v", got)
	}
}

// TestAnnotateDepthText says out loud that the full text surface is not built
// yet, which is better than a record that looks complete and is not.
func TestAnnotateDepthText(t *testing.T) {
	p := Paper{ID: "1706.03762"}
	annotateDepth(&p, DepthText)
	if p.Depth != "text" {
		t.Errorf("Depth: got %q", p.Depth)
	}
	if len(p.Missed) != 1 || !strings.Contains(p.Missed[0], "arxiv fulltext 1706.03762") {
		t.Errorf("Missed: got %v", p.Missed)
	}
}

func TestEstimateRead(t *testing.T) {
	e := EstimateRead(20, DepthFull)
	if e.Requests != 80 {
		t.Errorf("Requests: got %d, want 80", e.Requests)
	}
	if !e.CrossesHTML {
		t.Error("CrossesHTML: a full read of twenty papers is five minutes of pacing")
	}
	if e.Wall < 5*time.Minute {
		t.Errorf("Wall: got %s, which is too cheap for twenty pages on the slow plane", e.Wall)
	}
	if !strings.Contains(e.String(), "20 papers at depth full") {
		t.Errorf("String: got %q", e.String())
	}
}

func TestEnvelopeSurfaces(t *testing.T) {
	var e Envelope
	e.addSurface(SurfaceAPI, "https://export.arxiv.org/api/query")
	e.addSurface(SurfaceOAI, "https://oaipmh.arxiv.org/oai")
	// A surface read twice is one surface, and both URLs are kept, because two
	// OAI formats are two different requests worth reproducing.
	e.addSurface(SurfaceOAI, "https://oaipmh.arxiv.org/oai?metadataPrefix=arXivRaw")

	if len(e.Surfaces) != 2 {
		t.Errorf("Surfaces: got %v", e.Surfaces)
	}
	if len(e.Sources) != 3 {
		t.Errorf("Sources: got %v", e.Sources)
	}
	// via names the surface whose value is in the field, so the surface that
	// wrote last is the one named. The merges only write what they fill, which
	// is what keeps that true.
	e.setVia("title", SurfaceAPI)
	if e.Via["title"] != SurfaceAPI {
		t.Errorf("via.title: got %q, want %s", e.Via["title"], SurfaceAPI)
	}
	e.setVia("authors", SurfaceOAI)
	if e.Via["authors"] != SurfaceOAI {
		t.Errorf("via.authors: got %q, want %s", e.Via["authors"], SurfaceOAI)
	}
}

func TestSurfaceNames(t *testing.T) {
	for _, id := range []string{SurfaceAPI, SurfaceOAI, SurfaceAbs} {
		if SurfaceNames[id] == "" {
			t.Errorf("surface %s has no name", id)
		}
	}
}

func TestSplitClasses(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"68T50", []string{"68T50"}},
		// Measured on 1801.00001, which is comma separated.
		{"37-40, 51N20, 51M04, 51-04", []string{"37-40", "51N20", "51M04", "51-04"}},
		{"I.2.7; I.2.6", []string{"I.2.7", "I.2.6"}},
		// Measured on 2606.27343, where the bracket holds the secondary
		// classes for the primary one in front of it and the commas inside it
		// are not separators.
		{"18D10 (16T05, 16T15, 18D10)", []string{"18D10 (16T05, 16T15, 18D10)"}},
		{"Primary 60G51, 60J65 (Secondary 35K05, 60H15)", []string{"Primary 60G51", "60J65 (Secondary 35K05, 60H15)"}},
		{"  ", nil},
	}
	for _, tc := range cases {
		got := splitClasses(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitClasses(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitClasses(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestCrossLists(t *testing.T) {
	got := crossLists([]string{"cs.CL", "cs.LG", "stat.ML"}, "cs.CL")
	if len(got) != 2 || got[0] != "cs.LG" || got[1] != "stat.ML" {
		t.Errorf("got %v", got)
	}
	if len(crossLists([]string{"hep-ex"}, "hep-ex")) != 0 {
		t.Error("a single category paper is cross listed nowhere")
	}
}

func TestSortVersions(t *testing.T) {
	vs := []Version{{Version: 3}, {Version: 1}, {Version: 10}, {Version: 2}}
	sortVersions(vs)
	for i, want := range []int{1, 2, 3, 10} {
		if vs[i].Version != want {
			t.Fatalf("sortVersions gave %v", vs)
		}
	}
}
