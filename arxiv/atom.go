package arxiv

import (
	"encoding/xml"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// ─── the wire types for s1, the export API's Atom ───

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Total   int         `xml:"totalResults"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID              string     `xml:"id"`
	Title           string     `xml:"title"`
	Summary         string     `xml:"summary"`
	Published       string     `xml:"published"`
	Updated         string     `xml:"updated"`
	Authors         []atomName `xml:"author"`
	Links           []atomLink `xml:"link"`
	PrimaryCategory atomCat    `xml:"primary_category"`
	Categories      []atomCat  `xml:"category"`
	Comment         string     `xml:"comment"`
	DOI             string     `xml:"doi"`
	JournalRef      string     `xml:"journal_ref"`
}

type atomName struct {
	Name        string `xml:"name"`
	Affiliation string `xml:"affiliation"`
}

type atomLink struct {
	Href  string `xml:"href,attr"`
	Rel   string `xml:"rel,attr"`
	Type  string `xml:"type,attr"`
	Title string `xml:"title,attr"`
}

type atomCat struct {
	Term string `xml:"term,attr"`
}

// entryToPaper turns one Atom entry into a paper read at depth quick.
//
// Everything s1 publishes is kept. The old mapping dropped the version, the
// comment, the journal reference and the publisher DOI, all of which are on the
// wire and cost nothing to keep.
func entryToPaper(e atomEntry, source string, at time.Time) Paper {
	p := Paper{
		Envelope: Envelope{
			Kind:        "paper",
			RetrievedAt: at.UTC(),
		},
		Title:      cleanText(e.Title),
		Abstract:   cleanText(e.Summary),
		Comment:    cleanText(e.Comment),
		JournalRef: cleanText(e.JournalRef),
		Depth:      string(DepthQuick),
	}
	p.addSurface(SurfaceAPI, source)

	// Four surfaces carry these four fields, so the record says which one
	// answered here rather than leaving the reader to guess.
	for field, value := range map[string]string{
		"title":       p.Title,
		"abstract":    p.Abstract,
		"comment":     p.Comment,
		"journal_ref": p.JournalRef,
	} {
		if value != "" {
			p.setVia(field, SurfaceAPI)
		}
	}

	// The entry id is the one place s1 puts the version, so it is parsed
	// rather than trimmed. An id that will not parse is kept verbatim, because
	// an unreadable id is still better evidence than an empty one.
	if id, err := axid.Parse(e.ID); err == nil {
		p.ID = id.Canonical
		p.Version = id.Version
		p.Style = string(id.Style)
		p.OAIID = id.OAI()
		p.DOI = id.DOI()
		p.VersionedID = id.Versioned()
		p.URL = id.AbsURL()
		p.PDFURL = id.PDFURL()
	} else {
		p.ID = strings.TrimSpace(e.ID)
	}
	// s1 answers with the current version unless the request pinned one, so a
	// record built from it describes the latest.
	p.IsLatest = true

	if doi := cleanText(e.DOI); doi != "" {
		p.PublisherDOI = doi
		p.setVia("publisher_doi", SurfaceAPI)
	}

	for _, a := range e.Authors {
		// s1's names can carry leading whitespace, as in the ATLAS
		// collaboration's entry, so they are trimmed rather than trusted.
		name := cleanText(a.Name)
		if name == "" {
			continue
		}
		p.Authors = append(p.Authors, Author{
			Name:        name,
			Affiliation: cleanText(a.Affiliation),
			Via:         SurfaceAPI,
		})
	}
	p.AuthorLine = authorLine(p.Authors)

	p.PrimaryCategory = e.PrimaryCategory.Term
	for _, c := range e.Categories {
		if c.Term != "" && !contains(p.Categories, c.Term) {
			p.Categories = append(p.Categories, c.Term)
		}
	}
	if p.PrimaryCategory == "" && len(p.Categories) > 0 {
		p.PrimaryCategory = p.Categories[0]
	}
	p.Categories = primaryFirst(p.Categories, p.PrimaryCategory)
	p.CrossLists = crossLists(p.Categories, p.PrimaryCategory)
	if len(p.Categories) > 0 {
		p.setVia("categories", SurfaceAPI)
	}

	// published is the v1 submission and it is the only authoritative source
	// for it. OAI's created is the current version's date, measured on two
	// papers, so it is never used here.
	if t, ok := parseAtomTime(e.Published); ok {
		p.FirstSubmitted = t
		p.setVia("first_submitted", SurfaceAPI)
	}
	if t, ok := parseAtomTime(e.Updated); ok {
		p.LastUpdated = t
		p.setVia("last_updated", SurfaceAPI)
	}

	// The links block is the fallback for an id that would not parse. Its hrefs
	// are plain http and pinned to a version, so where the id did parse the
	// canonical https links derived from it are the better answer.
	for _, l := range e.Links {
		switch {
		case l.Type == "text/html" && l.Href != "" && p.URL == "":
			p.URL = l.Href
		case l.Type == "application/pdf" && l.Href != "" && p.PDFURL == "":
			p.PDFURL = l.Href
		}
	}

	p.Missed = DepthQuick.Missed(p.ID)
	return p
}

// primaryFirst reorders categories so the primary leads, which is announcement
// order and the order every arXiv surface prints them in.
func primaryFirst(cats []string, primary string) []string {
	if primary == "" || len(cats) == 0 || cats[0] == primary {
		return cats
	}
	out := make([]string, 0, len(cats)+1)
	out = append(out, primary)
	for _, c := range cats {
		if c != primary {
			out = append(out, c)
		}
	}
	return out
}

// parseAtomTime reads an RFC 3339 timestamp and returns it in UTC.
//
// The UTC conversion is not decoration. time.Parse hands back a zone and a
// golden test that pins a local-zone timestamp only passes in the zone it was
// written in, which cost a sibling tool a CI failure.
func parseAtomTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// absURL and pdfURL are the canonical links for a bare id.
func absURL(id string) string { return "https://" + Host + "/abs/" + id }
func pdfURL(id string) string { return "https://" + Host + "/pdf/" + id }
