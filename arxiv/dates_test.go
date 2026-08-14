package arxiv

import (
	"strings"
	"testing"
	"time"
)

// dates_test.go is about one field. first_submitted is the v1 submission time,
// three surfaces publish something that looks like it, and two of them are
// answering a different question.
//
// The export API's published element is the submission time of v1 and is the
// only one that is. The OAI arXiv record's created is the current version's
// date, and the abstract page prints the v1 date in prose next to a list of
// every other version's date. A read that merges OAI over the API without
// knowing this hands back a paper submitted a month after it was submitted, and
// nothing about the record looks wrong: the date is real, it is on arXiv, and
// it belongs to that paper.
//
// 1207.7214 is the ATLAS Higgs discovery paper and it is the clearest case in
// the suite. v1 went up on 31 July 2012 and v2 on 31 August, so the two answers
// are a month apart and a wrong one is obvious in a citation. 1706.03762 is the
// worse shape and the reason this is a rule rather than a special case: its
// created is 2023-08-02, six years out, because v7 was posted then.

// oaiCreated is the created element out of a saved OAI arXiv record.
func oaiCreated(t *testing.T, name string) string {
	t.Helper()
	created := oaiFixture(t, name).Metadata.Arxiv.Created
	if created == "" {
		t.Fatalf("%s has no created element, so it no longer covers the case", name)
	}
	return created
}

// The two surfaces disagree, on both papers, and the test says by how much.
//
// This is the assertion that the fixtures still cover the thing they were
// captured for. If arXiv ever changed created to mean v1, these fixtures would
// go on passing every other test in the suite while quietly testing nothing.
func TestTheOAIRecordDisagreesWithTheAPIAboutTheSubmissionDate(t *testing.T) {
	for _, c := range []struct {
		api, oai string
		want     string // the day created says, which is not the day of submission
	}{
		{"api_1207.7214.xml", "oai_arxiv_1207.7214.xml", "2012-08-31"},
		{"api_1706.03762.xml", "oai_arxiv_1706.03762.xml", "2023-08-02"},
	} {
		p := paperFixture(t, c.api)
		created := oaiCreated(t, c.oai)
		if created != c.want {
			t.Errorf("%s says created %s, and the fixture was captured because it says %s",
				c.oai, created, c.want)
		}
		if got := p.FirstSubmitted.Format("2006-01-02"); got == created {
			t.Errorf("%s and %s now agree on %s, so this pair no longer covers the disagreement",
				c.api, c.oai, got)
		}
	}
}

// created is the latest version's date. That is the whole explanation, and it
// is worth asserting rather than describing, because it is what makes the field
// useless for the question people ask of it and useful for a different one.
func TestCreatedIsTheLatestVersionNotTheFirst(t *testing.T) {
	for _, c := range []struct{ api, oai string }{
		{"api_1207.7214.xml", "oai_arxiv_1207.7214.xml"},
		{"api_1706.03762.xml", "oai_arxiv_1706.03762.xml"},
	} {
		p := paperFixture(t, c.api)
		created := oaiCreated(t, c.oai)
		if got := p.LastUpdated.UTC().Format("2006-01-02"); got != created {
			t.Errorf("%s says created %s and the API last updated %s, so created is not the current version's date",
				c.oai, created, got)
		}
	}
}

// Merging the OAI record leaves the date alone.
//
// This is the rule in doc 03 made into a test on the merge that would break it.
// mergeOAIArxiv fills in the report number, the license and the author name
// parts, all of which the API has nothing for, and the temptation with a merge
// like that is to take every field that is empty or looks better. created looks
// better: it is a clean day with no time on it.
func TestMergingOAIDoesNotMoveTheSubmissionDate(t *testing.T) {
	p := paperFixture(t, "api_1207.7214.xml")
	want := time.Date(2012, 7, 31, 11, 59, 59, 0, time.UTC)
	if !p.FirstSubmitted.Equal(want) {
		t.Fatalf("the API fixture says %s, want %s", p.FirstSubmitted, want)
	}

	mergeOAIArxiv(&p, oaiFixture(t, "oai_arxiv_1207.7214.xml"),
		"https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3A1207.7214&metadataPrefix=arXiv")
	if !p.FirstSubmitted.Equal(want) {
		t.Errorf("after the OAI merge first_submitted is %s, want %s", p.FirstSubmitted, want)
	}
	if p.Via["first_submitted"] != SurfaceAPI {
		t.Errorf("via.first_submitted is %q, and only %s answers this one",
			p.Via["first_submitted"], SurfaceAPI)
	}

	// The abstract page prints six dates for this paper and the merge reads
	// none of them into this field either.
	mergeAbs(&p, absFixture(t, "abs_1207.7214.html"), "https://arxiv.org/abs/1207.7214")
	if !p.FirstSubmitted.Equal(want) {
		t.Errorf("after the abstract page first_submitted is %s, want %s", p.FirstSubmitted, want)
	}
	if p.Via["first_submitted"] != SurfaceAPI {
		t.Errorf("via.first_submitted is %q after three surfaces", p.Via["first_submitted"])
	}
}

// The abstract page agrees with the API, in prose, which is what makes the
// choice checkable by a person. It says submitted on 31 July, not 31 August,
// and it is the surface a reader would go and look at.
func TestTheAbstractPageAgreesWithTheAPI(t *testing.T) {
	page := string(fixture(t, "abs_1207.7214.html"))
	if !strings.Contains(page, "Submitted on 31 Jul 2012") {
		t.Error("the abstract page no longer prints the v1 submission date in prose")
	}
	if !strings.Contains(page, "31 Aug 2012") {
		t.Error("the abstract page no longer prints the v2 date, so the two are no longer next to each other")
	}
	if i, j := strings.Index(page, "Submitted on 31 Jul 2012"), strings.Index(page, "31 Aug 2012"); i > j {
		t.Error("the v2 date comes first on the page, which is the order a naive read would take")
	}
}

// The record ends up saying both things, in two fields, with the version table
// holding the rest. Nothing is lost by picking one for first_submitted: the
// month later date is the last update and is on the record under that name.
func TestBothDatesSurviveOnTheRecord(t *testing.T) {
	p := higgsRecord(t)
	if got := p.FirstSubmitted.UTC().Format("2006-01-02"); got != "2012-07-31" {
		t.Errorf("first_submitted is %s", got)
	}
	if got := p.LastUpdated.UTC().Format("2006-01-02"); got != "2012-08-31" {
		t.Errorf("last_updated is %s, and the date the OAI record calls created has to be somewhere", got)
	}
	if p.FirstSubmitted.After(p.LastUpdated) {
		t.Error("the paper was updated before it was submitted")
	}
}
