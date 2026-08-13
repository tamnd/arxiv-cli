package arxiv

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// This file answers "who wrote this" twice, in two ways that are not the same
// claim, and keeps them apart.
//
// A search on the author field is a string match. arXiv's author field is text
// somebody typed, two people share a name, and one person publishes under three
// spellings, so a name match is a set of papers and not a person.
//
// The author identifier page is arXiv asserting that a registered person owns a
// set of papers, and it is the only surface that carries an ORCID. That is a
// person. The record says which of the two it is, and the two live in different
// URI spaces so a later join cannot quietly merge them.

// authorBase is the identifier page root.
//
// The unsuffixed form redirects: /a/baez_j_1 answers 303 to /a/baez_j_1.html,
// measured 2026-08-14. The suffixed form is asked for directly, because a hop
// on the fifteen second plane buys nothing.
const authorBase = "https://" + Host + "/a/"

// authorIDRe is the shape of an identifier, <lastname>_<initial>_<n>.
//
// It is checked rather than guessed. A name cannot be turned into one of these:
// the number on the end is arXiv's own disambiguator between two people with
// the same surname and initial, and there is no way to work out from a name
// whether somebody is the first Wang J or the fourteenth.
var authorIDRe = regexp.MustCompile(`^[a-z][a-z0-9_.'-]*_[a-z]+(_\d+)?$`)

// orcidRe reads an ORCID out of a link on the page. The last group is a check
// digit and can be an X, which is why it is not four digits.
var orcidRe = regexp.MustCompile(`(\d{4}-\d{4}-\d{4}-\d{3}[\dX])`)

// authorPageTitleRe strips the page's own wording off the heading, which reads
// "John Baez's articles on arXiv".
var authorPageTitleRe = regexp.MustCompile(`^(.*?)'s articles on arXiv$`)

// Person is one answer to "who wrote this", carrying which kind of answer it is.
//
// It is a separate type from the Author on a paper: that one is a credit line,
// this one is the thing the credit line might refer to.
type Person struct {
	Envelope

	// Name is the display name: what was searched for, or what the identifier
	// page calls itself.
	Name string `json:"name" kit:"id" table:"name"`
	// Mode is "name search" or "identifier page", in words, because the whole
	// point of this record is that a reader can tell the two apart at a glance.
	Mode string `json:"mode" table:"mode"`
	// Identified is the honest field. False means a string matched. True means
	// arXiv says a registered person owns these papers.
	//
	// It has no column of its own because a false bool renders as an empty
	// cell, and an empty cell under a heading reading "identified" is the one
	// thing this record must not be ambiguous about. Mode says the same fact in
	// words for a reader, and this says it in a bool for everything else.
	Identified bool `json:"identified" table:"-"`
	// ArxivID and ORCID exist only on an identified record.
	ArxivID string `json:"arxiv_id,omitempty" table:"-"`
	ORCID   string `json:"orcid,omitempty" table:"orcid"`
	// URI is the node this record is about. A name and a person are different
	// URI spaces, doc 04 section 2.
	URI string `json:"uri" table:"-"`
	// IdentifiedAs is the person URI a name node was joined to, set only when
	// an identifier page said so.
	IdentifiedAs string `json:"identified_as,omitempty" table:"-"`
	// Query is the search that produced a name match, so it can be rerun.
	Query string `json:"query,omitempty" table:"-"`
	// PaperCount is how many papers arXiv has under this name or this person.
	PaperCount int `json:"paper_count" table:"papers"`
	// Papers are the ones read, which on a name search is the first --limit of
	// them and on an identifier page is all of them.
	Papers []Paper `json:"papers,omitempty" table:"-"`
	// Warning is the sentence a name match needs beside it.
	Warning string `json:"warning,omitempty" table:"-"`
}

// The node builders live in pkg/graph, which is the only place a node is named.
// These stay as thin wrappers because the surrounding code reads better for
// them, and because a second spelling of a node kind is a second node.

// AuthorURI is the node for a registered person.
func AuthorURI(id string) string { return graph.Author(id) }

// NameURI is the node for an author string.
func NameURI(name string) string { return graph.Name(name) }

// ORCIDURI is the node for an ORCID, which is the identifier that survives a
// name change and is the only one shared with the world outside arXiv.
func ORCIDURI(orcid string) string { return graph.ORCID(orcid) }

// nameSlug normalises a name for joining: lowercased, accents folded,
// punctuation dropped, spaces to hyphens.
//
// This is a lossy join and it is meant to be, because the value of a name node
// is that two spellings of one string land together. It is also exactly why a
// name node is not a person node.
func nameSlug(name string) string { return graph.NormalizeName(name) }

// AuthorByName searches the author field and returns a name match.
//
// It is two requests: the count, which is the number worth knowing before
// deciding whether the name is specific enough, and the first n papers.
func (c *Client) AuthorByName(ctx context.Context, name string, limit int) (Person, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Person{}, errs.Usage("give an author name, or an arXiv author identifier with --id")
	}
	if limit <= 0 {
		limit = DefaultLimit
	}

	q := Term(FieldAuthor, name)
	p := Person{
		Envelope: Envelope{
			Kind:        "author",
			RetrievedAt: c.now().UTC(),
			Missed: []string{
				"no ORCID and no arXiv author identifier were read; arxiv author <identifier> --id reads a registered person's page",
			},
		},
		Name:    name,
		Mode:    "name search",
		URI:     NameURI(name),
		Query:   q.String(),
		Warning: "a name match is not a person: two authors can share a name and one author can publish under several spellings",
	}

	req := CountRequest(q)
	u, err := req.URL()
	if err != nil {
		return Person{}, err
	}
	c.logf(1, "GET %s", u)
	feed, err := c.getXML(ctx, u, TTLSearch)
	if err != nil {
		return Person{}, err
	}
	p.PaperCount = feed.Total
	p.addSurface(SurfaceAPI, u)
	p.setVia("paper_count", SurfaceAPI)

	papers, err := c.SearchByAuthor(ctx, name, limit)
	if err != nil {
		return Person{}, err
	}
	p.Papers = papers
	if len(papers) > 0 {
		p.addSurface(SurfaceAPI, papers[0].Sources[0])
	}
	if p.PaperCount > len(papers) {
		p.Truncated = fmt.Sprintf("%d papers match the name and the first %d were read; --limit reads more",
			p.PaperCount, len(papers))
	}
	return p, nil
}

// AuthorByID reads the identifier page, which is a person.
func (c *Client) AuthorByID(ctx context.Context, id string) (Person, error) {
	id, err := normaliseAuthorID(id)
	if err != nil {
		return Person{}, err
	}

	u := authorURL(id)
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLAuthor)
	if err != nil {
		if errs.KindOf(err) == errs.KindNotFound {
			return Person{}, authorNotFound(id)
		}
		return Person{}, err
	}

	page, err := parseAuthorPage(resp.Body)
	if err != nil {
		return Person{}, err
	}

	at := c.now().UTC()
	p := Person{
		Envelope:   Envelope{Kind: "author", RetrievedAt: at},
		Name:       page.Name,
		Mode:       "identifier page",
		Identified: true,
		ArxivID:    id,
		ORCID:      page.ORCID,
		URI:        AuthorURI(id),
		PaperCount: len(page.Rows),
	}
	p.addSurface(SurfaceAuthorID, u)
	p.setVia("papers", SurfaceAuthorID)
	if page.Name != "" {
		p.setVia("name", SurfaceAuthorID)
		// The join between the two URI spaces is written here and only here,
		// because this page is arXiv itself saying the name and the person are
		// the same thing.
		p.IdentifiedAs = NameURI(page.Name)
	}
	if page.ORCID != "" {
		p.setVia("orcid", SurfaceAuthorID)
	}

	for _, row := range page.Rows {
		paper := listToPaper(row, SurfaceAuthorID, u, at)
		c.noteUnknownCategories(paper.Categories...)
		p.Papers = append(p.Papers, paper)
	}
	if page.ORCID == "" {
		p.Missed = []string{"this page carries no ORCID, so there is no identifier for this person outside arXiv"}
	}
	return p, nil
}

// authorURL is the page for an identifier.
func authorURL(id string) string { return authorBase + id + ".html" }

// authorNotFound is what a 404 from the identifier page means.
//
// The obvious message, "author not found", would be wrong. The page exists only
// for authors who registered one, so its absence says nothing at all about
// whether the author exists or has papers, and the message points at the lookup
// that does work.
func authorNotFound(id string) error {
	return errs.NotFound("no registered identifier page for %s; identifier pages are opt-in, so this does not mean the author has no papers, and arxiv author \"<name>\" searches by name instead", id)
}

// normaliseAuthorID accepts what people will paste and refuses what cannot be
// an identifier.
func normaliseAuthorID(raw string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(raw))
	id = strings.TrimPrefix(id, "https://"+Host+"/a/")
	id = strings.TrimPrefix(id, "http://"+Host+"/a/")
	id = strings.TrimSuffix(id, ".html")
	id = strings.Trim(id, "/")
	if id == "" {
		return "", errs.Usage("give an arXiv author identifier, which looks like baez_j_1")
	}
	if !authorIDRe.MatchString(id) {
		// The message quotes what was typed, not what was left after
		// lowercasing it, because being told that "John baez" is wrong when you
		// typed "John Baez" reads like a second bug.
		return "", errs.Usage(
			"%q is not an arXiv author identifier; they look like baez_j_1 and cannot be worked out from a name, so search the name without --id instead", strings.TrimSpace(raw))
	}
	return id, nil
}

// ─── the page ────────────────────────────────────────────────────────────────

// authorPage is the identifier page in its own shape.
type authorPage struct {
	Name  string
	ORCID string
	// Rows are the same <dt>/<dd> pairs the category listing has, so the
	// listing's own row parser reads them.
	Rows []listRow
}

// parseAuthorPage reads the identifier page.
//
// The rows are s4's shape with one difference: each pair is wrapped in a
// <div class="mathjax"> and the list has no id, so the walk goes over the dt
// elements under the page's definition list and takes the dd beside each one,
// rather than over the list's own children.
func parseAuthorPage(body []byte) (*authorPage, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse author page: %w", err)
	}
	page := &authorPage{}

	heading := cleanText(doc.Find("#dlpage h1").First().Text())
	if heading == "" {
		heading = cleanText(doc.Find("h1").First().Text())
	}
	if m := authorPageTitleRe.FindStringSubmatch(heading); m != nil {
		page.Name = cleanText(m[1])
	} else {
		page.Name = heading
	}

	// The ORCID is a link to orcid.org in the paragraph under the heading, and
	// it is the reason this page is worth a request: nothing else on arXiv
	// publishes one.
	doc.Find("#dlpage a[href*='orcid.org']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if m := orcidRe.FindStringSubmatch(a.AttrOr("href", "")); m != nil {
			page.ORCID = m[1]
			return false
		}
		return true
	})

	dts := doc.Find("#dlpage dl dt")
	if dts.Length() == 0 {
		// A registered author with nothing on arXiv is not something this has
		// seen, but the page would still render, so an empty list is empty and
		// not a parse failure. A page with no definition list at all has
		// changed shape and says so.
		if doc.Find("#dlpage dl").Length() > 0 {
			return page, nil
		}
		return nil, fmt.Errorf("parse author page: no article list on it")
	}
	dts.Each(func(_ int, dt *goquery.Selection) {
		row := parseListDT(dt)
		if dd := dt.NextFiltered("dd"); dd.Length() > 0 {
			parseListDD(dd, &row)
		}
		page.Rows = append(page.Rows, row)
	})
	return page, nil
}
