package arxiv

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update rewrites the golden files. Run it after a deliberate change to the
// record and read the diff before committing it:
//
//	go test ./arxiv -run Golden -update
var update = flag.Bool("update", false, "rewrite the golden records")

// fullRecord runs the whole merge chain over the saved surfaces, which is what
// a --depth full read does once the four responses are in hand.
func fullRecord(t *testing.T) Paper {
	t.Helper()
	p := paperFixture(t, "api_1706.03762.xml")
	mergeOAIArxiv(&p, oaiFixture(t, "oai_arxiv_1706.03762.xml"),
		"https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3A1706.03762&metadataPrefix=arXiv")
	mergeOAIRaw(&p, oaiFixture(t, "oai_raw_1706.03762.xml"),
		"https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3A1706.03762&metadataPrefix=arXivRaw")
	mergeAbs(&p, absFixture(t, "abs_1706.03762.html"), "https://arxiv.org/abs/1706.03762")
	annotateDepth(&p, DepthFull)
	return p
}

// TestGoldenFullPaper pins the record a full read produces, field by field.
//
// The per-surface tests each check one merge. This checks what a caller
// actually receives, so a change to any of the four mappings shows up as a diff
// on one file rather than as a passing test suite and a different JSON.
func TestGoldenFullPaper(t *testing.T) {
	got, err := json.MarshalIndent(fullRecord(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "golden_1706.03762_full.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to write it)", err)
	}
	if string(got) != string(want) {
		t.Errorf("the record changed. Run go test ./arxiv -run Golden -update and read the diff.\n\ngot:\n%s", got)
	}
}

// TestFullRecordHasNoEmptyPromises checks the record does not claim a field it
// did not fill. A zero that looks like an answer is worse than an absent field.
func TestFullRecordHasNoEmptyPromises(t *testing.T) {
	p := fullRecord(t)

	for field, value := range map[string]string{
		"id":               p.ID,
		"versioned_id":     p.VersionedID,
		"oai_id":           p.OAIID,
		"doi":              p.DOI,
		"url":              p.URL,
		"pdf_url":          p.PDFURL,
		"title":            p.Title,
		"abstract":         p.Abstract,
		"comment":          p.Comment,
		"primary_category": p.PrimaryCategory,
		"license":          p.License,
		"submitter":        p.Submitter,
		"html_url":         p.HTMLURL,
	} {
		if value == "" {
			t.Errorf("%s is empty on a full read", field)
		}
	}
	if p.FirstSubmitted.IsZero() || p.LastUpdated.IsZero() || p.OAIDatestamp.IsZero() {
		t.Error("a full read left one of the three dates zero")
	}
	// announced has no source on s1, s2 or s3. The RSS surface is the only one
	// that carries an announcement date, so the field stays absent until then
	// rather than being filled from something close enough.
	if !p.Announced.IsZero() {
		t.Errorf("announced was filled from a surface that does not publish it: %s", p.Announced)
	}
	if len(p.Versions) != 7 || len(p.Authors) != 8 {
		t.Errorf("versions %d, authors %d", len(p.Versions), len(p.Authors))
	}
	// Three surfaces read, four requests, and every URL kept so any of them can
	// be replayed by hand.
	if len(p.Surfaces) != 3 {
		t.Errorf("Surfaces: got %v", p.Surfaces)
	}
	if len(p.Sources) != 4 {
		t.Errorf("Sources: got %v", p.Sources)
	}
}

// TestDepthsAreCumulative reads the same paper at each depth and checks that a
// deeper read is a superset of a shallower one. Anything else would make the
// depth flag a lottery.
func TestDepthsAreCumulative(t *testing.T) {
	quick := paperFixture(t, "api_1706.03762.xml")
	annotateDepth(&quick, DepthQuick)

	meta := paperFixture(t, "api_1706.03762.xml")
	mergeOAIArxiv(&meta, oaiFixture(t, "oai_arxiv_1706.03762.xml"), "https://oaipmh.arxiv.org/oai")
	annotateDepth(&meta, DepthMeta)

	full := fullRecord(t)

	if quick.License != "" || len(quick.Versions) != 0 {
		t.Errorf("a quick read reached past s1: license %q, %d versions", quick.License, len(quick.Versions))
	}
	if meta.License == "" {
		t.Error("meta did not pick up the licence, which is on s2")
	}
	if len(meta.Versions) != 0 {
		t.Errorf("meta read the version history, which costs the arXivRaw request full pays for")
	}
	if meta.HasHTML {
		t.Error("meta claims to know about the HTML rendering, which only the abstract page says")
	}
	if !full.HasHTML || len(full.Versions) == 0 {
		t.Error("full did not read what it charges for")
	}

	// Every depth says what it skipped, and the deepest of the three says the
	// least.
	if len(quick.Missed) <= len(meta.Missed) || len(meta.Missed) <= len(full.Missed) {
		t.Errorf("missed counts do not shrink with depth: %d, %d, %d",
			len(quick.Missed), len(meta.Missed), len(full.Missed))
	}
	// The title is the same paper's title at every depth, and it came from s1
	// every time, so nothing deeper should have rewritten it.
	if quick.Title != meta.Title || meta.Title != full.Title {
		t.Errorf("the title changed with depth: %q, %q, %q", quick.Title, meta.Title, full.Title)
	}
	for _, p := range []Paper{quick, meta, full} {
		if p.Via["first_submitted"] != SurfaceAPI {
			t.Errorf("depth %s attributed first_submitted to %q", p.Depth, p.Via["first_submitted"])
		}
	}
}

// TestEstimateNoticeThreshold checks the notice fires where it should. It is a
// unit test on the rule rather than on the plumbing, because the plumbing is
// eleven live requests.
func TestEstimateNoticeThreshold(t *testing.T) {
	cases := []struct {
		papers int
		depth  Depth
		want   bool
	}{
		{11, DepthFull, true},
		{10, DepthFull, false},
		{100, DepthMeta, false},
		{11, DepthText, true},
	}
	for _, tc := range cases {
		got := tc.papers > estimateFrom && tc.depth.CrossesHTMLPlane()
		if got != tc.want {
			t.Errorf("%d papers at %s: notice %v, want %v", tc.papers, tc.depth, got, tc.want)
		}
	}
	if got := EstimateRead(11, DepthFull).String(); got == "" {
		t.Error("the notice has nothing to say")
	}
}
