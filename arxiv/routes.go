package arxiv

import (
	"context"
	"net/url"
	"strings"

	"github.com/tamnd/any-cli/kit"
)

// RouteInfo is one URL shape this tool builds.
//
// The surface table says what the twelve places are. This says what is actually
// requested, which is not the same thing: s12 is one surface and two routes,
// one of which robots.txt allows and one of which it does not, and s2 is one
// route asked three ways.
//
// It exists so that "what will this command fetch" has an answer that does not
// involve reading the source. That question comes up before a crawl, when
// somebody is deciding whether to let this run against arXiv at all.
type RouteInfo struct {
	// Route is the URL with the varying part in braces, which is the form
	// somebody can compare against a proxy log.
	Route   string `json:"route" kit:"id" table:"route"`
	Method  string `json:"method" table:"method"`
	Surface string `json:"surface" table:"surface"`
	Plane   string `json:"plane" table:"plane"`
	// Allowed is what arxiv.org's robots.txt says about the path. Routes on the
	// API hosts are not covered by it and say so.
	Allowed string `json:"allowed" table:"robots"`
	// Asked is set on a route the tool will not follow by itself, and says what
	// has to happen before a request is made.
	Asked string `json:"asked,omitempty" table:"-"`
	Why   string `json:"why" table:"why,truncate"`
}

// routeRows is every request shape in the tool, in surface order.
//
// Building the strings from the base constants is what keeps this honest. A
// route written out by hand would go on describing the old URL after the
// constant moved, and this table's whole purpose is to be trusted without
// checking.
var routeRows = []RouteInfo{
	{Route: apiBase + "?search_query={query}&start={n}&max_results={n}", Surface: SurfaceAPI,
		Why: "every search, count and slice; start and max_results page the 10,000 result window"},
	{Route: apiBase + "?id_list={ids}", Surface: SurfaceAPI,
		Why: "up to 200 ids in one request, which is what makes hydrating a known list cheap"},
	{Route: oaiBase + "?verb=GetRecord&identifier={oai id}&metadataPrefix={arXiv|arXivRaw|oai_dc}", Surface: SurfaceOAI,
		Why: "one record in one of three formats; arXivRaw carries the version table and the submitter"},
	{Route: oaiBase + "?verb=ListSets", Surface: SurfaceOAI,
		Why: "the set list, which is arXiv's own grouping of the archives"},
	{Route: absBase + "{id}", Surface: SurfaceAbs,
		Why: "the abstract page, for category names, the file list and whether a rendering exists"},
	{Route: listBase + "{category}/{yyyy-mm}?skip={n}&show={n}", Surface: SurfaceList,
		Why: "a month of a category in announcement order"},
	{Route: s5Base + "?searchtype={field}&terms-0-field={field}&terms-0-term={value}", Surface: SurfaceSearch,
		Asked: "one of the seven flags the export API has no prefix for",
		Why:   "the only way to search by msc-class, acm-class, doi, orcid, licence, author id or full text"},
	{Route: rssBase + "{category}", Surface: SurfaceRSS,
		Why: "today's announcements, and the only surface carrying the announce type"},
	{Route: taxonomyURL, Surface: SurfaceTaxonomy,
		Why: "the whole category tree in one page, cached hard because it moves about once a year"},
	{Route: authorBase + "{author id}", Surface: SurfaceAuthorID,
		Why: "an author's own page, which is opt in and carries the ORCID when there is one"},
	{Route: bibtexBase + "{id}", Surface: SurfaceBibTeX,
		Why: "arXiv's own BibTeX entry, passed through unchanged so it can be compared against ours"},
	{Route: htmlBase + "{id}v{n}", Surface: SurfaceFullText,
		Why: "the LaTeXML rendering, for affiliations, the section tree and the bibliography"},
	{Route: trackbackBase + "{id}", Surface: SurfaceTrackback,
		Asked: "arxiv trackbacks, or a crawl run with --trackbacks",
		Why:   "who linked to this paper from outside arXiv"},
	{Route: trackbackRecent + "?views={n}", Surface: SurfaceTrackback,
		Asked: "arxiv trackbacks --recent",
		Why:   "the most recent trackbacks across all of arXiv"},
	{Route: pdfBase + "{id}v{n}", Surface: SurfaceFiles,
		Why: "the PDF, by HEAD for its real size and by GET to download it"},
	{Route: srcBase + "{id}v{n}", Surface: SurfaceFiles,
		Asked: "arxiv download --kind source, one paper at a time",
		Why:   "the submission source, which is a tarball or a single file depending on the paper"},
}

// routeInfos fills in the columns that follow from the route itself.
func routeInfos() []RouteInfo {
	out := make([]RouteInfo, 0, len(routeRows))
	for _, r := range routeRows {
		r.Method = "GET"
		if strings.HasPrefix(r.Route, pdfBase) || strings.HasPrefix(r.Route, srcBase) {
			// The files command asks for the size and nothing else, so it takes
			// the headers and hangs up. The download command takes the body.
			r.Method = "HEAD, GET"
		}
		r.Plane = planeOfBase(r.Route)
		r.Allowed = robotsVerdict(r.Route)
		out = append(out, r)
	}
	return out
}

// robotsVerdict says what arxiv.org's robots.txt has to say about a route.
//
// The API hosts serve their own and it says nothing about them, so saying
// "allowed" there would be a claim about a file this tool has not read. The
// three routes on disallowed paths are read only when asked for, and the value
// says so rather than pretending the file permits them.
func robotsVerdict(route string) string {
	u, err := url.Parse(route)
	if err != nil || u.Host != Host {
		return "not covered"
	}
	switch {
	case strings.HasPrefix(u.Path, "/search"),
		strings.HasPrefix(u.Path, "/tb"),
		strings.HasPrefix(u.Path, "/src"):
		return "disallowed"
	}
	return "allowed"
}

type routesIn struct{}

func registerRoutes(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "routes",
		Group:   "explain",
		List:    true,
		URIType: "route",
		Summary: "Show every URL this tool will ever request",
		Long: `Show every URL shape this tool builds, with the surface it belongs to, the pace
it is read at, and what arxiv.org's robots.txt says about it.

This is the answer to "what will this fetch", which is a fair question to ask
before letting a tool loose on a public service. The routes are built from the
same constants the reads use, so a base URL that moved moves here too.

Three routes sit on paths robots.txt disallows. The tool does not follow any of
them on its own. Each is requested only when a command names it, and the asked
field says which one. The API hosts serve their own robots.txt and arxiv.org's
says nothing about them, so those rows read "not covered" rather than claiming
a permission that was never checked.

No network call.`,
	}, func(_ context.Context, _ routesIn, emit func(*RouteInfo) error) error {
		return emitAll(routeInfos(), emit)
	})
}
