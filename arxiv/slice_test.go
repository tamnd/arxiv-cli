package arxiv

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeIndex is an arXiv-shaped result set: every paper has a submission minute,
// and a count is however many fall inside the range in the query. It is enough
// to drive the slicer, and unlike a live index it can be asked ten thousand
// questions in a millisecond.
type fakeIndex struct {
	minutes []time.Time // sorted submission times
	calls   int
}

// count answers the way arXiv does: the size of the whole result set, not of a
// page. It reads the range back out of the query text, which also checks the
// slicer is putting a range in there at all.
func (f *fakeIndex) count(_ context.Context, q Query) (int, error) {
	f.calls++
	r, ok := rangeOf(q.String())
	if !ok {
		return len(f.minutes), nil
	}
	n := 0
	for _, m := range f.minutes {
		if !m.Before(r.From) && !m.After(r.To) {
			n++
		}
	}
	return n, nil
}

// rangeOf pulls the range back out of a rendered query.
func rangeOf(s string) (Range, bool) {
	i := strings.Index(s, ":[")
	if i < 0 {
		return Range{}, false
	}
	j := strings.Index(s[i:], "]")
	if j < 0 {
		return Range{}, false
	}
	parts := strings.Split(s[i+2:i+j], " TO ")
	if len(parts) != 2 {
		return Range{}, false
	}
	from, err := time.ParseInLocation(stampLayout, parts[0], time.UTC)
	if err != nil {
		return Range{}, false
	}
	to, err := time.ParseInLocation(stampLayout, parts[1], time.UTC)
	if err != nil {
		return Range{}, false
	}
	return Range{From: from, To: to}, true
}

func TestSplitHalvesShareNoMinute(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for minutes := 1; minutes <= 200; minutes++ {
		r := Range{From: base, To: base.Add(time.Duration(minutes-1) * time.Minute)}
		left, right, ok := split(r)
		if minutes == 1 {
			if ok {
				t.Errorf("a one minute range was split into %s and %s", left, right)
			}
			continue
		}
		if !ok {
			t.Fatalf("a %d minute range would not split", minutes)
		}
		if left.Minutes() == 0 || right.Minutes() == 0 {
			t.Fatalf("%d minutes split into %d and %d", minutes, left.Minutes(), right.Minutes())
		}
		if left.Minutes()+right.Minutes() != minutes {
			t.Errorf("%d minutes split into %d and %d, which do not add up",
				minutes, left.Minutes(), right.Minutes())
		}
		// The gap is the whole reason the halves are correct: arXiv's ranges are
		// inclusive at both ends, so halves meeting at T would return every paper
		// submitted in minute T twice.
		if !right.From.Equal(left.To.Add(time.Minute)) {
			t.Errorf("%d minutes: left ends %s and right starts %s", minutes, left.To, right.From)
		}
	}
}

// TestSlicePlanPartitionsTheRange is the property that makes the results
// correct, checked over generated shapes rather than over one example. Every
// minute in the range belongs to exactly one slice: none twice, none missing.
func TestSlicePlanPartitionsTheRange(t *testing.T) {
	rng := rand.New(rand.NewSource(20260813))
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	for trial := 0; trial < 40; trial++ {
		span := 60 + rng.Intn(3000) // minutes in the whole range
		full := Range{From: base, To: base.Add(time.Duration(span-1) * time.Minute)}

		// Papers clumped rather than spread, because arXiv is clumped: a weekday
		// afternoon holds far more submissions than a Sunday night.
		idx := &fakeIndex{}
		for i := 0; i < span; i++ {
			m := base.Add(time.Duration(i) * time.Minute)
			n := rng.Intn(4)
			if i%7 == 0 {
				n += 40
			}
			for j := 0; j < n; j++ {
				idx.minutes = append(idx.minutes, m)
			}
		}

		// A window of a few hundred instead of ten thousand, so the bisection
		// actually recurses on a range this small.
		plan, err := slicePlanWindow(t, idx, full, 200)
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}

		covered := map[time.Time]int{}
		for _, s := range plan.Slices {
			if !s.Range.Valid() {
				t.Fatalf("trial %d: slice %s runs backwards", trial, s.Range)
			}
			for m := s.Range.From; !m.After(s.Range.To); m = m.Add(time.Minute) {
				covered[m]++
			}
		}
		for i := 0; i < span; i++ {
			m := base.Add(time.Duration(i) * time.Minute)
			switch covered[m] {
			case 1:
			case 0:
				t.Fatalf("trial %d: minute %s is in no slice", trial, Stamp(m))
			default:
				t.Fatalf("trial %d: minute %s is in %d slices", trial, Stamp(m), covered[m])
			}
		}
		if len(covered) != span {
			t.Fatalf("trial %d: slices cover %d minutes, want %d", trial, len(covered), span)
		}

		// In time order, which is what makes a walk resumable and the output
		// readable.
		if !sort.SliceIsSorted(plan.Slices, func(i, j int) bool {
			return plan.Slices[i].Range.From.Before(plan.Slices[j].Range.From)
		}) {
			t.Fatalf("trial %d: slices are out of order", trial)
		}

		// Every leaf fits, which is the whole point.
		for _, s := range plan.Slices {
			if s.Total > 200 && !s.Truncated {
				t.Fatalf("trial %d: slice %s holds %d, over the window", trial, s.Range, s.Total)
			}
		}
		// And nothing was lost on the way down the tree.
		sum := 0
		for _, s := range plan.Slices {
			sum += s.Total
		}
		if sum != plan.Total {
			t.Fatalf("trial %d: slices hold %d, the whole query holds %d", trial, sum, plan.Total)
		}
	}
}

// slicePlanWindow runs the slicer against a smaller window than arXiv's, by
// scaling the fake index rather than by making the constant a variable. Every
// count is multiplied so that `window` results in the fake are `ResultWindow` in
// the slicer's eyes.
func slicePlanWindow(t *testing.T, idx *fakeIndex, full Range, window int) (Plan, error) {
	t.Helper()
	scale := ResultWindow / window
	scaled := func(ctx context.Context, q Query) (int, error) {
		n, err := idx.count(ctx, q)
		return n * scale, err
	}
	plan, err := SlicePlan(context.Background(), Term(FieldCategory, "cs.CL"), SubmittedDate, full, scaled)
	if err != nil {
		return plan, err
	}
	plan.Total /= scale
	for i := range plan.Slices {
		plan.Slices[i].Total /= scale
	}
	return plan, nil
}

// TestSmallQueryIsNotSliced is the common case and it must cost one request.
func TestSmallQueryIsNotSliced(t *testing.T) {
	idx := &fakeIndex{}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 500; i++ {
		idx.minutes = append(idx.minutes, base.Add(time.Duration(i)*time.Minute))
	}
	full := Range{From: base, To: base.Add(1000 * time.Minute)}

	plan, err := SlicePlan(context.Background(), Term(FieldCategory, "cs.CL"), SubmittedDate, full, idx.count)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Slices) != 1 {
		t.Fatalf("a 500 result query was cut into %d slices", len(plan.Slices))
	}
	if plan.Slices[0].Range != full {
		t.Errorf("the single slice is %s, want the whole range %s", plan.Slices[0].Range, full)
	}
	if plan.Counts != 1 {
		t.Errorf("planning cost %d requests, want 1", plan.Counts)
	}
}

// TestUncuttableSliceIsReported is doc 00 principle four: no silent caps. A
// single minute over the window cannot be subdivided, and the answer is to say
// so rather than to return short and look complete.
func TestUncuttableSliceIsReported(t *testing.T) {
	idx := &fakeIndex{}
	m := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < ResultWindow+50; i++ {
		idx.minutes = append(idx.minutes, m)
	}
	full := Range{From: m.Add(-time.Minute), To: m.Add(time.Minute)}

	plan, err := SlicePlan(context.Background(), Term(FieldCategory, "cs.CL"), SubmittedDate, full, idx.count)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Truncated() {
		t.Fatal("a minute holding more than the window was not reported as truncated")
	}
	if plan.Total != ResultWindow+50 {
		t.Errorf("total = %d, want the real count %d", plan.Total, ResultWindow+50)
	}
	if plan.Reachable() != ResultWindow {
		t.Errorf("reachable = %d, want the window %d", plan.Reachable(), ResultWindow)
	}
}

func TestSlicePlanRejectsABackwardsRange(t *testing.T) {
	idx := &fakeIndex{}
	m := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	full := Range{From: m, To: m.Add(-time.Hour)}
	if _, err := SlicePlan(context.Background(), Query{}, SubmittedDate, full, idx.count); err == nil {
		t.Fatal("a backwards range was planned")
	}
}

// TestPagesCoverTheSlice checks the pager reaches every result exactly once and
// never asks for a page the window would refuse.
func TestPagesCoverTheSlice(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := Range{From: base, To: base.Add(time.Hour)}
	q := Term(FieldCategory, "cs.CL")

	for _, total := range []int{0, 1, 99, 100, 101, 250, ResultWindow} {
		s := Slice{Range: r, Total: total}
		pages := s.Pages(q, SubmittedDate)
		got := 0
		for i, p := range pages {
			if p.Start != got {
				t.Errorf("total %d: page %d starts at %d, want %d", total, i, p.Start, got)
			}
			if p.Max > PageSize {
				t.Errorf("total %d: page %d asks for %d, over the page size", total, i, p.Max)
			}
			if _, err := p.Values(); err != nil {
				t.Errorf("total %d: page %d does not build: %v", total, i, err)
			}
			// Paging must be on submission time. Relevance is recomputed per
			// request, so a paper can move across a page boundary and be
			// returned twice or never.
			if p.Sort != SortSubmitted || p.Order != Ascending {
				t.Errorf("total %d: page %d sorts by %s %s", total, i, p.Sort, p.Order)
			}
			got += p.Max
		}
		if got != total {
			t.Errorf("total %d: pages cover %d results", total, got)
		}
	}
}

// TestPagesStopAtTheWindow covers the truncated slice: it pages what it can
// reach rather than building requests arXiv will refuse.
func TestPagesStopAtTheWindow(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := Slice{Range: Range{From: base, To: base}, Total: ResultWindow + 500, Truncated: true}
	got := 0
	for _, p := range s.Pages(Term(FieldCategory, "cs.CL"), SubmittedDate) {
		if _, err := p.Values(); err != nil {
			t.Fatalf("page at %d does not build: %v", p.Start, err)
		}
		got += p.Max
	}
	if got != ResultWindow {
		t.Errorf("pages cover %d results, want the window %d", got, ResultWindow)
	}
}
