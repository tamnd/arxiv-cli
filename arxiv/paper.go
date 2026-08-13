package arxiv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// PaperOptions controls how deeply a paper is read.
type PaperOptions struct {
	// Depth is how many surfaces to cross. Empty means meta, because a single
	// paper is worth a second request.
	Depth Depth
}

// PaperAt fetches one paper at the given depth.
//
// The surfaces are read cheapest first and each one folds into the record the
// previous one built, so a failure on a deeper surface leaves a shallower
// record rather than nothing. That is deliberate: an abstract page that times
// out should not throw away the metadata already in hand.
func (c *Client) PaperAt(ctx context.Context, ref string, opts PaperOptions) (Paper, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return Paper{}, err
	}
	depth := opts.Depth
	if depth == "" {
		depth = DepthMeta
	}

	papers, err := c.papersAt(ctx, []axid.ID{id}, depth)
	if err != nil {
		return Paper{}, err
	}
	if len(papers) == 0 {
		return Paper{}, fmt.Errorf("%s: %w", id.Canonical, ErrNotFound)
	}
	return papers[0], nil
}

// PapersAt fetches many papers at the given depth.
//
// The s1 read batches, so a hundred papers at depth quick is one request. The
// deeper surfaces take one id each and there is no batching to be had, which is
// why the estimate exists.
func (c *Client) PapersAt(ctx context.Context, refs []string, opts PaperOptions) ([]Paper, error) {
	ids := make([]axid.ID, 0, len(refs))
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	depth := opts.Depth
	if depth == "" {
		depth = DepthMeta
	}
	return c.papersAt(ctx, ids, depth)
}

// papersAt is the shared body: one batched s1 read, then the per-paper reads.
func (c *Client) papersAt(ctx context.Context, ids []axid.ID, depth Depth) ([]Paper, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// A reference that pinned a version asks s1 for that version, because the
	// title and the abstract can differ between them.
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, id.Versioned())
	}

	var papers []Paper
	for _, batch := range BatchIDs(refs) {
		req := Request{IDs: batch, Max: len(batch)}
		u, err := req.URL()
		if err != nil {
			return nil, err
		}
		feed, err := c.getXML(ctx, u, TTLPaper)
		if err != nil {
			return papers, err
		}
		for _, e := range feed.Entries {
			papers = append(papers, entryToPaper(e, u, c.now()))
		}
	}

	if depth == DepthQuick {
		return papers, nil
	}
	for i := range papers {
		if err := c.deepen(ctx, &papers[i], depth); err != nil {
			return papers, err
		}
	}
	return papers, nil
}

// deepen reads the surfaces past s1 and folds each one in.
//
// Not found on a deeper surface is not fatal. OAI's earliest datestamp is 2005
// and a handful of old records are simply not in it, and a paper that s1
// answered for is a real paper whatever OAI thinks.
func (c *Client) deepen(ctx context.Context, p *Paper, depth Depth) error {
	if depth.AtLeast(DepthMeta) {
		rec, source, err := c.getOAI(ctx, p.ID, FormatArxiv)
		switch {
		case err == nil:
			mergeOAIArxiv(p, rec, source)
		case errors.Is(err, ErrNotFound):
			c.logf(1, "%s is not in OAI, keeping the s1 record", p.ID)
		default:
			return err
		}
	}
	if depth.AtLeast(DepthFull) {
		rec, source, err := c.getOAI(ctx, p.ID, FormatArxivRaw)
		switch {
		case err == nil:
			mergeOAIRaw(p, rec, source)
		case errors.Is(err, ErrNotFound):
			c.logf(1, "%s has no arXivRaw record", p.ID)
		default:
			return err
		}

		page, source, err := c.getAbs(ctx, p.ID)
		switch {
		case err == nil:
			mergeAbs(p, page, source)
		case errors.Is(err, ErrNotFound):
			c.logf(1, "%s has no abstract page", p.ID)
		default:
			return err
		}
	}

	annotateDepth(p, depth)
	return nil
}

// annotateDepth records how deep the read went and what that left out.
func annotateDepth(p *Paper, depth Depth) {
	p.Depth = string(depth)
	p.Missed = depth.Missed(p.ID)
	if !depth.AtLeast(DepthText) {
		return
	}
	// The full text surface is milestone 7. Until it lands, asking for it reads
	// everything below it and says plainly that the last surface was not read,
	// which beats pretending it was.
	p.Missed = append(p.Missed, "the full text was not read; arxiv fulltext "+p.ID+" reads it")
}

// Estimate is what a read is going to cost in wall clock time.
//
// It is a record rather than a printed sentence so the same number is available
// to a script through the JSON output and to a person through -v.
type Estimate struct {
	Papers   int           `json:"papers"`
	Depth    string        `json:"depth"`
	Requests int           `json:"requests"`
	Wall     time.Duration `json:"wall"`
	// CrossesHTML says whether the fifteen second plane is involved, which is
	// the difference between seconds and minutes.
	CrossesHTML bool `json:"crosses_html_plane"`
}

// Estimate reports the cost of reading n papers at a depth.
func EstimateRead(n int, depth Depth) Estimate {
	return Estimate{
		Papers:      n,
		Depth:       string(depth),
		Requests:    n * depth.Requests(),
		Wall:        depth.Cost(n),
		CrossesHTML: depth.CrossesHTMLPlane(),
	}
}

// String is the sentence -v prints before a long read starts.
func (e Estimate) String() string {
	return fmt.Sprintf("%d papers at depth %s is %d requests and about %s",
		e.Papers, e.Depth, e.Requests, e.Wall.Round(time.Second))
}
