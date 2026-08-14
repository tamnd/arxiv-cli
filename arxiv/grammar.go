package arxiv

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
)

// GrammarInfo is one token of arXiv's query language.
//
// arXiv documents the prefixes in a table with no examples and says almost
// nothing about precedence or quoting, which is how a tool ends up sending a
// query that returns zero results and looks like a search that matched nothing.
// The two rules worth knowing are not written down anywhere on arXiv: an
// unquoted multi-word value is a list of terms rather than a phrase, and a
// mixed AND and OR needs parentheses to mean what it looks like it means.
type GrammarInfo struct {
	// Token is the thing you type.
	Token string `json:"token" kit:"id" table:"token"`
	// Kind groups the table: field, operator, range or rule.
	Kind string `json:"kind" table:"kind"`
	// Plane is which pace a query using this is read at. The seven fields only
	// the search UI indexes route the whole query onto the HTML plane, which is
	// fifteen seconds a request rather than three.
	Plane string `json:"plane" table:"plane"`
	// Flag is the command line spelling, where there is one.
	Flag    string `json:"flag,omitempty" table:"flag"`
	Means   string `json:"means" table:"means,truncate"`
	Example string `json:"example" table:"-"`
}

// grammarRows is the table. The nine API prefixes come from Fields, so a prefix
// added to the constant list has to be given a row here or the test fails.
var grammarRows = []GrammarInfo{
	{Token: "all:", Kind: "field", Means: "every indexed field at once, which is what a bare search term becomes",
		Example: `arxiv search attention`},
	{Token: "ti:", Kind: "field", Flag: "--title", Means: "the title",
		Example: `arxiv search 'ti:"attention is all you need"'`},
	{Token: "au:", Kind: "field", Flag: "--author", Means: "the author list as arXiv spells it, which is not a person",
		Example: `arxiv search 'au:"Vaswani, Ashish"'`},
	{Token: "abs:", Kind: "field", Flag: "--abstract", Means: "the abstract",
		Example: `arxiv search 'abs:transformer AND cat:cs.CL'`},
	{Token: "co:", Kind: "field", Flag: "--comment", Means: "the author's comment, which is where page counts and conference names live",
		Example: `arxiv search 'co:"accepted at NeurIPS"'`},
	{Token: "jr:", Kind: "field", Flag: "--journal", Means: "the journal reference, present only on papers that were published",
		Example: `arxiv search 'jr:"Phys.Lett"'`},
	{Token: "cat:", Kind: "field", Flag: "--cat", Means: "a category code; a bare archive is expanded because cat:cs and cat:cs.* match different things",
		Example: `arxiv search attention --cat cs.CL`},
	{Token: "rn:", Kind: "field", Flag: "--report", Means: "the report number, which is a lab's own identifier",
		Example: `arxiv search 'rn:CERN-PH-EP-2012-218'`},
	{Token: "id:", Kind: "field", Means: "the id, though id_list is the cheaper way to ask for known ids",
		Example: `arxiv paper 1706.03762`},

	{Token: "acm_class", Kind: "field", Flag: "--acm-class", Means: "the ACM classification, indexed by the search UI and by no API prefix",
		Example: `arxiv search --acm-class I.2.7`},
	{Token: "msc_class", Kind: "field", Flag: "--msc-class", Means: "the MSC classification, search UI only",
		Example: `arxiv search --msc-class 18D10`},
	{Token: "doi", Kind: "field", Flag: "--doi", Means: "the publisher DOI, search UI only",
		Example: `arxiv search --doi 10.1016/j.physletb.2012.08.020`},
	{Token: "orcid", Kind: "field", Flag: "--orcid", Means: "an ORCID, search UI only",
		Example: `arxiv search --orcid 0000-0002-3300-2109`},
	{Token: "license", Kind: "field", Flag: "--license", Means: "the licence, search UI only",
		Example: `arxiv search --license cc-by-4.0 --cat cs.CL`},
	{Token: "author_id", Kind: "field", Flag: "--author-id", Means: "an arXiv author identifier, search UI only",
		Example: `arxiv search --author-id baez_j_1`},
	{Token: "all_fields", Kind: "field", Flag: "--full-text", Means: "the full text, which the API does not index at all",
		Example: `arxiv search --full-text "attention is all you need"`},

	{Token: "AND", Kind: "operator", Means: "both, and the default between two flags on one command",
		Example: `arxiv search 'ti:transformer AND cat:cs.CL'`},
	{Token: "OR", Kind: "operator", Means: "either; two --cat flags are OR'd for you",
		Example: `arxiv search --cat cs.CL --cat cs.LG`},
	{Token: "ANDNOT", Kind: "operator", Means: "the first without the second, spelled as one word by arXiv",
		Example: `arxiv search 'cat:cs.CL ANDNOT cat:cs.LG'`},
	{Token: "( )", Kind: "operator", Means: "grouping, and a mixed AND and OR needs it to mean what it looks like",
		Example: `arxiv search '(cat:cs.CL OR cat:cs.LG) AND ti:attention'`},

	{Token: "submittedDate:[a TO b]", Kind: "range", Flag: "--from, --to",
		Means:   "the v1 submission time, which never moves, so it is the only safe sort for a long walk",
		Example: `arxiv search --cat cs.CL --from 2026-01 --to 2026-01`},
	{Token: "lastUpdatedDate:[a TO b]", Kind: "range", Flag: "--updated-from, --updated-to",
		Means:   "the most recent version's time, which moves when a paper is revised",
		Example: `arxiv search --cat cs.CL --updated-from 2026-08-01`},
	{Token: "YYYYMMDDHHMM", Kind: "range",
		Means:   "the timestamp a range takes; the eight digit form works too and this tool always sends twelve, because minute resolution is what lets a busy day be cut in half",
		Example: `submittedDate:[202601010000 TO 202601312359]`},

	{Token: `"..."`, Kind: "rule",
		Means:   "a phrase; without the quotes the words are separate terms, which is why ti:attention is all you need returns hundreds of thousands and the quoted form returns tens",
		Example: `arxiv search 'ti:"attention is all you need"'`},
	{Token: "start, max_results", Kind: "rule", Flag: "-n, --all",
		Means:   "paging, capped at 10,000 results per query; --all slices a query by date to walk past that",
		Example: `arxiv search cat:cs.CL --all -n 25000`},
	{Token: "id_list", Kind: "rule",
		Means:   "up to 200 known ids in one request, which is a different question from a search and much cheaper",
		Example: `arxiv paper 1706.03762 1207.7214 hep-th/9711200`},
}

// grammarInfos fills in the plane, which follows from whether the token is one
// of the seven the export API has no prefix for.
func grammarInfos() []GrammarInfo {
	out := make([]GrammarInfo, 0, len(grammarRows))
	for _, r := range grammarRows {
		r.Plane = APIPlane.Name
		if r.Flag != "" && contains(s5Only, r.Flag) {
			r.Plane = HTMLPlane.Name
		}
		out = append(out, r)
	}
	return out
}

// apiPrefixes is the token spelling of the nine fields, which the test uses to
// check every prefix has a row.
func apiPrefixes() []string {
	out := make([]string, 0, len(Fields))
	for _, f := range Fields {
		out = append(out, string(f)+":")
	}
	return out
}

type grammarIn struct {
	Kind string `kit:"flag" help:"show only one kind: field, operator, range or rule"`
}

func registerGrammar(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "grammar",
		Group:   "explain",
		List:    true,
		URIType: "grammar",
		Summary: "Show arXiv's query language, with examples that work",
		Long: `Show the query language the export API and the search UI accept.

arXiv publishes the prefixes in a table with no examples and says almost nothing
about quoting or precedence, so the two rules that actually catch people are not
written down anywhere: an unquoted multi-word value is a list of terms and not a
phrase, and a mixed AND and OR needs parentheses to mean what it reads like.

The plane column matters more than it looks. Seven of these fields are indexed
only by the search UI, and using any one of them routes the whole query onto
arxiv.org at fifteen seconds a request instead of three.

Every example is a command that runs. No network call.`,
	}, func(_ context.Context, in grammarIn, emit func(*GrammarInfo) error) error {
		rows := grammarInfos()
		if kind := strings.ToLower(strings.TrimSpace(in.Kind)); kind != "" {
			var kept []GrammarInfo
			for _, r := range rows {
				if r.Kind == kind {
					kept = append(kept, r)
				}
			}
			rows = kept
		}
		return emitAll(rows, emit)
	})
}
