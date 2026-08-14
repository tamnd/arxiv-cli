package arxiv

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/tamnd/any-cli/kit/errs"
)

// apiBase is the export API endpoint. Every request in this package is built
// from it and from nowhere else.
const apiBase = "https://export.arxiv.org/api/query"

// ResultWindow is the hard ceiling on start + max_results.
//
// Measured 2026-08-13 against search_query=cat:cs.CL: start=9999 with
// max_results=1 returns 200 and one entry, and max_results=2 returns 500 with
// an Atom error entry. It is Elasticsearch's index.max_result_window and it
// does not move with the query, the sort, or the time of day. Getting past it
// means slicing the query, which is what slice.go is for.
const ResultWindow = 10000

// PageSize is how many results one request asks for.
//
// Small enough to be reliable: max_results=10000 in a single request answers
// 503 rather than 500, which is the server failing to build the response rather
// than the window refusing it. Large enough that a full 10,000 result walk is
// 100 requests, which at the three second API pace is five minutes.
const PageSize = 100

// MaxIDsPerRequest caps an id_list batch.
//
// The real limit is on request line length, not on ids: 300 ids returned 200
// with 300 entries and 400 ids returned a 400 with an HTML body rather than an
// Atom feed. 200 leaves headroom, because an old-style id is longer than a new
// one and a batch of them is longer than a batch of these.
const MaxIDsPerRequest = 200

// maxURLLen is the encoded length a batch is cut at.
//
// The ceiling is arXiv's, and it is on the request line rather than on the id
// count: a longer one comes back as HTTP 400 with an HTML body reading
// "Request Line is too large (4245 > 4094)", measured 2026-08-14. Four thousand
// leaves ninety bytes of headroom for the method and the protocol version.
const maxURLLen = 4000

// A Request is one call to the export API.
//
// It exists so there is exactly one place that turns a query into a URL. Every
// read in this package goes through Values, so the encoding happens once and
// the window guard cannot be skipped by a caller who builds a URL by hand.
type Request struct {
	// Query is the search_query, unescaped. It may be empty when IDs is not.
	Query Query
	// IDs is the id_list. Combined with Query it filters the query rather than
	// replacing it, which is arXiv's behaviour and not a choice made here.
	IDs []string
	// Start is the offset into the result set.
	Start int
	// Max is how many results to return. Zero means PageSize.
	Max int
	// Sort and Order are the ordering. An empty Sort means relevance.
	Sort  Sort
	Order Order
}

// Values renders the request's query parameters, validating everything that can
// be validated without asking arXiv.
func (r Request) Values() (url.Values, error) {
	if r.Query.Empty() && len(r.IDs) == 0 {
		// arXiv's own words for this case, so the message a user sees matches
		// the one they would get from the API directly.
		return nil, errs.Usage("either a search query or a list of ids must be given")
	}
	if len(r.IDs) > MaxIDsPerRequest {
		return nil, errs.Usage("%d ids in one request; the ceiling is %d, so batch them",
			len(r.IDs), MaxIDsPerRequest)
	}
	if r.Start < 0 {
		return nil, errs.Usage("start %d is negative", r.Start)
	}

	max := r.Max
	if max <= 0 {
		max = PageSize
	}
	// The window guard is here rather than at the call sites, because a request
	// this tool knows will fail should never leave the process.
	if r.Start+max > ResultWindow {
		return nil, errs.New(errs.KindGeneric,
			"start %d plus %d results is past arxiv's %d result window; slice the query by date instead",
			r.Start, max, ResultWindow)
	}

	sort := r.Sort
	if sort == "" {
		sort = SortRelevance
	}
	if !validSort(sort) {
		return nil, errs.Usage("sort order %q is not one of %s", sort, joinSorts())
	}
	order := r.Order
	if order == "" {
		order = Descending
	}
	if order != Ascending && order != Descending {
		return nil, errs.Usage("sort direction %q is not ascending or descending", order)
	}

	v := url.Values{}
	if !r.Query.Empty() {
		v.Set("search_query", r.Query.String())
	}
	if len(r.IDs) > 0 {
		v.Set("id_list", strings.Join(r.IDs, ","))
	}
	if r.Start > 0 {
		v.Set("start", strconv.Itoa(r.Start))
	}
	v.Set("max_results", strconv.Itoa(max))
	v.Set("sortBy", string(sort))
	v.Set("sortOrder", string(order))
	return v, nil
}

// URL is the full request URL. This is the only place in the package that
// concatenates a base and an encoded query string.
func (r Request) URL() (string, error) {
	v, err := r.Values()
	if err != nil {
		return "", err
	}
	return apiBase + "?" + v.Encode(), nil
}

// CountRequest is the request that asks only how many results there are.
//
// It asks for one result rather than none. max_results=0 answers 500 with an
// internal error entry, measured 2026-08-13, so counting costs one entry's
// worth of bytes and there is no way around it.
func CountRequest(q Query) Request {
	return Request{Query: q, Max: 1, Sort: SortSubmitted, Order: Ascending}
}

func validSort(s Sort) bool {
	for _, want := range Sorts {
		if s == want {
			return true
		}
	}
	return false
}

func joinSorts() string {
	names := make([]string, len(Sorts))
	for i, s := range Sorts {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// BatchIDs splits ids into batches that will fit in one request.
//
// It cuts on encoded length as well as on count, because the limit arXiv
// enforces is on the request line and an id_list of old-style ids is far longer
// per id than a list of new ones.
func BatchIDs(ids []string) [][]string {
	if len(ids) == 0 {
		return nil
	}
	var out [][]string
	batch := make([]string, 0, MaxIDsPerRequest)
	used := requestOverhead

	for _, id := range ids {
		cost := idCost(id)
		if len(batch) > 0 && (len(batch) >= MaxIDsPerRequest || used+cost > maxURLLen) {
			out = append(out, batch)
			batch = make([]string, 0, MaxIDsPerRequest)
			used = requestOverhead
		}
		batch = append(batch, id)
		used += cost
	}
	if len(batch) > 0 {
		out = append(out, batch)
	}
	return out
}

// requestOverhead is everything in the URL that is not an id.
var requestOverhead = len(apiBase) + len("?id_list=&max_results=200&sortBy=submittedDate&sortOrder=ascending")

// idCost is what one id adds to the encoded length.
//
// A comma encodes as %2C and an old-style id's slash as %2F, so the worst case
// is three characters per byte plus the separator.
func idCost(id string) int { return len(url.QueryEscape(id)) + len("%2C") }

// encodedLen is what a batch of ids will actually take up in the request line.
//
// It is the number BatchIDs cuts on, exported to the tests so the live test can
// measure the same thing the batcher measures rather than a re-derivation of it
// that could disagree.
func encodedLen(ids []string) int {
	used := requestOverhead
	for _, id := range ids {
		used += idCost(id)
	}
	return used
}
