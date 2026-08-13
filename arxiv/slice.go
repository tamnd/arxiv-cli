package arxiv

import (
	"context"
	"fmt"
	"time"
)

// Epoch is the minute arXiv's first paper was submitted, and the left edge of
// any range the slicer is asked to cover without one.
//
// hep-th/9108001 is the first id in the first archive. Nothing was submitted
// before it, so a slice that starts here misses nothing.
var Epoch = time.Date(1991, 8, 1, 0, 0, 0, 0, time.UTC)

// A Slice is one leaf of the bisection: a date range small enough to page.
type Slice struct {
	// Range is the closed interval this slice covers.
	Range Range `json:"range"`
	// Total is what arXiv said the range holds.
	Total int `json:"total"`
	// Truncated is set when the range is a single minute holding more results
	// than the window allows, so paging it cannot return all of them. It does
	// not happen on arXiv today. If it ever does, the count is reported rather
	// than the shortfall being hidden.
	Truncated bool `json:"truncated,omitempty"`
}

func (s Slice) String() string {
	return fmt.Sprintf("%s: %d", s.Range, s.Total)
}

// Counter reports how many results a query has. The slicer takes one rather
// than a client so the bisection can be tested without a network.
type Counter func(ctx context.Context, q Query) (int, error)

// A Plan is a slicing of one query into pageable pieces.
type Plan struct {
	// Query is the query without any date clause.
	Query Query `json:"query"`
	// Field is the timestamp the slices are cut on.
	Field DateField `json:"field"`
	// Total is the whole query's result count, before slicing.
	Total int `json:"total"`
	// Slices are the leaves, in time order. A plan that needed no slicing has
	// exactly one, covering the whole range.
	Slices []Slice `json:"slices"`
	// Counts is how many requests the slicing itself cost, which is the price
	// paid before a single result comes back.
	Counts int `json:"counts"`
}

// Truncated reports whether any slice could not be cut small enough.
func (p Plan) Truncated() bool {
	for _, s := range p.Slices {
		if s.Truncated {
			return true
		}
	}
	return false
}

// Reachable is how many results the plan can actually return, which is less
// than Total only when a slice was truncated.
func (p Plan) Reachable() int {
	n := 0
	for _, s := range p.Slices {
		if s.Truncated {
			n += ResultWindow
			continue
		}
		n += s.Total
	}
	return n
}

// SlicePlan cuts a query into ranges that each fit inside the result window.
//
// The bisection is on time and not on count, so the slices come out uneven and
// that is fine. The only invariant that matters is that every leaf fits, and
// the two that make the result correct are that the leaves are disjoint and
// that their union is the range asked for.
//
// The count on the whole query is asked first, because a query under the window
// needs no slicing at all and that is the common case.
func SlicePlan(ctx context.Context, q Query, field DateField, full Range, count Counter) (Plan, error) {
	if !full.Valid() {
		return Plan{}, fmt.Errorf("range %s ends before it starts", full)
	}
	plan := Plan{Query: q, Field: field}

	total, err := count(ctx, And(q, Between(field, full)))
	if err != nil {
		return Plan{}, err
	}
	plan.Counts++
	plan.Total = total
	if total <= ResultWindow {
		plan.Slices = []Slice{{Range: full, Total: total}}
		return plan, nil
	}

	slices, calls, err := bisect(ctx, q, field, full, total, count)
	if err != nil {
		return Plan{}, err
	}
	plan.Counts += calls
	plan.Slices = slices
	return plan, nil
}

// bisect splits a range that is known to be over the window, recursing into
// each half. total is the caller's already-known count for r, so the recursion
// never asks twice for the same range.
func bisect(ctx context.Context, q Query, field DateField, r Range, total int, count Counter) ([]Slice, int, error) {
	left, right, ok := split(r)
	if !ok {
		// One minute holding more than the window. Nothing to subdivide, so say
		// so on the slice instead of quietly returning short.
		return []Slice{{Range: r, Total: total, Truncated: true}}, 0, nil
	}

	var out []Slice
	calls := 0
	for _, half := range []Range{left, right} {
		n, err := count(ctx, And(q, Between(field, half)))
		if err != nil {
			return nil, calls, err
		}
		calls++
		if n <= ResultWindow {
			out = append(out, Slice{Range: half, Total: n})
			continue
		}
		sub, subCalls, err := bisect(ctx, q, field, half, n, count)
		calls += subCalls
		if err != nil {
			return nil, calls, err
		}
		out = append(out, sub...)
	}
	return out, calls, nil
}

// split cuts a range in two at the minute.
//
// The halves share no minute: the left one ends at T and the right one begins
// at T plus one minute. arXiv's ranges are inclusive at both ends, so halves
// that met at T would return every paper submitted in that minute twice.
//
// It returns false for a range of one minute, which cannot be cut further.
func split(r Range) (Range, Range, bool) {
	minutes := r.Minutes()
	if minutes < 2 {
		return Range{}, Range{}, false
	}
	mid := r.From.Add(time.Duration(minutes/2-1) * time.Minute)
	return Range{From: r.From, To: mid},
		Range{From: mid.Add(time.Minute), To: r.To},
		true
}

// Pages walks a slice, yielding the request for each page in submission order.
//
// The order is not negotiable and is not taken from the caller. Relevance is
// recomputed per request, so a paper can move across a page boundary between
// request one and request two, which shows up as a duplicate on one page and a
// paper that is never returned at all. Submission time is immutable, so the
// ordering is total and the walk cannot skip.
func (s Slice) Pages(q Query, field DateField) []Request {
	n := s.Total
	if n > ResultWindow {
		n = ResultWindow
	}
	if n == 0 {
		return nil
	}
	ranged := And(q, Between(field, s.Range))
	var out []Request
	for start := 0; start < n; start += PageSize {
		max := PageSize
		if start+max > n {
			max = n - start
		}
		out = append(out, Request{
			Query: ranged,
			Start: start,
			Max:   max,
			Sort:  SortSubmitted,
			Order: Ascending,
		})
	}
	return out
}

// Plan slices a query into pageable ranges. A query under the window comes back
// as a single slice and costs one request to find that out.
//
// An open range is closed here rather than in the slicer, because the slicer
// works on minutes and has no business knowing what time it is.
func (c *Client) Plan(ctx context.Context, q Query, field DateField, full Range) (Plan, error) {
	if full.To.IsZero() {
		full = NewRange(full.From, time.Now())
	}
	if full.From.IsZero() {
		full = NewRange(Epoch, full.To)
	}
	return SlicePlan(ctx, q, field, full, c.Count)
}
