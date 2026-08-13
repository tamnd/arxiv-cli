// Package latexml reads arXiv's LaTeXML rendering of a paper.
//
// arXiv runs LaTeX submissions through LaTeXML and serves the result at
// arxiv.org/html/<id>v<n>. The markup is machine written and every element
// that matters carries an ltx_ class, so the page can be read back into the
// structure the author typed rather than into a wall of text: a title, a list
// of authors with their affiliations, an abstract, and a tree of sections that
// nests exactly as \section, \subsection and \subsubsection nested.
//
// Three things live here and nowhere else on arXiv. Affiliations are one, and
// they are the field bibliographic datasets ask for most. The section tree is
// the second, and it gives a paper an outline without going near a PDF. The
// stamp line is the third: "arXiv:2401.00001v1 [q-fin.PM] 18 Nov 2023" is the
// version, the primary category and the announcement date in one string, and
// it is the version arXiv considers canonical for this rendering.
//
// Maths comes back as the LaTeX the author wrote, taken from the alttext
// attribute, because that is what a downstream reader can parse. Inline maths
// is wrapped in $ and displayed maths in \[ \], which is how it was delimited
// before LaTeXML got to it.
package latexml

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Document is a whole rendering.
type Document struct {
	// Title is the document title with any line break inside it flattened to a
	// space, because LaTeXML keeps the author's \\ as a <br>.
	Title string
	// Authors carry affiliations. Nothing else on arXiv does.
	Authors []Author
	// Dates is the date line as the author typed it, "Sept 2023". It is not
	// parsed, because it is free text and half of it is not a date at all.
	Dates string
	// License is the licence name from the info box, "CC Zero".
	License string
	// Stamp is the watermark line, "arXiv:2401.00001v1 [q-fin.PM] 18 Nov 2023".
	Stamp string
	// Abstract is the abstract text.
	Abstract string
	// Sections is the section tree in reading order.
	Sections []Section
	// References is the bibliography, empty when the paper's References section
	// was written as prose rather than as \bibitem entries.
	References []Reference
}

// Section is one heading and everything under it.
type Section struct {
	// ID is LaTeXML's own anchor, "S3.SS1.SSS2", which is also what a reader
	// puts after a # to link to it.
	ID string
	// Kind is the LaTeXML class without its prefix: section, subsection,
	// subsubsection, paragraph or appendix.
	Kind string
	// Level is 1 for a section, 2 for a subsection, 3 for a subsubsection and
	// 4 for a paragraph. An appendix is a section, so it is level 1.
	Level int
	// Title has the numbering tag stripped, so it is "Introduction" and not
	// "1 Introduction". The number is already in the ID.
	Title string
	// Text is the prose directly under this heading, with the text of any
	// nested subsection left to that subsection.
	Text string
	// Sections are the subsections, in reading order.
	Sections []Section
}

// Reference is one bibliography entry.
//
// The fields are split the way LaTeXML split them, which is by BibTeX field
// rather than by guesswork over a citation string. Anything that has no class
// of its own stays in Text, so nothing is dropped.
type Reference struct {
	// ID is the anchor, "bib.bib38", which is what the citations in the body
	// point at.
	ID string
	// Kind is the entry type: article, inproceedings, book, inbook,
	// incollection or misc.
	Kind string
	// Label is the printed marker, "Bai et al. (2023)", which is where the
	// year lives when the style is author-year.
	Label     string
	Authors   string
	Editors   string
	Title     string
	Journal   string
	Volume    string
	Number    string
	Pages     string
	Publisher string
	Place     string
	Note      string
	// Links are the URLs the entry offers, usually a DOI or an arXiv abs page.
	Links []string
	// CitedIn are the sections that cite this entry, as LaTeXML prints them:
	// "§1", "§5.2".
	CitedIn []string
	// Text is the whole entry as one string, for when the split fields are not
	// enough.
	Text string
}

// levels maps a LaTeXML sectioning class to its depth.
var levels = map[string]int{
	"ltx_section":       1,
	"ltx_appendix":      1,
	"ltx_subsection":    2,
	"ltx_subsubsection": 3,
	"ltx_paragraph":     4,
}

// nameGap splits the names in a one line author block.
//
// LaTeXML runs several authors into one ltx_personname when the layout is
// ltx_authors_1line, separated by the gap the class file asked for. On
// 2601.00086v3 that gap is an em space followed by a hair space, which is what
// \quad renders to; elsewhere it is a run of ordinary spaces or a newline. A
// single ordinary space is never a separator here, because a single space is
// what sits between a forename and a surname.
var nameGap = regexp.MustCompile(`[\x{2000}-\x{200a}\x{2028}\x{2029}\x{3000}]+|[ \t]{2,}|\n`)

// Parse reads a LaTeXML page.
func Parse(body []byte) (*Document, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse latexml page: %w", err)
	}
	article := doc.Find("article.ltx_document").First()
	if article.Length() == 0 {
		return nil, fmt.Errorf("no latexml document on the page")
	}

	out := &Document{
		Title:   text(article.Find("h1.ltx_title_document").First()),
		Dates:   text(article.Find(".ltx_dates").First()),
		License: strings.TrimSpace(strings.TrimPrefix(text(doc.Find("#license-tr").First()), "License:")),
		Stamp:   text(doc.Find("#watermark-tr").First()),
	}
	parseAuthors(article, out)
	parseBody(article, out)
	parseReferences(doc.Selection, out)
	return out, nil
}

// parseAuthors reads the author block.
func parseAuthors(article *goquery.Selection, out *Document) {
	article.Find(".ltx_creator.ltx_role_author").Each(func(_ int, creator *goquery.Selection) {
		names := splitNames(raw(creator.Find(".ltx_personname").First()))
		var affiliations []string
		creator.Find(".ltx_contact.ltx_role_affiliation").Each(func(_ int, contact *goquery.Selection) {
			whole := text(contact)
			label := text(contact.Find(".ltx_contact_name").First())
			affiliations = append(affiliations, strings.TrimSpace(strings.TrimPrefix(whole, label)))
		})
		for i, name := range names {
			out.Authors = append(out.Authors, Author{Name: name, Affiliation: affiliationFor(affiliations, i, len(names))})
		}
	})
}

// Author is a name and, where the page says one, an institution.
type Author struct {
	Name        string
	Affiliation string
}

// affiliationFor pairs a name with an affiliation.
//
// One affiliation for a block of names means the block shares it, which is the
// common case for a group from one lab. An equal count means they were listed
// in order and pair off. Anything else is a layout this parser cannot read
// without guessing, and a wrong affiliation is worse than none.
func affiliationFor(affiliations []string, i, names int) string {
	switch {
	case len(affiliations) == 1:
		return affiliations[0]
	case len(affiliations) == names && i < len(affiliations):
		return affiliations[i]
	default:
		return ""
	}
}

// splitNames pulls the names out of one personname span.
func splitNames(s string) []string {
	var out []string
	for _, part := range nameGap.Split(s, -1) {
		if part = tidy(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parseBody reads the abstract and the section tree.
//
// Two shapes turn up. A paper with \begin{abstract} gets an ltx_abstract div,
// and a paper whose abstract was typed as an unnumbered section gets an
// ordinary section titled Abstract, which is what 2401.00001v1 has. Both mean
// the same thing to a reader, so both end up in Abstract and neither is left
// sitting in the section tree pretending to be chapter one.
func parseBody(article *goquery.Selection, out *Document) {
	if abstract := article.Find(".ltx_abstract").First(); abstract.Length() > 0 {
		out.Abstract = sectionText(abstract)
	}
	for _, sec := range childSections(article) {
		if out.Abstract == "" && len(out.Sections) == 0 && strings.EqualFold(sec.Title, "Abstract") {
			out.Abstract = sec.Text
			continue
		}
		out.Sections = append(out.Sections, sec)
	}
}

// childSections reads the sections directly under a node, recursing into each.
func childSections(sel *goquery.Selection) []Section {
	var out []Section
	sel.ChildrenFiltered("section").Each(func(_ int, s *goquery.Selection) {
		kind, level := sectionLevel(s)
		if level == 0 {
			// A bibliography is a section element too, and it is read as
			// references rather than as prose.
			return
		}
		out = append(out, Section{
			ID:       s.AttrOr("id", ""),
			Kind:     strings.TrimPrefix(kind, "ltx_"),
			Level:    level,
			Title:    text(s.ChildrenFiltered("h1,h2,h3,h4,h5,h6").First()),
			Text:     sectionText(s),
			Sections: childSections(s),
		})
	})
	return out
}

// sectionLevel says what kind of section a node is.
func sectionLevel(s *goquery.Selection) (string, int) {
	for class, level := range levels {
		if s.HasClass(class) {
			return class, level
		}
	}
	return "", 0
}

// sectionText is the prose under a heading, without the heading and without
// anything that belongs to a nested subsection.
func sectionText(sel *goquery.Selection) string {
	if sel.Length() == 0 {
		return ""
	}
	var b strings.Builder
	for c := sel.Nodes[0].FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if c.Data == "section" || isHeading(c.Data) {
				continue
			}
		}
		writeNode(&b, c)
	}
	return tidyBlocks(b.String())
}

func isHeading(tag string) bool {
	return len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6'
}

// parseReferences reads the bibliography.
func parseReferences(doc *goquery.Selection, out *Document) {
	doc.Find(".ltx_bibliography li.ltx_bibitem").Each(func(_ int, li *goquery.Selection) {
		ref := Reference{
			ID:        li.AttrOr("id", ""),
			Kind:      bibKind(li),
			Label:     text(li.Find(".ltx_tag_bibitem").First()),
			Authors:   text(li.Find(".ltx_bib_author").First()),
			Editors:   text(li.Find(".ltx_bib_editor").First()),
			Title:     text(li.Find(".ltx_bib_title").First()),
			Journal:   text(li.Find(".ltx_bib_journal").First()),
			Volume:    text(li.Find(".ltx_bib_volume").First()),
			Number:    text(li.Find(".ltx_bib_number").First()),
			Pages:     text(li.Find(".ltx_bib_pages").First()),
			Publisher: text(li.Find(".ltx_bib_publisher").First()),
			Place:     text(li.Find(".ltx_bib_place").First()),
			Note:      text(li.Find(".ltx_bib_note").First()),
			Text:      bibText(li),
		}
		li.Find("a.ltx_bib_external").Each(func(_ int, a *goquery.Selection) {
			if href := a.AttrOr("href", ""); href != "" {
				ref.Links = append(ref.Links, href)
			}
		})
		li.Find(".ltx_bib_cited a").Each(func(_ int, a *goquery.Selection) {
			if where := text(a); where != "" {
				ref.CitedIn = append(ref.CitedIn, where)
			}
		})
		out.References = append(out.References, ref)
	})
}

// bibText is the citation as it reads, which is the entry's blocks without the
// two that are apparatus: the external links line and the list of sections
// that cite it. Both are kept as fields of their own, so nothing is lost by
// leaving them out of the sentence.
func bibText(li *goquery.Selection) string {
	var blocks []string
	li.Find(".ltx_bibblock").Each(func(_ int, block *goquery.Selection) {
		if block.HasClass("ltx_bib_cited") || block.Find(".ltx_bib_links").Length() > 0 {
			return
		}
		if s := text(block); s != "" {
			blocks = append(blocks, s)
		}
	})
	return strings.Join(blocks, " ")
}

// bibKind reads the entry type off the item's classes, where LaTeXML puts the
// BibTeX entry type it started from.
func bibKind(li *goquery.Selection) string {
	for _, class := range strings.Fields(li.AttrOr("class", "")) {
		switch class {
		case "ltx_bib_author", "ltx_bib_title", "ltx_bib_cited":
			continue
		}
		if rest, ok := strings.CutPrefix(class, "ltx_bib_"); ok {
			return rest
		}
	}
	return ""
}

// Section returns the section with this id, at any depth, and whether it was
// found.
func (d *Document) Section(id string) (Section, bool) {
	return findSection(d.Sections, id)
}

func findSection(sections []Section, id string) (Section, bool) {
	for _, s := range sections {
		if s.ID == id {
			return s, true
		}
		if found, ok := findSection(s.Sections, id); ok {
			return found, true
		}
	}
	return Section{}, false
}

// text is the readable text of a node: markup gone, maths as LaTeX, blocks
// separated by blank lines.
func text(sel *goquery.Selection) string {
	if sel.Length() == 0 {
		return ""
	}
	var b strings.Builder
	writeChildren(&b, sel.Nodes[0])
	return tidy(b.String())
}

// raw is the text of a node with its spacing untouched, for the one caller
// that needs the spacing to tell two names apart.
func raw(sel *goquery.Selection) string {
	if sel.Length() == 0 {
		return ""
	}
	var b strings.Builder
	writeChildren(&b, sel.Nodes[0])
	return b.String()
}

// writeChildren walks what is inside a node.
//
// The node itself is never asked whether it should be skipped, because a
// caller that selected an element has already said it wants that element: the
// numbering tag of a bibliography item is skipped inside a paragraph and read
// on its own when it is what was asked for.
func writeChildren(b *strings.Builder, n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeNode(b, c)
	}
}

// writeNode walks a node, writing what a reader would see.
func writeNode(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		if n.Data == "math" {
			b.WriteString(mathText(n))
			return
		}
		if skipNode(n) {
			return
		}
		if isBlock(n.Data) {
			b.WriteString(blockBreak)
		}
	default:
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeNode(b, c)
	}
	if n.Type == html.ElementNode && isBlock(n.Data) {
		b.WriteString(blockBreak)
	}
}

// blockBreak marks where one block ended and the next began.
//
// It is a sentinel rather than a newline because the newlines already in the
// markup are the author's line wrapping and mean nothing: a paragraph in the
// source is broken every eighty columns and it is still one paragraph. Keeping
// the two apart is what stops a sentence coming out cut in half.
const blockBreak = "\x1f"

// skipNode says whether a node's text belongs in the reading text at all.
//
// A footnote's body is set at the foot of the page and would otherwise land in
// the middle of the sentence that carries its mark, and the mark itself would
// come out as a digit glued to the last word. A numbering tag is dropped from a
// heading, where the number is already in the id and "1 Introduction" is worse
// than "Introduction", and kept everywhere else, because the same class is what
// makes a caption say "Figure 1: " and a caption without that reads as a stray
// sentence in the middle of the prose.
func skipNode(n *html.Node) bool {
	switch n.Data {
	case "nav", "script", "style":
		return true
	}
	for _, class := range strings.Fields(attr(n, "class")) {
		switch class {
		case "ltx_note_outer", "ltx_note_mark", "ltx_TOC", "ltx_page_logo":
			return true
		case "ltx_tag":
			if n.Parent != nil && n.Parent.Type == html.ElementNode && isHeading(n.Parent.Data) {
				return true
			}
		}
	}
	return false
}

// mathText renders one maths element as the LaTeX it came from.
func mathText(n *html.Node) string {
	tex := attr(n, "alttext")
	if tex == "" {
		tex = annotation(n)
	}
	if tex == "" {
		return ""
	}
	if attr(n, "display") == "block" {
		return blockBreak + "\\[" + tex + "\\]" + blockBreak
	}
	return "$" + tex + "$"
}

// annotation is the TeX annotation MathML carries, which says the same thing
// as alttext and is there on the rare element that has no alttext.
func annotation(n *html.Node) string {
	var out string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if out != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "annotation" && attr(n, "encoding") == "application/x-tex" {
			var b strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					b.WriteString(c.Data)
				}
			}
			out = strings.TrimSpace(b.String())
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// isBlock says whether an element ends the line it is on.
func isBlock(tag string) bool {
	switch tag {
	case "p", "div", "table", "tr", "li", "ul", "ol", "dl", "dt", "dd",
		"section", "article", "figure", "figcaption", "blockquote", "pre", "br",
		"h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

// tidy collapses a run of whitespace to one space and trims the result, for
// text that is one line by nature: a title, a name, a heading.
func tidy(s string) string {
	s = strings.ReplaceAll(s, blockBreak, " ")
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

// tidyBlocks tidies a body: one paragraph to a line, a blank line between
// paragraphs, nothing left of the markup's own wrapping.
func tidyBlocks(s string) string {
	var out []string
	for _, block := range strings.Split(s, blockBreak) {
		if block = tidy(block); block != "" {
			out = append(out, block)
		}
	}
	return strings.Join(out, "\n\n")
}
