package arxiv

import (
	"context"
	"reflect"
	"strings"

	"github.com/tamnd/any-cli/kit"
)

// FieldInfo is one field of a paper record, with where it comes from and what
// it costs to get.
//
// This is the census the whole tool is built around. A record carries a via map
// naming a surface per field and a missed list naming what was skipped, and
// both of those are only useful next to a table saying which fields exist at
// all. Somebody looking at a quick read and wondering why there is no submitter
// wants one place that says the submitter is on s2 and needs --depth full.
type FieldInfo struct {
	// Field is the json name, which is what a consumer actually sees.
	Field string `json:"field" kit:"id" table:"field"`
	// Type is the Go type as reflect prints it.
	Type  string `json:"type" table:"type"`
	Group string `json:"group" table:"group"`
	// Depth is the cheapest read that fills this field. A field only a listing,
	// a search result or a feed item carries has no depth, because depth is a
	// property of reading one paper and those are read a page at a time.
	Depth string `json:"depth,omitempty" table:"depth"`
	// Surfaces are every surface that can answer for it, cheapest first. More
	// than one means the merge picks, and via on the record says which won.
	Surfaces []string `json:"surfaces" table:"-"`
	// SurfacesText is the same joined, because a table prints a list as its
	// length.
	SurfacesText string `json:"-" table:"from"`
	Note         string `json:"note" table:"note,truncate"`
}

// fieldRows is the hand-written half of the census: the surfaces, the depth and
// the sentence. The other half, the field names and their types, is reflected
// off Paper, and a test insists the two halves name exactly the same set. That
// is what stops this table from quietly describing a model that has moved.
//
// The order matches the declaration order in model.go, so reading the two side
// by side works.
var fieldRows = []FieldInfo{
	// ─── envelope ───

	{Field: "kind", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "the record type, set by this tool and not read from anywhere"},
	{Field: "surfaces", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "the surface ids that contributed, in read order"},
	{Field: "sources", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "the URLs actually fetched, so the record can be rebuilt by hand"},
	{Field: "retrieved_at", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "when the first fetch happened, UTC"},
	{Field: "via", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "field name to surface id, for the fields more than one surface carries"},
	{Field: "missed", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "what this read did not look at, and which read would"},
	{Field: "truncated", Group: "envelope", Surfaces: []string{},
		Note: "set when a result set was cut short, with the reason"},

	// ─── identity ───

	{Field: "id", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI},
		Note: "canonical, no version and no subject class"},
	{Field: "version", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI},
		Note: "the version this record describes, parsed off the entry id"},
	{Field: "is_latest", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI},
		Note: "s1 answers with the current version unless the request pinned one"},
	{Field: "versioned_id", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI},
		Note: "the string arXiv prints, 1706.03762v7"},
	{Field: "oai_id", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI},
		Note: "computed from the id, and confirmed by the OAI header at meta"},
	{Field: "doi", Group: "identity", Depth: "quick", Surfaces: []string{},
		Note: "the arXiv DataCite DOI, computed from the id because it is a function of it"},
	{Field: "publisher_doi", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs},
		Note: "the journal's DOI, absent on a paper that was never published"},
	{Field: "url", Group: "identity", Depth: "quick", Surfaces: []string{SurfaceAPI},
		Note: "the abstract page for this version"},
	{Field: "style", Group: "identity", Depth: "quick", Surfaces: []string{},
		Note: "new or old, derived from the id, because the two page and sort differently"},

	// ─── content ───

	{Field: "title", Group: "content", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs, SurfaceList, SurfaceSearch},
		Note: "on every metadata surface, and they agree"},
	{Field: "abstract", Group: "content", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs},
		Note: "the listing rows carry it too, but truncated, so they are not used for it"},
	{Field: "comment", Group: "content", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs},
		Note: "the author's free text, where page counts and conference names live"},
	{Field: "journal_ref", Group: "content", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs},
		Note: "present only on a published paper"},
	{Field: "report_no", Group: "content", Depth: "meta", Surfaces: []string{SurfaceOAI, SurfaceAbs},
		Note: "not on s1 at all, which is one of the reasons meta exists"},
	{Field: "msc_class", Group: "content", Depth: "meta", Surfaces: []string{SurfaceOAI, SurfaceAbs},
		Note: "split on commas and semicolons, but not inside brackets"},
	{Field: "acm_class", Group: "content", Depth: "meta", Surfaces: []string{SurfaceOAI, SurfaceAbs},
		Note: "same splitting as msc_class"},

	// ─── authors ───

	{Field: "authors", Group: "authors", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs, SurfaceFullText},
		Note: "s1 gives display names, s2 splits them, s10 adds affiliations at text"},

	// ─── categories ───

	{Field: "primary_category", Group: "categories", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs},
		Note: "the first category in announcement order"},
	{Field: "categories", Group: "categories", Depth: "quick", Surfaces: []string{SurfaceAPI, SurfaceOAI, SurfaceAbs, SurfaceList, SurfaceSearch},
		Note: "primary first, which is the order every arXiv surface prints"},
	{Field: "cross_lists", Group: "categories", Depth: "quick", Surfaces: []string{},
		Note: "categories minus the primary, computed here so nobody gets the slice wrong"},
	{Field: "subject_names", Group: "categories", Depth: "full", Surfaces: []string{SurfaceAbs, SurfaceList, SurfaceSearch, SurfaceTaxonomy},
		Note: "the code to name pair, which the API never publishes"},

	// ─── time ───

	{Field: "first_submitted", Group: "time", Depth: "quick", Surfaces: []string{SurfaceAPI},
		Note: "s1's published, and never OAI's created, which is the current version's date"},
	{Field: "last_updated", Group: "time", Depth: "quick", Surfaces: []string{SurfaceAPI},
		Note: "the current version's timestamp, which moves when a paper is revised"},
	{Field: "announced_month", Group: "time", Surfaces: []string{SurfaceSearch},
		Note: "originally announced July 2026, a month and not a day, so it stays a string"},
	{Field: "announced", Group: "time", Surfaces: []string{SurfaceList, SurfaceRSS},
		Note: "the announcement day, which is not the submission day"},
	{Field: "oai_datestamp", Group: "time", Depth: "meta", Surfaces: []string{SurfaceOAI},
		Note: "a modification date at day granularity, what an incremental harvest resumes from"},

	// ─── versions ───

	{Field: "versions", Group: "versions", Depth: "full", Surfaces: []string{SurfaceOAI, SurfaceAbs},
		Note: "arXivRaw has the sizes and source types, the abstract page has the dates"},

	// ─── files and capabilities ───

	{Field: "license", Group: "files", Depth: "meta", Surfaces: []string{SurfaceOAI, SurfaceAbs},
		Note: "the licence URL; a paper from before 2003 carries the assumed one"},
	{Field: "license_name", Group: "files", Depth: "text", Surfaces: []string{SurfaceFullText},
		Note: "the licence written out, which only the rendering carries"},
	{Field: "pdf_url", Group: "files", Depth: "quick", Surfaces: []string{SurfaceAPI},
		Note: "derived from the id, and confirmed by the link block"},
	{Field: "html_url", Group: "files", Depth: "full", Surfaces: []string{SurfaceAbs, SurfaceList},
		Note: "only present when arXiv actually rendered the paper"},
	{Field: "has_html", Group: "files", Depth: "full", Surfaces: []string{SurfaceAbs, SurfaceList},
		Note: "a capability rather than a URL, and what gates the text read"},
	{Field: "has_source", Group: "files", Depth: "full", Surfaces: []string{SurfaceAbs},
		Note: "whether arXiv offers the submission source at all"},
	{Field: "submitter", Group: "files", Depth: "full", Surfaces: []string{SurfaceOAI, SurfaceAbs},
		Note: "arXivRaw names them, and the abstract page prints them beside v1"},
	{Field: "withdrawn", Group: "files", Depth: "meta", Surfaces: []string{SurfaceOAI},
		Note: "from an OAI header with status deleted; a withdrawn paper still has a history"},

	// ─── search and listing only ───

	{Field: "hits", Group: "search", Surfaces: []string{SurfaceSearch},
		Note: "the terms arXiv highlighted in this result, which the paper itself does not know"},
	{Field: "extra", Group: "search", Surfaces: []string{SurfaceList},
		Note: "a label the listing published that this model has no field for, kept rather than dropped"},

	// ─── full text ───

	{Field: "sections", Group: "full text", Depth: "text", Surfaces: []string{SurfaceFullText},
		Note: "the section tree and the prose, which is a megabyte rather than a kilobyte"},

	// ─── read metadata ───

	{Field: "depth", Group: "envelope", Depth: "quick", Surfaces: []string{},
		Note: "how deeply this record was read, so a consumer can tell absent from missing"},
}

// fieldInfos fills in the types from the model and joins the surface list.
//
// The types are reflected rather than written down because a field that changed
// from a string to a slice is exactly the kind of thing a hand-kept table gets
// wrong, and it is also the kind of thing that breaks a consumer.
func fieldInfos() []FieldInfo {
	types := paperFieldTypes()
	out := make([]FieldInfo, 0, len(fieldRows))
	for _, r := range fieldRows {
		r.Type = types[r.Field]
		// An empty surface list is derived here rather than read from anywhere,
		// which is a different thing from "we did not look".
		r.SurfacesText = "computed"
		if len(r.Surfaces) > 0 {
			r.SurfacesText = strings.Join(r.Surfaces, ", ")
		}
		out = append(out, r)
	}
	return out
}

// paperFieldTypes reflects the json name and Go type of every field on a paper,
// walking the embedded envelope as though it were declared inline, because that
// is how it appears in the json.
//
// A field tagged json:"-" is skipped. AuthorLine is the only one, and it exists
// for the table renderer rather than for a consumer.
func paperFieldTypes() map[string]string {
	out := map[string]string{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type)
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			out[name] = f.Type.String()
		}
	}
	walk(reflect.TypeFor[Paper]())
	return out
}

type fieldsIn struct {
	Group string `kit:"flag" help:"show one group: envelope, identity, content, authors, categories, time, versions, files, search or full text"`
	Depth string `kit:"flag" enum:"quick,meta,full,text" help:"show only the fields a read at this depth fills"`
}

func registerFields(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "fields",
		Group:   "explain",
		List:    true,
		URIType: "field",
		Summary: "Show every field on a paper, where it comes from and what it costs",
		Long: `Show the field census: every field a paper record can carry, the surfaces that
answer for it, and the cheapest depth that fills it.

This is the table the via map on a record points into. A record says a field
came from s2 and this says s2 is OAI-PMH, that the abstract page carries it too,
and that a quick read will not have it at all.

--depth filters to what a read at that depth fills, which is the honest way to
answer "will this be there if I do not pay for the html plane". The fields with
no depth are the ones only a search result, a listing row or a feed item
carries, because those are read a page at a time rather than a paper at a time.

The names and types are reflected off the model, so a field renamed in the code
is renamed here. No network call.`,
	}, func(_ context.Context, in fieldsIn, emit func(*FieldInfo) error) error {
		rows := fieldInfos()
		if group := strings.ToLower(strings.TrimSpace(in.Group)); group != "" {
			rows = keepFields(rows, func(r FieldInfo) bool { return r.Group == group })
		}
		if in.Depth != "" {
			want, err := ParseDepth(in.Depth)
			if err != nil {
				return err
			}
			rows = keepFields(rows, func(r FieldInfo) bool {
				return r.Depth != "" && want.AtLeast(Depth(r.Depth))
			})
		}
		return emitAll(rows, emit)
	})
}

func keepFields(rows []FieldInfo, ok func(FieldInfo) bool) []FieldInfo {
	var out []FieldInfo
	for _, r := range rows {
		if ok(r) {
			out = append(out, r)
		}
	}
	return out
}
