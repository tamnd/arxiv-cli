package arxiv

import (
	"net/url"
	"testing"
	"time"
)

func TestOAIURL(t *testing.T) {
	u := oaiURL("GetRecord", "oai:arXiv.org:1706.03762", FormatArxiv)
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Host != "oaipmh.arxiv.org" {
		t.Errorf("host: got %q, want the endpoint the documented base redirects to", parsed.Host)
	}
	q := parsed.Query()
	if q.Get("verb") != "GetRecord" {
		t.Errorf("verb: got %q", q.Get("verb"))
	}
	if q.Get("identifier") != "oai:arXiv.org:1706.03762" {
		t.Errorf("identifier: got %q", q.Get("identifier"))
	}
	if q.Get("metadataPrefix") != FormatArxiv {
		t.Errorf("metadataPrefix: got %q", q.Get("metadataPrefix"))
	}
}

// TestOAICreatedIsNotFirstSubmitted is the measurement that shaped the model.
// OAI says this paper was created on 2023-08-02 and the export API says it was
// submitted on 2017-06-12. The 2023 date is v7's, so first_submitted is filled
// from s1 alone and the merge must leave it be.
func TestOAICreatedIsNotFirstSubmitted(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	rec := oaiFixture(t, "oai_arxiv_1706.03762.xml")

	if rec.Metadata.Arxiv.Created != "2023-08-02" {
		t.Fatalf("the fixture no longer carries the created value this test is about: %q",
			rec.Metadata.Arxiv.Created)
	}
	mergeOAIArxiv(&p, rec, "https://oaipmh.arxiv.org/oai")

	want := time.Date(2017, 6, 12, 17, 57, 34, 0, time.UTC)
	if !p.FirstSubmitted.Equal(want) {
		t.Errorf("FirstSubmitted: got %s, want %s", p.FirstSubmitted, want)
	}
	if p.Via["first_submitted"] != SurfaceAPI {
		t.Errorf("via.first_submitted: got %q, want %s", p.Via["first_submitted"], SurfaceAPI)
	}
}

func TestMergeOAIArxiv(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	rec := oaiFixture(t, "oai_arxiv_1706.03762.xml")
	mergeOAIArxiv(&p, rec, "https://oaipmh.arxiv.org/oai")

	if len(p.Surfaces) != 2 || p.Surfaces[1] != SurfaceOAI {
		t.Errorf("Surfaces: got %v", p.Surfaces)
	}
	if p.License != "http://arxiv.org/licenses/nonexclusive-distrib/1.0/" {
		t.Errorf("License: got %q", p.License)
	}
	// The structured names live on this surface and nowhere else.
	if len(p.Authors) != 8 {
		t.Fatalf("Authors: got %d, want 8", len(p.Authors))
	}
	first := p.Authors[0]
	if first.Name != "Ashish Vaswani" || first.Keyname != "Vaswani" || first.Forenames != "Ashish" {
		t.Errorf("Authors[0]: got %#v", first)
	}
	if first.Via != SurfaceOAI {
		t.Errorf("Authors[0].Via: got %q, want %s", first.Via, SurfaceOAI)
	}
	if p.AuthorLine != "Ashish Vaswani, Noam Shazeer, Niki Parmar, and 5 more" {
		t.Errorf("AuthorLine: got %q", p.AuthorLine)
	}
	want := time.Date(2023, 8, 3, 0, 0, 0, 0, time.UTC)
	if !p.OAIDatestamp.Equal(want) {
		t.Errorf("OAIDatestamp: got %s, want %s", p.OAIDatestamp, want)
	}
	if p.Withdrawn {
		t.Error("Withdrawn: got true on a live paper")
	}
}

// TestMergeOAICollaboration is the reason a keyname with no forenames is not
// claimed as a surname. "The ATLAS Collaboration" is the whole name.
func TestMergeOAICollaboration(t *testing.T) {
	p := paperFixture(t, "api_1207.7214.xml")
	rec := oaiFixture(t, "oai_arxiv_1207.7214.xml")
	mergeOAIArxiv(&p, rec, "https://oaipmh.arxiv.org/oai")

	if len(p.Authors) != 1 {
		t.Fatalf("Authors: got %d, want 1", len(p.Authors))
	}
	a := p.Authors[0]
	if a.Name != "The ATLAS Collaboration" {
		t.Errorf("Name: got %q", a.Name)
	}
	if a.Keyname != "" || a.Forenames != "" {
		t.Errorf("a collaboration was split into name parts: %#v", a)
	}
	// The report number appears on this surface and on the abstract page, and
	// on neither of the two the export API answers with.
	if p.ReportNo != "CERN-PH-EP-2012-218" {
		t.Errorf("ReportNo: got %q", p.ReportNo)
	}
	if p.Via["report_no"] != SurfaceOAI {
		t.Errorf("via.report_no: got %q", p.Via["report_no"])
	}
	// The journal reference came from s1 first, so the via still names s1.
	if p.Via["journal_ref"] != SurfaceAPI {
		t.Errorf("via.journal_ref: got %q, want %s", p.Via["journal_ref"], SurfaceAPI)
	}
}

func TestMergeOAIRawVersions(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	rec := oaiFixture(t, "oai_raw_1706.03762.xml")
	mergeOAIRaw(&p, rec, "https://oaipmh.arxiv.org/oai")

	if p.Submitter != "Llion Jones" {
		t.Errorf("Submitter: got %q", p.Submitter)
	}
	if len(p.Versions) != 7 {
		t.Fatalf("Versions: got %d, want 7", len(p.Versions))
	}
	for i, v := range p.Versions {
		if v.Version != i+1 {
			t.Fatalf("Versions[%d].Version = %d, want the history in order", i, v.Version)
		}
		if v.Date.IsZero() {
			t.Errorf("Versions[%d] has no date", i)
		}
		if v.Date.Location() != time.UTC {
			t.Errorf("Versions[%d].Date is in %s, want UTC", i, v.Date.Location())
		}
		if v.Via != SurfaceOAI {
			t.Errorf("Versions[%d].Via = %q", i, v.Via)
		}
	}
	v1 := p.Versions[0]
	if got := v1.Date.Format(time.RFC3339); got != "2017-06-12T17:57:34Z" {
		t.Errorf("v1 date: got %q", got)
	}
	// v1's date and s1's published are the same instant, which is why the two
	// surfaces can be trusted to describe the same paper.
	if !v1.Date.Equal(p.FirstSubmitted) {
		t.Errorf("v1 %s and first_submitted %s disagree", v1.Date, p.FirstSubmitted)
	}
	if v1.SizeBytes != 1102*1024 {
		t.Errorf("v1 size: got %d, want %d", v1.SizeBytes, 1102*1024)
	}
	if v1.SourceType != "D" || v1.SourceKind != "pdf-only" {
		t.Errorf("v1 source: got %q, %q", v1.SourceType, v1.SourceKind)
	}
	if !p.IsLatest {
		t.Error("IsLatest: v7 of seven versions is the latest")
	}
}

// TestMergeOAIRawOldVersionIsNotLatest reads the same history against a pinned
// earlier version, which is the case is_latest exists for.
func TestMergeOAIRawOldVersionIsNotLatest(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	p.Version = 3
	rec := oaiFixture(t, "oai_raw_1706.03762.xml")
	mergeOAIRaw(&p, rec, "https://oaipmh.arxiv.org/oai")

	if p.IsLatest {
		t.Error("IsLatest: v3 of seven versions is not the latest")
	}
}

func TestRawVersionsSkipsUnparseable(t *testing.T) {
	got := rawVersions([]oaiVersion{
		{Version: "v2", Date: "Mon, 19 Jun 2017 16:49:45 GMT", Size: "1124kb", SourceType: "I"},
		{Version: "vX", Date: "Mon, 19 Jun 2017 16:49:45 GMT"},
		{Version: "v1", Date: "not a date", Size: "nonsense", SourceType: "Z"},
	})
	if len(got) != 2 {
		t.Fatalf("got %d versions, want the two with readable numbers", len(got))
	}
	if got[0].Version != 1 || got[1].Version != 2 {
		t.Errorf("history out of order: %v", got)
	}
	// An unreadable date and size leave zeroes rather than guesses, and an
	// unknown source letter is kept without an interpretation.
	if !got[0].Date.IsZero() || got[0].SizeBytes != 0 {
		t.Errorf("v1: got %#v", got[0])
	}
	if got[0].SourceType != "Z" || got[0].SourceKind != "" {
		t.Errorf("v1 source: got %q, %q", got[0].SourceType, got[0].SourceKind)
	}
	if got[1].SourceKind != "tex" {
		t.Errorf("v2 source kind: got %q", got[1].SourceKind)
	}
}

// TestParseKilobytes covers both spellings arXiv uses for the same number. The
// export side writes 1102kb and the abstract page writes 1,102 KB.
func TestParseKilobytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"1102kb", 1102 * 1024, true},
		{"1,102 KB", 1102 * 1024, true},
		{" 1124kb ", 1124 * 1024, true},
		{"", 0, false},
		{"kb", 0, false},
		{"big", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseKilobytes(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseKilobytes(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseRawDate(t *testing.T) {
	cases := []string{
		"Mon, 12 Jun 2017 17:57:34 GMT",
		"Wed, 6 Dec 2017 03:30:32 GMT",
	}
	for _, in := range cases {
		got, ok := parseRawDate(in)
		if !ok {
			t.Errorf("parseRawDate(%q) failed", in)
			continue
		}
		if got.Location() != time.UTC {
			t.Errorf("parseRawDate(%q) is in %s, want UTC", in, got.Location())
		}
	}
	if _, ok := parseRawDate("yesterday"); ok {
		t.Error("parseRawDate accepted a string that is not a date")
	}
}

func TestMergeAuthorsKeepsAffiliation(t *testing.T) {
	have := []Author{
		{Name: "Ashish Vaswani", Affiliation: "Google Brain", Via: SurfaceAPI},
		{Name: "Noam Shazeer", Via: SurfaceAPI},
	}
	structured := []Author{
		{Name: "Ashish Vaswani", Keyname: "Vaswani", Forenames: "Ashish", Via: SurfaceOAI},
		{Name: "Noam Shazeer", Keyname: "Shazeer", Forenames: "Noam", Via: SurfaceOAI},
	}
	got := mergeAuthors(have, structured)
	if got[0].Affiliation != "Google Brain" {
		t.Errorf("the affiliation only s1 had was dropped: %#v", got[0])
	}
	if got[0].Keyname != "Vaswani" {
		t.Errorf("the structured name did not win: %#v", got[0])
	}

	// Different lengths mean the surfaces disagree about who wrote the paper,
	// and the structured list wins whole rather than being zipped by position.
	got = mergeAuthors(have[:1], structured)
	if len(got) != 2 || got[0].Affiliation != "" {
		t.Errorf("mismatched lengths were merged positionally: %#v", got)
	}
}
