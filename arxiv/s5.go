package arxiv

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// This file is the search UI, s5, which is the only surface that answers for
// seven fields the export API does not index. It is a whole second search
// implementation, and it exists because `arxiv search --orcid 0000-0002-0609-9836`
// has no other route: the API has no orcid prefix and never will.
//
// A read here costs fifteen seconds of pacing and about 250 KB for fifty
// results, against three seconds and 30 KB on the API, so nothing comes here
// unless the query names a field that forces it.

// s5Base is the search UI, and s5Advanced is the form that takes more than one
// field at a time.
const (
	s5Base     = "https://arxiv.org/search/"
	s5Advanced = "https://arxiv.org/search/advanced"
)

// s5Window is the result window, the same rule the API has and measured the
// same way. start=9800&size=200 answers 200 results and start=10000 answers
// HTTP 400, so start + size has to stay inside ten thousand.
const s5Window = 10000

// s5Sizes are the page sizes the form itself offers. A size outside this list
// is not obviously refused, but sending one arXiv's own UI never sends is how a
// scraper gets noticed, and 200 is the biggest of them anyway.
var s5Sizes = []int{25, 50, 100, 200}

// The search UI's field names. They are not the API's prefixes, they are not
// spelled like them, and the two lists are not the same length: doc 01 section
// 5.1 has all sixteen values with the seven that only exist here.
const (
	s5All        = "all"
	s5Title      = "title"
	s5Author     = "author"
	s5Abstract   = "abstract"
	s5Comments   = "comments"
	s5JournalRef = "journal_ref"
	s5ReportNum  = "report_num"
	s5ACMClass   = "acm_class"
	s5MSCClass   = "msc_class"
	s5DOI        = "doi"
	s5ORCID      = "orcid"
	s5AuthorID   = "author_id"
	s5License    = "license"
	s5FullText   = "full_text"
)

// s5Term is one row of the advanced form: a field and what to look for in it.
type s5Term struct {
	Field string
	Term  string
}

// s5Plan is a search that has to go to the HTML plane, already validated.
type s5Plan struct {
	// Terms are AND'd, in the order the flags are documented in.
	Terms []s5Term
	// Simple says to use the one-field form rather than the advanced one.
	// Only --license needs it: the advanced form has no licence row, measured
	// by sending one and getting the empty form back.
	Simple bool
	// From and To are yyyy-mm-dd. To is exclusive on arXiv's side, which is not
	// what our --to means, so it is converted on the way in.
	From string
	To   string
	// Order is the form's own value, "" for relevance.
	Order string
	Limit int
	All   bool
}

// s5Only are the fields that force a query onto this plane, in the order their
// flags are listed in help.
var s5Only = []string{"--acm-class", "--msc-class", "--doi", "--orcid", "--license", "--author-id", "--full-text"}

// wanted reports whether opts names any field only the search UI has.
func wantsS5(o SearchOptions) bool {
	return o.ACMClass != "" || o.MSCClass != "" || o.DOI != "" || o.ORCID != "" ||
		o.License != "" || o.AuthorID != "" || o.FullText != ""
}

// buildS5 turns options into a plan for the search UI, or refuses.
//
// The refusals are the interesting part of this function. Each one is a thing
// arXiv's own UI cannot do either, and the message says which, because a user
// who is told "no" without a reason will just try it again tomorrow.
func buildS5(o SearchOptions, sort Sort, order Order) (*s5Plan, error) {
	if o.FullText != "" {
		// Measured 2026-08-13: /search/?searchtype=full_text 301s to
		// search.arxiv.org, a different host running the old full text engine,
		// and that host's robots.txt is "User-agent: * / Disallow: /". It is
		// not ours to crawl, and the whole pacing argument in doc 02 section 5
		// falls apart the moment this tool ignores a host that said no.
		return nil, errs.Usage("full text search lives on search.arxiv.org, whose robots.txt disallows every path for every client, so this tool will not read it; open %s in a browser instead",
			s5Base+"?"+url.Values{"searchtype": {s5FullText}, "query": {o.FullText}}.Encode())
	}
	if o.Raw != "" {
		return nil, errs.Usage("the search UI does not speak the export API's query grammar, so --raw cannot be combined with %s",
			strings.Join(s5Only[:len(s5Only)-1], ", "))
	}
	if len(o.Categories) > 0 {
		// Measured 2026-08-13: the advanced form's category row is
		// cross_list_category, and cs.CL through it returns 693 papers for
		// January 2026 against the API's 2237, with not one of the first
		// twenty five having cs.CL as its primary. It matches cross lists
		// only. Sending it anyway would answer a different question quietly.
		return nil, errs.Usage("the search UI's only category row matches cross listed categories and not primary ones, so --cat cannot be combined with %s without quietly answering a narrower question than the one you asked",
			strings.Join(s5Only[:len(s5Only)-1], ", "))
	}
	if o.UpdatedFrom != "" || o.UpdatedTo != "" {
		return nil, errs.Usage("the search UI filters on the submission and announcement dates only, so --updated-from and --updated-to cannot be combined with %s",
			strings.Join(s5Only[:len(s5Only)-1], ", "))
	}
	if sort == SortUpdated {
		return nil, errs.Usage("the search UI sorts by announcement date, submission date and relevance, and has no last updated ordering, so --sort updated cannot be combined with %s",
			strings.Join(s5Only[:len(s5Only)-1], ", "))
	}

	p := &s5Plan{Limit: o.Limit, All: o.All}
	for _, t := range []s5Term{
		{s5All, o.Query},
		{s5Title, o.Title},
		{s5Author, o.Author},
		{s5Abstract, o.Abstract},
		{s5Comments, o.Comment},
		{s5JournalRef, o.Journal},
		{s5ReportNum, o.Report},
		{s5ACMClass, o.ACMClass},
		{s5MSCClass, o.MSCClass},
		{s5DOI, o.DOI},
		{s5ORCID, o.ORCID},
		{s5AuthorID, o.AuthorID},
		{s5License, o.License},
	} {
		if v := strings.TrimSpace(t.Term); v != "" {
			p.Terms = append(p.Terms, s5Term{Field: t.Field, Term: v})
		}
	}
	if o.License != "" {
		if len(p.Terms) > 1 {
			return nil, errs.Usage("the licence is the one field the advanced form does not carry, so --license goes through the single field form and cannot be combined with another term; run it as a search of its own")
		}
		p.Simple = true
	}

	if err := p.dates(o); err != nil {
		return nil, err
	}
	p.Order = s5Order(sort, order)
	return p, nil
}

// dates converts the two date flags into the form's own range.
//
// arXiv's to_date is exclusive: cs.CL cross lists for 2026-01-01 to 2026-02-01
// answered 693 and the same range ending 2026-01-31 answered 666, so the last
// day is dropped. Our --to means the end of the period it names, so the bound
// is pushed on by a day on the way out.
func (p *s5Plan) dates(o SearchOptions) error {
	if strings.TrimSpace(o.From) == "" && strings.TrimSpace(o.To) == "" {
		return nil
	}
	if s := strings.TrimSpace(o.From); s != "" {
		t, err := ParseBound(s, false)
		if err != nil {
			return errs.Usage("the --from value %s", err.Error())
		}
		p.From = t.Format("2006-01-02")
	}
	if s := strings.TrimSpace(o.To); s != "" {
		t, err := ParseBound(s, true)
		if err != nil {
			return errs.Usage("the --to value %s", err.Error())
		}
		p.To = t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	if p.From != "" && p.To != "" && p.From > p.To {
		return errs.Usage("the range --from to --to ends before it starts")
	}
	return nil
}

// s5Order maps our two ordering flags onto the form's single one.
func s5Order(sort Sort, order Order) string {
	if sort != SortSubmitted {
		// The form's relevance ordering is the empty string, which is also its
		// default, so this is one value and not two.
		return ""
	}
	if order == Ascending {
		return "submitted_date"
	}
	return "-submitted_date"
}

// String renders the query the way the page's own heading does, so the line
// `arxiv count` prints is the line arXiv prints back.
func (p *s5Plan) String() string {
	parts := make([]string, 0, len(p.Terms)+1)
	for _, t := range p.Terms {
		parts = append(parts, t.Field+":"+t.Term)
	}
	if p.From != "" || p.To != "" {
		parts = append(parts, "submitted_date:["+p.From+" TO "+p.To+")")
	}
	return strings.Join(parts, " AND ")
}

// URL builds one request.
func (p *s5Plan) URL(start, size int) string {
	v := url.Values{}
	if p.Simple {
		v.Set("searchtype", p.Terms[0].Field)
		v.Set("query", p.Terms[0].Term)
	} else {
		v.Set("advanced", "")
		for i, t := range p.Terms {
			n := strconv.Itoa(i)
			// The operator is on every row including the first, which is what
			// the form itself submits, and AND is the only one this tool builds
			// because every flag it has is a narrowing one.
			v.Set("terms-"+n+"-operator", "AND")
			v.Set("terms-"+n+"-term", t.Term)
			v.Set("terms-"+n+"-field", t.Field)
		}
		if p.From != "" || p.To != "" {
			v.Set("date-filter_by", "date_range")
			v.Set("date-date_type", "submitted_date")
			v.Set("date-from_date", p.From)
			v.Set("date-to_date", p.To)
		}
	}
	v.Set("start", strconv.Itoa(start))
	v.Set("size", strconv.Itoa(size))
	if p.Order != "" {
		v.Set("order", p.Order)
	}
	base := s5Advanced
	if p.Simple {
		base = s5Base
	}
	return base + "?" + v.Encode()
}

// s5Size picks a page size from the four the form offers.
//
// A search for ten results asks for twenty five rather than two hundred,
// because a page here is five kilobytes per result and there is no reason to
// pull a quarter of a megabyte to print ten lines.
func s5Size(limit int) int {
	if limit <= 0 {
		return s5Sizes[len(s5Sizes)-1]
	}
	for _, n := range s5Sizes {
		if limit <= n {
			return n
		}
	}
	return s5Sizes[len(s5Sizes)-1]
}

// searchS5 runs a search on the HTML plane and streams what comes back.
func (c *Client) searchS5(ctx context.Context, p searchPlan, emit func(*Paper) error) error {
	h := p.HTML
	limit := p.limit()
	size := s5Size(limit)
	c.logf(1, "query: %s", h)
	c.logf(1, "the query names a field the export API does not index, so it goes to the search UI on the fifteen second plane")

	sent, planned := 0, false
	for start := 0; ; start += size {
		if start+size > s5Window {
			return c.s5Truncated(start, emit)
		}
		u := h.URL(start, size)
		page, err := c.getS5(ctx, u)
		if err != nil {
			return err
		}
		if !planned && h.All {
			c.notice("%s", s5PlanLine(page.Total, size))
			planned = true
		}
		at := c.now()
		for i := range page.Results {
			if limit > 0 && sent >= limit {
				return nil
			}
			paper := s5ToPaper(page.Results[i], u, at)
			c.noteUnknownCategories(paper.Categories...)
			sent++
			if err := emit(&paper); err != nil {
				return err
			}
		}
		// A short page is the end of the results, the same end marker the API
		// walk trusts, and it saves the empty request that would follow.
		if len(page.Results) < size {
			return nil
		}
		if limit > 0 && sent >= limit {
			return nil
		}
	}
}

// s5Truncated stops a walk at the window and says so out loud rather than
// returning quietly short. Doc 00 principle four: no silent caps.
func (c *Client) s5Truncated(start int, emit func(*Paper) error) error {
	c.notice("stopping at %d results: the search UI will not page past %d, and slicing by date is only available on the export API",
		start, s5Window)
	return nil
}

// s5PlanLine is the sentence a walk prints before it settles in.
func s5PlanLine(total, size int) string {
	pages := (min(total, s5Window) + size - 1) / size
	line := fmt.Sprintf("%d results, %d requests at the fifteen second pace, so about %s",
		total, pages, (time.Duration(pages) * HTMLPlane.Pace).Round(time.Second))
	if total > s5Window {
		line += fmt.Sprintf("; %d of them are past the %d result window and cannot be reached", total-s5Window, s5Window)
	}
	return line
}

// countS5 answers `arxiv count` for a query that has to go to the HTML plane.
//
// The count is on the page's own heading, so it costs one request like the API
// count does, and it asks for the smallest page the form offers because the
// results themselves are thrown away.
func (c *Client) countS5(ctx context.Context, p searchPlan) (Count, error) {
	h := p.HTML
	u := h.URL(0, s5Sizes[0])
	c.logf(1, "query: %s", h)
	page, err := c.getS5(ctx, u)
	if err != nil {
		return Count{}, err
	}
	out := Count{Query: h.String(), Total: page.Total}
	out.Kind = "count"
	out.RetrievedAt = c.now()
	out.addSurface(SurfaceSearch, u)
	return out, nil
}

// getS5 fetches and parses one page of results.
func (c *Client) getS5(ctx context.Context, u string) (*s5Page, error) {
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLSearch)
	if err != nil {
		return nil, err
	}
	return parseS5(resp.Body)
}

// ─── the page ────────────────────────────────────────────────────────────────

// s5Page is one page of results, in the shape the page itself has.
type s5Page struct {
	// Total is the number in "Showing 1-50 of 130 results", which is the whole
	// result set and not the page.
	Total   int
	Results []s5Result
}

// s5Result is one <li class="arxiv-result">, kept whole.
//
// It is a type of its own rather than a Paper so the parser can be tested
// against a saved page with no client anywhere near it, which is the same
// reason absPage exists.
type s5Result struct {
	ID           string
	Version      int
	AbsURL       string
	PDFURL       string
	Title        string
	Abstract     string
	Authors      []string
	Categories   []string
	SubjectNames map[string]string
	Comment      string
	JournalRef   string
	ReportNo     string
	MSCClass     []string
	ACMClass     []string
	// Submitted is the current version's date and FirstSubmitted is v1's. The
	// page prints both, and only prints the second when they differ.
	Submitted      time.Time
	FirstSubmitted time.Time
	// AnnouncedMonth is "July 2026" as the page writes it.
	AnnouncedMonth string
	// Hits are the query terms arXiv highlighted in this result.
	Hits []string
}

var (
	// totalRe reads the heading. The number is grouped with commas.
	s5TotalRe = regexp.MustCompile(`of ([\d,]+) results`)
	// s5DateRe reads the stamp line, whose middle clause is only there for a
	// paper with more than one version.
	s5DateRe = regexp.MustCompile(`Submitted\s+(\d{1,2} \w+, \d{4});(?:\s*v1 submitted\s+(\d{1,2} \w+, \d{4});)?\s*originally announced\s+(\w+ \d{4})`)
	// s5VersionRe pulls the version out of an element id, which is the only
	// place on the page that carries one. The visible link does not.
	s5VersionRe = regexp.MustCompile(`v(\d+)-abstract`)
)

// parseS5 reads a results page.
func parseS5(body []byte) (*s5Page, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse search page: %w", err)
	}
	page := &s5Page{}

	// A search with no hits has no heading and says so in a sentence instead,
	// which is a zero and not a parse failure.
	heading := cleanText(doc.Find("h1.title").First().Text())
	if m := s5TotalRe.FindStringSubmatch(heading); m != nil {
		page.Total, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	}

	doc.Find("li.arxiv-result").Each(func(_ int, li *goquery.Selection) {
		page.Results = append(page.Results, parseS5Result(li))
	})
	return page, nil
}

// parseS5Result reads one result block.
func parseS5Result(li *goquery.Selection) s5Result {
	r := s5Result{SubjectNames: map[string]string{}}
	hits := map[string]bool{}

	link := li.Find("p.list-title a").First()
	r.AbsURL = strings.TrimSpace(link.AttrOr("href", ""))
	if id, err := axid.Parse(cleanText(link.Text())); err == nil {
		r.ID = id.Canonical
	}
	li.Find("p.list-title span a").Each(func(_ int, a *goquery.Selection) {
		if strings.EqualFold(cleanText(a.Text()), "pdf") {
			r.PDFURL = strings.TrimSpace(a.AttrOr("href", ""))
		}
	})

	li.Find("div.tags span.tag").Each(func(_ int, s *goquery.Selection) {
		code := cleanText(s.Text())
		if code == "" || contains(r.Categories, code) {
			return
		}
		r.Categories = append(r.Categories, code)
		// The tooltip is the human readable name, which makes this the one
		// search surface that carries a slice of the taxonomy inline.
		if name := cleanText(s.AttrOr("data-tooltip", "")); name != "" {
			r.SubjectNames[code] = name
		}
	})

	title := li.Find("p.title").First()
	r.Title = s5Text(title, hits)

	li.Find("p.authors a").Each(func(_ int, a *goquery.Selection) {
		if name := cleanText(a.Text()); name != "" {
			r.Authors = append(r.Authors, name)
		}
	})

	// Read the full abstract, never the short one: the short one starts with an
	// ellipsis and is a fragment from the middle of the text, chosen to show
	// the query terms.
	full := li.Find("span.abstract-full").First()
	if id, ok := full.Attr("id"); ok {
		if m := s5VersionRe.FindStringSubmatch(id); m != nil {
			r.Version, _ = strconv.Atoi(m[1])
		}
	}
	// The "△ Less" toggle lives inside the abstract span, so it is dropped
	// before the text is taken rather than trimmed off the end afterwards.
	full.Find("a").Remove()
	r.Abstract = s5Text(full, hits)

	li.Find("p").Each(func(_ int, p *goquery.Selection) {
		text := cleanText(p.Text())
		if m := s5DateRe.FindStringSubmatch(text); m != nil {
			r.Submitted, _ = s5Date(m[1])
			r.FirstSubmitted = r.Submitted
			if m[2] != "" {
				r.FirstSubmitted, _ = s5Date(m[2])
			}
			r.AnnouncedMonth = m[3]
		}
	})

	li.Find("p.comments").Each(func(_ int, p *goquery.Selection) {
		label := strings.TrimSuffix(cleanText(p.Find("span").First().Text()), ":")
		value := cleanText(strings.TrimPrefix(cleanText(p.Text()), label+":"))
		switch strings.ToLower(label) {
		case "comments":
			r.Comment = value
		case "journal ref":
			r.JournalRef = value
		case "report number":
			r.ReportNo = value
		case "msc class":
			r.MSCClass = splitClasses(value)
		case "acm class":
			r.ACMClass = splitClasses(value)
		}
	})

	for h := range hits {
		r.Hits = append(r.Hits, h)
	}
	slices.Sort(r.Hits)
	return r
}

// s5Text reads an element's text and collects the highlighted terms on the way
// through.
//
// The highlight spans are stripped rather than kept, because a title with
// markup in it is not a title, but which terms matched is worth having and is
// the one thing a search result knows that the paper does not.
func s5Text(s *goquery.Selection, hits map[string]bool) string {
	s.Find("span.search-hit").Each(func(_ int, hit *goquery.Selection) {
		if t := cleanText(hit.Text()); t != "" {
			hits[strings.ToLower(t)] = true
		}
	})
	return cleanText(s.Text())
}

// s5Date reads "26 July, 2026".
//
// The page gives a day and no time, so a record built from it is a day
// accurate record and says so through its via map. The API's own timestamps
// are exact, which is one more reason not to come here without a reason.
func s5Date(s string) (time.Time, bool) {
	t, err := time.Parse("2 January, 2006", cleanText(s))
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// s5ToPaper maps a result onto the record every other surface also produces.
func s5ToPaper(r s5Result, source string, at time.Time) Paper {
	p := Paper{
		Envelope: Envelope{
			Kind:        "paper",
			RetrievedAt: at.UTC(),
		},
		ID:         r.ID,
		Version:    r.Version,
		Title:      r.Title,
		Abstract:   r.Abstract,
		Comment:    r.Comment,
		JournalRef: r.JournalRef,
		ReportNo:   r.ReportNo,
		MSCClass:   r.MSCClass,
		ACMClass:   r.ACMClass,
		// A search returns the current version of a paper, because the form's
		// include_older_versions box is off and this tool never ticks it.
		IsLatest:       true,
		Hits:           r.Hits,
		AnnouncedMonth: r.AnnouncedMonth,
		URL:            r.AbsURL,
		PDFURL:         r.PDFURL,
		Depth:          string(DepthQuick),
	}
	p.addSurface(SurfaceSearch, source)

	if id, err := axid.Parse(r.ID); err == nil {
		p.Style = string(id.Style)
		p.OAIID = id.OAI()
		p.DOI = id.DOI()
		id.Version = r.Version
		p.VersionedID = id.Versioned()
		// The page links to the bare id and the record wants the version it is
		// describing, which the page states only in an element id. Where that
		// parsed, the derived links are the more precise answer and the page's
		// own hrefs are the fallback.
		if r.Version > 0 {
			p.URL = id.AbsURL()
			p.PDFURL = id.PDFURL()
		}
		if p.URL == "" {
			p.URL = id.AbsURL()
		}
		if p.PDFURL == "" {
			p.PDFURL = id.PDFURL()
		}
	}

	for _, name := range r.Authors {
		p.Authors = append(p.Authors, Author{Name: name, Via: SurfaceSearch})
	}
	p.AuthorLine = authorLine(p.Authors)

	if len(r.Categories) > 0 {
		p.PrimaryCategory = r.Categories[0]
		p.Categories = r.Categories
		p.CrossLists = crossLists(p.Categories, p.PrimaryCategory)
		p.SubjectNames = r.SubjectNames
		p.setVia("categories", SurfaceSearch)
		p.setVia("subject_names", SurfaceSearch)
	}

	for field, value := range map[string]string{
		"title":       p.Title,
		"abstract":    p.Abstract,
		"comment":     p.Comment,
		"journal_ref": p.JournalRef,
	} {
		if value != "" {
			p.setVia(field, SurfaceSearch)
		}
	}

	if !r.FirstSubmitted.IsZero() {
		p.FirstSubmitted = r.FirstSubmitted
		p.setVia("first_submitted", SurfaceSearch)
	}
	if !r.Submitted.IsZero() {
		p.LastUpdated = r.Submitted
		p.setVia("last_updated", SurfaceSearch)
	}

	// The depth ladder describes a paper read and a search result is not one,
	// so the missed line is written out here rather than taken from Depth. s5
	// answers for the report number and the two class fields, which --depth
	// meta would otherwise claim, and it answers for none of the rest.
	p.Missed = []string{
		"the search UI gives dates to the day and not the minute; arxiv paper " + r.ID + " reads the exact timestamps",
		"the licence, the submitter, the version history and structured author names were not read; arxiv paper " + r.ID + " --depth full reads them",
	}
	return p
}
