package arxiv

import (
	"testing"
	"time"
)

// absFixture parses the saved abstract page.
func absFixture(t *testing.T, name string) *absPage {
	t.Helper()
	page, err := parseAbs(fixture(t, name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return page
}

func TestParseAbsFixture(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")

	if page.ID != "1706.03762" {
		t.Errorf("ID: got %q", page.ID)
	}
	if page.Version != 7 {
		t.Errorf("Version: got %d, want 7", page.Version)
	}
	if page.Title != "Attention Is All You Need" {
		t.Errorf("Title: got %q", page.Title)
	}
	if page.Comment != "15 pages, 5 figures" {
		t.Errorf("Comment: got %q", page.Comment)
	}
	if page.Submitter != "Llion Jones" {
		t.Errorf("Submitter: got %q", page.Submitter)
	}
	if page.License != "http://arxiv.org/licenses/nonexclusive-distrib/1.0/" {
		t.Errorf("License: got %q", page.License)
	}
}

// TestParseAbsAuthors covers the third of the three name formats arXiv
// publishes. The citation tags are surname first with a comma, so the page can
// be read for structured names as well as display ones.
func TestParseAbsAuthors(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")
	if len(page.Authors) != 8 {
		t.Fatalf("Authors: got %d, want 8", len(page.Authors))
	}
	a := page.Authors[0]
	if a.Name != "Ashish Vaswani" || a.Keyname != "Vaswani" || a.Forenames != "Ashish" {
		t.Errorf("Authors[0]: got %#v", a)
	}
	if a.Via != SurfaceAbs {
		t.Errorf("Authors[0].Via: got %q", a.Via)
	}
}

func TestCitationAuthor(t *testing.T) {
	cases := []struct {
		in                       string
		name, keyname, forenames string
		ok                       bool
	}{
		{"Vaswani, Ashish", "Ashish Vaswani", "Vaswani", "Ashish", true},
		{"ATLAS Collaboration", "ATLAS Collaboration", "", "", true},
		{"Vaswani,", "Vaswani,", "", "", true},
		{"", "", "", "", false},
	}
	for _, tc := range cases {
		got, ok := citationAuthor(tc.in)
		if ok != tc.ok {
			t.Errorf("citationAuthor(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Name != tc.name || got.Keyname != tc.keyname || got.Forenames != tc.forenames {
			t.Errorf("citationAuthor(%q) = %#v", tc.in, got)
		}
	}
}

// TestParseAbsSubjects covers the one thing this page has that no machine
// readable surface does: the human readable name beside each code.
func TestParseAbsSubjects(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")

	if page.PrimaryCategory != "cs.CL" {
		t.Errorf("PrimaryCategory: got %q", page.PrimaryCategory)
	}
	if len(page.Categories) != 2 || page.Categories[0] != "cs.CL" {
		t.Errorf("Categories: got %v", page.Categories)
	}
	if got := page.SubjectNames["cs.CL"]; got != "Computation and Language" {
		t.Errorf("SubjectNames[cs.CL]: got %q", got)
	}
	if got := page.SubjectNames["cs.LG"]; got != "Machine Learning" {
		t.Errorf("SubjectNames[cs.LG]: got %q", got)
	}
}

// TestParseAbsFullText reads the capability list. Whether a LaTeXML rendering
// exists and whether TeX source was submitted are on this page alone.
func TestParseAbsFullText(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")

	if !page.HasHTML {
		t.Error("HasHTML: got false, the page links an HTML rendering")
	}
	if page.HTMLURL != "https://arxiv.org/html/1706.03762v7" {
		t.Errorf("HTMLURL: got %q", page.HTMLURL)
	}
	if !page.HasSource {
		t.Error("HasSource: got false, the page links TeX source")
	}
}

// TestParseAbsHistory reads the version history from the page, which is the
// fallback for the version history when OAI cannot be reached.
func TestParseAbsHistory(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")

	if len(page.Versions) != 7 {
		t.Fatalf("Versions: got %d, want 7", len(page.Versions))
	}
	for i, v := range page.Versions {
		if v.Version != i+1 {
			t.Fatalf("Versions[%d].Version = %d, want the history in order", i, v.Version)
		}
		if v.Date.IsZero() {
			t.Errorf("Versions[%d] has no date", i)
		}
		if v.Date.Location() != time.UTC {
			t.Errorf("Versions[%d].Date is in %s, want UTC", i, v.Date.Location())
		}
		if v.Via != SurfaceAbs {
			t.Errorf("Versions[%d].Via = %q", i, v.Via)
		}
		if v.SizeBytes == 0 {
			t.Errorf("Versions[%d] has no size", i)
		}
	}
	if got := page.Versions[0].Date.Format(time.RFC3339); got != "2017-06-12T17:57:34Z" {
		t.Errorf("v1 date: got %q", got)
	}
	if page.Versions[0].SizeBytes != 1102*1024 {
		t.Errorf("v1 size: got %d", page.Versions[0].SizeBytes)
	}
}

// TestAbsAndRawSizesDisagreeByRounding records a measured fact rather than
// asserting a bug. arXivRaw writes 1124kb for v2 and this page writes 1,125 KB.
// Both mean kilobytes, the two differ by one, and via names which one answered.
func TestAbsAndRawSizesDisagreeByRounding(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")
	raw := rawVersions(oaiFixture(t, "oai_raw_1706.03762.xml").Metadata.ArxivRaw.Versions)

	if len(page.Versions) != len(raw) {
		t.Fatalf("the two surfaces list %d and %d versions", len(page.Versions), len(raw))
	}
	for i := range raw {
		if !page.Versions[i].Date.Equal(raw[i].Date) {
			t.Errorf("v%d dates differ: page %s, raw %s",
				i+1, page.Versions[i].Date, raw[i].Date)
		}
		diff := page.Versions[i].SizeBytes - raw[i].SizeBytes
		if diff < -1024 || diff > 1024 {
			t.Errorf("v%d sizes differ by more than rounding: page %d, raw %d",
				i+1, page.Versions[i].SizeBytes, raw[i].SizeBytes)
		}
	}
	if page.Versions[1].SizeBytes != 1125*1024 || raw[1].SizeBytes != 1124*1024 {
		t.Logf("the rounding this test is about has changed: page %d, raw %d",
			page.Versions[1].SizeBytes, raw[1].SizeBytes)
	}
}

func TestMergeAbs(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	mergeOAIRaw(&p, oaiFixture(t, "oai_raw_1706.03762.xml"), "https://oaipmh.arxiv.org/oai")
	mergeAbs(&p, absFixture(t, "abs_1706.03762.html"), "https://arxiv.org/abs/1706.03762")

	if !p.HasHTML || !p.HasSource {
		t.Errorf("capabilities: got html %v, source %v", p.HasHTML, p.HasSource)
	}
	if p.Via["has_html"] != SurfaceAbs {
		t.Errorf("via.has_html: got %q", p.Via["has_html"])
	}
	if p.SubjectNames["cs.CL"] != "Computation and Language" {
		t.Errorf("SubjectNames: got %v", p.SubjectNames)
	}
	// The fast plane answered for the history first, so the page does not
	// overwrite it and the source types survive.
	if p.Via["versions"] != SurfaceOAI {
		t.Errorf("via.versions: got %q, want %s", p.Via["versions"], SurfaceOAI)
	}
	if p.Versions[0].SourceType != "D" {
		t.Errorf("the page overwrote the history that names the source type: %#v", p.Versions[0])
	}
	if len(p.Surfaces) != 3 || p.Surfaces[2] != SurfaceAbs {
		t.Errorf("Surfaces: got %v", p.Surfaces)
	}
}

// TestMergeAbsFillsHistoryWhenOAIIsSilent is the outage case. A read that could
// not reach OAI still comes back with a version history, one without source
// types, and via says where it came from.
func TestMergeAbsFillsHistoryWhenOAIIsSilent(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	mergeAbs(&p, absFixture(t, "abs_1706.03762.html"), "https://arxiv.org/abs/1706.03762")

	if len(p.Versions) != 7 {
		t.Fatalf("Versions: got %d, want 7", len(p.Versions))
	}
	if p.Via["versions"] != SurfaceAbs {
		t.Errorf("via.versions: got %q, want %s", p.Via["versions"], SurfaceAbs)
	}
	if p.Versions[0].SourceType != "" {
		t.Errorf("the page does not publish a source type: %#v", p.Versions[0])
	}
	if p.Submitter != "Llion Jones" {
		t.Errorf("Submitter: got %q", p.Submitter)
	}
	if p.Via["submitter"] != SurfaceAbs {
		t.Errorf("via.submitter: got %q", p.Via["submitter"])
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		number, unit string
		want         int64
		ok           bool
	}{
		{"1,102", "KB", 1102 * 1024, true},
		{"1102", "kb", 1102 * 1024, true},
		{"3", "MB", 3 * 1024 * 1024, true},
		{"3", "GB", 0, false},
		{"lots", "KB", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseSize(tc.number, tc.unit)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseSize(%q, %q) = (%d, %v), want (%d, %v)",
				tc.number, tc.unit, got, ok, tc.want, tc.ok)
		}
	}
}

func TestVersionFromText(t *testing.T) {
	if v, ok := versionFromText("arXiv:1706.03762v7 [cs.CL]"); !ok || v != 7 {
		t.Errorf("got (%d, %v)", v, ok)
	}
	if v, ok := versionFromText("arXiv:hep-th/9711200v3"); !ok || v != 3 {
		t.Errorf("old style: got (%d, %v)", v, ok)
	}
	if _, ok := versionFromText("arXiv:1706.03762"); ok {
		t.Error("an unversioned cite line reported a version")
	}
}

func TestAbsolute(t *testing.T) {
	if got := absolute("/html/1706.03762v7"); got != "https://arxiv.org/html/1706.03762v7" {
		t.Errorf("got %q", got)
	}
	if got := absolute("https://arxiv.org/html/1706.03762v7"); got != "https://arxiv.org/html/1706.03762v7" {
		t.Errorf("an absolute href was rewritten: %q", got)
	}
}

// TestParseAbsWithdrawn reads a paper the author withdrew.
//
// 0902.4054 is v1 in February 2009 and a one kilobyte v2 a month later that
// arXiv marks (withdrawn) in the submission history. It is the common case by a
// long way: arXiv declares its deletedRecord policy persistent, but sampling
// 40,000 OAI headers across four windows on 2026-08-15 turned up no header with
// status="deleted" at all, and a search of the comment field returns thousands
// of withdrawals.
func TestParseAbsWithdrawn(t *testing.T) {
	page := absFixture(t, "abs_0902.4054_withdrawn.html")

	if !page.Withdrawn {
		t.Fatal("a page whose newest version is marked (withdrawn) did not say so")
	}
	if len(page.Versions) != 2 {
		t.Fatalf("Versions: got %d, want 2", len(page.Versions))
	}
	if page.Versions[0].Withdrawn {
		t.Error("v1 is not the withdrawn one")
	}
	if !page.Versions[1].Withdrawn {
		t.Error("v2 is the withdrawn one and did not say so")
	}
	if page.Comment != "This paper has been withdrawn" {
		t.Errorf("Comment: got %q", page.Comment)
	}
}

// TestParseAbsNotWithdrawn is the other half of the pair. A page with no marker
// leaves every version alone, which is the assertion that stops the regexp
// group from matching whatever follows the size.
func TestParseAbsNotWithdrawn(t *testing.T) {
	page := absFixture(t, "abs_1706.03762.html")

	if page.Withdrawn {
		t.Error("the Attention paper is not withdrawn")
	}
	for _, v := range page.Versions {
		if v.Withdrawn {
			t.Errorf("v%d of a live paper is marked withdrawn", v.Version)
		}
	}
}

// TestMarkWithdrawnCarriesOver covers the merge arXivRaw wins. It names the
// source type, so it keeps the history, and it has no withdrawal marker at all,
// so the flag has to be carried across by version number.
func TestMarkWithdrawnCarriesOver(t *testing.T) {
	raw := []Version{
		{Version: 1, Via: SurfaceOAI, SourceType: "I"},
		{Version: 2, Via: SurfaceOAI, SourceType: "I"},
	}
	markWithdrawn(raw, []Version{{Version: 2, Withdrawn: true}})

	if raw[0].Withdrawn {
		t.Error("v1 was marked and should not have been")
	}
	if !raw[1].Withdrawn {
		t.Error("v2 lost its marker on the way over from the page")
	}
	if raw[1].SourceType != "I" {
		t.Errorf("the carry over overwrote the source type: %q", raw[1].SourceType)
	}
}
