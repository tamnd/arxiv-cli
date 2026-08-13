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
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// This file is s4, the category listing. It answers a different question from
// search: it is arXiv's own listing for a month, in arXiv's own order, which is
// what somebody browsing the archive sees, and it is the only surface that
// publishes a per month total.
//
// It has no ten thousand result window, so it is also the only way to walk a
// whole month of a busy category. That walk is on the fifteen second plane, and
// at 2000 rows a page a month of cs.CL is two requests rather than the
// eighty seven a twenty five row page would be.

// listBase is the listing root. A listing URL is this plus the category, a
// slash, and either a YYYY-MM month or the word recent.
const listBase = "https://" + Host + "/list/"

// listSizes are the page sizes arXiv accepts, and they are its own list rather
// than a guess: skip=0&show=7 answers HTTP 400 with the body "Invalid show
// value. Valid values: 25, 50, 100, 250, 500, 1000, 2000", measured 2026-08-13.
// A --show it will refuse is refused here instead, one request earlier.
var listSizes = []int{25, 50, 100, 250, 500, 1000, 2000}

// listDefaultShow is the size arXiv's own pages use, and listWalkShow is the
// biggest it offers, which is what an --all walk asks for.
const (
	listDefaultShow = 50
	listWalkShow    = 2000
)

// monthRe is the only date form the listing takes. The four digit short form
// that older guides document is gone: /list/cs.CL/2601 answers 404 and
// /list/cs.CL/2026-01 answers 200, measured 2026-08-13.
var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// shortMonthRe is the form people will type anyway, so the refusal can say what
// to type instead rather than just no.
var shortMonthRe = regexp.MustCompile(`^(\d{2})(\d{2})$`)

// ListOptions is one listing read.
type ListOptions struct {
	// Category is a category or archive code, cs.CL or hep-th.
	Category string
	// Month is YYYY-MM. Empty means the recent listing.
	Month string
	// Recent asks for the last few announcement days instead of a month.
	Recent bool
	// Skip and Show are arXiv's own paging parameters, kept under their own
	// names because they are what the URL carries and what the page prints.
	Skip int
	Show int
	// All walks every page at the plane's pace.
	All bool
}

// period is the path segment after the category: a month, or "recent".
func (o ListOptions) period() string {
	if o.Month == "" {
		return "recent"
	}
	return o.Month
}

// URL builds one listing request.
func (o ListOptions) URL(skip, show int) string {
	return fmt.Sprintf("%s%s/%s?skip=%d&show=%d", listBase, o.Category, o.period(), skip, show)
}

// validate checks everything that can be checked without a request.
//
// Each of these costs fifteen seconds to find out the slow way, and two of them
// come back as an HTTP 400 page that says nothing useful about which parameter
// was wrong.
func (o *ListOptions) validate() error {
	o.Category = strings.TrimSpace(o.Category)
	if o.Category == "" {
		return errs.Usage("give a category code, such as cs.CL")
	}
	if err := checkCategories([]string{o.Category}); err != nil {
		return err
	}

	o.Month = strings.TrimSpace(o.Month)
	if o.Month != "" && o.Recent {
		return errs.Usage("the month %s and --recent are two different pages, so ask for one of them", o.Month)
	}
	if o.Month != "" && !monthRe.MatchString(o.Month) {
		if m := shortMonthRe.FindStringSubmatch(o.Month); m != nil {
			// 2601 is the form the id itself uses and the form older guides
			// document, and arXiv answers it with a 404 rather than a redirect.
			return errs.Usage("the month wants four digits of year and two of month, so write 20%s-%s rather than %s", m[1], m[2], o.Month)
		}
		return errs.Usage("the month wants the form 2026-01, and %s is not it", o.Month)
	}

	if o.Skip < 0 {
		return errs.Usage("--skip counts from zero and cannot be negative")
	}
	if o.Show == 0 {
		o.Show = listDefaultShow
		if o.All {
			o.Show = listWalkShow
		}
	}
	for _, n := range listSizes {
		if o.Show == n {
			return nil
		}
	}
	return errs.Usage("arxiv takes %s entries a page and nothing else, so --show %d would come back as an HTTP 400",
		strings.Join(listSizeNames(), ", "), o.Show)
}

// listSizeNames is the accepted sizes as strings, for the message above.
func listSizeNames() []string {
	out := make([]string, len(listSizes))
	for i, n := range listSizes {
		out[i] = strconv.Itoa(n)
	}
	return out
}

// ListStream reads a listing and streams the papers on it.
//
// A row is not a paper read: there is no abstract on the page and no timestamp
// finer than a day, and every record says so in its missed field with the
// command that would fill the gap.
func (c *Client) ListStream(ctx context.Context, o ListOptions, emit func(*Paper) error) error {
	if err := o.validate(); err != nil {
		return err
	}

	skip, show := o.Skip, o.Show
	planned := false
	for {
		u := o.URL(skip, show)
		c.logf(1, "GET %s", u)
		page, err := c.getList(ctx, u)
		if err != nil {
			return err
		}
		if !planned && o.All {
			c.notice("%s", listPlanLine(page.Total-skip, show))
			planned = true
		}
		at := c.now()
		for i := range page.Rows {
			paper := listToPaper(page.Rows[i], u, at)
			c.noteUnknownCategories(paper.Categories...)
			if err := emit(&paper); err != nil {
				return err
			}
		}
		skip += len(page.Rows)
		// A short page is the end of the listing, and the total is the other
		// end marker. Either one alone would loop forever on the day the other
		// one is wrong.
		if !o.All || len(page.Rows) == 0 || len(page.Rows) < show || skip >= page.Total {
			return nil
		}
	}
}

// listPlanLine is the sentence an --all walk prints before it settles in, so
// nobody watches a blank terminal wondering whether it is working.
func listPlanLine(remaining, show int) string {
	if remaining < 0 {
		remaining = 0
	}
	pages := (remaining + show - 1) / show
	if pages < 1 {
		pages = 1
	}
	requests := "requests"
	if pages == 1 {
		requests = "request"
	}
	return fmt.Sprintf("%d entries, %d %s at the fifteen second pace, so about %s",
		remaining, pages, requests, (time.Duration(pages) * HTMLPlane.Pace).Round(time.Second))
}

// getList fetches and parses one listing page.
//
// A month that has ended is fixed forever and a month in progress gains rows
// every announcement, but both are cached for a day: the difference is one
// stale afternoon on a page that costs fifteen seconds to refresh.
func (c *Client) getList(ctx context.Context, u string) (*listPage, error) {
	resp, err := c.fetch(ctx, u, TTLListing)
	if err != nil {
		return nil, err
	}
	return parseList(resp.Body)
}

// ─── the page ────────────────────────────────────────────────────────────────

// listPage is one listing page in the shape the page itself has.
type listPage struct {
	// Name is the category's own name, off the page's heading.
	Name string
	// Total is the number in "Total of 2168 entries", which is the whole
	// listing and not the page.
	Total int
	Rows  []listRow
}

// listRow is one <dt>/<dd> pair.
type listRow struct {
	ID      string
	Version int
	AbsURL  string
	PDFURL  string
	HTMLURL string
	HasHTML bool

	Title      string
	Authors    []string
	Comment    string
	JournalRef string
	ReportNo   string
	MSCClass   []string
	ACMClass   []string

	PrimaryCategory string
	Categories      []string
	SubjectNames    map[string]string

	// Announced is the day heading this row sat under, which only the recent
	// listing has.
	Announced time.Time
	// Extra is every labelled value with no field of its own.
	Extra map[string]string
}

var (
	// listTotalRe reads the count off the paging line. The number is grouped
	// with commas on a big archive.
	listTotalRe = regexp.MustCompile(`Total of ([\d,]+) entries`)
	// listDayRe reads a day heading: "Thu, 13 Aug 2026 (showing first 50 of 92
	// entries )". The bracket is the page's own note about paging and is not
	// part of the date.
	listDayRe = regexp.MustCompile(`^\w+, (\d{1,2} \w+ \d{4})`)
	// listVersionRe pulls the version off the html link, which is the only
	// element on a listing row that carries one. The abstract link does not.
	listVersionRe = regexp.MustCompile(`v(\d+)$`)
	// listSubjectRe reads "Computation and Language (cs.CL)".
	listSubjectRe = regexp.MustCompile(`^(.*)\s+\(([^()]+)\)$`)
)

// parseList reads a listing page.
//
// The rows are a definition list with the headings inline, so the parser walks
// the list's own children in order: an h3 sets the day the rows under it were
// announced, a dt opens a row and the dd after it fills the row in. Pairing dts
// and dds by index would work until one row is missing its dd.
func parseList(body []byte) (*listPage, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse listing page: %w", err)
	}
	page := &listPage{Name: cleanText(doc.Find("#dlpage h1").First().Text())}

	if m := listTotalRe.FindStringSubmatch(cleanText(doc.Find("div.paging").First().Text())); m != nil {
		page.Total, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	}

	list := doc.Find("dl#articles")
	if list.Length() == 0 {
		// A month arXiv has nothing for still renders the page, so an empty
		// listing is an empty listing and not a parse failure. A page with no
		// list at all is something else and says so.
		if page.Total == 0 && doc.Find("#dlpage").Length() > 0 {
			return page, nil
		}
		return nil, fmt.Errorf("parse listing page: no article list on it")
	}

	var day time.Time
	list.Children().Each(func(_ int, s *goquery.Selection) {
		switch {
		case s.Is("h3"):
			day = listDay(cleanText(s.Text()))
		case s.Is("dt"):
			row := parseListDT(s)
			row.Announced = day
			page.Rows = append(page.Rows, row)
		case s.Is("dd"):
			if len(page.Rows) == 0 {
				return
			}
			parseListDD(s, &page.Rows[len(page.Rows)-1])
		}
	})
	// The paging line is the only place the total is published, and a month
	// that fits on one page still has one, so a missing total is a page whose
	// shape has changed rather than a small month.
	if page.Total == 0 {
		page.Total = len(page.Rows)
	}
	return page, nil
}

// listDay reads a day heading into a date.
func listDay(s string) time.Time {
	m := listDayRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}
	}
	t, err := time.Parse("2 Jan 2006", m[1])
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// parseListDT reads the identifier line: the id, and the format links beside it.
func parseListDT(dt *goquery.Selection) listRow {
	r := listRow{SubjectNames: map[string]string{}}
	dt.Find("a").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		switch strings.ToLower(cleanText(a.AttrOr("title", ""))) {
		case "abstract":
			r.AbsURL = absoluteURL(href)
			if id, err := axid.Parse(cleanText(a.Text())); err == nil {
				r.ID = id.Canonical
			}
		case "download pdf":
			r.PDFURL = absoluteURL(href)
		case "view html":
			r.HasHTML = true
			r.HTMLURL = absoluteURL(href)
			if m := listVersionRe.FindStringSubmatch(href); m != nil {
				r.Version, _ = strconv.Atoi(m[1])
			}
		}
	})
	return r
}

// parseListDD reads the meta block: a set of labelled divs, and the author list
// which is the one block with no label on it.
//
// The switch keys on the descriptor span rather than on the div class because
// the label is the thing arXiv is consistent about, and a label with no field
// of its own is kept in Extra rather than dropped.
func parseListDD(dd *goquery.Selection, r *listRow) {
	dd.Find("div.meta > div").Each(func(_ int, div *goquery.Selection) {
		if div.HasClass("list-authors") {
			div.Find("a").Each(func(_ int, a *goquery.Selection) {
				if name := cleanText(a.Text()); name != "" {
					r.Authors = append(r.Authors, name)
				}
			})
			return
		}
		label := strings.TrimSuffix(cleanText(div.Find("span.descriptor").First().Text()), ":")
		if label == "" {
			return
		}
		value := cleanText(strings.TrimPrefix(cleanText(div.Text()), label+":"))
		if value == "" {
			return
		}
		switch strings.ToLower(label) {
		case "title":
			r.Title = value
		case "comments":
			r.Comment = value
		case "journal-ref":
			r.JournalRef = value
		case "report-no":
			r.ReportNo = value
		case "msc-class", "msc classes":
			r.MSCClass = splitClasses(value)
		case "acm-class", "acm classes":
			r.ACMClass = splitClasses(value)
		case "subjects":
			parseListSubjects(div, value, r)
		default:
			if r.Extra == nil {
				r.Extra = map[string]string{}
			}
			r.Extra[label] = value
		}
	})
}

// parseListSubjects reads the subject line.
//
// It is one primary subject and any number of cross lists, joined with
// semicolons: "Algebraic Geometry (math.AG); Representation Theory (math.RT)".
// The primary is marked up as its own span, so it is read from the markup and
// not from the position, and the codes come with their names, which makes this
// one of the two surfaces that carry a slice of the taxonomy inline.
func parseListSubjects(div *goquery.Selection, value string, r *listRow) {
	primary := cleanText(div.Find("span.primary-subject").First().Text())
	for _, part := range strings.Split(value, ";") {
		name, code, ok := splitSubject(part)
		if !ok {
			continue
		}
		if !contains(r.Categories, code) {
			r.Categories = append(r.Categories, code)
		}
		r.SubjectNames[code] = name
		if cleanText(part) == primary || (primary == "" && r.PrimaryCategory == "") {
			r.PrimaryCategory = code
		}
	}
}

// splitSubject reads "Computation and Language (cs.CL)" into its two halves.
func splitSubject(s string) (name, code string, ok bool) {
	m := listSubjectRe.FindStringSubmatch(cleanText(s))
	if m == nil {
		return "", "", false
	}
	return cleanText(m[1]), cleanText(m[2]), true
}

// absoluteURL makes a listing's site relative href absolute, so a record
// carries a link that can be opened.
func absoluteURL(href string) string {
	if href == "" || strings.HasPrefix(href, "http") {
		return href
	}
	return "https://" + Host + "/" + strings.TrimPrefix(href, "/")
}

// listToPaper maps a row onto the record every other surface also produces.
func listToPaper(r listRow, source string, at time.Time) Paper {
	p := Paper{
		Envelope: Envelope{
			Kind:        "paper",
			RetrievedAt: at.UTC(),
		},
		ID:         r.ID,
		Version:    r.Version,
		Title:      r.Title,
		Comment:    r.Comment,
		JournalRef: r.JournalRef,
		ReportNo:   r.ReportNo,
		MSCClass:   r.MSCClass,
		ACMClass:   r.ACMClass,
		// A listing row is the current version of a paper: arXiv lists a paper
		// once, under the id it was registered with, and links the version it
		// is showing.
		IsLatest: true,
		URL:      r.AbsURL,
		PDFURL:   r.PDFURL,
		HTMLURL:  r.HTMLURL,
		HasHTML:  r.HasHTML,
		Extra:    r.Extra,
		Depth:    string(DepthQuick),
	}
	p.addSurface(SurfaceList, source)

	if id, err := axid.Parse(r.ID); err == nil {
		p.Style = string(id.Style)
		p.OAIID = id.OAI()
		p.DOI = id.DOI()
		id.Version = r.Version
		p.VersionedID = id.Versioned()
		if p.URL == "" {
			p.URL = id.AbsURL()
		}
		if p.PDFURL == "" {
			p.PDFURL = id.PDFURL()
		}
	}

	for _, name := range r.Authors {
		p.Authors = append(p.Authors, Author{Name: name, Via: SurfaceList})
	}
	p.AuthorLine = authorLine(p.Authors)

	if len(r.Categories) > 0 {
		p.Categories = r.Categories
		p.PrimaryCategory = r.PrimaryCategory
		if p.PrimaryCategory == "" {
			p.PrimaryCategory = r.Categories[0]
		}
		p.CrossLists = crossLists(p.Categories, p.PrimaryCategory)
		p.SubjectNames = r.SubjectNames
		p.setVia("categories", SurfaceList)
		p.setVia("subject_names", SurfaceList)
	}

	// The recent listing groups its rows under the day they were announced,
	// which is a fact about the announcement and not about the submission. A
	// monthly listing has no such heading: it is the month of the id, which is
	// when the paper was registered, so nothing is claimed about it here.
	if !r.Announced.IsZero() {
		p.Announced = r.Announced
		p.setVia("announced", SurfaceList)
	}

	for field, value := range map[string]string{
		"title":       p.Title,
		"comment":     p.Comment,
		"journal_ref": p.JournalRef,
		"report_no":   p.ReportNo,
	} {
		if value != "" {
			p.setVia(field, SurfaceList)
		}
	}

	// The depth ladder describes a paper read and a listing row is not one, so
	// the missed lines are written here. The listing publishes no abstract and
	// no timestamp at all, which is the gap worth naming.
	p.Missed = []string{
		"a listing row has no abstract and no submission time; arxiv paper " + r.ID + " reads both",
		"the licence, the submitter and the version history were not read; arxiv paper " + r.ID + " --depth full reads them",
	}
	return p
}
