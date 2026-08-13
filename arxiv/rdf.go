package arxiv

import (
	"context"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/graph"
	"github.com/tamnd/arxiv-cli/pkg/rdf"
)

// The bridge between a read and pkg/rdf.
//
// pkg/rdf knows about claims and about nothing in this package, which is what
// keeps the mapping testable on its own. This file is the other half: a record
// has fields that are values rather than edges, a title and an abstract and a
// date, and those have to be walked over here where the record is.
//
// Every literal carries the URL that answered for it, which the envelope
// already knows. That is the whole reason a record keeps a via map: without it
// the provenance on a merged record would be "one of these four pages", which
// is not provenance.

// RDFOptions is what a document costs to build.
type RDFOptions struct {
	// Depth is how deeply each paper is read, which decides how much there is
	// to write. It is the same knob `arxiv edges` takes and it costs the same.
	Depth Depth
	// Trackbacks adds the inbound links, one request on the fifteen second
	// plane.
	Trackbacks bool
	// Predicates keeps only these claims, empty meaning all of them. The
	// literals are not filtered by it, because a title is not a predicate
	// anybody can name here.
	Predicates []string
}

// RDF reads one reference and returns everything it says as statements.
func (c *Client) RDF(ctx context.Context, ref string, o RDFOptions) (*rdf.Doc, error) {
	p, edges, err := c.edgesWithPaper(ctx, ref, EdgeOptions{
		Depth:      o.Depth,
		Trackbacks: o.Trackbacks,
		Predicates: o.Predicates,
	})
	if err != nil {
		return nil, err
	}
	d := rdf.New()
	AddPaper(d, p)
	AddClaims(d, edges)
	return d, nil
}

// AddClaims writes every claim into a document.
func AddClaims(d *rdf.Doc, edges []graph.Edge) {
	for _, e := range edges {
		d.AddEdge(e)
	}
}

// AddPaper writes the fields of a paper record that are values rather than
// edges. The edges come from EdgesOfPaper and are added separately, so a
// document built from a store and one built from a live read say the same
// thing.
func AddPaper(d *rdf.Doc, p Paper) {
	if p.ID == "" {
		return
	}
	subject := rdf.NodeIRI(graph.Paper(p.ID))
	d.Type(subject, rdf.Classes(graph.KindPaper)...)

	// text writes one string field under every term the table gives it, with
	// the URL of whichever surface answered for it.
	text := func(field, value, fallback string) {
		writeField(d, subject, field, rdf.Text(value), p.Envelope, fallback)
	}
	text("title", p.Title, SurfaceAPI)
	text("abstract", p.Abstract, SurfaceAPI)
	text("comment", p.Comment, SurfaceAPI)
	text("report_no", p.ReportNo, SurfaceOAI)
	for _, v := range p.MSCClass {
		text("msc_class", v, SurfaceOAI)
	}
	for _, v := range p.ACMClass {
		text("acm_class", v, SurfaceOAI)
	}

	// A submission date is a day on the citation_date tag and a timestamp on
	// the API, and the day is the one two sources can be compared on. The
	// timestamp is not lost: it is on the version this record describes.
	if !p.FirstSubmitted.IsZero() {
		writeField(d, subject, "first_submitted", rdf.Typed(dayOf(p.FirstSubmitted), rdf.XSDDate), p.Envelope, SurfaceAPI)
	}
	if !p.LastUpdated.IsZero() {
		stamp := p.LastUpdated.UTC().Format(time.RFC3339)
		writeField(d, subject, "last_updated", rdf.Typed(stamp, rdf.XSDDateTime), p.Envelope, SurfaceAPI)
	}
	if p.Withdrawn {
		writeField(d, subject, "withdrawn", rdf.Typed("true", rdf.XSDBoolean), p.Envelope, SurfaceOAI)
	}

	// The PDF is a thing with its own address rather than a string on the
	// paper, which is what schema:encoding wants and what makes the file joinable
	// with the has_file claims.
	if p.PDFURL != "" {
		file := rdf.IRI(p.PDFURL)
		writeField(d, subject, "pdf_url", file, p.Envelope, SurfaceAPI)
		d.Type(file, rdf.SchemaMediaObject)
		d.Add(file, rdf.SchemaContentURL, file, sourceOf(p.Envelope, viaOr(p.Envelope, "pdf_url", SurfaceAPI)))
		d.Add(file, rdf.SchemaEncodingFmt, rdf.Text("application/pdf"))
	}
}

// AddCategory writes what the taxonomy says about one category.
func AddCategory(d *rdf.Doc, c Category) {
	if c.Code == "" {
		return
	}
	subject := rdf.NodeIRI(graph.Category(c.Code))
	d.Type(subject, rdf.Classes(graph.KindCategory)...)
	d.Add(subject, rdf.SKOSInScheme, rdf.Scheme)
	writeField(d, subject, "category.name", rdf.Text(c.Name), c.Envelope, SurfaceTaxonomy)
	writeField(d, subject, "category.description", rdf.Text(c.Description), c.Envelope, SurfaceTaxonomy)
}

// AddSet writes what OAI-PMH says about one set.
func AddSet(d *rdf.Doc, s Set) {
	if s.SetSpec == "" {
		return
	}
	subject := rdf.NodeIRI(graph.Set(s.SetSpec))
	d.Type(subject, rdf.Classes(graph.KindSet)...)
	writeField(d, subject, "set.name", rdf.Text(s.Name), s.Envelope, SurfaceOAI)
}

// AddPerson writes an author record.
//
// Only an identified record gets a person node, because only an identifier page
// is arXiv saying a person exists. A name search produces a name node, which is
// a string that several people may share, and giving it a schema:name would
// read as a claim about somebody.
func AddPerson(d *rdf.Doc, p Person) {
	if p.URI == "" {
		return
	}
	subject := rdf.NodeIRI(p.URI)
	if subject == "" {
		return
	}
	kind, _ := graph.KindOf(p.URI)
	d.Type(subject, rdf.Classes(kind)...)
	if p.Identified {
		writeField(d, subject, "person.name", rdf.Text(p.Name), p.Envelope, SurfaceAuthorID)
		return
	}
	// A name node still deserves the spelling it was searched under, and a
	// label is the one thing that says nothing about whose name it is.
	if source := sourceOf(p.Envelope, SurfaceSearch); source != "" {
		d.Label(subject, p.Name, source)
	}
}

// writeField puts one value under every term the mapping gives the field, with
// the source that answered for it.
//
// A field the table has never heard of is dropped rather than invented, and
// that is the one place in this file where something is lost on purpose: an
// unknown predicate has a name that can be written into the ax namespace, and
// an unknown record field does not, because nothing has decided yet whether it
// is a value or a link.
func writeField(d *rdf.Doc, subject rdf.IRI, field string, value rdf.Term, e Envelope, fallback string) {
	row, ok := rdf.Field(field)
	if !ok {
		return
	}
	source := sourceOf(e, viaOr(e, strings.TrimPrefix(field, kindPrefix(field)), fallback))
	for _, term := range row.Terms {
		d.Add(subject, term, value, source)
	}
}

// kindPrefix strips the record name off a dotted field, so category.name looks
// itself up in the via map as name, which is what the record wrote there.
func kindPrefix(field string) string {
	if i := strings.Index(field, "."); i >= 0 {
		return field[:i+1]
	}
	return ""
}
