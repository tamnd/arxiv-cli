package arxiv

import (
	"context"

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

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	registerSearch(app)
	registerPaper(app)
	registerAuthor(app)
	registerCategories(app)
	registerID(app)
}

// newClient is the factory kit calls once per run. An unset framework flag
// leaves the library default in place.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient(DefaultConfig())
	if cfg.UserAgent != "" {
		c.userAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.httpClient.Timeout = cfg.Timeout
	}
	return c, nil
}

type searchIn struct {
	Query    string  `kit:"arg" help:"what to search for"`
	Category string  `kit:"flag,name=cat" help:"restrict to a category code, for example cs.LG"`
	Sort     string  `kit:"flag" default:"relevance" enum:"relevance,date,updated" help:"sort order"`
	Limit    int     `kit:"flag,short=n,inherit" default:"10" help:"how many papers to return"`
	Client   *Client `kit:"inject"`
}

func registerSearch(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		URIType: "paper",
		Summary: "Search arXiv papers",
		Args:    []kit.Arg{{Name: "query", Help: "what to search for"}},
		Long: `Search arXiv papers.

The query is matched against every field. Use --cat to restrict to a category
code such as cs.LG or math.NT, and --sort to order by relevance, submission
date, or last update.`,
	}, func(ctx context.Context, in searchIn, emit func(*Paper) error) error {
		papers, err := in.Client.Search(ctx, SearchOptions{
			Query:    in.Query,
			Category: in.Category,
			Sort:     in.Sort,
			Limit:    in.Limit,
		})
		if err != nil {
			return mapErr(err)
		}
		return emitAll(papers, emit)
	})
}

type paperIn struct {
	ID     string  `kit:"arg" help:"an arXiv id, a versioned id, or an abs URL"`
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
(hep-th/9711200), or a full URL.`,
	}, func(ctx context.Context, in paperIn, emit func(*Paper) error) error {
		id, err := ParsePaperID(in.ID)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		paper, err := in.Client.Paper(ctx, id)
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
