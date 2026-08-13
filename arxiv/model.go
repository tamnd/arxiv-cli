package arxiv

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The surface ids from spec 3006 doc 01. They are the vocabulary of the via
// map, so a consumer that wants to know where a field came from gets an id it
// can look up rather than a sentence it has to parse.
const (
	SurfaceAPI = "s1" // export API Atom
	SurfaceOAI = "s2" // OAI-PMH
	SurfaceAbs = "s3" // the abstract page
	// SurfaceList is the monthly category listing, which is arXiv's own
	// announcement order and the only surface that publishes a per month total.
	SurfaceList = "s4" // the category listing
	// SurfaceSearch is the search UI, which is the only surface that answers
	// for the seven fields in doc 02 section 2.3.
	SurfaceSearch = "s5" // the search UI
	// SurfaceRSS is the announcement feed, and the only surface anywhere that
	// says whether an item is a new paper, a cross list or a replacement.
	SurfaceRSS = "s6" // the announcement feed
	// SurfaceTaxonomy is the category taxonomy page. It is declared here with
	// the rest of the vocabulary and used in taxonomy.go.
	SurfaceTaxonomy = "s7" // the category taxonomy
	// SurfaceAuthorID is the author identifier page, which is the only surface
	// that carries an ORCID and the only one where arXiv says a named person
	// owns a set of papers.
	SurfaceAuthorID = "s8" // the author identifier page
	// SurfaceBibTeX is arXiv's own BibTeX entry. Nothing on it is new, which is
	// why no record carries it in a via map, but a read of it is still a
	// request and the read log names it like any other.
	SurfaceBibTeX = "s9" // the BibTeX entry
	// SurfaceFullText is the LaTeXML rendering, which is the only surface that
	// carries an affiliation, a section tree or the body of a paper at all.
	SurfaceFullText = "s10" // the LaTeXML full text
	// SurfaceTrackback is the trackback page, and the only surface anywhere
	// that points inward: an external page linking to a paper.
	SurfaceTrackback = "s11" // trackbacks
	// SurfaceFiles is the bytes themselves, the PDF and the submission source.
	// It is the only surface with no metadata on it, and the only one this tool
	// writes to disk.
	SurfaceFiles = "s12" // the files
)

// SurfaceNames is what each id is, for `arxiv planes` and for help text.
var SurfaceNames = map[string]string{
	SurfaceAPI:       "the export API",
	SurfaceOAI:       "OAI-PMH",
	SurfaceAbs:       "the abstract page",
	SurfaceList:      "the category listing",
	SurfaceSearch:    "the search UI",
	SurfaceRSS:       "the announcement feed",
	SurfaceTaxonomy:  "the category taxonomy",
	SurfaceAuthorID:  "the author identifier page",
	SurfaceBibTeX:    "the BibTeX entry",
	SurfaceFullText:  "the LaTeXML full text",
	SurfaceTrackback: "the trackback page",
	SurfaceFiles:     "the files",
}

// Envelope is what every record carries about its own provenance.
//
// It exists because the two hardest questions to answer about a scraped record
// are "where did this field come from" and "what did you not look at". The old
// tool answered neither: it returned nine fields with no way to tell that it
// had never asked about the other twenty.
type Envelope struct {
	// Kind is the record type.
	Kind string `json:"kind" table:"-"`
	// Surfaces are the surface ids that contributed, in read order.
	Surfaces []string `json:"surfaces" table:"-"`
	// Sources are the URLs actually fetched, so a record can be reproduced by
	// hand.
	Sources []string `json:"sources" table:"-"`
	// RetrievedAt is when the first fetch happened, UTC.
	RetrievedAt time.Time `json:"retrieved_at" table:"-"`
	// Via maps a field name to the surface id that answered for it, for the
	// fields more than one surface carries.
	Via map[string]string `json:"via,omitempty" table:"-"`
	// Missed names, in sentences, what this read did not look at and which
	// read would.
	Missed []string `json:"missed,omitempty" table:"-"`
	// Truncated is set when a result set was cut short, with the reason.
	Truncated string `json:"truncated,omitempty" table:"-"`
}

// addSurface records a surface and the URL it was read from, once each.
func (e *Envelope) addSurface(id, source string) {
	if !contains(e.Surfaces, id) {
		e.Surfaces = append(e.Surfaces, id)
	}
	if source != "" && !contains(e.Sources, source) {
		e.Sources = append(e.Sources, source)
	}
}

// setVia records which surface answered for a field.
//
// Every caller sets the value and the attribution together, and a merge only
// writes a field it is filling, so the last call is the surface whose value is
// standing in the record.
func (e *Envelope) setVia(field, surface string) {
	if e.Via == nil {
		e.Via = map[string]string{}
	}
	e.Via[field] = surface
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Paper is the central record. Which fields are populated depends on the
// depth, and Missed says which ones were not looked at.
//
// A field that was not read is absent, not zero. That is why the version list
// is omitzero rather than omitempty-on-an-always-present-slice: an empty
// versions array on a paper with seven of them would be a lie, and a consumer
// has no way to tell a lie from a fact.
type Paper struct {
	Envelope

	// ─── identity ───

	// ID is canonical: no version, no subject class.
	ID string `json:"id" kit:"id" table:"id"`
	// Version is the version this record describes, and it is never dropped.
	// The old tool stripped v7 off the id and stored nothing, so a record for
	// v7 was indistinguishable from a record for v1.
	Version int `json:"version,omitzero" table:"-"`
	// IsLatest says whether Version is the current one.
	IsLatest bool `json:"is_latest" table:"-"`
	// VersionedID is the string arXiv prints, "1706.03762v7".
	VersionedID string `json:"versioned_id,omitempty" table:"-"`
	// OAIID is the OAI-PMH identifier.
	OAIID string `json:"oai_id,omitempty" table:"-"`
	// DOI is the arXiv-issued DataCite DOI, computed from the id rather than
	// scraped, because it is a function of the id.
	DOI string `json:"doi,omitempty" table:"-"`
	// PublisherDOI is the journal's DOI, absent on an unpublished paper.
	PublisherDOI string `json:"publisher_doi,omitempty" table:"-"`
	// URL is the abstract page for this version.
	URL string `json:"url,omitempty" table:"-,url"`
	// Style is "new" or "old". The two sort differently and page differently,
	// and code that assumes one shape breaks silently on the other.
	Style string `json:"style,omitempty" table:"-"`

	// ─── content ───

	Title    string `json:"title" table:"title,truncate"`
	Abstract string `json:"abstract,omitempty" table:"-"`
	// Comment is the author's free text, the field the old tool parsed off the
	// wire and threw away.
	Comment    string   `json:"comment,omitempty" table:"-"`
	JournalRef string   `json:"journal_ref,omitempty" table:"-"`
	ReportNo   string   `json:"report_no,omitempty" table:"-"`
	MSCClass   []string `json:"msc_class,omitempty" table:"-"`
	ACMClass   []string `json:"acm_class,omitempty" table:"-"`

	// ─── authors ───

	Authors []Author `json:"authors,omitempty" table:"-"`
	// AuthorLine is the display names joined, for the table alone. The
	// renderer prints a list as its length and "8" is not what anyone came to
	// read.
	AuthorLine string `json:"-" table:"authors,truncate"`

	// ─── categories ───

	PrimaryCategory string `json:"primary_category,omitempty" table:"primary"`
	// Categories is every category in announcement order, primary first.
	Categories []string `json:"categories,omitempty" table:"-"`
	// CrossLists is Categories minus the primary. "Which categories is this
	// cross-listed into" is a question people actually ask, and computing it at
	// the call site invites off-by-one bugs.
	CrossLists []string `json:"cross_lists,omitempty" table:"-"`
	// SubjectNames maps a category code to the name arXiv prints beside it, as
	// in cs.CL to "Computation and Language". Only the abstract page publishes
	// the pair, so this is empty below --depth full.
	SubjectNames map[string]string `json:"subject_names,omitempty" table:"-"`

	// ─── time ───

	// FirstSubmitted is the v1 submission time and has exactly one
	// authoritative source: s1's published element. It is never filled from
	// OAI's created, which is the current version's date.
	FirstSubmitted time.Time `json:"first_submitted,omitzero" table:"submitted,time"`
	// LastUpdated is the current version's timestamp.
	LastUpdated time.Time `json:"last_updated,omitzero" table:"-"`
	// AnnouncedMonth is the search UI's "originally announced July 2026",
	// which is a month and not a day. It is a string rather than a time
	// because writing it into Announced would mean inventing a day of the
	// month, and 2026-07-01 is a specific false claim where "July 2026" is a
	// vague true one.
	AnnouncedMonth string `json:"announced_month,omitempty" table:"-"`
	// Announced is the announcement date, which is not the submission date: a
	// paper submitted at 22:00 on a Friday is announced the following Monday.
	Announced time.Time `json:"announced,omitzero" table:"-"`
	// OAIDatestamp is a modification date at day granularity, kept because it
	// is what an incremental harvest resumes from.
	OAIDatestamp time.Time `json:"oai_datestamp,omitzero" table:"-"`

	// ─── versions ───

	Versions []Version `json:"versions,omitempty" table:"-"`

	// ─── files, licence and capabilities ───

	License     string `json:"license,omitempty" table:"-"`
	LicenseName string `json:"license_name,omitempty" table:"-"`
	PDFURL      string `json:"pdf_url,omitempty" table:"-"`
	HTMLURL     string `json:"html_url,omitempty" table:"-"`
	// HasHTML and HasSource are capabilities, not URLs, because the URLs are
	// derivable and a URL that might 404 is worse than a boolean that says
	// whether it will. HasHTML is what gates the full text read.
	HasHTML   bool   `json:"has_html" table:"-"`
	HasSource bool   `json:"has_source" table:"-"`
	Submitter string `json:"submitter,omitempty" table:"-"`
	// Withdrawn comes from an OAI header with status="deleted". A withdrawn
	// paper still has an id, a title and a history, and it must not silently
	// vanish from a result set.
	Withdrawn bool `json:"withdrawn" table:"-"`

	// Hits are the query terms arXiv highlighted in this result, which only a
	// search result has and only the search UI publishes. It is the one thing
	// a result knows that the paper itself does not.
	Hits []string `json:"hits,omitempty" table:"-"`

	// Extra holds a labelled value a surface published that this model has no
	// field for, keyed by the label as arXiv wrote it. The listing rows carry a
	// variable set of labels and arXiv adds one from time to time, so an
	// unrecognised label is kept here rather than dropped on the floor.
	Extra map[string]string `json:"extra,omitempty" table:"-"`

	// ─── full text ───

	// Sections is the section tree of the LaTeXML rendering, present at
	// --depth text and only for a paper arXiv rendered. The prose is in it: a
	// paper read this deep is a megabyte rather than a kilobyte, which is why
	// nothing shallower goes near it.
	Sections []Section `json:"sections,omitempty" table:"-"`

	// Depth is how deeply this record was read.
	Depth string `json:"depth" table:"-"`
}

// Author is one author on a paper.
//
// Three surfaces publish names in three formats and this keeps all of them:
// s1 gives a display string, s2 gives keyname and forenames separately, and s3
// gives surname-comma-given.
type Author struct {
	// Name is the display form, "Aidan N. Gomez".
	Name string `json:"name"`
	// Keyname and Forenames are only set when a surface gave them apart.
	//
	// They are never guessed by splitting a display string on the last space.
	// That split is wrong for "van der Waals", for "The ATLAS Collaboration",
	// and for every name written surname first, and a guess that is wrong for
	// millions of records is worse than an absent field.
	Keyname     string `json:"keyname,omitempty"`
	Forenames   string `json:"forenames,omitempty"`
	Affiliation string `json:"affiliation,omitempty"`
	ORCID       string `json:"orcid,omitempty"`
	ArxivID     string `json:"arxiv_id,omitempty"`
	// Via is the surface that produced this entry, because a deep read merges
	// two author lists and the merge has to be auditable.
	Via string `json:"via,omitempty"`
}

// Version is one entry in a paper's submission history.
type Version struct {
	Version int       `json:"version"`
	Date    time.Time `json:"date"`
	// SizeBytes is normalised from two units: arXivRaw says "1102kb" and the
	// abstract page says "1,102 KB", and both mean kilobytes. The two differ by
	// one kilobyte on some versions, which is rounding, and Via says which one
	// answered so nobody mistakes that for a disagreement about the facts.
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// SourceType is arXiv's raw letter and SourceKind is the interpretation.
	// An unrecognised letter leaves SourceKind absent rather than guessing.
	SourceType string `json:"source_type,omitempty"`
	SourceKind string `json:"source_kind,omitempty"`
	Via        string `json:"via,omitempty"`
}

// sourceKinds is what the letters mean. D and I are the two seen in the wild as
// of the date in Measured, and the map is the only place a letter is
// interpreted so an unknown one is preserved instead of folded into a guess.
var sourceKinds = map[string]string{
	"D": "pdf-only",
	"I": "tex",
}

// Ref is every reference from one record to another, so a consumer never has to
// guess whether a bare string is an id or a URL.
type Ref struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// ─── depth ───

// Depth is how many surfaces a read crosses.
//
// It is a knob on cost as much as on completeness: quick is one request on the
// fast plane, full crosses onto arxiv.org and pays fifteen seconds per paper.
type Depth string

const (
	// DepthQuick is s1 alone: one request.
	DepthQuick Depth = "quick"
	// DepthMeta adds the OAI arXiv format: two requests, both on the API plane.
	DepthMeta Depth = "meta"
	// DepthFull adds arXivRaw and the abstract page: four requests, and the
	// last one is on the fifteen second plane.
	DepthFull Depth = "full"
	// DepthText adds the LaTeXML rendering: five requests.
	DepthText Depth = "text"
)

// Depths is the set, cheapest first.
var Depths = []Depth{DepthQuick, DepthMeta, DepthFull, DepthText}

// ParseDepth resolves a depth name.
func ParseDepth(s string) (Depth, error) {
	d := Depth(strings.ToLower(strings.TrimSpace(s)))
	if d == "" {
		return DepthMeta, nil
	}
	for _, want := range Depths {
		if d == want {
			return d, nil
		}
	}
	return "", fmt.Errorf("depth %q is not one of %s", s, joinDepths())
}

func joinDepths() string {
	names := make([]string, len(Depths))
	for i, d := range Depths {
		names[i] = string(d)
	}
	return strings.Join(names, ", ")
}

// rank orders the depths so a comparison reads as "at least this deep".
func (d Depth) rank() int {
	for i, want := range Depths {
		if d == want {
			return i
		}
	}
	return 0
}

// AtLeast reports whether d reads everything other does.
func (d Depth) AtLeast(other Depth) bool { return d.rank() >= other.rank() }

// Requests is how many requests one paper costs at this depth.
func (d Depth) Requests() int {
	switch d {
	case DepthQuick:
		return 1
	case DepthMeta:
		return 2
	case DepthFull:
		return 4
	case DepthText:
		return 5
	}
	return 1
}

// CrossesHTMLPlane reports whether this depth touches arxiv.org, which is the
// difference between three seconds per paper and fifteen.
func (d Depth) CrossesHTMLPlane() bool { return d.AtLeast(DepthFull) }

// Cost estimates the wall clock for n papers at this depth.
//
// It is the pace times the number of requests on each plane, which is what the
// limiter will actually make the caller wait. It is an estimate rather than a
// promise: a cache hit costs nothing and a retry costs more.
func (d Depth) Cost(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	api, html := d.PlaneRequests()
	return time.Duration(n) * (time.Duration(api)*APIPlane.Pace + time.Duration(html)*HTMLPlane.Pace)
}

// PlaneRequests splits one paper's cost across the two planes.
//
// A crawl budgets the planes separately, so it needs the split rather than the
// total: three API requests and two HTML ones are five requests and about four
// fifths of a minute, and the same five all on the API plane are fifteen
// seconds.
func (d Depth) PlaneRequests() (api, html int) {
	switch d {
	case DepthQuick:
		return 1, 0
	case DepthMeta:
		return 2, 0
	case DepthFull:
		return 3, 1
	case DepthText:
		return 3, 2
	}
	return 1, 0
}

// Missed is the sentences naming what this depth did not look at.
//
// They are sentences and not codes because the reader is a person deciding
// whether to pay for a deeper read, and "--depth full reads them" is the answer
// to the question they are actually asking.
func (d Depth) Missed(id string) []string {
	deeper := func(fields, depth string) string {
		return fields + " were not read; arxiv paper " + id + " --depth " + depth + " reads them"
	}
	var out []string
	if !d.AtLeast(DepthMeta) {
		out = append(out, deeper("report number, MSC and ACM class, the licence and structured author names", "meta"))
	}
	if !d.AtLeast(DepthFull) {
		out = append(out, deeper("the submitter, the version history and the html and source capabilities", "full"))
	}
	if !d.AtLeast(DepthText) {
		out = append(out, deeper("affiliations, the licence name and the section tree", "text"))
	}
	return out
}

// ─── small shared helpers ───

// cleanText unwraps a field stored with arXiv's own line wrapping.
//
// Newlines and tabs become spaces and runs of spaces collapse. LaTeX is left
// exactly as submitted: \textbf{attention} is what the author wrote and
// normalising it would be a lossy guess.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}

// splitClasses splits an MSC or ACM class field.
//
// Submitters use both separators and arXiv stores what they typed, so both are
// honoured: 1801.00001 carries "37-40, 51N20, 51M04, 51-04" and other records
// carry "I.2.7; I.2.6". A part like "Primary 60G51" keeps its qualifier,
// because dropping it would lose the submitter's own ranking.
//
// A separator inside brackets is not a separator. 2606.27343 carries
// "18D10 (16T05, 16T15, 18D10)", where the bracket holds the secondary classes
// for the primary one in front of it, and splitting through it turns one class
// into three that are each missing half of themselves.
func splitClasses(s string) []string {
	s = cleanText(s)
	if s == "" {
		return nil
	}
	var out []string
	depth, start := 0, 0
	flush := func(end int) {
		if p := strings.TrimSpace(s[start:end]); p != "" {
			out = append(out, p)
		}
	}
	for i, r := range s {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ';', ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(s))
	return out
}

// crossLists is Categories minus the primary, in the order they were announced.
func crossLists(cats []string, primary string) []string {
	var out []string
	for _, c := range cats {
		if c != primary {
			out = append(out, c)
		}
	}
	return out
}

// authorLine renders the display names for the table, the way a citation would:
// the first three and a count, because a fifty author collaboration paper is
// not worth a fifty name cell.
func authorLine(authors []Author) string {
	names := make([]string, 0, len(authors))
	for _, a := range authors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= 3:
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:3], ", ") + ", and " + strconv.Itoa(len(names)-3) + " more"
}

// sortVersions puts a version history in ascending order, which is the order it
// happened in and the order the last element is the latest in.
func sortVersions(vs []Version) {
	sort.Slice(vs, func(i, j int) bool { return vs[i].Version < vs[j].Version })
}
