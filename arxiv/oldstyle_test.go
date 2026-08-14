package arxiv

import (
	"testing"
)

// oldstyle_test.go reads a paper from before April 2007 through all four
// merges, because every other full record in the suite is a new style id and an
// id is not a string this tool passes through.
//
// hep-th/9711200 is Maldacena, which is a good choice for more than its citation
// count. The id has a slash in it, so it goes through a URL path, a query
// parameter and an OAI identifier and has to come back the same both times. It
// has one author, three versions, a journal reference, a publisher DOI and a
// report number, and its primary category is an archive with no dot in it,
// which is the case a category parser written against cs.CL gets wrong.

// oldRecord is the old style paper read to depth full, built the same way
// fullRecord is.
func oldRecord(t *testing.T) Paper {
	t.Helper()
	p := paperFixture(t, "api_hep-th_9711200.xml")
	mergeOAIArxiv(&p, oaiFixture(t, "oai_arxiv_hep-th_9711200.xml"),
		"https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3Ahep-th%2F9711200&metadataPrefix=arXiv")
	mergeOAIRaw(&p, oaiFixture(t, "oai_raw_hep-th_9711200.xml"),
		"https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3Ahep-th%2F9711200&metadataPrefix=arXivRaw")
	mergeAbs(&p, absFixture(t, "abs_hep-th_9711200.html"), "https://arxiv.org/abs/hep-th/9711200")
	annotateDepth(&p, DepthFull)
	return p
}

// The id survives four surfaces with its slash intact and is not confused with a
// version, a category or a path.
func TestAnOldStyleIDComesBackWhole(t *testing.T) {
	p := oldRecord(t)
	if p.ID != "hep-th/9711200" {
		t.Errorf("id = %q, want hep-th/9711200", p.ID)
	}
	if p.Style != "old" {
		t.Errorf("style = %q, want old", p.Style)
	}
	if p.OAIID != "oai:arXiv.org:hep-th/9711200" {
		t.Errorf("oai id = %q", p.OAIID)
	}
	if p.DOI != "10.48550/arXiv.hep-th/9711200" {
		t.Errorf("arXiv doi = %q, want the id inside it unescaped", p.DOI)
	}
	if p.VersionedID != "hep-th/9711200v3" {
		t.Errorf("versioned id = %q", p.VersionedID)
	}
	if p.URL != "https://arxiv.org/abs/hep-th/9711200v3" {
		t.Errorf("url = %q", p.URL)
	}
}

// An archive with no dot in it is a category. cs.CL has a full stop and hep-th
// has a hyphen, and a split on the dot leaves the second one with no name.
func TestAnArchiveWithNoDotIsStillACategory(t *testing.T) {
	p := oldRecord(t)
	if p.PrimaryCategory != "hep-th" {
		t.Errorf("primary category = %q", p.PrimaryCategory)
	}
	if len(p.Categories) != 1 || p.Categories[0] != "hep-th" {
		t.Errorf("categories = %v, want just hep-th", p.Categories)
	}
	if got := p.SubjectNames["hep-th"]; got != "High Energy Physics - Theory" {
		t.Errorf("the abs page named hep-th %q", got)
	}
}

// The rest of the record, which is the same shape as any other paper. This is
// the assertion that an old id is only an id: nothing else about the read
// changes.
func TestTheOldStylePaperIsAWholeRecord(t *testing.T) {
	p := oldRecord(t)
	if p.Title != "The Large N Limit of Superconformal Field Theories and Supergravity" {
		t.Errorf("title = %q", p.Title)
	}
	if len(p.Authors) != 1 || p.Authors[0].Keyname != "Maldacena" {
		t.Errorf("authors = %+v, want one with a surname off the OAI record", p.Authors)
	}
	if p.JournalRef != "Adv.Theor.Math.Phys.2:231-252,1998" {
		t.Errorf("journal ref = %q", p.JournalRef)
	}
	if p.PublisherDOI != "10.1023/A:1026654312961" {
		t.Errorf("publisher doi = %q", p.PublisherDOI)
	}
	if p.ReportNo != "HUTP-98/A097" {
		t.Errorf("report number = %q, and it only ever comes off the OAI record", p.ReportNo)
	}
	if len(p.Versions) != 3 {
		t.Fatalf("%d versions, want three", len(p.Versions))
	}
	if p.Version != 3 || !p.IsLatest {
		t.Errorf("version = %d, latest = %v", p.Version, p.IsLatest)
	}
	if got := p.FirstSubmitted.UTC().Format("2006-01-02"); got != "1997-11-27" {
		t.Errorf("first submitted %s, want the day in 1997", got)
	}
}

// A paper from 1997 has no license element, because arXiv started asking for one
// in 2007. An empty license is the truth here rather than a missing read, and
// the record says so by leaving it empty and naming nothing in via.
func TestAPaperOlderThanLicensesHasNone(t *testing.T) {
	p := oldRecord(t)
	if p.License != "" && p.Via["license"] == "" {
		t.Errorf("license = %q with nothing in via saying where it came from", p.License)
	}
}
