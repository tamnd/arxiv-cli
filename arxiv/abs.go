package arxiv

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// absPage is everything the abstract page says, in the shape the model wants.
//
// It is a type of its own rather than a merge straight onto a Paper so the
// parser can be tested against a saved page without a client, and so the merge
// rules sit in one readable place below it.
type absPage struct {
	ID       string
	Version  int
	Title    string
	Abstract string
	// Authors here are surname-comma-given, the third of the three name
	// formats arXiv publishes.
	Authors []Author
	// SubjectNames maps a category code to its human readable name, which is
	// the only place on a paper record the two appear side by side.
	SubjectNames    map[string]string
	PrimaryCategory string
	Categories      []string
	Comment         string
	JournalRef      string
	ReportNo        string
	MSCClass        []string
	ACMClass        []string
	PublisherDOI    string
	License         string
	Submitter       string
	Versions        []Version
	// Withdrawn is set when the newest version in the history carries arXiv's
	// (withdrawn) marker.
	Withdrawn bool
	HasHTML   bool
	HasSource bool
	HTMLURL   string
}

// getAbs fetches and parses the abstract page.
//
// This is the only read in a paper fetch that crosses onto the HTML plane, so
// it costs fifteen seconds of pacing and it is worth knowing that before
// asking for it over a list.
func (c *Client) getAbs(ctx context.Context, id string) (*absPage, string, error) {
	u := absURL(id)
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLPaper)
	if err != nil {
		return nil, u, err
	}
	page, err := parseAbs(resp.Body)
	if err != nil {
		return nil, u, err
	}
	return page, u, nil
}

// parseAbs reads the abstract page.
//
// The citation meta tags carry the title, the authors and the dates, and the
// page body carries the three things nothing else has: the full-text link
// block, the human readable subject names, and the submission history in a
// second unit.
func parseAbs(body []byte) (*absPage, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse abstract page: %w", err)
	}
	page := &absPage{SubjectNames: map[string]string{}}

	meta := func(name string) string {
		return cleanText(doc.Find(`meta[name="`+name+`"]`).AttrOr("content", ""))
	}
	page.ID = meta("citation_arxiv_id")
	page.Title = meta("citation_title")
	page.Abstract = meta("citation_abstract")

	doc.Find(`meta[name="citation_author"]`).Each(func(_ int, s *goquery.Selection) {
		if a, ok := citationAuthor(s.AttrOr("content", "")); ok {
			page.Authors = append(page.Authors, a)
		}
	})

	parseAbsMetatable(doc, page)
	parseAbsFullText(doc, page)
	parseAbsHistory(doc, page)

	// The versioned cite line is the only place the page states which version
	// it is showing, and it states it as a string.
	if v, ok := versionFromText(doc.Find(".arxividv").First().Text()); ok {
		page.Version = v
	}
	return page, nil
}

// citationAuthor reads a citation_author tag, which is "Surname, Given".
//
// The comma is the surface's own split, so the two parts are kept as structured
// names rather than guessed at. A value with no comma is a collaboration or a
// mononym and stays one name.
func citationAuthor(raw string) (Author, bool) {
	raw = cleanText(raw)
	if raw == "" {
		return Author{}, false
	}
	surname, given, ok := strings.Cut(raw, ",")
	if !ok {
		return Author{Name: raw, Via: SurfaceAbs}, true
	}
	surname = strings.TrimSpace(surname)
	given = strings.TrimSpace(given)
	if surname == "" || given == "" {
		return Author{Name: raw, Via: SurfaceAbs}, true
	}
	return Author{
		Name:      given + " " + surname,
		Keyname:   surname,
		Forenames: given,
		Via:       SurfaceAbs,
	}, true
}

// subjectRe pulls the code out of "Computation and Language (cs.CL)".
var subjectRe = regexp.MustCompile(`^(.*)\(([^()]+)\)$`)

// parseAbsMetatable reads the labelled rows: comments, subjects, the journal
// reference, the report number and the DOIs.
func parseAbsMetatable(doc *goquery.Document, page *absPage) {
	doc.Find(".metatable tr").Each(func(_ int, row *goquery.Selection) {
		label := strings.TrimSuffix(cleanText(row.Find("td.label").First().Text()), ":")
		value := row.Find("td").Last()
		switch strings.ToLower(label) {
		case "comments":
			page.Comment = cleanText(value.Text())
		case "subjects":
			parseSubjects(value, page)
		case "journal reference":
			page.JournalRef = cleanText(value.Text())
		case "report number":
			page.ReportNo = cleanText(value.Text())
		case "msc classes", "msc class":
			page.MSCClass = splitClasses(value.Text())
		case "acm classes", "acm class":
			page.ACMClass = splitClasses(value.Text())
		case "doi":
			page.PublisherDOI = cleanText(value.Find("a").First().Text())
		}
	})
}

// parseSubjects reads the subjects row, which is the only place the codes and
// the human readable names appear together on a paper record.
func parseSubjects(cell *goquery.Selection, page *absPage) {
	primary := cleanText(cell.Find(".primary-subject").First().Text())
	for _, part := range strings.Split(cleanText(cell.Text()), ";") {
		part = strings.TrimSpace(part)
		m := subjectRe.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		code := strings.TrimSpace(m[2])
		if code == "" {
			continue
		}
		page.SubjectNames[code] = name
		if !contains(page.Categories, code) {
			page.Categories = append(page.Categories, code)
		}
		if part == primary {
			page.PrimaryCategory = code
		}
	}
	if page.PrimaryCategory == "" && len(page.Categories) > 0 {
		page.PrimaryCategory = page.Categories[0]
	}
	page.Categories = primaryFirst(page.Categories, page.PrimaryCategory)
}

// parseAbsFullText reads the full-text link block and the licence.
//
// The links are a capability list: whether a LaTeXML rendering exists and
// whether TeX source was submitted are facts available on no other surface, and
// they are stored as booleans because the URLs are derivable from the id.
func parseAbsFullText(doc *goquery.Document, page *absPage) {
	block := doc.Find(".full-text")
	block.Find("a").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		switch {
		case strings.Contains(href, "/html/"):
			page.HasHTML = true
			page.HTMLURL = absolute(href)
		case strings.Contains(href, "/src/"), strings.Contains(href, "/e-print/"):
			page.HasSource = true
		}
	})
	page.License = strings.TrimSpace(block.Find(".abs-license a").AttrOr("href", ""))
}

// historyRe matches one line of the submission history: the version, the
// timestamp, the size, and the marker arXiv puts after a version it withdrew.
//
// The marker is its own em element on the page, so it is a fact arXiv states
// rather than something read out of the comment. That distinction matters: the
// comments on withdrawn papers run from "Withdrawn" to four sentences of
// explanation to "v1's main result is withdrawn" on a paper that is still very
// much there, and no amount of prose matching separates those reliably.
var historyRe = regexp.MustCompile(`\[v(\d+)\]\s*(.+?)\s*\(([\d,]+)\s*([KMkm]B)\)\s*(\(withdrawn\))?`)

// absDateLayouts are the submission history's timestamps, which are RFC 1123
// with a UTC zone name and a day that is sometimes space padded.
var absDateLayouts = []string{
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05 MST",
}

// parseAbsHistory reads the submission history block: the submitter and the
// per-version date and size.
func parseAbsHistory(doc *goquery.Document, page *absPage) {
	block := doc.Find(".submission-history")
	if block.Length() == 0 {
		return
	}
	text := block.Text()
	if _, after, ok := strings.Cut(text, "From:"); ok {
		// The submitter's name runs up to the "view email" link, which is the
		// next thing on the line.
		name, _, _ := strings.Cut(after, "[")
		page.Submitter = cleanText(name)
	}
	for _, m := range historyRe.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		v := Version{Version: n, Via: SurfaceAbs}
		if t, ok := parseAbsDate(m[2]); ok {
			v.Date = t
		}
		if size, ok := parseSize(m[3], m[4]); ok {
			v.SizeBytes = size
		}
		v.Withdrawn = m[5] != ""
		page.Versions = append(page.Versions, v)
	}
	sortVersions(page.Versions)
	if n := len(page.Versions); n > 0 {
		// The paper is withdrawn when its newest version is, and not when any
		// version is. A paper withdrawn at v2 and replaced at v3 is a paper that
		// is there.
		page.Withdrawn = page.Versions[n-1].Withdrawn
	}
}

// parseAbsDate reads a submission history timestamp in UTC.
func parseAbsDate(s string) (time.Time, bool) {
	s = cleanText(s)
	for _, layout := range absDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// parseSize normalises "1,102" plus "KB" into bytes.
func parseSize(number, unit string) (int64, bool) {
	n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(number), ",", ""), 10, 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "KB":
		return n * 1024, true
	case "MB":
		return n * 1024 * 1024, true
	}
	return 0, false
}

// versionRe pulls the version out of "arXiv:1706.03762v7 [cs.CL]".
var versionRe = regexp.MustCompile(`arXiv:\S+?v(\d+)`)

func versionFromText(s string) (int, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// absolute turns a site-relative href into a full URL.
func absolute(href string) string {
	if strings.HasPrefix(href, "/") {
		return "https://" + Host + href
	}
	return href
}

// markWithdrawn copies the abstract page's withdrawal markers onto a history
// that came from somewhere else, matching on the version number.
func markWithdrawn(into, from []Version) {
	for _, f := range from {
		if !f.Withdrawn {
			continue
		}
		for i := range into {
			if into[i].Version == f.Version {
				into[i].Withdrawn = true
			}
		}
	}
}

// mergeAbs folds an abstract page into a paper.
//
// The page is the last surface a full read touches, so almost everything it
// carries is already known. It is fetched for the four things that are not:
// the full-text capabilities, the human readable subject names, the browse
// context, and the version history when OAI could not be reached.
func mergeAbs(p *Paper, page *absPage, source string) {
	p.addSurface(SurfaceAbs, source)

	if page.HasHTML {
		p.HasHTML = true
		p.HTMLURL = page.HTMLURL
		p.setVia("has_html", SurfaceAbs)
	}
	if page.HasSource {
		p.HasSource = true
		p.setVia("has_source", SurfaceAbs)
	}
	if p.Submitter == "" && page.Submitter != "" {
		p.Submitter = page.Submitter
		p.setVia("submitter", SurfaceAbs)
	}
	// arXivRaw is preferred for the history because it is on the fast plane and
	// it names the source type, which this page does not. This is the fallback
	// that makes an OAI outage survivable rather than fatal.
	if len(p.Versions) == 0 && len(page.Versions) > 0 {
		p.Versions = page.Versions
		p.setVia("versions", SurfaceAbs)
	} else {
		// arXivRaw won the history, but it has no withdrawal marker at all, so
		// the flag is carried over by version number rather than lost with the
		// rest of the page's copy.
		markWithdrawn(p.Versions, page.Versions)
	}
	if page.Withdrawn && !p.Withdrawn {
		p.Withdrawn = true
		p.setVia("withdrawn", SurfaceAbs)
	}
	if p.License == "" && page.License != "" {
		p.License = page.License
		p.setVia("license", SurfaceAbs)
	}
	if p.Comment == "" && page.Comment != "" {
		p.Comment = page.Comment
		p.setVia("comment", SurfaceAbs)
	}
	if p.JournalRef == "" && page.JournalRef != "" {
		p.JournalRef = page.JournalRef
		p.setVia("journal_ref", SurfaceAbs)
	}
	if p.ReportNo == "" && page.ReportNo != "" {
		p.ReportNo = page.ReportNo
		p.setVia("report_no", SurfaceAbs)
	}
	if p.PublisherDOI == "" && page.PublisherDOI != "" {
		p.PublisherDOI = page.PublisherDOI
		p.setVia("publisher_doi", SurfaceAbs)
	}
	if len(p.MSCClass) == 0 && len(page.MSCClass) > 0 {
		p.MSCClass = page.MSCClass
		p.setVia("msc_class", SurfaceAbs)
	}
	if len(p.ACMClass) == 0 && len(page.ACMClass) > 0 {
		p.ACMClass = page.ACMClass
		p.setVia("acm_class", SurfaceAbs)
	}
	if len(page.SubjectNames) > 0 {
		p.SubjectNames = page.SubjectNames
		p.setVia("subject_names", SurfaceAbs)
	}
	if len(p.Categories) == 0 && len(page.Categories) > 0 {
		p.Categories = page.Categories
		p.PrimaryCategory = page.PrimaryCategory
		p.CrossLists = crossLists(p.Categories, p.PrimaryCategory)
		p.setVia("categories", SurfaceAbs)
	}
	if len(p.Authors) == 0 && len(page.Authors) > 0 {
		p.Authors = page.Authors
		p.AuthorLine = authorLine(p.Authors)
		p.setVia("authors", SurfaceAbs)
	}
	if p.Title == "" {
		p.Title = page.Title
		p.setVia("title", SurfaceAbs)
	}
	if p.Abstract == "" {
		p.Abstract = page.Abstract
		p.setVia("abstract", SurfaceAbs)
	}
	if p.ID == "" && page.ID != "" {
		if id, err := axid.Parse(page.ID); err == nil {
			p.ID = id.Canonical
			p.Style = string(id.Style)
			p.DOI = id.DOI()
			p.OAIID = id.OAI()
		}
	}
}
