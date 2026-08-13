package cli

import (
	"context"
	"os"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/arxiv"
	"github.com/tamnd/arxiv-cli/pkg/rdf"
)

// cmd_rdf.go writes what was read in somebody else's vocabulary.
//
// It is hand written rather than a kit operation for the same reason `arxiv
// export` is: an operation emits records and the renderer decides how they
// look, and this is not records. It is one byte stream in a format that has its
// own rules about ordering, prefixes and provenance, and pushing it through a
// row renderer would mean pretending a Turtle file is a table.
//
// It still reads arXiv like everything else, so with a reference rather than a
// store it costs exactly what `arxiv edges` costs at the same depth.

func newRDFCmd() kit.Command {
	store := &storeFlag{}
	var (
		format     string
		depth      string
		limit      int
		fromStore  bool
		trackbacks bool
		noProv     bool
		mapping    bool
	)
	return kit.Command{
		Use:   "rdf [ref...]",
		Short: "Write claims as RDF, in Dublin Core and schema.org",
		Group: "graph",
		Long: `Write what arxiv read as RDF.

The vocabulary is not invented. arXiv publishes Dublin Core about every paper
through OAI-PMH and Highwire citation_* tags on every abstract page, so the core
of the mapping is read off arXiv's own surfaces; --mapping prints the table with
a column saying which surface said so. The rows that column calls inferred are
the ones nothing on arXiv asserts, and they use the terms the other tools in
this family export into so that a merged store joins.

  arxiv rdf 1706.03762                        one paper, n-triples on stdout
  arxiv rdf 1706.03762 --format turtle        the same, grouped by subject
  arxiv rdf --from-store --format jsonld      everything a crawl kept
  arxiv rdf --mapping                         what becomes what, and why

Every statement carries where it came from. In n-triples and turtle that is a
quoted triple, << s p o >> prov:wasDerivedFrom <url>, one line per source. In
JSON-LD it is a named graph per source, because JSON-LD has had named graphs
since 1.0 and has no quoted triples at all. --no-provenance turns it off and
roughly halves the file.

Two runs over the same input give the same bytes in all three formats. That is
what makes a dump diffable, and a diff is how somebody notices that arXiv
started saying something different.

Three things are worth knowing before trusting the output. authored and
linked_by turn round on the way out, because schema:author runs from the work to
its author and arxiv writes it the other way. owl:sameAs appears exactly twice,
on identified_as and has_orcid, which are the two places arXiv itself asserts an
identity; a name node is never sameAs anything, because a name is not a person.
And rdf:type carries no provenance where it was inferred: arXiv said a paper's
dc:type is text, it never said schema:ScholarlyArticle.`,
		Flags: func(f *kit.FlagSet) {
			store.bind(f)
			f.StringVar(&format, "format", rdf.FormatNT, "nt, turtle or jsonld")
			f.StringVar(&depth, "depth", "meta", "how many surfaces to read: quick, meta, full or text")
			f.BoolVar(&fromStore, "from-store", false, "write a store instead of reading arXiv")
			f.IntVar(&limit, "limit", 0, "stop after this many records from the store")
			f.BoolVar(&trackbacks, "trackbacks", false, "add the inbound links, one request on the fifteen second plane")
			f.BoolVar(&noProv, "no-provenance", false, "leave out where each statement came from")
			f.BoolVar(&mapping, "mapping", false, "print the mapping table and write nothing")
		},
		Run: func(ctx context.Context, args []string) error {
			if mapping {
				return emitAll(ctx, mappingRows())
			}
			if !contains(rdf.Formats, format) {
				return errs.Usage("the --format value %q is not one of %s", format, strings.Join(rdf.Formats, ", "))
			}
			o := rdf.Options{Format: format, Provenance: !noProv}

			switch {
			case fromStore && len(args) > 0:
				return errs.Usage("--from-store writes the whole store, so it takes no references")
			case fromStore:
				d, err := docFromStore(store.resolve(ctx), "", limit)
				if err != nil {
					return err
				}
				return writeDoc(d, o)
			case len(args) == 0:
				return errs.Usage("give a reference to write, or --from-store to write a store")
			}

			c, err := clientOf(ctx)
			if err != nil {
				return err
			}
			d, err := arxiv.ParseDepth(depth)
			if err != nil {
				return errs.Usage("%s", err.Error())
			}
			doc := rdf.New()
			for _, ref := range args {
				one, err := c.RDF(ctx, ref, arxiv.RDFOptions{Depth: d, Trackbacks: trackbacks})
				if err != nil {
					return err
				}
				merge(doc, one)
			}
			return writeDoc(doc, o)
		},
	}
}

// writeDoc puts a document on stdout and says nothing if there is nothing to
// say, because an empty file is a result and a message on stdout would corrupt
// the one thing this command writes.
func writeDoc(d *rdf.Doc, o rdf.Options) error {
	if d.Len() == 0 {
		return errs.NoResults("there is nothing to write")
	}
	if n := d.Refused(); n > 0 {
		say("arxiv: %d claims named a node with no rdf name and were left out", n)
	}
	return rdf.Write(os.Stdout, d, o)
}

// merge folds one document into another, which is how several references end up
// in one file with the shared nodes joined rather than repeated.
func merge(into, from *rdf.Doc) {
	for _, s := range from.Statements() {
		into.Add(s.Subject, s.Predicate, s.Object, s.Sources...)
	}
}

// docFromStore builds a document out of everything a store kept.
//
// The claims are the graph and the records carry the literals, so both are read:
// a store of ten thousand papers with no titles in the output would be a graph
// nobody can read. kind narrows it to one record type for `arxiv export --kind`,
// and empty means all of them.
func docFromStore(path, kind string, limit int) (*rdf.Doc, error) {
	st, err := arxiv.OpenStoreReadOnly(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = st.Close() }()

	d := rdf.New()
	want := func(k string) bool { return kind == "" || kind == k }
	if want("paper") {
		papers, err := st.Papers(limit)
		if err != nil {
			return nil, err
		}
		for _, p := range papers {
			arxiv.AddPaper(d, p)
		}
	}
	if want("category") {
		cats, err := st.Categories(limit)
		if err != nil {
			return nil, err
		}
		for _, c := range cats {
			arxiv.AddCategory(d, c)
		}
	}
	if want("author") {
		people, err := st.People(limit)
		if err != nil {
			return nil, err
		}
		for _, p := range people {
			arxiv.AddPerson(d, p)
		}
	}
	if want("set") {
		sets, err := st.Sets(limit)
		if err != nil {
			return nil, err
		}
		for _, s := range sets {
			arxiv.AddSet(d, s)
		}
	}

	// The claims come last and unfiltered by kind, because a claim is about two
	// nodes and dropping the ones that touch another kind would leave a paper
	// with no authors in an export of papers.
	claims, err := st.Claims("", "", "", 0)
	if err != nil {
		return nil, err
	}
	arxiv.AddClaims(d, claims)
	return d, nil
}

// mappingRow is one line of `arxiv rdf --mapping`.
type mappingRow struct {
	What     string `json:"arxiv" kit:"id" table:"arxiv"`
	Kind     string `json:"kind" table:"kind"`
	RDF      string `json:"rdf" table:"rdf"`
	Evidence string `json:"evidence" table:"evidence"`
}

// mappingRows is the table with the empty evidence column spelled out.
//
// An empty cell under a heading reading evidence would read as "nobody looked",
// and the difference between a term arXiv publishes and a term this tool chose
// is the one thing somebody reading this table came for.
func mappingRows() []mappingRow {
	out := make([]mappingRow, 0, len(rdf.Mapping))
	for _, r := range rdf.Mapping {
		evidence := r.Evidence
		if evidence == "" {
			evidence = "inferred"
		}
		what := r.What
		if r.Reverse {
			what += " (reversed)"
		}
		out = append(out, mappingRow{What: what, Kind: r.Kind, RDF: r.Written, Evidence: evidence})
	}
	return out
}
