package arxiv

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/graph"
)

type edgesIn struct {
	Ref        string   `kit:"arg" help:"an arXiv id, a versioned id, or an abs URL"`
	Depth      string   `kit:"flag" default:"meta" enum:"quick,meta,full,text" help:"how many surfaces to read, which decides how many claims there are"`
	Predicate  []string `kit:"flag" help:"keep only this predicate, repeatable"`
	Trackbacks bool     `kit:"flag" help:"add the inbound links, one request on the fifteen second plane"`
	Client     *Client  `kit:"inject"`
}

func registerEdges(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "edges",
		Group:   "graph",
		URIType: "claim",
		Summary: "The claims one read asserts, as subject, predicate and object",
		Args:    []kit.Arg{{Name: "ref", Help: "an arXiv id, a versioned id, or an abs URL"}},
		Long: `Print the claims a read already contains.

The claims are free. They come out of the record the read built, so this costs
exactly what arxiv paper costs and returns twenty claims rather than one record:
one export API request carries the author list, the category set, the journal
reference and both DOIs.

--depth buys more of them. full adds the version history, the licence, the
submitter and the files. text adds the author affiliations and the
bibliography, and the bibliography is the only place cites comes from.

Every claim is checked against the predicate table before it is written, so an
edge pointing at the wrong kind of node is refused rather than stored. Nothing
should ever be refused, and when something is, it is printed on stderr rather
than dropped quietly. arxiv predicates prints the table.

Three of them read backwards from the record and are worth knowing about.
authored runs from the name to the paper. submitted_by runs from the submitter's
name to the paper. linked_by runs from the external page to the paper, because a
trackback is somebody else's page linking here.

cites is partial and says so. arXiv publishes no citation graph, so cites is
written from the rendered bibliography and nowhere else, which means --depth
text and a paper arXiv rendered. The fraction of the bibliography that resolved
to something is printed on stderr, because a citation set that looks complete
and is not is worse than one that admits what it missed.`,
	}, func(ctx context.Context, in edgesIn, emit func(*Claim) error) error {
		depth, err := ParseDepth(in.Depth)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		if err := knownPredicates(in.Predicate); err != nil {
			return err
		}
		edges, err := in.Client.Edges(ctx, in.Ref, EdgeOptions{
			Depth:      depth,
			Trackbacks: in.Trackbacks,
			Predicates: in.Predicate,
		})
		if err != nil {
			return mapErr(err)
		}
		return emitAll(claimsOf(edges, in.Client.now().UTC()), emit)
	})
}

type graphIn struct {
	Ref        string   `kit:"arg" help:"an arXiv id, a versioned id, or an abs URL"`
	Hops       int      `kit:"flag" default:"2" help:"how far to walk, where one hop is the seed's own claims"`
	Depth      string   `kit:"flag" default:"meta" enum:"quick,meta,full,text" help:"how deeply each paper is read"`
	Predicate  []string `kit:"flag" help:"keep only this predicate, repeatable"`
	Names      bool     `kit:"flag" help:"follow author names, which is one search each"`
	Budget     int      `kit:"flag" default:"25" help:"the request ceiling for the whole walk"`
	Limit      int      `kit:"flag,name=per-name" default:"25" help:"how many papers a name search reads"`
	Trackbacks bool     `kit:"flag" help:"add the seed's inbound links"`
	Client     *Client  `kit:"inject"`
}

func registerGraph(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "graph",
		Group:   "graph",
		URIType: "claim",
		Summary: "Walk the claim graph out from a reference",
		Args:    []kit.Arg{{Name: "ref", Help: "an arXiv id, a versioned id, or an abs URL"}},
		Long: `Walk out from a paper, breadth first, on a request budget.

One hop is what arxiv edges prints. Each hop after that reads the nodes the last
one named and adds their claims, so a walk is a small crawl that keeps its
result in memory rather than in a store. arxiv crawl is the one that writes to a
store.

The frontier is drained cheapest first. Papers go in one batched id_list read,
which is two hundred papers and several thousand claims for a single request, so
they are always taken before anything else. Categories come out of the taxonomy,
which is one cached table however many of them there are.

Author names are off the frontier unless --names says otherwise, and this is the
flag that costs money. Eight authors is eight searches, and a name search matches
strings, so most of what comes back is papers by other people who write their
name the same way. Doc 04 section 3.3 has the reasoning.

--budget is in requests, because requests are the unit the rate limits are
written in, and it is checked before each read rather than before each request,
so a walk finishes what it started. What went unread is printed rather than left
to be inferred from a short answer.

--predicate filters what comes back and not what is followed. Filtering the
frontier would quietly change what the walk reached, which is a different answer
wearing the same shape.`,
	}, func(ctx context.Context, in graphIn, emit func(*Claim) error) error {
		depth, err := ParseDepth(in.Depth)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		if err := knownPredicates(in.Predicate); err != nil {
			return err
		}
		edges, err := in.Client.Walk(ctx, in.Ref, WalkOptions{
			Hops:       in.Hops,
			Depth:      depth,
			Names:      in.Names,
			Limit:      in.Limit,
			Budget:     in.Budget,
			Trackbacks: in.Trackbacks,
			Predicates: in.Predicate,
		})
		if err != nil {
			return mapErr(err)
		}
		return emitAll(claimsOf(edges, in.Client.now().UTC()), emit)
	})
}

// knownPredicates refuses a filter naming something that is not a predicate.
//
// A typo would otherwise be an empty result, which reads as "arXiv says nothing
// about this" rather than as "you asked for a predicate that does not exist".
func knownPredicates(names []string) error {
	for _, n := range names {
		if _, ok := graph.Lookup(n); !ok {
			return errs.Usage("%q is not a predicate; the twenty are %s", n, strings.Join(graph.Names(), ", "))
		}
	}
	return nil
}
