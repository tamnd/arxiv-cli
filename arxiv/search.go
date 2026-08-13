package arxiv

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// DefaultLimit is how many results a search returns when nobody says.
//
// Ten is a screenful. It is a default rather than a cap: -n takes it up to the
// window and --all takes it past that.
const DefaultLimit = 10

// SearchOptions is everything a search can ask for.
//
// The field-scoped strings are the arXiv prefixes under readable names, so
// --title attention --author vaswani builds ti:attention AND au:vaswani and the
// user never types a prefix. Raw is the escape hatch for the rest of the
// grammar in spec 3006 doc 02 section 2.
type SearchOptions struct {
	// Query is free text, matched against every indexed field.
	Query string
	// Raw is a query written in arXiv's own grammar, sent unchanged.
	Raw string
	// Categories are OR'd together. A bare archive code is expanded; see
	// categoryTerm.
	Categories []string

	Author   string
	Title    string
	Abstract string
	Comment  string
	Journal  string
	Report   string

	// From and To bound submittedDate, UpdatedFrom and UpdatedTo bound
	// lastUpdatedDate. Each accepts 2026, 2026-01 or 2026-01-01.
	From        string
	To          string
	UpdatedFrom string
	UpdatedTo   string

	Sort  string
	Order string
	Limit int
	// All walks the whole result set through the slicer, past the ten thousand
	// result window.
	All bool
}

// searchPlan is a validated SearchOptions: a query with no date clause in it, a
// range beside it, and the ordering.
//
// The date clause is kept out of the query because the slicer needs to put its
// own range in, and a query that already carries one would end up with two.
type searchPlan struct {
	Query Query
	// Extra is a date clause on the timestamp the slicer is not cutting on. It
	// rides along in every request rather than being sliced.
	Extra Query
	Field DateField
	Range Range
	// Ranged says whether the user gave any bound on Field.
	Ranged bool
	Sort   Sort
	Order  Order
	Limit  int
	All    bool
}

// full is the query as it goes on the wire for an unsliced read.
func (p searchPlan) full() Query {
	q := And(p.Query, p.Extra)
	if p.Ranged {
		q = And(q, Between(p.Field, p.Range))
	}
	return q
}

// buildSearch turns options into a plan, rejecting everything that can be
// rejected without asking arXiv.
func buildSearch(o SearchOptions) (searchPlan, error) {
	var p searchPlan

	// A walk has no use for relevance, so an unset sort means submission date
	// under --all and relevance everywhere else. Asking for relevance out loud
	// is still refused below rather than quietly switched, because a user who
	// typed it wants to hear that a walk cannot give them what they asked for.
	sortFlag := o.Sort
	if sortFlag == "" && o.All {
		sortFlag = "submitted"
	}
	sort, err := ParseSort(sortFlag)
	if err != nil {
		return p, errs.Usage("%s", err.Error())
	}
	order, err := ParseOrder(o.Order)
	if err != nil {
		return p, errs.Usage("%s", err.Error())
	}
	p.Sort, p.Order, p.Limit, p.All = sort, order, o.Limit, o.All

	q, err := searchQuery(o)
	if err != nil {
		return p, err
	}
	p.Query = q

	submitted, gotSubmitted, err := boundRange(o.From, o.To, "--from", "--to")
	if err != nil {
		return p, err
	}
	updated, gotUpdated, err := boundRange(o.UpdatedFrom, o.UpdatedTo, "--updated-from", "--updated-to")
	if err != nil {
		return p, err
	}
	// Both ranges are honoured, and the slicer cuts on the submission date
	// whenever there is one, because that timestamp never moves. A
	// lastUpdatedDate clause alongside it rides in every request instead.
	switch {
	case gotSubmitted:
		p.Field, p.Range, p.Ranged = SubmittedDate, submitted, true
		if gotUpdated {
			p.Extra = Between(LastUpdatedDate, updated)
		}
	case gotUpdated:
		p.Field, p.Range, p.Ranged = LastUpdatedDate, updated, true
	default:
		p.Field = SubmittedDate
	}

	if p.Query.Empty() && !p.Ranged {
		return p, errs.Usage("nothing to search for; give a query, a --cat, a field flag or a date range")
	}
	if err := checkAll(p, o); err != nil {
		return p, err
	}
	return p, nil
}

// rawConflicts are the flags --raw cannot be combined with, in the order the
// message lists them.
var rawConflicts = []string{
	"--cat", "--author", "--title", "--abstract", "--comment", "--journal", "--report",
}

// searchQuery assembles the query itself.
func searchQuery(o SearchOptions) (Query, error) {
	if o.Raw != "" {
		// Untouched means untouched. Anding a --cat onto a hand written query
		// would change the grammar the user is testing, and the whole point of
		// the flag is that what goes out is what was typed.
		if o.Query != "" || len(o.Categories) > 0 || fieldTerms(o) != nil {
			return Query{}, errs.Usage("raw passes a query through untouched, so it cannot be combined with a query argument or %s",
				strings.Join(rawConflicts, ", "))
		}
		return Raw(o.Raw), nil
	}

	terms := make([]Query, 0, 8)
	for _, w := range strings.Fields(o.Query) {
		terms = append(terms, Term(FieldAll, w))
	}
	terms = append(terms, fieldTerms(o)...)
	if cats := categoryTerm(o.Categories); !cats.Empty() {
		terms = append(terms, cats)
	}
	return And(terms...), nil
}

// fieldTerms is the field-scoped flags as query terms, nil when none was given.
func fieldTerms(o SearchOptions) []Query {
	var out []Query
	for _, pair := range []struct {
		field Field
		value string
	}{
		{FieldAuthor, o.Author},
		{FieldTitle, o.Title},
		{FieldAbstract, o.Abstract},
		{FieldComment, o.Comment},
		{FieldJournal, o.Journal},
		{FieldReport, o.Report},
	} {
		if t := Term(pair.field, pair.value); !t.Empty() {
			out = append(out, t)
		}
	}
	return out
}

// categoryTerm builds the category clause, OR'ing the codes and grouping them
// so a following AND means what it looks like it means.
func categoryTerm(cats []string) Query {
	terms := make([]Query, 0, len(cats))
	for _, c := range cats {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		terms = append(terms, expandCategory(c))
	}
	if len(terms) == 0 {
		return Query{}
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return Group(Or(terms...))
}

// expandCategory turns one code into the clause that matches what a person
// means by it.
//
// A code with a dot is a category and matches itself. A bare code is either an
// archive that was never split, where the papers sit on the bare code, or one
// that was, where they sit on the subcategories and the bare code matches
// nothing at all. Measured 2026-08-13: cat:cs returns 0 and cat:cs.* returns
// 920,922, while cat:hep-th returns 184,714 and cat:hep-th.* returns 0. OR'ing
// the two forms answers correctly for both without needing the taxonomy, which
// is milestone 7.
func expandCategory(code string) Query {
	if strings.Contains(code, ".") {
		return Term(FieldCategory, code)
	}
	return Group(Or(Term(FieldCategory, code), Term(FieldCategory, code+".*")))
}

// checkAll enforces the two rules a full walk has.
func checkAll(p searchPlan, o SearchOptions) error {
	if !p.All {
		return nil
	}
	if p.Sort == SortRelevance {
		// Relevance is recomputed per request, so a paper can move across a
		// page boundary between request one and request two. That shows up as
		// a duplicate on one page and a paper that is never returned at all.
		return errs.Usage("all cannot sort by relevance, because relevance is recomputed on every request and a walk would both repeat and skip papers; use --sort submitted or --sort updated")
	}
	if len(o.Categories) == 0 && !p.Ranged && p.Limit <= 0 {
		return errs.Usage("all needs a bound: give a --cat, a date range, or a --limit, because arXiv is 2.7 million papers and walking all of it is days of pacing")
	}
	return nil
}

// boundRange parses a pair of date flags into a range.
//
// An open end is closed at the other end's natural limit rather than being left
// zero, because arXiv's range syntax has no open form and a half range would
// have to be invented somewhere. Epoch and now are the honest edges.
func boundRange(from, to, fromFlag, toFlag string) (Range, bool, error) {
	if strings.TrimSpace(from) == "" && strings.TrimSpace(to) == "" {
		return Range{}, false, nil
	}
	start := Epoch
	if strings.TrimSpace(from) != "" {
		t, err := ParseBound(from, false)
		if err != nil {
			return Range{}, false, errs.Usage("the %s value %s", fromFlag, err.Error())
		}
		start = t
	}
	end := endOfToday()
	if strings.TrimSpace(to) != "" {
		t, err := ParseBound(to, true)
		if err != nil {
			return Range{}, false, errs.Usage("the %s value %s", toFlag, err.Error())
		}
		end = t
	}
	r := NewRange(start, end)
	if !r.Valid() {
		return Range{}, false, errs.Usage("the range %s to %s ends before it starts", fromFlag, toFlag)
	}
	return r, true, nil
}

// endOfToday is the right edge of an open range.
//
// It is the last minute of today rather than this minute, so the same open
// range asks the same question all day. A bound that moved every minute would
// miss the cache on every run and produce a different URL each time, which
// makes a result impossible to reproduce by hand an hour later.
func endOfToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, time.UTC)
}

// boundLayouts are the three shapes a date flag takes, widest first.
var boundLayouts = []struct {
	layout string
	// end is the last instant of the period the layout names.
	end func(time.Time) time.Time
}{
	{"2006", func(t time.Time) time.Time { return t.AddDate(1, 0, 0).Add(-time.Minute) }},
	{"2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, 0).Add(-time.Minute) }},
	{"2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1).Add(-time.Minute) }},
}

// ParseBound reads a date flag.
//
// A bound names a period, not an instant, so which end of it is meant depends
// on which flag it came from. --from 2026 is the first minute of the year and
// --to 2026 is the last one, which is what a person means by "papers from 2026"
// and is not what a naive parse of both to midnight would give them.
func ParseBound(s string, end bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range boundLayouts {
		t, err := time.ParseInLocation(l.layout, s, time.UTC)
		if err != nil {
			continue
		}
		if end {
			return l.end(t), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a date; write it as 2026, 2026-01 or 2026-01-01", s)
}

// ParseOrder resolves a sort direction, taking the short spellings a flag uses.
func ParseOrder(s string) (Order, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "desc", "descending":
		return Descending, nil
	case "asc", "ascending":
		return Ascending, nil
	}
	return "", fmt.Errorf("sort direction %q is not desc or asc", s)
}

// Search returns up to opts.Limit papers, collected.
func (c *Client) Search(ctx context.Context, opts SearchOptions) ([]Paper, error) {
	var out []Paper
	err := c.SearchStream(ctx, opts, func(p *Paper) error {
		out = append(out, *p)
		return nil
	})
	return out, err
}

// SearchStream runs a search and hands over each paper as it arrives.
//
// Streaming is the point: a full walk of a category is thousands of results
// over minutes, and holding all of them to print them at the end would mean
// watching nothing happen and then losing the lot to a ctrl-c.
func (c *Client) SearchStream(ctx context.Context, opts SearchOptions, emit func(*Paper) error) error {
	plan, err := buildSearch(opts)
	if err != nil {
		return err
	}
	if plan.All {
		return c.searchAll(ctx, plan, emit)
	}
	c.logf(1, "query: %s", plan.full())
	return c.pageThrough(ctx, plan.full(), plan.Sort, plan.Order, plan.limit(), emit)
}

// limit is how many results to stop at, with the default applied.
func (p searchPlan) limit() int {
	if p.Limit > 0 {
		return p.Limit
	}
	if p.All {
		return 0
	}
	return DefaultLimit
}

// pageThrough reads a query in pages until it has n results or arXiv runs out.
// n of zero means everything the window will give.
func (c *Client) pageThrough(ctx context.Context, q Query, sort Sort, order Order, n int, emit func(*Paper) error) error {
	if n <= 0 || n > ResultWindow {
		n = ResultWindow
	}
	for start := 0; start < n; start += PageSize {
		size := min(PageSize, n-start)
		req := Request{Query: q, Start: start, Max: size, Sort: sort, Order: order}
		papers, err := c.page(ctx, req, emit)
		if err != nil {
			return err
		}
		// A short page is the end of the result set. arXiv fills a page
		// whenever it can, so this is the only end marker worth trusting and it
		// saves a request that would come back empty.
		if papers < size {
			return nil
		}
	}
	return nil
}

// page sends one request and emits what came back, returning how many that was.
func (c *Client) page(ctx context.Context, req Request, emit func(*Paper) error) (int, error) {
	u, err := req.URL()
	if err != nil {
		return 0, err
	}
	c.logf(1, "GET %s", u)
	feed, err := c.getXML(ctx, u, TTLSearch)
	if err != nil {
		return 0, err
	}
	at := c.now()
	for _, e := range feed.Entries {
		p := entryToPaper(e, u, at)
		if err := emit(&p); err != nil {
			return len(feed.Entries), err
		}
	}
	return len(feed.Entries), nil
}

// searchAll walks the whole result set, slicing by date when it is over the
// window.
//
// The walk order is submission time ascending inside every slice, whatever
// --sort said, because that is the only ordering arXiv keeps stable across
// requests. --sort still decides which timestamp gets sliced.
func (c *Client) searchAll(ctx context.Context, p searchPlan, emit func(*Paper) error) error {
	field := p.Field
	if !p.Ranged && p.Sort == SortUpdated {
		field = LastUpdatedDate
	}
	full := p.Range
	if !p.Ranged {
		full = NewRange(Epoch, endOfToday())
	}
	base := And(p.Query, p.Extra)

	c.logf(1, "query: %s", base)
	plan, err := c.Plan(ctx, base, field, full)
	if err != nil {
		return err
	}
	c.notice("%s", planLine(plan))

	limit := p.Limit
	sent := 0
	for _, s := range plan.Slices {
		for _, req := range s.Pages(base, field) {
			n, err := c.page(ctx, req, func(paper *Paper) error {
				if limit > 0 && sent >= limit {
					return nil
				}
				sent++
				return emit(paper)
			})
			if err != nil {
				return err
			}
			if limit > 0 && sent >= limit {
				return nil
			}
			if n == 0 {
				break
			}
		}
	}
	return nil
}

// planLine is the sentence a walk prints before it starts.
func planLine(p Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d results in %d slice", p.Total, len(p.Slices))
	if len(p.Slices) != 1 {
		b.WriteString("s")
	}
	fmt.Fprintf(&b, ", %d count request", p.Counts)
	if p.Counts != 1 {
		b.WriteString("s")
	}
	fmt.Fprintf(&b, " to plan and about %d to walk", pageCount(p))
	if p.Truncated() {
		fmt.Fprintf(&b, "; %d of them are past the %d result window and cannot be reached",
			p.Total-p.Reachable(), ResultWindow)
	}
	return b.String()
}

// pageCount is how many result requests the plan will cost.
func pageCount(p Plan) int {
	n := 0
	for _, s := range p.Slices {
		total := s.Total
		if total > ResultWindow {
			total = ResultWindow
		}
		n += (total + PageSize - 1) / PageSize
	}
	return n
}

// Count is what `arxiv count` prints: how many results a query has, and the
// query it asked.
//
// It is a record rather than a bare number so the query that produced it
// travels with it. A count with no query beside it is a number nobody can
// check.
type Count struct {
	Envelope
	// Query is the search_query as arXiv read it, unescaped.
	Query string `json:"query" kit:"id" table:"query,truncate"`
	Total int    `json:"total" table:"total"`
}

// Count returns how many results a query has, which is the number the slicer
// makes all of its decisions on.
func (c *Client) Count(ctx context.Context, q Query) (int, error) {
	feed, err := c.Do(ctx, CountRequest(q), TTLSearch)
	if err != nil {
		return 0, err
	}
	return feed.Total, nil
}

// CountSearch answers the same question for a whole set of search options, and
// returns the record rather than the number.
func (c *Client) CountSearch(ctx context.Context, opts SearchOptions) (Count, error) {
	plan, err := buildSearch(opts)
	if err != nil {
		return Count{}, err
	}
	q := plan.full()
	req := CountRequest(q)
	u, err := req.URL()
	if err != nil {
		return Count{}, err
	}
	c.logf(1, "query: %s", q)
	c.logf(1, "GET %s", u)
	feed, err := c.getXML(ctx, u, TTLSearch)
	if err != nil {
		return Count{}, err
	}
	out := Count{Query: q.String(), Total: feed.Total}
	out.Kind = "count"
	out.RetrievedAt = c.now()
	out.addSurface(SurfaceAPI, u)
	return out, nil
}
