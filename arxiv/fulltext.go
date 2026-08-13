package arxiv

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-cli/pkg/latexml"
)

// htmlBase is where the LaTeXML renderings live. The version is not optional:
// a rendering belongs to one version of one paper and arXiv will not guess
// which.
const htmlBase = "https://" + Host + "/html/"

// FullText is a paper's body, read from the LaTeXML rendering.
//
// It is a record of its own rather than more fields on a paper because it is a
// different size of thing: a paper is a few hundred bytes and a rendering is
// three quarters of a megabyte, and asking for one should never quietly fetch
// the other.
type FullText struct {
	Envelope

	// PaperID is canonical, with no version on it.
	PaperID string `json:"paper_id" kit:"id" table:"id"`
	// Version is the version that was rendered, which is the version this text
	// is the text of.
	Version int `json:"version" table:"version"`
	// URL is the rendering.
	URL   string `json:"url" table:"-,url"`
	Title string `json:"title" table:"title"`
	// Authors carry affiliations here, and nowhere else on arXiv.
	Authors []Author `json:"authors,omitempty" table:"-"`
	// LicenseName is the licence as the info box names it, "CC Zero", which is
	// the human form of the licence URI a paper record carries.
	LicenseName string `json:"license_name,omitempty" table:"-"`
	// Stamp is the watermark line, which says the version, the primary
	// category and the announcement date in one string.
	Stamp string `json:"stamp,omitempty" table:"-"`
	// Dates is the date line the author typed, "Sept 2023". It is free text and
	// it is kept as free text.
	Dates    string `json:"dates,omitempty" table:"-"`
	Abstract string `json:"abstract,omitempty" table:"-"`
	// Sections is the tree, in reading order.
	Sections []Section `json:"sections,omitempty" table:"-"`
	// References is the bibliography, empty when the paper wrote its references
	// as prose rather than as entries.
	References []Reference `json:"references,omitempty" table:"-"`
	// SectionCount counts every heading at every level, and Words counts the
	// body. Both are here so the table output says something useful about a
	// record whose interesting fields are all too big to print.
	SectionCount int `json:"section_count" table:"sections"`
	Words        int `json:"words" table:"words"`
}

// Section is one heading and everything under it.
type Section struct {
	// ID is LaTeXML's anchor, "S3.SS1.SSS2", which is what --section takes and
	// what a URL fragment on the rendering points at.
	ID string `json:"id"`
	// Kind is section, subsection, subsubsection, paragraph or appendix.
	Kind  string `json:"kind,omitempty"`
	Level int    `json:"level"`
	Title string `json:"title"`
	// Text is the prose directly under this heading, with maths as the LaTeX
	// the author wrote. The prose of a nested subsection belongs to that
	// subsection and is not repeated here.
	Text     string    `json:"text,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

// Reference is one bibliography entry.
//
// The fields are split the way LaTeXML split them, which is by BibTeX field.
// ArxivID and DOI are pulled out of the links because those two are what turn
// an entry into an edge to something else that can be read.
type Reference struct {
	ID        string `json:"id"`
	Kind      string `json:"kind,omitempty"`
	Label     string `json:"label,omitempty"`
	Authors   string `json:"authors,omitempty"`
	Editors   string `json:"editors,omitempty"`
	Title     string `json:"title,omitempty"`
	Journal   string `json:"journal,omitempty"`
	Volume    string `json:"volume,omitempty"`
	Number    string `json:"number,omitempty"`
	Pages     string `json:"pages,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Place     string `json:"place,omitempty"`
	Note      string `json:"note,omitempty"`
	// ArxivID is set when the entry links to an arXiv paper, which makes it a
	// citation this tool can follow.
	ArxivID string `json:"arxiv_id,omitempty"`
	DOI     string `json:"doi,omitempty"`
	// Links are every URL the entry offers, in the order it offers them.
	Links []string `json:"links,omitempty"`
	// CitedIn are the sections that cite this entry, as the page prints them:
	// "§1", "§5.2".
	CitedIn []string `json:"cited_in,omitempty"`
	// Text is the entry as one citation string.
	Text string `json:"text,omitempty"`
}

// FullTextOptions narrows what comes back.
//
// The narrowing happens after the read, not before it, because the rendering
// arrives as one document however little of it was asked for. What it saves is
// the size of the answer, not the size of the request.
type FullTextOptions struct {
	// Sections drops the body text and keeps the tree, which is a table of
	// contents.
	Sections bool
	// Section is one section id, with its children.
	Section string
	// Refs keeps the bibliography and drops the body.
	Refs bool
}

// FullText reads the LaTeXML rendering of a paper.
//
// It is a paper read at full depth followed by the rendering itself, because
// has_html lives on the abstract page and it is the only honest way to know
// whether a rendering exists. Fetching arxiv.org/html/<id> and reading the 404
// would cost the same fifteen seconds and would not tell the difference
// between a paper arXiv never rendered and a paper id that does not exist.
func (c *Client) FullText(ctx context.Context, ref string, opts FullTextOptions) (FullText, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return FullText{}, err
	}

	paper, err := c.PaperAt(ctx, ref, PaperOptions{Depth: DepthFull})
	if err != nil {
		return FullText{}, err
	}
	if !paper.HasHTML {
		return FullText{}, noRendering(id.Canonical)
	}

	doc, source, err := c.getFullText(ctx, paper)
	if err != nil {
		return FullText{}, err
	}

	out := fullTextFrom(doc, paper, source, c.now().UTC())
	if err := narrow(&out, opts); err != nil {
		return FullText{}, err
	}
	return out, nil
}

// noRendering is what a paper with no HTML gets.
//
// It is unsupported rather than not found because the paper exists and the
// question was answerable: arXiv has never rendered it, and no amount of
// retrying will change that.
func noRendering(id string) error {
	return errs.Unsupported("no LaTeXML HTML for %s; arXiv renders HTML for papers submitted since December 2023 and for some earlier ones", id)
}

// getFullText fetches and parses the rendering.
func (c *Client) getFullText(ctx context.Context, p Paper) (*latexml.Document, string, error) {
	u := p.HTMLURL
	if u == "" {
		u = htmlURL(p.ID, p.Version)
	}
	c.logf(1, "GET %s", u)
	// A rendering never changes. A new version is a new URL, so the long TTL
	// costs nothing in staleness and saves fifteen seconds every time.
	resp, err := c.fetch(ctx, u, TTLRendered)
	if err != nil {
		return nil, u, err
	}
	doc, err := latexml.Parse(resp.Body)
	if err != nil {
		return nil, u, err
	}
	return doc, u, nil
}

// htmlURL is the rendering of one version.
func htmlURL(id string, version int) string {
	if version > 0 {
		return htmlBase + fmt.Sprintf("%sv%d", id, version)
	}
	return htmlBase + id
}

// fullTextFrom builds the record.
func fullTextFrom(doc *latexml.Document, p Paper, source string, at time.Time) FullText {
	out := FullText{
		Envelope: Envelope{
			Kind:        "fulltext",
			Surfaces:    append([]string(nil), p.Surfaces...),
			Sources:     append([]string(nil), p.Sources...),
			RetrievedAt: at,
		},
		PaperID:     p.ID,
		Version:     p.Version,
		URL:         source,
		Title:       doc.Title,
		LicenseName: doc.License,
		Stamp:       doc.Stamp,
		Dates:       doc.Dates,
		Abstract:    doc.Abstract,
		Sections:    toSections(doc.Sections),
		References:  toReferences(doc.References),
		Authors:     toFullTextAuthors(doc.Authors),
	}
	out.addSurface(SurfaceFullText, source)
	for _, field := range []string{"title", "abstract", "sections", "affiliations", "license_name", "stamp"} {
		out.setVia(field, SurfaceFullText)
	}
	out.SectionCount = countSections(out.Sections)
	out.Words = countWords(out)
	if len(out.References) == 0 {
		out.Missed = append(out.Missed,
			"no bibliography entries were read; this rendering has none, which usually means the paper wrote its references as prose")
	}
	return out
}

func toFullTextAuthors(authors []latexml.Author) []Author {
	out := make([]Author, 0, len(authors))
	for _, a := range authors {
		out = append(out, Author{Name: a.Name, Affiliation: a.Affiliation, Via: SurfaceFullText})
	}
	return out
}

func toSections(sections []latexml.Section) []Section {
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
		out = append(out, Section{
			ID:       s.ID,
			Kind:     s.Kind,
			Level:    s.Level,
			Title:    s.Title,
			Text:     s.Text,
			Sections: toSections(s.Sections),
		})
	}
	return out
}

func toReferences(refs []latexml.Reference) []Reference {
	out := make([]Reference, 0, len(refs))
	for _, r := range refs {
		ref := Reference{
			ID:        r.ID,
			Kind:      r.Kind,
			Label:     r.Label,
			Authors:   r.Authors,
			Editors:   r.Editors,
			Title:     r.Title,
			Journal:   r.Journal,
			Volume:    r.Volume,
			Number:    r.Number,
			Pages:     r.Pages,
			Publisher: r.Publisher,
			Place:     r.Place,
			Note:      r.Note,
			Links:     r.Links,
			CitedIn:   r.CitedIn,
			Text:      r.Text,
		}
		ref.ArxivID, ref.DOI = identifiersIn(r.Links)
		out = append(out, ref)
	}
	return out
}

// identifiersIn reads an arXiv id and a DOI out of an entry's links.
//
// The id is parsed rather than pattern matched off the end of the URL, so
// arxiv.org/abs/hep-th/9711200 and arxiv.org/abs/2312.11805v2 both come back as
// ids this tool can go and read.
func identifiersIn(links []string) (arxivID, doi string) {
	for _, link := range links {
		switch {
		case arxivID == "" && strings.Contains(link, "arxiv.org/abs/"):
			if id, err := axid.Parse(link); err == nil {
				arxivID = id.Canonical
			}
		case doi == "" && strings.Contains(link, "doi.org/"):
			if _, rest, ok := strings.Cut(link, "doi.org/"); ok {
				doi = rest
			}
		}
	}
	return arxivID, doi
}

// narrow applies the options, and says so when a section id is not there.
func narrow(f *FullText, opts FullTextOptions) error {
	if opts.Section != "" {
		got, ok := findSection(f.Sections, opts.Section)
		if !ok {
			return errs.NotFound("no section %s in %s; arxiv fulltext %s --sections lists the ids", opts.Section, f.PaperID, f.PaperID)
		}
		f.Sections = []Section{got}
		f.SectionCount = countSections(f.Sections)
		f.Words = countWords(*f)
	}
	if opts.Sections {
		f.Sections = stripText(f.Sections)
	}
	if opts.Refs {
		f.Sections = nil
		f.Abstract = ""
	}
	return nil
}

func findSection(sections []Section, id string) (Section, bool) {
	for _, s := range sections {
		if s.ID == id {
			return s, true
		}
		if got, ok := findSection(s.Sections, id); ok {
			return got, true
		}
	}
	return Section{}, false
}

// stripText keeps the tree and drops the prose, which is a table of contents.
func stripText(sections []Section) []Section {
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
		s.Text = ""
		s.Sections = stripText(s.Sections)
		out = append(out, s)
	}
	return out
}

func countSections(sections []Section) int {
	n := 0
	for _, s := range sections {
		n += 1 + countSections(s.Sections)
	}
	return n
}

func countWords(f FullText) int {
	n := len(strings.Fields(f.Abstract))
	var walk func([]Section)
	walk = func(sections []Section) {
		for _, s := range sections {
			n += len(strings.Fields(s.Text))
			walk(s.Sections)
		}
	}
	walk(f.Sections)
	return n
}

// PlainText flattens a record to text in reading order: title, authors,
// abstract, then every section under its own heading.
//
// Headings are kept because a body without them is a wall, and the numbering
// is not put back because the ids are already in the tree for anyone who wants
// to point at a section.
func (f FullText) PlainText() string {
	var b strings.Builder
	write := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s)
	}
	write(f.Title)
	for _, a := range f.Authors {
		if a.Affiliation != "" {
			write(a.Name + ", " + a.Affiliation)
			continue
		}
		write(a.Name)
	}
	if f.Abstract != "" {
		write("Abstract")
		write(f.Abstract)
	}
	var walk func([]Section)
	walk = func(sections []Section) {
		for _, s := range sections {
			write(s.Title)
			write(s.Text)
			walk(s.Sections)
		}
	}
	walk(f.Sections)
	return b.String()
}
