package arxiv

import (
	"testing"
	"time"
)

func TestAtomFixtureAttention(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")

	if p.ID != "1706.03762" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.Version != 7 {
		t.Errorf("Version: got %d, want 7", p.Version)
	}
	if p.VersionedID != "1706.03762v7" {
		t.Errorf("VersionedID: got %q", p.VersionedID)
	}
	if p.Style != "new" {
		t.Errorf("Style: got %q, want new", p.Style)
	}
	if p.OAIID != "oai:arXiv.org:1706.03762" {
		t.Errorf("OAIID: got %q", p.OAIID)
	}
	if p.DOI != "10.48550/arXiv.1706.03762" {
		t.Errorf("DOI: got %q", p.DOI)
	}
	if p.Title != "Attention Is All You Need" {
		t.Errorf("Title: got %q", p.Title)
	}
	if p.Comment != "15 pages, 5 figures" {
		t.Errorf("Comment: got %q", p.Comment)
	}
	if len(p.Authors) != 8 {
		t.Fatalf("Authors: got %d, want 8", len(p.Authors))
	}
	if p.Authors[7].Name != "Illia Polosukhin" {
		t.Errorf("Authors[7]: got %q", p.Authors[7].Name)
	}
	if p.AuthorLine != "Ashish Vaswani, Noam Shazeer, Niki Parmar, and 5 more" {
		t.Errorf("AuthorLine: got %q", p.AuthorLine)
	}
	if p.PrimaryCategory != "cs.CL" {
		t.Errorf("PrimaryCategory: got %q", p.PrimaryCategory)
	}
	if len(p.Categories) != 2 || p.Categories[0] != "cs.CL" || p.Categories[1] != "cs.LG" {
		t.Errorf("Categories: got %v", p.Categories)
	}
	if len(p.CrossLists) != 1 || p.CrossLists[0] != "cs.LG" {
		t.Errorf("CrossLists: got %v", p.CrossLists)
	}
	// This is the value the whole first_submitted rule exists for. OAI says
	// 2023-08-02 for the same paper, because its created is the current
	// version's date.
	want := time.Date(2017, 6, 12, 17, 57, 34, 0, time.UTC)
	if !p.FirstSubmitted.Equal(want) {
		t.Errorf("FirstSubmitted: got %s, want %s", p.FirstSubmitted, want)
	}
	if p.Via["first_submitted"] != SurfaceAPI {
		t.Errorf("via.first_submitted: got %q, want %s", p.Via["first_submitted"], SurfaceAPI)
	}
	if got := p.LastUpdated.Format(time.RFC3339); got != "2023-08-02T00:41:18Z" {
		t.Errorf("LastUpdated: got %q", got)
	}
	if !p.IsLatest {
		t.Error("IsLatest: got false, want true on an unpinned read")
	}
	if p.Depth != string(DepthQuick) {
		t.Errorf("Depth: got %q, want quick", p.Depth)
	}
	if len(p.Missed) == 0 {
		t.Error("Missed: a quick read should name what it did not look at")
	}
}

func TestAtomFixtureATLAS(t *testing.T) {
	p := paperFixture(t, "api_1207.7214.xml")

	if p.ID != "1207.7214" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.JournalRef != "Phys.Lett. B716 (2012) 1-29" {
		t.Errorf("JournalRef: got %q", p.JournalRef)
	}
	if p.PublisherDOI != "10.1016/j.physletb.2012.08.020" {
		t.Errorf("PublisherDOI: got %q", p.PublisherDOI)
	}
	if p.Via["publisher_doi"] != SurfaceAPI {
		t.Errorf("via.publisher_doi: got %q", p.Via["publisher_doi"])
	}
	// The name in the feed is " The ATLAS Collaboration", leading space and all.
	if len(p.Authors) != 1 || p.Authors[0].Name != "The ATLAS Collaboration" {
		t.Errorf("Authors: got %#v", p.Authors)
	}
	if len(p.CrossLists) != 0 {
		t.Errorf("CrossLists: got %v, want none on a single category paper", p.CrossLists)
	}
	if p.Abstract == "" || len(p.Abstract) < 200 {
		t.Errorf("Abstract: got %d characters", len(p.Abstract))
	}
}

// TestAtomTimesAreUTC guards the one property a golden test cannot recover
// from. time.Parse hands back the zone it read, and a record stamped in a local
// zone compares unequal to the same instant in UTC.
func TestAtomTimesAreUTC(t *testing.T) {
	for _, name := range []string{"api_1706.03762.xml", "api_1207.7214.xml"} {
		p := paperFixture(t, name)
		for field, got := range map[string]time.Time{
			"first_submitted": p.FirstSubmitted,
			"last_updated":    p.LastUpdated,
		} {
			if got.IsZero() {
				t.Errorf("%s: %s is zero", name, field)
				continue
			}
			if got.Location() != time.UTC {
				t.Errorf("%s: %s is in %s, want UTC", name, field, got.Location())
			}
		}
	}
}

// TestAtomURLsAreCanonical pins the choice between the two URLs on offer. The
// links block is plain http, the id derived form is https, and the record keeps
// the version either way.
func TestAtomURLsAreCanonical(t *testing.T) {
	p := paperFixture(t, "api_1706.03762.xml")
	if p.URL != "https://arxiv.org/abs/1706.03762v7" {
		t.Errorf("URL: got %q", p.URL)
	}
	if p.PDFURL != "https://arxiv.org/pdf/1706.03762v7" {
		t.Errorf("PDFURL: got %q", p.PDFURL)
	}
}

func TestPrimaryFirst(t *testing.T) {
	cases := []struct {
		cats    []string
		primary string
		want    []string
	}{
		{[]string{"cs.CL", "cs.LG"}, "cs.CL", []string{"cs.CL", "cs.LG"}},
		{[]string{"cs.LG", "cs.CL"}, "cs.CL", []string{"cs.CL", "cs.LG"}},
		{[]string{"cs.LG"}, "", []string{"cs.LG"}},
		// A primary the list does not carry is still the primary, so it leads.
		{[]string{"cs.LG"}, "stat.ML", []string{"stat.ML", "cs.LG"}},
	}
	for _, tc := range cases {
		got := primaryFirst(tc.cats, tc.primary)
		if len(got) != len(tc.want) {
			t.Errorf("primaryFirst(%v, %q) = %v", tc.cats, tc.primary, got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("primaryFirst(%v, %q) = %v, want %v", tc.cats, tc.primary, got, tc.want)
				break
			}
		}
	}
}

func TestParseAtomTime(t *testing.T) {
	if _, ok := parseAtomTime("not a time"); ok {
		t.Error("parseAtomTime accepted a string that is not a time")
	}
	got, ok := parseAtomTime("  2017-06-12T17:57:34Z  ")
	if !ok {
		t.Fatal("parseAtomTime rejected a padded timestamp")
	}
	if got.Location() != time.UTC {
		t.Errorf("location: got %s, want UTC", got.Location())
	}
}
