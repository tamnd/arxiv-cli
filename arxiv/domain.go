package arxiv

import (
	"context"
	"os"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the arxiv kit driver. It carries no state; the per-run Client is
// built by the factory Register hands kit.
//
// Every read is declared here once. That one registration gives the command
// line subcommand, the HTTP route under `arxiv serve`, and the MCP tool under
// `arxiv mcp`, all rendering the same records.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "arxiv",
		Hosts:  []string{Host, "export.arxiv.org"},
		Identity: kit.Identity{
			Binary: "arxiv",
			Short:  "A command line for arXiv papers.",
			Long: `A command line for arXiv papers.

Search arXiv and pull paper metadata, authors, and abstracts. No API key required.

  arxiv search "attention" --cat cs.CL    search a category
  arxiv paper 1706.03762                  one paper by id
  arxiv author "Yann LeCun" -n 20         papers under an author name
  arxiv categories                        the category codes`,
			Site: "https://" + Host,
			Repo: "https://github.com/tamnd/arxiv-cli",
		},
	}
}

// htmlRateFlag is the name of the second pacing flag. arXiv's two planes are
// five times apart, so one --rate cannot serve both.
const htmlRateFlag = "html-rate"

// Register installs the client factory, the extra global flag, and every
// operation onto app.
func (Domain) Register(app *kit.App) {
	// The flag binds to a variable owned by this registration rather than to
	// package state, so two apps in one process do not share a pace.
	htmlRate := HTMLPlane.Pace
	app.GlobalFlags(func(f *kit.FlagSet) {
		f.DurationVar(&htmlRate, htmlRateFlag, HTMLPlane.Pace,
			"minimum gap between arxiv.org requests (floor "+HTMLPlane.Floor.String()+")")
	})
	app.Finalize(func(c *kit.Config) {
		if c.Extra == nil {
			c.Extra = map[string]string{}
		}
		c.Extra[htmlRateFlag] = htmlRate.String()
	})
	app.SetClient(newClient)

	registerSearch(app)
	registerCount(app)
	registerPaper(app)
	registerAuthor(app)
	registerCategories(app)
	registerID(app)
	registerPlanes(app)
}

// newClient is the factory kit calls once per run. An unset framework flag
// leaves the library default in place.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if d, err := htmlRateOf(cfg); err != nil {
		return nil, err
	} else if d > 0 {
		c.HTMLRate = d
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	c.CacheDir = cfg.CacheDir
	c.NoCache = cfg.NoCache
	c.Verbose = cfg.Verbose
	// Stderr is the log whatever the verbosity, because the notices that ignore
	// -v need somewhere to go. logf still waits to be asked.
	c.Log = os.Stderr

	client, err := NewClient(c)
	if err != nil {
		// A pace under its floor is a flag the user typed, so it is a usage
		// error rather than a failure to build a client.
		return nil, errs.Usage("%s", err.Error())
	}
	return client, nil
}

// htmlRateOf reads the --html-rate value the finalize hook parked on the
// config. An unparseable value cannot come from the flag parser, so it can only
// come from a caller building a Config by hand, and it says so.
func htmlRateOf(cfg kit.Config) (time.Duration, error) {
	raw, ok := cfg.Extra[htmlRateFlag]
	if !ok || raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, errs.Usage("--%s %q is not a duration", htmlRateFlag, raw)
	}
	return d, nil
}

// searchIn is the flag set of `arxiv search`. Every query flag here maps to one
// clause of the grammar in spec 3006 doc 02 section 2.
//
// countIn below repeats the query half rather than embedding it, because kit
// binds the fields a struct declares and not the ones it promotes. A test
// asserts the two lists stay identical, which is the part that would otherwise
// drift.
type searchIn struct {
	Query    string   `kit:"arg" help:"what to search for, matched against every field"`
	Raw      string   `kit:"flag" help:"a query in arXiv's own grammar, sent unchanged"`
	Category []string `kit:"flag,name=cat" help:"a category code such as cs.LG, repeatable and OR'd"`
	Author   string   `kit:"flag" help:"match the author field"`
	Title    string   `kit:"flag" help:"match the title"`
	Abstract string   `kit:"flag" help:"match the abstract"`
	Comment  string   `kit:"flag" help:"match the author comment"`
	Journal  string   `kit:"flag" help:"match the journal reference"`
	Report   string   `kit:"flag" help:"match the report number"`

	From        string `kit:"flag" help:"submitted on or after this date: 2026, 2026-01 or 2026-01-01"`
	To          string `kit:"flag" help:"submitted on or before this date, inclusive to the end of the period"`
	UpdatedFrom string `kit:"flag,name=updated-from" help:"last updated on or after this date"`
	UpdatedTo   string `kit:"flag,name=updated-to" help:"last updated on or before this date"`

	Sort   string  `kit:"flag" enum:"relevance,submitted,updated" help:"sort order, relevance by default and submitted under --all"`
	Order  string  `kit:"flag" default:"desc" enum:"desc,asc" help:"sort direction"`
	All    bool    `kit:"flag" help:"walk the whole result set, past the ten thousand result window"`
	Limit  int     `kit:"flag,short=n,inherit" help:"how many papers to return"`
	Client *Client `kit:"inject"`
}

// options is the library form of the flags.
func (in searchIn) options() SearchOptions {
	return SearchOptions{
		Query:       in.Query,
		Raw:         in.Raw,
		Categories:  in.Category,
		Author:      in.Author,
		Title:       in.Title,
		Abstract:    in.Abstract,
		Comment:     in.Comment,
		Journal:     in.Journal,
		Report:      in.Report,
		From:        in.From,
		To:          in.To,
		UpdatedFrom: in.UpdatedFrom,
		UpdatedTo:   in.UpdatedTo,
		Sort:        in.Sort,
		Order:       in.Order,
		Limit:       in.Limit,
		All:         in.All,
	}
}

func registerSearch(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		URIType: "paper",
		Summary: "Search arXiv papers",
		Args:    []kit.Arg{{Name: "query", Help: "what to search for", Optional: true}},
		Long: `Search arXiv papers.

The positional query is matched against every indexed field. The field flags
are arXiv's own search prefixes under readable names, so --title attention
--author vaswani sends ti:attention AND au:vaswani, and -v prints both the
query that was built and the URL it went out on.

--cat takes a category code and repeats, and the codes are OR'd together. A
bare archive code such as hep-th or cs matches whether or not that archive was
split into categories.

The date flags take 2026, 2026-01 or 2026-01-01. A bound names a period rather
than an instant, so --to 2026-01 means the end of January and not the start of
it.

--raw sends a query through untouched, for the parts of the grammar the flags
do not cover: parentheses, OR, ANDNOT and quoted phrases.

--all walks the whole result set. arXiv will not page past ten thousand
results, so a bigger set is cut into date slices that each fit, and the walk
runs in submission order because relevance is recomputed on every request and a
walk ordered by it would both repeat and skip papers.

To learn only how many results there are, use arxiv count, which costs one
request.`,
	}, func(ctx context.Context, in searchIn, emit func(*Paper) error) error {
		if err := in.Client.SearchStream(ctx, in.options(), emit); err != nil {
			return mapErr(err)
		}
		return nil
	})
}

// countIn is searchIn without the ordering flags, which a count has no use for.
type countIn struct {
	Query    string   `kit:"arg" help:"what to search for, matched against every field"`
	Raw      string   `kit:"flag" help:"a query in arXiv's own grammar, sent unchanged"`
	Category []string `kit:"flag,name=cat" help:"a category code such as cs.LG, repeatable and OR'd"`
	Author   string   `kit:"flag" help:"match the author field"`
	Title    string   `kit:"flag" help:"match the title"`
	Abstract string   `kit:"flag" help:"match the abstract"`
	Comment  string   `kit:"flag" help:"match the author comment"`
	Journal  string   `kit:"flag" help:"match the journal reference"`
	Report   string   `kit:"flag" help:"match the report number"`

	From        string `kit:"flag" help:"submitted on or after this date: 2026, 2026-01 or 2026-01-01"`
	To          string `kit:"flag" help:"submitted on or before this date, inclusive to the end of the period"`
	UpdatedFrom string `kit:"flag,name=updated-from" help:"last updated on or after this date"`
	UpdatedTo   string `kit:"flag,name=updated-to" help:"last updated on or before this date"`

	Client *Client `kit:"inject"`
}

// options is the library form of the flags. A count has no ordering, so the
// sort is left at its default and never reaches the wire in a way that matters.
func (in countIn) options() SearchOptions {
	return SearchOptions{
		Query:       in.Query,
		Raw:         in.Raw,
		Categories:  in.Category,
		Author:      in.Author,
		Title:       in.Title,
		Abstract:    in.Abstract,
		Comment:     in.Comment,
		Journal:     in.Journal,
		Report:      in.Report,
		From:        in.From,
		To:          in.To,
		UpdatedFrom: in.UpdatedFrom,
		UpdatedTo:   in.UpdatedTo,
	}
}

func registerCount(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "count",
		Group:   "read",
		Single:  true,
		URIType: "count",
		Summary: "Count the results a query has",
		Args:    []kit.Arg{{Name: "query", Help: "what to search for", Optional: true}},
		Long: `Count the results a query has, without fetching any of them.

Takes the same flags as arxiv search. One request, and the number comes from
the opensearch:totalResults element the API puts on every feed, so this is the
cheapest way to find out whether a query is worth running.

The request asks for one result rather than none, because max_results=0 answers
500 with an internal error.`,
	}, func(ctx context.Context, in countIn, emit func(*Count) error) error {
		count, err := in.Client.CountSearch(ctx, in.options())
		if err != nil {
			return mapErr(err)
		}
		return emit(&count)
	})
}

type paperIn struct {
	ID     string  `kit:"arg" help:"an arXiv id, a versioned id, or an abs URL"`
	Depth  string  `kit:"flag" default:"meta" enum:"quick,meta,full,text" help:"how many surfaces to read"`
	Client *Client `kit:"inject"`
}

func registerPaper(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "paper",
		Group:   "read",
		Single:  true,
		URIType: "paper",
		Summary: "Fetch a single paper by arXiv id",
		Args:    []kit.Arg{{Name: "id", Help: "an arXiv id, a versioned id, or an abs URL"}},
		Long: `Fetch a single paper by its arXiv id.

The id may be bare (1706.03762), versioned (1706.03762v7), an old-style id
(hep-th/9711200), or a full URL.

--depth chooses how many of arXiv's surfaces to read. quick is one request to
the export API. meta adds the OAI record, which is where the report number, the
subject classes and the structured author names live. full adds the version
history and the abstract page, and the abstract page is on the fifteen second
plane, so it costs fifteen seconds a paper. Whatever a depth did not look at is
named in the missed field rather than left as a zero.`,
	}, func(ctx context.Context, in paperIn, emit func(*Paper) error) error {
		depth, err := ParseDepth(in.Depth)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		paper, err := in.Client.PaperAt(ctx, in.ID, PaperOptions{Depth: depth})
		if err != nil {
			return mapErr(err)
		}
		return emit(&paper)
	})
}

type authorIn struct {
	Name   string  `kit:"arg" help:"an author name, surname or full"`
	Limit  int     `kit:"flag,short=n,inherit" default:"10" help:"how many papers to return"`
	Client *Client `kit:"inject"`
}

func registerAuthor(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "author",
		Group:   "read",
		List:    true,
		URIType: "paper",
		Summary: "List papers under an author name",
		Args:    []kit.Arg{{Name: "name", Help: "an author name, surname or full"}},
		Long: `List papers under an author name.

The name goes to the arXiv au: field prefix, so this is a string match on the
author field rather than a person. Results come back newest first.`,
	}, func(ctx context.Context, in authorIn, emit func(*Paper) error) error {
		papers, err := in.Client.SearchByAuthor(ctx, in.Name, in.Limit)
		if err != nil {
			return mapErr(err)
		}
		return emitAll(papers, emit)
	})
}

type categoriesIn struct{}

func registerCategories(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "categories",
		Group:   "read",
		List:    true,
		URIType: "category",
		Summary: "List arXiv category codes",
		Long:    "Print the arXiv category codes. No network call.",
	}, func(_ context.Context, _ categoriesIn, emit func(*Category) error) error {
		return emitAll(CommonCategories(), emit)
	})
}

// emitAll streams a slice through the emit callback by pointer, so nothing is
// copied on the way out.
func emitAll[T any](records []T, emit func(*T) error) error {
	for i := range records {
		if err := emit(&records[i]); err != nil {
			return err
		}
	}
	return nil
}
