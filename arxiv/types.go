package arxiv

import (
	"encoding/xml"
	"strings"
	"time"
)

// Paper is the canonical record emitted for every search, paper, and author result.
// JSON keys are stable.
type Paper struct {
	ID        string   `json:"id"        kit:"id" table:"id"`
	Title     string   `json:"title"              table:"title"`
	Authors   []string `json:"authors"            table:"authors"`
	Category  string   `json:"category"           table:"category"`
	Published string   `json:"published"          table:"date"`
	Updated   string   `json:"updated"            table:"-"`
	Summary   string   `json:"summary"            table:"-"`
	URL       string   `json:"url"                table:"abs,url"`
	PDF       string   `json:"pdf"                table:"pdf,url"`
}

// Category is the static record used by the categories command.
type Category struct {
	Code        string `json:"code" kit:"id"`
	Description string `json:"description"`
}

// ─── wire types from arXiv Atom XML ─────────────────────────────────────────

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Total   int         `xml:"totalResults"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID              string     `xml:"id"`
	Title           string     `xml:"title"`
	Summary         string     `xml:"summary"`
	Published       string     `xml:"published"`
	Updated         string     `xml:"updated"`
	Authors         []atomName `xml:"author"`
	Links           []atomLink `xml:"link"`
	PrimaryCategory atomCat    `xml:"primary_category"`
	Categories      []atomCat  `xml:"category"`
	Comment         string     `xml:"comment"`
	DOI             string     `xml:"doi"`
	JournalRef      string     `xml:"journal_ref"`
}

type atomName struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr"`
	Title string `xml:"title,attr"`
}

type atomCat struct {
	Term string `xml:"term,attr"`
}

// ─── normalization helpers ────────────────────────────────────────────────────

// stripArxivID normalizes a raw id element value to a bare NNNN.NNNNN string.
// Input may be http://arxiv.org/abs/1706.03762v7 or just 1706.03762.
func stripArxivID(raw string) string {
	// strip URL prefix
	raw = strings.TrimSpace(raw)
	for _, prefix := range []string{
		"https://arxiv.org/abs/",
		"http://arxiv.org/abs/",
		"https://export.arxiv.org/abs/",
		"http://export.arxiv.org/abs/",
	} {
		if strings.HasPrefix(raw, prefix) {
			raw = raw[len(prefix):]
			break
		}
	}
	// strip version suffix vN
	if idx := strings.LastIndex(raw, "v"); idx >= 0 {
		suffix := raw[idx+1:]
		allDigits := len(suffix) > 0
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			raw = raw[:idx]
		}
	}
	return raw
}

// cleanText trims outer whitespace and collapses embedded newlines and
// adjacent spaces to a single space.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	// replace newlines and tabs with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// collapse runs of spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// authorNames extracts the name strings from an entry's author list.
func authorNames(authors []atomName) []string {
	out := make([]string, 0, len(authors))
	for _, a := range authors {
		n := strings.TrimSpace(a.Name)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// absURL returns the canonical https://arxiv.org/abs/ID URL (version stripped).
func absURL(id string) string {
	return "https://arxiv.org/abs/" + id
}

// pdfURL returns the canonical https://arxiv.org/pdf/ID URL (version stripped).
func pdfURL(id string) string {
	return "https://arxiv.org/pdf/" + id
}

// stripPDFID extracts the bare arXiv id from a PDF URL like
// http://arxiv.org/pdf/1706.03762v7. Falls back to stripArxivID.
func stripPDFID(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, prefix := range []string{
		"https://arxiv.org/pdf/",
		"http://arxiv.org/pdf/",
		"https://export.arxiv.org/pdf/",
		"http://export.arxiv.org/pdf/",
	} {
		if strings.HasPrefix(raw, prefix) {
			raw = raw[len(prefix):]
			// strip version suffix
			return stripArxivID(raw)
		}
	}
	// fall back: try /abs/ prefix stripping
	return stripArxivID(raw)
}

// parseRFC3339Date parses an RFC 3339 date string and returns a date-only form.
// If parsing fails, the original string is returned unchanged.
func parseRFC3339Date(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("2006-01-02")
}

// entryToPaper converts a wire atomEntry to a Paper record.
func entryToPaper(e atomEntry) Paper {
	id := stripArxivID(e.ID)

	// find HTML link and PDF link from link elements
	urlStr := absURL(id)
	pdfStr := pdfURL(id)
	for _, lnk := range e.Links {
		switch lnk.Type {
		case "text/html":
			if lnk.Href != "" {
				// strip version from URL as well
				bare := stripArxivID(lnk.Href)
				urlStr = absURL(bare)
			}
		case "application/pdf":
			if lnk.Href != "" {
				bare := stripPDFID(lnk.Href)
				pdfStr = pdfURL(bare)
			}
		}
	}

	// primary category: prefer explicit primary_category element, fall back to
	// first category element.
	cat := e.PrimaryCategory.Term
	if cat == "" && len(e.Categories) > 0 {
		cat = e.Categories[0].Term
	}

	return Paper{
		ID:        id,
		Title:     cleanText(e.Title),
		Authors:   authorNames(e.Authors),
		Category:  cat,
		Published: parseRFC3339Date(e.Published),
		Updated:   parseRFC3339Date(e.Updated),
		Summary:   cleanText(e.Summary),
		URL:       urlStr,
		PDF:       pdfStr,
	}
}
