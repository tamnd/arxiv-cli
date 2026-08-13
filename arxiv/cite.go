package arxiv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/any-cli/kit/errs"
)

// Style is a citation format.
type Style string

const (
	StyleBibTeX  Style = "bibtex"
	StyleAPA     Style = "apa"
	StyleMLA     Style = "mla"
	StyleChicago Style = "chicago"
	StyleRIS     Style = "ris"
	StyleCSLJSON Style = "csl-json"
	StyleText    Style = "text"
)

// Styles is every style, in the order the help lists them.
var Styles = []Style{StyleBibTeX, StyleAPA, StyleMLA, StyleChicago, StyleRIS, StyleCSLJSON, StyleText}

// ParseStyle resolves a style name, taking the spellings people type.
func ParseStyle(s string) (Style, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "bibtex", "bib":
		return StyleBibTeX, nil
	case "apa":
		return StyleAPA, nil
	case "mla":
		return StyleMLA, nil
	case "chicago":
		return StyleChicago, nil
	case "ris":
		return StyleRIS, nil
	case "csl-json", "csl", "json":
		return StyleCSLJSON, nil
	case "text", "plain":
		return StyleText, nil
	}
	return "", errs.Usage("citation style %q is not one of %s", s, strings.Join(styleNames(), ", "))
}

// Cite formats papers in one style.
//
// Every style is built from the record and none of them are fetched, so this is
// two requests on the API plane whatever style was asked for. The one arXiv
// itself serves is BibTeX, and `arxiv bibtex` is the command for that.
func (c *Client) Cite(ctx context.Context, refs []string, style Style) (string, error) {
	papers, err := c.citePapers(ctx, refs)
	if err != nil {
		return "", err
	}
	if style == StyleCSLJSON {
		// CSL is a list format. Two entries are one array and not two
		// documents, because a reference manager reads the file as a whole.
		return renderCSLJSON(papers)
	}

	sep := "\n"
	if style == StyleBibTeX || style == StyleRIS {
		sep = "\n\n"
	}
	out := make([]string, 0, len(papers))
	for _, p := range papers {
		out = append(out, citeOne(p, style))
	}
	return strings.Join(out, sep), nil
}

func citeOne(p Paper, style Style) string {
	switch style {
	case StyleAPA:
		return renderAPA(p)
	case StyleMLA:
		return renderMLA(p)
	case StyleChicago:
		return renderChicago(p)
	case StyleRIS:
		return renderRIS(p)
	case StyleText:
		return renderPlain(p)
	default:
		return renderBibTeX(p)
	}
}

// A note that applies to all five prose styles.
//
// The title is printed as arXiv holds it. APA wants sentence case and this does
// not convert to it, because lowercasing "Standard Model Higgs boson" correctly
// needs to know which words are names, and a formatter that guesses gets it
// wrong on exactly the papers where it matters.
//
// A name arXiv only published as a display string is printed as that string. It
// is not split into a surname and initials, because the split is wrong for "van
// der Waals", for "The ATLAS Collaboration", and for every name written surname
// first. That is the same rule the record follows, per doc 03 section 2.3.

// renderAPA is APA 7 for a preprint: authors, year, title, repository, DOI.
func renderAPA(p Paper) string {
	names := make([]string, 0, len(p.Authors))
	for _, a := range p.Authors {
		names = append(names, apaName(a))
	}
	var b strings.Builder
	b.WriteString(apaList(names))
	if !p.FirstSubmitted.IsZero() {
		fmt.Fprintf(&b, " (%d).", p.FirstSubmitted.Year())
	}
	fmt.Fprintf(&b, " %s.", trimDot(p.Title))
	if p.JournalRef != "" {
		fmt.Fprintf(&b, " %s.", trimDot(p.JournalRef))
	} else {
		b.WriteString(" arXiv.")
	}
	doi := p.PublisherDOI
	if doi == "" {
		doi = p.DOI
	}
	if doi != "" {
		fmt.Fprintf(&b, " https://doi.org/%s", doi)
	}
	return b.String()
}

// apaName is "Vaswani, A. N." when the surface gave the name apart, and the
// display string when it did not.
func apaName(a Author) string {
	if a.Keyname == "" {
		return a.Name
	}
	if a.Forenames == "" {
		return a.Keyname
	}
	return a.Keyname + ", " + initials(a.Forenames)
}

// initials turns "Aidan N." into "A. N.".
func initials(forenames string) string {
	parts := strings.FieldsFunc(forenames, func(r rune) bool { return r == ' ' || r == '-' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, r := range part {
			out = append(out, string(r)+".")
			break
		}
	}
	return strings.Join(out, " ")
}

// apaList joins names APA's way: commas throughout, an ampersand before the
// last, and for twenty one authors or more the first nineteen, an ellipsis and
// the last one.
func apaList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return trimDot(names[0]) + "."
	case 2:
		return names[0] + ", & " + trimDot(names[1]) + "."
	}
	if len(names) > 20 {
		head := strings.Join(names[:19], ", ")
		return head + ", ... " + trimDot(names[len(names)-1]) + "."
	}
	head := strings.Join(names[:len(names)-1], ", ")
	return head + ", & " + trimDot(names[len(names)-1]) + "."
}

// renderMLA is MLA 9: one author inverted, two joined with "and", three or more
// as "et al.".
func renderMLA(p Paper) string {
	var b strings.Builder
	switch len(p.Authors) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "%s. ", trimDot(invertedName(p.Authors[0])))
	case 2:
		fmt.Fprintf(&b, "%s, and %s. ", invertedName(p.Authors[0]), trimDot(p.Authors[1].Name))
	default:
		fmt.Fprintf(&b, "%s, et al. ", invertedName(p.Authors[0]))
	}
	fmt.Fprintf(&b, "\"%s.\" ", trimDot(p.Title))
	if p.JournalRef != "" {
		fmt.Fprintf(&b, "%s, ", trimDot(p.JournalRef))
	}
	b.WriteString("arXiv, ")
	if !p.FirstSubmitted.IsZero() {
		fmt.Fprintf(&b, "%s, ", p.FirstSubmitted.Format("2 January 2006"))
	}
	fmt.Fprintf(&b, "%s.", strings.TrimPrefix(paperURL(p), "https://"))
	return b.String()
}

// renderChicago is Chicago author-date: the first author inverted, the rest in
// reading order, and "et al." past ten.
func renderChicago(p Paper) string {
	var b strings.Builder
	names := make([]string, 0, len(p.Authors))
	for i, a := range p.Authors {
		if i == 0 {
			names = append(names, invertedName(a))
			continue
		}
		names = append(names, a.Name)
	}
	switch {
	case len(names) == 0:
	case len(names) == 1:
		fmt.Fprintf(&b, "%s. ", trimDot(names[0]))
	case len(names) > 10:
		fmt.Fprintf(&b, "%s, et al. ", strings.Join(names[:7], ", "))
	default:
		fmt.Fprintf(&b, "%s, and %s. ", strings.Join(names[:len(names)-1], ", "), trimDot(names[len(names)-1]))
	}
	if !p.FirstSubmitted.IsZero() {
		fmt.Fprintf(&b, "%d. ", p.FirstSubmitted.Year())
	}
	fmt.Fprintf(&b, "\"%s.\" ", trimDot(p.Title))
	if p.JournalRef != "" {
		fmt.Fprintf(&b, "%s. ", trimDot(p.JournalRef))
	}
	fmt.Fprintf(&b, "arXiv. %s.", paperURL(p))
	return b.String()
}

// invertedName is "Vaswani, Ashish", or the display string when the surface
// never gave the parts.
func invertedName(a Author) string {
	if a.Keyname == "" {
		return a.Name
	}
	if a.Forenames == "" {
		return a.Keyname
	}
	return a.Keyname + ", " + a.Forenames
}

// renderRIS is the tagged format every reference manager imports.
//
// The type is JOUR for a published paper and GEN for a preprint. RIS has no
// preprint type, and calling an unrefereed preprint a journal article is the one
// thing in this format that would be a lie.
func renderRIS(p Paper) string {
	var b strings.Builder
	line := func(tag, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s  - %s\n", tag, value)
	}
	if p.JournalRef != "" {
		line("TY", "JOUR")
	} else {
		line("TY", "GEN")
	}
	for _, a := range p.Authors {
		line("AU", invertedName(a))
	}
	line("TI", p.Title)
	if !p.FirstSubmitted.IsZero() {
		line("PY", fmt.Sprint(p.FirstSubmitted.Year()))
		line("DA", p.FirstSubmitted.Format("2006/01/02"))
	}
	line("JO", p.JournalRef)
	line("AB", p.Abstract)
	for _, cat := range p.Categories {
		line("KW", cat)
	}
	line("PB", "arXiv")
	doi := p.PublisherDOI
	if doi == "" {
		doi = p.DOI
	}
	line("DO", doi)
	line("UR", paperURL(p))
	line("N1", "arXiv:"+versionedID(p))
	b.WriteString("ER  -")
	return b.String()
}

// renderPlain is the one line form, for a chat message or a commit body.
func renderPlain(p Paper) string {
	var b strings.Builder
	switch {
	case len(p.Authors) == 0:
	case len(p.Authors) > 3:
		fmt.Fprintf(&b, "%s et al., ", p.Authors[0].Name)
	default:
		fmt.Fprintf(&b, "%s, ", strings.Join(authorNames(p), ", "))
	}
	fmt.Fprintf(&b, "\"%s\", arXiv:%s", trimDot(p.Title), versionedID(p))
	if p.JournalRef != "" {
		fmt.Fprintf(&b, ", %s", trimDot(p.JournalRef))
	}
	if !p.FirstSubmitted.IsZero() {
		fmt.Fprintf(&b, " (%d)", p.FirstSubmitted.Year())
	}
	b.WriteString(".")
	return b.String()
}

// cslItem is one CSL-JSON entry, which is what every reference manager eats.
type cslItem struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Title          string         `json:"title"`
	Author         []cslName      `json:"author,omitempty"`
	Issued         *cslDate       `json:"issued,omitempty"`
	ContainerTitle string         `json:"container-title,omitempty"`
	Publisher      string         `json:"publisher,omitempty"`
	DOI            string         `json:"DOI,omitempty"`
	URL            string         `json:"URL,omitempty"`
	Number         string         `json:"number,omitempty"`
	Abstract       string         `json:"abstract,omitempty"`
	Categories     []string       `json:"categories,omitempty"`
	Accessed       *cslDate       `json:"accessed,omitempty"`
	Note           string         `json:"note,omitempty"`
	Custom         map[string]any `json:"custom,omitempty"`
}

// cslName is a split name, or a literal one when the name was never split.
type cslName struct {
	Family  string `json:"family,omitempty"`
	Given   string `json:"given,omitempty"`
	Literal string `json:"literal,omitempty"`
}

type cslDate struct {
	DateParts [][]int `json:"date-parts"`
}

// renderCSLJSON writes the array.
//
// This is the style worth having: it feeds every reference manager, it is
// generated from the record rather than from the BibTeX, so it carries the
// abstract, the categories and the version that BibTeX drops.
func renderCSLJSON(papers []Paper) (string, error) {
	items := make([]cslItem, 0, len(papers))
	for _, p := range papers {
		item := cslItem{
			ID:         "arxiv-" + p.ID,
			Type:       "article",
			Title:      p.Title,
			Publisher:  "arXiv",
			URL:        paperURL(p),
			Number:     "arXiv:" + versionedID(p),
			Abstract:   p.Abstract,
			Categories: p.Categories,
		}
		if p.JournalRef != "" {
			// CSL has no preprint type, so a published paper is a journal
			// article and everything else is the generic article.
			item.Type = "article-journal"
			item.ContainerTitle = p.JournalRef
		}
		item.DOI = p.PublisherDOI
		if item.DOI == "" {
			item.DOI = p.DOI
		}
		for _, a := range p.Authors {
			if a.Keyname == "" {
				item.Author = append(item.Author, cslName{Literal: a.Name})
				continue
			}
			item.Author = append(item.Author, cslName{Family: a.Keyname, Given: a.Forenames})
		}
		if !p.FirstSubmitted.IsZero() {
			t := p.FirstSubmitted
			item.Issued = &cslDate{DateParts: [][]int{{t.Year(), int(t.Month()), t.Day()}}}
		}
		if p.License != "" {
			item.Custom = map[string]any{"license": p.License}
		}
		items = append(items, item)
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// versionedID is "1706.03762v7", or the bare id when the record has no version.
func versionedID(p Paper) string {
	if p.VersionedID != "" {
		return p.VersionedID
	}
	if p.Version > 0 {
		return fmt.Sprintf("%sv%d", p.ID, p.Version)
	}
	return p.ID
}

// trimDot drops a trailing full stop so the style can add its own without
// writing two.
func trimDot(s string) string { return strings.TrimRight(strings.TrimSpace(s), ".") }

// styleNames is every style as a string, in the order the help lists them.
func styleNames() []string {
	out := make([]string, 0, len(Styles))
	for _, s := range Styles {
		out = append(out, string(s))
	}
	return out
}
