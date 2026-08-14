package arxiv

import (
	"context"
	"net/url"
	"strings"

	"github.com/tamnd/any-cli/kit"
)

// SurfaceInfo is one row of the surface table, printed rather than fetched.
//
// The table exists because "twelve surfaces" is the first thing anyone is told
// about this tool and there was nowhere to go and see them. An agent deciding
// whether a field is reachable needs to know that affiliations are only on the
// LaTeXML rendering and that the rendering does not exist for a 1997 paper, and
// neither of those is in help text for a command.
type SurfaceInfo struct {
	ID   string `json:"id" kit:"id" table:"id"`
	Name string `json:"name" table:"surface"`
	// Plane is which pace this surface is read at, which follows from its host
	// and never from the caller.
	Plane string `json:"plane" table:"plane"`
	Base  string `json:"base" table:"-"`
	// Only is what this surface is the sole source of. Empty means everything
	// on it is also somewhere else, which is worth knowing before paying for a
	// request.
	Only string `json:"only,omitempty" table:"only,truncate"`
	// Gated is set on a surface robots.txt disallows. The tool reads these when
	// somebody asks for one by name and never on its own, and the value says
	// what has to happen first.
	Gated string `json:"gated,omitempty" table:"-"`
	// Reads are the commands that touch it, so the table answers "what will
	// this cost me" in the direction people actually ask it.
	Reads []string `json:"reads" table:"-"`
	// ReadsText is the same list joined, because a list renders as its length.
	ReadsText string `json:"-" table:"read_by"`
}

// SurfaceURI names a surface, so a store can hold the table alongside the
// claims that cite it.
func SurfaceURI(id string) string { return "ax://surface/" + id }

// surfaceRows is the table. It is written here rather than derived, because
// what a surface is uniquely good for is a judgement about arXiv and not a fact
// about this code, and a generated version would say twelve true things nobody
// needed.
//
// The ids and names come from the constants, so a surface renamed in model.go
// is renamed here.
var surfaceRows = []SurfaceInfo{
	{ID: SurfaceAPI, Base: apiBase,
		Only:  "the only surface that searches, and the only one that answers a result count",
		Reads: []string{"search", "count", "paper", "id", "cite", "edges", "graph"}},
	{ID: SurfaceOAI, Base: oaiBase,
		Only:  "the version history with sizes and source types, the submitter, the report number and the licence",
		Reads: []string{"paper --depth meta", "sets", "edges", "crawl"}},
	{ID: SurfaceAbs, Base: absBase,
		Only:  "the category names in full, the file list arXiv offers, and whether a rendering exists",
		Reads: []string{"paper --depth full", "files", "archive"}},
	{ID: SurfaceList, Base: listBase,
		Only:  "arXiv's own idea of a month, which is announcement order rather than submission order",
		Reads: []string{"list", "crawl"}},
	{ID: SurfaceSearch, Base: s5Base,
		Only:  "the seven fields the export API has no prefix for: acm-class, msc-class, doi, orcid, licence, author id and full text",
		Gated: "robots.txt disallows /search, so this is only read when one of those seven flags is passed",
		Reads: []string{"search --msc-class and the other six"}},
	{ID: SurfaceRSS, Base: rssBase,
		Only:  "the announce type, which says whether an item is new, replaced or cross listed",
		Reads: []string{"new"}},
	{ID: SurfaceTaxonomy, Base: taxonomyURL,
		Only:  "the group and archive a category sits under, and the description arXiv writes for it",
		Reads: []string{"categories", "category", "graph"}},
	{ID: SurfaceAuthorID, Base: authorBase,
		Only:  "an ORCID against an arXiv author id, which is the only identity claim arXiv makes",
		Reads: []string{"author --id"}},
	{ID: SurfaceBibTeX, Base: bibtexBase,
		Only:  "nothing, and that is the point: it is arXiv's own entry, so a citation can be compared against it",
		Reads: []string{"bibtex"}},
	{ID: SurfaceFullText, Base: htmlBase,
		Only:  "affiliations, the section tree and the bibliography, which is where cites comes from",
		Reads: []string{"paper --depth text", "fulltext", "edges --cites"}},
	{ID: SurfaceTrackback, Base: trackbackBase,
		Only:  "who linked to a paper from outside arXiv",
		Gated: "robots.txt disallows /tb, so this is only read when trackbacks is asked for by name",
		Reads: []string{"trackbacks", "crawl --trackbacks"}},
	{ID: SurfaceFiles, Base: pdfBase,
		Only:  "the bytes, and the real size, which the metadata rounds to the nearest kilobyte",
		Gated: "the source route /src is disallowed; the PDF route is not",
		Reads: []string{"files", "download"}},
}

// surfaceInfos fills in the derived columns.
func surfaceInfos() []SurfaceInfo {
	out := make([]SurfaceInfo, 0, len(surfaceRows))
	for _, r := range surfaceRows {
		r.Name = SurfaceNames[r.ID]
		r.Plane = planeOfBase(r.Base)
		r.ReadsText = strings.Join(r.Reads, ", ")
		out = append(out, r)
	}
	return out
}

// planeOfBase resolves a base URL to its plane name. A base that resolves to
// nothing would be a request with no pace in front of it, which the invariant
// tests refuse, so an empty answer here means the table has drifted rather than
// that the plane is optional.
func planeOfBase(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	p, ok := PlaneFor(u.Host)
	if !ok {
		return ""
	}
	return p.Name
}

type surfacesIn struct{}

func registerSurfaces(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "surfaces",
		Group:   "explain",
		List:    true,
		URIType: "surface",
		Summary: "Show the twelve places this tool reads and what each is for",
		Long: `Show the twelve surfaces this tool reads, what each one is uniquely good for,
and which pace it is read at.

Every record carries a surfaces list and a via map naming one of these ids per
field, so this is the table that turns "via: s10" into "the LaTeXML rendering,
which is the only place affiliations are published".

Three of the routes are disallowed by arxiv.org's robots.txt. The tool never
follows those on its own; it reads them when a command names one, which is a
browser request made from a command line rather than a crawl. The gated field
says what has to happen first.

No network call.`,
	}, func(_ context.Context, _ surfacesIn, emit func(*SurfaceInfo) error) error {
		return emitAll(surfaceInfos(), emit)
	})
}
