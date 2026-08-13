package arxiv

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// PredicateInfo is one row of the predicate table, printed rather than fetched.
//
// The table is worth a command of its own because it is the schema. An agent
// about to write a query needs to know that authored runs from a name and not
// from a paper, and that cites only ever comes from s10, and neither of those is
// guessable from the data.
type PredicateInfo struct {
	Name string `json:"name" kit:"id" table:"predicate"`
	// From and To are the node kinds each end may be.
	From []string `json:"from" table:"-"`
	To   []string `json:"to" table:"-"`
	// Surfaces is which of s1 to s12 may assert it. A claim from anywhere else
	// is refused before it is written.
	Surfaces []string `json:"surfaces" table:"-"`
	// The three joined forms are for the table only, because a list renders as
	// its length there and "2" is not what somebody reading a schema wants.
	FromText     string `json:"-" table:"from"`
	ToText       string `json:"-" table:"to"`
	SurfacesText string `json:"-" table:"surfaces"`
	Help         string `json:"help" table:"means,truncate"`
	// Example is a real claim in that shape, so the URI spelling is visible
	// rather than described.
	Example string `json:"example" table:"-"`
}

// PredicateURI names the predicate itself, so a store can hold the table.
func PredicateURI(name string) string { return "ax://predicate/" + name }

// examples are one real claim per predicate, taken from papers used throughout
// these docs so they can be checked against a live read.
var examples = map[string]string{
	graph.Authored:        "ax://name/ashish-vaswani authored ax://paper/1706.03762",
	graph.IdentifiedAs:    "ax://name/john-baez identified_as ax://author/baez_j_1",
	graph.HasORCID:        "ax://author/baez_j_1 has_orcid ax://orcid/0000-0002-3300-2109",
	graph.AffiliatedWith:  "ax://name/ashish-vaswani affiliated_with ax://external/{sha256-of-the-affiliation}",
	graph.PrimaryCategory: "ax://paper/1706.03762 primary_category ax://category/cs.CL",
	graph.InCategory:      "ax://paper/1706.03762 in_category ax://category/cs.LG",
	graph.CrossListed:     "ax://paper/1706.03762 cross_listed ax://category/cs.LG",
	graph.SubcategoryOf:   "ax://category/cs.CL subcategory_of ax://archive/cs",
	graph.PartOfGroup:     "ax://archive/cs part_of_group ax://group/computer-science",
	graph.InSet:           "ax://category/cs.CL in_set ax://set/cs",
	graph.HasVersion:      "ax://paper/1706.03762 has_version ax://paper/1706.03762#v7",
	graph.Supersedes:      "ax://paper/1706.03762#v7 supersedes ax://paper/1706.03762#v6",
	graph.PublishedIn:     "ax://paper/1207.7214 published_in ax://journal/{sha256-of-the-normalised-ref}",
	graph.HasDOI:          "ax://paper/1706.03762 has_doi ax://doi/10.48550/arxiv.1706.03762",
	graph.LicensedUnder:   "ax://paper/1706.03762 licensed_under ax://license/arxiv-nonexclusive-distrib-1.0",
	graph.SubmittedBy:     "ax://name/llion-jones submitted_by ax://paper/1706.03762",
	graph.AnnouncedAs:     "ax://paper/2608.02757 announced_as ax://category/cs.CL",
	graph.LinkedBy:        "ax://external/{sha256-of-the-url} linked_by ax://paper/hep-th/9711200",
	graph.Cites:           "ax://paper/1706.03762 cites ax://paper/1409.0473",
	graph.HasFile:         "ax://paper/1706.03762 has_file ax://file/1706.03762#v7.pdf",
}

// PredicateTable is the table as records.
func PredicateTable() []PredicateInfo {
	out := make([]PredicateInfo, 0, len(graph.Predicates))
	for _, p := range graph.Predicates {
		out = append(out, PredicateInfo{
			Name:         p.Name,
			From:         p.From,
			To:           p.To,
			Surfaces:     p.Surfaces,
			FromText:     strings.Join(p.From, ", "),
			ToText:       strings.Join(p.To, ", "),
			SurfacesText: strings.Join(p.Surfaces, ", "),
			Help:         p.Help,
			Example:      examples[p.Name],
		})
	}
	return out
}

type predicatesIn struct {
	Name string `kit:"arg,optional" help:"one predicate to print, all of them by default"`
}

func registerPredicates(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "predicates",
		Group:   "explain",
		URIType: "predicate",
		Summary: "The twenty predicates, with what may be at each end",
		Args:    []kit.Arg{{Name: "name", Help: "one predicate to print", Optional: true}},
		Long: `Print the predicate table.

Every edge this tool writes uses one of these twenty predicates, and an edge
using anything else is refused before it reaches a store. Each row says which
node kinds may be at each end and which surfaces are allowed to assert it.

Two of them read backwards from the record and are worth knowing about.

authored runs from the name to the paper, not the other way, because the query
worth answering is everything this name touched and a claim indexed from the
name answers it without a scan.

linked_by runs from the external page to the paper, because a trackback is
somebody else's page linking here. Writing it the other way round would say the
paper cites the blog.

cites is the one with a surface restriction that matters. arXiv publishes no
citation graph, so cites is only ever written from the rendered bibliography in
the LaTeXML HTML, only at --depth text, and every row says s10 so nobody
mistakes it for something arXiv asserts.

Nothing here is fetched.`,
	}, func(_ context.Context, in predicatesIn, emit func(*PredicateInfo) error) error {
		rows := PredicateTable()
		if in.Name == "" {
			for i := range rows {
				if err := emit(&rows[i]); err != nil {
					return err
				}
			}
			return nil
		}
		for i := range rows {
			if rows[i].Name == in.Name {
				return emit(&rows[i])
			}
		}
		return errs.NotFound("%q is not a predicate; the twenty are %s", in.Name, strings.Join(graph.Names(), ", "))
	})
}
