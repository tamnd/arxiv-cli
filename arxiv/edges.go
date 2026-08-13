package arxiv

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// Claim is one edge as a record: subject, predicate, object, and who said so.
//
// It is the graph edge with the tags the renderer needs and nothing else, so
// what `arxiv edges` prints and what a store writes are the same rows.
type Claim struct {
	From      string `json:"from" kit:"id" table:"from,truncate"`
	Predicate string `json:"predicate" table:"predicate"`
	To        string `json:"to" table:"to,truncate"`
	// Note is a name for the end that a URI hides: the spelling a name node
	// normalised away, the journal reference behind a hash, the category name
	// beside its code. It is what makes a table of URIs readable before
	// anything has been fetched.
	Note string `json:"note,omitempty" table:"note,truncate"`
	// Position is the order an ordered read gave the claim, which on authored
	// is the author order.
	Position int `json:"position,omitempty" table:"-"`
	// Surface is which of s1 to s12 asserted it and Source is the URL.
	Surface     string    `json:"surface" table:"-"`
	Source      string    `json:"source" table:"-,url"`
	RetrievedAt time.Time `json:"retrieved_at" table:"-"`
}

// claimsOf turns edges into records.
func claimsOf(edges []graph.Edge, at time.Time) []Claim {
	out := make([]Claim, 0, len(edges))
	for _, e := range edges {
		out = append(out, Claim{
			From:        e.From,
			Predicate:   e.Predicate,
			To:          e.To,
			Note:        e.Note,
			Position:    e.Position,
			Surface:     e.Surface,
			Source:      e.Source,
			RetrievedAt: at,
		})
	}
	return out
}

// Coverage is how much of a bibliography became claims.
//
// It exists because cites is the one predicate that can look complete and not
// be. arXiv publishes no citation graph, so every cites row comes out of a
// rendered bibliography, and an entry that names no arXiv id, no DOI and no
// link resolves to nothing at all. Reporting the fraction is the difference
// between a partial citation set and a wrong one.
type Coverage struct {
	Entries  int `json:"entries"`
	Resolved int `json:"resolved"`
}

func (c Coverage) String() string {
	if c.Entries == 0 {
		return "this rendering has no bibliography entries, so there are no cites claims"
	}
	return fmt.Sprintf("cites covers %d of %d bibliography entries (%d%%); the rest name no arXiv id, no DOI and no link",
		c.Resolved, c.Entries, c.Resolved*100/c.Entries)
}

// edgeSet collects claims, refuses anything the predicate table does not allow,
// and keeps one row per (from, predicate, to, source).
//
// A refusal is kept rather than dropped. An edge pointing at the wrong kind of
// node joins to nothing and looks like missing data rather than like a bug, so
// the thing that catches it has to be loud.
type edgeSet struct {
	out     []graph.Edge
	seen    map[string]bool
	refused []string
}

// add validates one claim and keeps it if it is new.
//
// An end that could not be named is not an error: graph.Journal of an empty
// reference is the empty string, and a paper with no journal reference has
// nothing to say rather than something wrong to say.
func (s *edgeSet) add(e graph.Edge) {
	if e.From == "" || e.To == "" {
		return
	}
	if err := e.Validate(); err != nil {
		s.refused = append(s.refused, err.Error())
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	key := e.Key()
	if s.seen[key] {
		return
	}
	s.seen[key] = true
	s.out = append(s.out, e)
}

// addAll folds another extractor's edges in.
func (s *edgeSet) addAll(edges []graph.Edge) {
	for _, e := range edges {
		s.add(e)
	}
}

// surfaceOfURL says which surface a URL belongs to.
//
// The envelope keeps surfaces and sources as two lists appended together, which
// is one URL per surface until OAI is read twice for its two formats, and then
// the indices no longer line up. Matching on the URL is the fix, and it is
// useful on its own: a record read from a cache or a fixture still knows which
// surface each of its sources was.
func surfaceOfURL(raw string) string {
	switch {
	case strings.HasPrefix(raw, apiBase):
		return SurfaceAPI
	case strings.HasPrefix(raw, oaiBase):
		return SurfaceOAI
	case strings.HasPrefix(raw, rssBase):
		return SurfaceRSS
	case strings.HasPrefix(raw, listBase):
		return SurfaceList
	case strings.HasPrefix(raw, s5Base):
		return SurfaceSearch
	case strings.HasPrefix(raw, taxonomyURL):
		return SurfaceTaxonomy
	case strings.HasPrefix(raw, authorBase):
		return SurfaceAuthorID
	case strings.HasPrefix(raw, htmlBase):
		return SurfaceFullText
	case strings.HasPrefix(raw, trackbackBase):
		return SurfaceTrackback
	case strings.HasPrefix(raw, pdfBase), strings.HasPrefix(raw, srcBase):
		return SurfaceFiles
	case strings.HasPrefix(raw, absBase):
		return SurfaceAbs
	}
	return ""
}

// sourceOf is the URL a given surface contributed to a record.
//
// The last match wins, which on OAI is the arXivRaw read, and that is the
// record carrying the version table and the submitter.
func sourceOf(e Envelope, surface string) string {
	out := ""
	for _, s := range e.Sources {
		if surfaceOfURL(s) == surface {
			out = s
		}
	}
	if out == "" && len(e.Sources) == 1 && contains(e.Surfaces, surface) {
		// One surface and one source that is not a URL, which is the taxonomy
		// answering from the bundled snapshot after a failed fetch.
		return e.Sources[0]
	}
	return out
}

// viaOr says which surface answered for a field, falling back when the record
// never recorded one.
func viaOr(e Envelope, field, fallback string) string {
	if s, ok := e.Via[field]; ok && s != "" {
		return s
	}
	return fallback
}

// EdgesOfPaper is every claim a paper record already contains.
//
// This is the whole point of the plane. One export API request returns the
// author list, the category set, the journal reference and the DOI, so twenty
// claims come out of three seconds of waiting, and a deeper read adds the
// version history, the licence and the files without another surface being
// invented for it.
func EdgesOfPaper(p Paper) []graph.Edge {
	var s edgeSet
	paper := graph.Paper(p.ID)

	// authored runs from the name to the paper, which reads backwards compared
	// to the record. It is that way round because the query worth answering is
	// everything this name touched, and a claim indexed from the name answers
	// it without a scan. The note keeps the spelling the normalisation dropped.
	authors := viaOr(p.Envelope, "authors", SurfaceAPI)
	authorSource := sourceOf(p.Envelope, authors)
	rendering := sourceOf(p.Envelope, SurfaceFullText)
	for i, a := range p.Authors {
		s.add(graph.Edge{
			From: graph.Name(a.Name), Predicate: graph.Authored, To: paper,
			Source: authorSource, Surface: authors, Note: a.Name, Position: i + 1,
		})
		// An affiliation is only ever on the rendering, so it is only ever
		// asserted by s10 and only present at --depth text.
		if a.Affiliation != "" && rendering != "" {
			s.add(graph.Edge{
				From: graph.Name(a.Name), Predicate: graph.AffiliatedWith, To: graph.External(a.Affiliation),
				Source: rendering, Surface: SurfaceFullText, Note: a.Affiliation,
			})
		}
	}

	cats := viaOr(p.Envelope, "categories", SurfaceAPI)
	catSource := sourceOf(p.Envelope, cats)
	if p.PrimaryCategory != "" {
		s.add(graph.Edge{
			From: paper, Predicate: graph.PrimaryCategory, To: graph.Category(p.PrimaryCategory),
			Source: catSource, Surface: cats, Note: p.SubjectNames[p.PrimaryCategory],
		})
	}
	for _, code := range p.Categories {
		s.add(graph.Edge{
			From: paper, Predicate: graph.InCategory, To: graph.Category(code),
			Source: catSource, Surface: cats, Note: p.SubjectNames[code],
		})
	}
	for _, code := range p.CrossLists {
		s.add(graph.Edge{
			From: paper, Predicate: graph.CrossListed, To: graph.Category(code),
			Source: catSource, Surface: cats, Note: p.SubjectNames[code],
		})
	}

	// The version list is the one edge set derived rather than read: supersedes
	// is the order of the list and costs no request of its own.
	versions := viaOr(p.Envelope, "versions", SurfaceOAI)
	versionSource := sourceOf(p.Envelope, versions)
	for i, v := range p.Versions {
		s.add(graph.Edge{
			From: paper, Predicate: graph.HasVersion, To: graph.Version(p.ID, v.Version),
			Source: versionSource, Surface: versions, Note: dayOf(v.Date), Position: v.Version,
		})
		if i > 0 {
			s.add(graph.Edge{
				From: graph.Version(p.ID, v.Version), Predicate: graph.Supersedes,
				To:     graph.Version(p.ID, p.Versions[i-1].Version),
				Source: versionSource, Surface: versions,
			})
		}
	}

	if p.JournalRef != "" {
		journal := viaOr(p.Envelope, "journal_ref", SurfaceAPI)
		s.add(graph.Edge{
			From: paper, Predicate: graph.PublishedIn, To: graph.Journal(p.JournalRef),
			Source: sourceOf(p.Envelope, journal), Surface: journal, Note: p.JournalRef,
		})
	}

	// arXiv's own DOI is a formula on the id rather than a field anybody read,
	// and the note says so, because a claim that hides how it was made is worse
	// than one that admits it.
	if p.DOI != "" && authorSource != "" {
		s.add(graph.Edge{
			From: paper, Predicate: graph.HasDOI, To: graph.DOI(p.DOI),
			Source: authorSource, Surface: authors, Note: "arXiv's own, computed from the id",
		})
	}
	if p.PublisherDOI != "" {
		doi := viaOr(p.Envelope, "publisher_doi", SurfaceAPI)
		s.add(graph.Edge{
			From: paper, Predicate: graph.HasDOI, To: graph.DOI(p.PublisherDOI),
			Source: sourceOf(p.Envelope, doi), Surface: doi, Note: "the publisher's",
		})
	}

	if p.License != "" {
		license := viaOr(p.Envelope, "license", SurfaceOAI)
		s.add(graph.Edge{
			From: paper, Predicate: graph.LicensedUnder, To: graph.License(p.License),
			Source: sourceOf(p.Envelope, license), Surface: license, Note: p.LicenseName,
		})
	}

	// The submitter is one of the authors and often not the first, which is why
	// it is its own claim rather than a position on authored.
	if p.Submitter != "" {
		by := viaOr(p.Envelope, "submitter", SurfaceOAI)
		s.add(graph.Edge{
			From: graph.Name(p.Submitter), Predicate: graph.SubmittedBy, To: paper,
			Source: sourceOf(p.Envelope, by), Surface: by, Note: p.Submitter,
		})
	}

	// Which files exist came off the abstract page, so has_file is only known
	// at --depth full and s3 is what asserted it.
	if abs := sourceOf(p.Envelope, SurfaceAbs); abs != "" {
		for _, f := range filesOf(p, time.Time{}) {
			s.add(graph.Edge{
				From: paper, Predicate: graph.HasFile, To: graph.File(p.ID, p.Version, f.Kind),
				Source: abs, Surface: SurfaceAbs, Note: f.URL,
			})
		}
	}

	return s.out
}

// EdgesOfAnnouncement is what one feed item claims.
//
// The announcement is the only surface that says which category a paper was
// announced in and whether it was new, a cross list or a replacement, and that
// is a different fact from the categories on the paper.
func EdgesOfAnnouncement(a Announcement) []graph.Edge {
	var s edgeSet
	paper := graph.Paper(a.PaperID)
	source := sourceOf(a.Envelope, SurfaceRSS)

	if a.Feed != "" {
		s.add(graph.Edge{
			From: paper, Predicate: graph.AnnouncedAs, To: graph.Category(a.Feed),
			Source: source, Surface: SurfaceRSS, Note: a.AnnounceType,
		})
	}
	for i, name := range a.Authors {
		s.add(graph.Edge{
			From: graph.Name(name), Predicate: graph.Authored, To: paper,
			Source: source, Surface: SurfaceRSS, Note: name, Position: i + 1,
		})
	}
	for i, code := range a.Categories {
		s.add(graph.Edge{
			From: paper, Predicate: graph.InCategory, To: graph.Category(code),
			Source: source, Surface: SurfaceRSS,
		})
		if i == 0 {
			s.add(graph.Edge{
				From: paper, Predicate: graph.PrimaryCategory, To: graph.Category(code),
				Source: source, Surface: SurfaceRSS,
			})
			continue
		}
		s.add(graph.Edge{
			From: paper, Predicate: graph.CrossListed, To: graph.Category(code),
			Source: source, Surface: SurfaceRSS,
		})
	}
	if a.License != "" {
		s.add(graph.Edge{
			From: paper, Predicate: graph.LicensedUnder, To: graph.License(a.License),
			Source: source, Surface: SurfaceRSS,
		})
	}
	if a.DOI != "" {
		s.add(graph.Edge{
			From: paper, Predicate: graph.HasDOI, To: graph.DOI(a.DOI),
			Source: source, Surface: SurfaceRSS, Note: "the publisher's",
		})
	}
	return s.out
}

// EdgesOfPerson is what an author identifier page claims.
//
// It is the only surface that joins a name to a person, and that join is the
// one claim on arXiv that a string match cannot make: everything else here is
// "papers whose author string normalises the same way", which may be one
// physicist or three.
func EdgesOfPerson(p Person) []graph.Edge {
	var s edgeSet
	if !p.Identified || p.ArxivID == "" {
		// A name search matched strings. The claims are on the papers it found
		// and this record has none of its own to make.
		return nil
	}
	source := sourceOf(p.Envelope, SurfaceAuthorID)
	author := graph.Author(p.ArxivID)

	s.add(graph.Edge{
		From: graph.Name(p.Name), Predicate: graph.IdentifiedAs, To: author,
		Source: source, Surface: SurfaceAuthorID, Note: p.Name,
	})
	if p.ORCID != "" {
		s.add(graph.Edge{
			From: author, Predicate: graph.HasORCID, To: graph.ORCID(p.ORCID),
			Source: source, Surface: SurfaceAuthorID,
		})
	}
	for _, paper := range p.Papers {
		s.add(graph.Edge{
			From: author, Predicate: graph.Authored, To: graph.Paper(paper.ID),
			Source: source, Surface: SurfaceAuthorID, Note: paper.Title,
		})
	}
	return s.out
}

// EdgesOfCategory is what the taxonomy claims about one category.
func EdgesOfCategory(c Category) []graph.Edge {
	var s edgeSet
	code := graph.Category(c.Code)
	taxonomy := sourceOf(c.Envelope, SurfaceTaxonomy)

	if c.Archive != "" {
		// hep-th is an archive that is also a category, so it is its own
		// parent. That looks odd until it is removed and half the physics
		// archives fall out of the tree.
		s.add(graph.Edge{
			From: code, Predicate: graph.SubcategoryOf, To: graph.Archive(c.Archive),
			Source: taxonomy, Surface: SurfaceTaxonomy, Note: c.Name,
		})
		if c.Group != "" {
			s.add(graph.Edge{
				From: graph.Archive(c.Archive), Predicate: graph.PartOfGroup, To: graph.Group(c.Group),
				Source: taxonomy, Surface: SurfaceTaxonomy, Note: c.Group,
			})
		}
	}
	if c.SetSpec != "" {
		s.add(graph.Edge{
			From: code, Predicate: graph.InSet, To: graph.Set(c.SetSpec),
			Source: sourceOf(c.Envelope, SurfaceOAI), Surface: SurfaceOAI, Note: c.SetSpec,
		})
	}
	return s.out
}

// EdgesOfTrackback is one inbound link.
//
// linked_by points inward: the external page is the subject, because a
// trackback is somebody else's page linking here. Writing it the other way
// round would say the paper cites the blog, which is backwards and would
// corrupt any merge with a real citation set.
//
// The external node is arXiv's redirect until `arxiv trackbacks --resolve` has
// followed it, because that is the only address arXiv publishes. Resolving
// costs fifteen seconds a ping and names the page itself, which is the node
// worth having, so the two are not the same node and the record says which one
// this is.
func EdgesOfTrackback(t Trackback) []graph.Edge {
	var s edgeSet
	page := t.TargetURL
	note := t.Title
	if page == "" {
		page = t.URL
		note = strings.TrimSpace(t.Title + " (arxiv's redirect, unresolved)")
	}
	s.add(graph.Edge{
		From: graph.External(page), Predicate: graph.LinkedBy, To: graph.Paper(t.PaperID),
		Source: sourceOf(t.Envelope, SurfaceTrackback), Surface: SurfaceTrackback, Note: note,
	})
	return s.out
}

// EdgesOfFullText is the bibliography as claims, with the coverage of it.
//
// cites is written from here and nowhere else. An entry resolves to an arXiv
// paper when it links to one, to a DOI when it names one, and to the page it
// links to otherwise. An entry that offers none of the three is counted and not
// written, which is what the coverage is for.
func EdgesOfFullText(f FullText) ([]graph.Edge, Coverage) {
	var s edgeSet
	paper := graph.Paper(f.PaperID)
	source := f.URL
	if source == "" {
		source = sourceOf(f.Envelope, SurfaceFullText)
	}

	for _, a := range f.Authors {
		if a.Affiliation == "" {
			continue
		}
		s.add(graph.Edge{
			From: graph.Name(a.Name), Predicate: graph.AffiliatedWith, To: graph.External(a.Affiliation),
			Source: source, Surface: SurfaceFullText, Note: a.Affiliation,
		})
	}

	cover := Coverage{Entries: len(f.References)}
	for _, r := range f.References {
		to := citedNode(r)
		if to == "" {
			continue
		}
		cover.Resolved++
		s.add(graph.Edge{
			From: paper, Predicate: graph.Cites, To: to,
			Source: source, Surface: SurfaceFullText, Note: citedNote(r),
		})
	}
	return s.out, cover
}

// citedNode is what one bibliography entry resolves to, in the order the
// identifiers are worth having.
func citedNode(r Reference) string {
	if r.ArxivID != "" {
		if id, err := axid.Parse(r.ArxivID); err == nil {
			return graph.Paper(id.Canonical)
		}
	}
	if r.DOI != "" {
		return graph.DOI(r.DOI)
	}
	// The entry as the author typed it, which on plenty of papers is where the
	// identifier is. 1706.03762 is rendered with forty entries, not one of them
	// linked, and thirty nine of them name an arXiv id in the citation string:
	// "arXiv preprint arXiv:1607.06450" and "CoRR, abs/1409.0473". Reading past
	// the link and refusing to read the text would report no citations for a
	// paper that cites thirty nine arXiv papers by number.
	if id := citedID(r.Text); id != "" {
		return graph.Paper(id)
	}
	if m := citedDOIRe.FindStringSubmatch(r.Text); m != nil {
		return graph.DOI(strings.TrimRight(m[1], ".,;)"))
	}
	for _, link := range r.Links {
		if strings.HasPrefix(link, "http") {
			return graph.External(link)
		}
	}
	return ""
}

// citedIDRe finds an arXiv id in a citation string, and only where a marker
// says it is one. A bare 1607.06450 in a page range would otherwise become a
// citation, and a wrong edge is worse than a missing one.
var citedIDRe = regexp.MustCompile(`(?i)(?:arxiv:|arxiv\.org/abs/|\babs/)([a-z-]+(?:\.[a-z]{2})?/\d{7}(?:v\d+)?|\d{4}\.\d{4,5}(?:v\d+)?)`)

// citedDOIRe finds a DOI written out in the entry.
var citedDOIRe = regexp.MustCompile(`(?i)\bdoi:\s*(10\.\d{4,9}/[^\s,;]+)`)

// citedID is the canonical arXiv id a citation string names, checked against
// the id grammar rather than trusted from the regexp.
func citedID(text string) string {
	m := citedIDRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	id, err := axid.Parse(m[1])
	if err != nil {
		return ""
	}
	return id.Canonical
}

// citedNote names the entry, so a table of citations reads as a bibliography
// rather than as a column of hashes.
func citedNote(r Reference) string {
	if r.Title != "" {
		return r.Title
	}
	if r.Text != "" {
		return r.Text
	}
	return r.Label
}

// dayOf is a date as the note on a version claim, empty when there is none.
func dayOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}
