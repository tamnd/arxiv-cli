package arxiv

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// EdgeOptions is what one reference's claims cost.
type EdgeOptions struct {
	// Depth is how deeply the paper is read, which decides how many claims
	// there are. meta gives the authors, the categories, the journal reference
	// and the DOIs. full adds the version history, the licence and the files.
	// text adds the affiliations and the bibliography.
	Depth Depth
	// Trackbacks adds the inbound links, which is one request on the fifteen
	// second plane and is nothing at all for most papers.
	Trackbacks bool
	// Predicates keeps only these, empty meaning all of them.
	Predicates []string
}

// Edges is every claim one read asserts.
//
// The claims are free. They come out of the record the read already built, so
// `arxiv edges 1706.03762` costs exactly what `arxiv paper 1706.03762` costs and
// returns twenty claims instead of one record.
func (c *Client) Edges(ctx context.Context, ref string, o EdgeOptions) ([]graph.Edge, error) {
	depth := o.Depth
	if depth == "" {
		depth = DepthMeta
	}
	p, err := c.PaperAt(ctx, ref, PaperOptions{Depth: depth})
	if err != nil {
		return nil, err
	}

	var s edgeSet
	s.addAll(EdgesOfPaper(p))

	// cites lives on the rendering and only on the rendering. The paper record
	// folds the section tree in at this depth but not the bibliography, so the
	// rendering is read again here, which is a cache hit and not a request.
	if depth.AtLeast(DepthText) && p.HasHTML {
		full, err := c.FullText(ctx, ref, FullTextOptions{})
		switch {
		case err == nil:
			edges, cover := EdgesOfFullText(full)
			s.addAll(edges)
			c.notice("%s", cover)
		case errors.Is(err, ErrNotFound):
			c.logf(1, "%s claims a rendering and there is none, so there are no cites claims", p.ID)
		default:
			return nil, err
		}
	}
	if depth.AtLeast(DepthText) && !p.HasHTML {
		c.notice("arxiv has no rendering of %s, so this read has no cites claims; nothing else publishes them", p.ID)
	}

	if o.Trackbacks {
		tbs, err := c.Trackbacks(ctx, ref)
		switch {
		case err == nil:
			for _, t := range tbs {
				s.addAll(EdgesOfTrackback(t))
			}
		case errors.Is(err, ErrNotFound):
			c.logf(1, "%s has no trackbacks", p.ID)
		default:
			return nil, err
		}
	}

	c.report(s.refused)
	return filterEdges(s.out, o.Predicates), nil
}

// report says what was refused. Nothing should ever be, so it is said out loud
// rather than logged behind -v.
func (c *Client) report(refused []string) {
	for _, r := range refused {
		c.notice("arxiv: a claim was refused by the predicate table and not written: %s", r)
	}
}

// filterEdges keeps the named predicates, all of them when none are named.
func filterEdges(edges []graph.Edge, names []string) []graph.Edge {
	if len(names) == 0 {
		return edges
	}
	keep := make(map[string]bool, len(names))
	for _, n := range names {
		keep[n] = true
	}
	out := make([]graph.Edge, 0, len(edges))
	for _, e := range edges {
		if keep[e.Predicate] {
			out = append(out, e)
		}
	}
	return out
}

// ─── the walk ────────────────────────────────────────────────────────────────

// WalkOptions is a breadth first walk out from one reference.
type WalkOptions struct {
	// Hops is how far to walk. One hop is the claims of the seed itself.
	Hops int
	// Depth is how deeply each paper is read.
	Depth Depth
	// Names follows author names, which means one search a name. It is off by
	// default because it explodes: eight authors a paper, one request each, and
	// most of them come back with papers by somebody else of the same name.
	Names bool
	// Limit is how many papers a name search reads.
	Limit int
	// Budget is the request ceiling. The walk stops when it would go over and
	// says what it left unread.
	Budget int
	// Trackbacks adds the inbound links of the seed.
	Trackbacks bool
	// Predicates filters what comes back. The walk itself follows everything,
	// because filtering the frontier would silently change what was reached.
	Predicates []string
}

// walk is one run's state.
type walk struct {
	c    *Client
	opts WalkOptions
	set  edgeSet
	// expanded is every node that has been read or that has nothing to read.
	expanded map[string]bool
	// label is the readable name a claim gave a node, which is how a name node
	// gets searched for by the spelling arXiv printed rather than by its slug.
	label map[string]string
	spent int
}

// Walk follows claims out from a reference, breadth first, on a request budget.
//
// The frontier is ordered the way doc 04 section 3.3 orders it: a batched
// id_list read is two hundred papers and several thousand claims for one
// request, so papers are drained first and everything else waits behind them.
func (c *Client) Walk(ctx context.Context, ref string, o WalkOptions) ([]graph.Edge, error) {
	if o.Hops < 1 {
		o.Hops = 1
	}
	if o.Depth == "" {
		o.Depth = DepthMeta
	}
	if o.Limit <= 0 {
		o.Limit = 25
	}
	if o.Budget <= 0 {
		o.Budget = 25
	}
	id, err := axid.Parse(ref)
	if err != nil {
		return nil, err
	}
	w := &walk{c: c, opts: o, expanded: map[string]bool{}, label: map[string]string{}}

	seed, err := c.Edges(ctx, ref, EdgeOptions{Depth: o.Depth, Trackbacks: o.Trackbacks})
	if err != nil {
		return nil, err
	}
	w.set.addAll(seed)
	w.note(seed)
	w.spent += o.Depth.Requests()
	w.expanded[graph.Paper(id.Canonical)] = true

	for hop := 2; hop <= o.Hops; hop++ {
		before := len(w.set.out)
		if !w.expand(ctx, hop) {
			break
		}
		c.logf(1, "hop %d added %d claims for %d requests", hop, len(w.set.out)-before, w.spent)
	}

	c.report(w.set.refused)
	return filterEdges(w.set.out, o.Predicates), nil
}

// note records the readable name a claim gave each end.
func (w *walk) note(edges []graph.Edge) {
	for _, e := range edges {
		if e.Note == "" {
			continue
		}
		switch e.Predicate {
		case graph.Authored, graph.SubmittedBy:
			// The note on these is the author string, which belongs to the
			// subject: the name node, whose slug cannot be searched for.
			if _, ok := w.label[e.From]; !ok {
				w.label[e.From] = e.Note
			}
		default:
			if _, ok := w.label[e.To]; !ok {
				w.label[e.To] = e.Note
			}
		}
	}
}

// frontier is every node a claim named that has not been expanded yet, grouped
// by kind.
func (w *walk) frontier() map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	for _, e := range w.set.out {
		for _, uri := range []string{e.From, e.To} {
			if w.expanded[uri] || seen[uri] {
				continue
			}
			kind, ok := graph.KindOf(uri)
			if !ok {
				continue
			}
			seen[uri] = true
			out[kind] = append(out[kind], uri)
		}
	}
	return out
}

// expand reads one hop's worth of the frontier and reports whether anything
// happened.
func (w *walk) expand(ctx context.Context, hop int) bool {
	front := w.frontier()
	if len(front) == 0 {
		w.c.notice("nothing left to expand at hop %d", hop)
		return false
	}

	// Everything with nothing behind it is marked read now, so it is not
	// counted as unreached at the end. A DOI, a licence and a hashed URL are
	// nodes this tool names and does not fetch.
	for _, kind := range []string{graph.KindDOI, graph.KindExternal, graph.KindJournal,
		graph.KindLicense, graph.KindFile, graph.KindORCID, graph.KindSet,
		graph.KindGroup, graph.KindArchive, graph.KindTrackback} {
		for _, uri := range front[kind] {
			w.expanded[uri] = true
		}
	}

	did := false
	if papers := front[graph.KindPaper]; len(papers) > 0 {
		did = w.readPapers(ctx, papers) || did
	}
	if cats := front[graph.KindCategory]; len(cats) > 0 {
		did = w.readCategories(ctx, cats) || did
	}
	if names := front[graph.KindName]; len(names) > 0 {
		did = w.readNames(ctx, names) || did
	}
	return did
}

// readPapers drains the paper queue in one batched read, which is the cheapest
// thing on arXiv by a wide margin.
func (w *walk) readPapers(ctx context.Context, uris []string) bool {
	refs := make([]string, 0, len(uris))
	for _, uri := range uris {
		if graph.IsVersion(uri) {
			// A version is a fragment on a paper that is already in hand.
			w.expanded[uri] = true
			continue
		}
		refs = append(refs, strings.TrimPrefix(uri, graph.Scheme+graph.KindPaper+"/"))
	}
	if len(refs) == 0 {
		return false
	}
	cost := len(BatchIDs(refs)) + len(refs)*(w.opts.Depth.Requests()-1)
	if !w.afford(cost, "%d papers", len(refs)) {
		return false
	}
	papers, err := w.c.PapersAt(ctx, refs, PaperOptions{Depth: w.opts.Depth})
	w.spent += cost
	if err != nil {
		w.c.notice("arxiv: reading %d papers failed, so the walk stops here: %s", len(refs), err)
		return false
	}
	for _, p := range papers {
		w.expanded[graph.Paper(p.ID)] = true
		edges := EdgesOfPaper(p)
		w.set.addAll(edges)
		w.note(edges)
	}
	return len(papers) > 0
}

// readCategories expands categories out of the taxonomy, which is one cached
// table for all of them however many there are.
func (w *walk) readCategories(ctx context.Context, uris []string) bool {
	if !w.afford(2, "the taxonomy") {
		return false
	}
	cats, err := w.c.Categories(ctx)
	w.spent += 2
	if err != nil {
		w.c.notice("arxiv: could not read the taxonomy, so the categories stay unexpanded: %s", err)
		return false
	}
	want := map[string]bool{}
	for _, uri := range uris {
		want[strings.TrimPrefix(uri, graph.Scheme+graph.KindCategory+"/")] = true
		w.expanded[uri] = true
	}
	did := false
	for _, cat := range cats {
		if !want[cat.Code] {
			continue
		}
		edges := EdgesOfCategory(cat)
		w.set.addAll(edges)
		w.note(edges)
		did = did || len(edges) > 0
	}
	return did
}

// readNames follows author names, one search each.
//
// This is the expansion that explodes, so it is opt in and it is the last queue
// drained. A name search matches strings and nothing else, so the papers it
// finds may be somebody else's, and the claims it writes say s1 and are
// indistinguishable from the ones the seed paper made about itself.
func (w *walk) readNames(ctx context.Context, uris []string) bool {
	if !w.opts.Names {
		w.c.notice("%d author names are off the frontier; --names follows them at one search each", len(uris))
		return false
	}
	sort.Strings(uris)
	did := false
	for _, uri := range uris {
		name := w.label[uri]
		if name == "" {
			// Only the slug is known, and searching for a slug finds nothing.
			w.expanded[uri] = true
			continue
		}
		if !w.afford(1, "the search for %s", name) {
			return did
		}
		person, err := w.c.AuthorByName(ctx, name, w.opts.Limit)
		w.spent++
		w.expanded[uri] = true
		if err != nil {
			w.c.notice("arxiv: the search for %s failed: %s", name, err)
			continue
		}
		for _, p := range person.Papers {
			edges := EdgesOfPaper(p)
			w.set.addAll(edges)
			w.note(edges)
			did = true
		}
	}
	return did
}

// afford checks the budget before a read rather than before a request, so the
// walk finishes what it started or does not start it.
func (w *walk) afford(cost int, what string, args ...any) bool {
	if w.spent+cost <= w.opts.Budget {
		return true
	}
	w.c.notice("the budget of %d requests is spent (%d used), so "+what+" went unread",
		append([]any{w.opts.Budget, w.spent}, args...)...)
	return false
}
