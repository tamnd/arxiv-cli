package arxiv

import (
	"context"

	"github.com/tamnd/any-cli/kit"
)

func init() { kit.Register(Domain{}) }

// Domain is the arxiv kit driver. It carries no state; the per-run Client is
// built by the factory Register hands kit.
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

Search arXiv and pull paper metadata, authors, and abstracts. No API key required.`,
			Site: "https://" + Host,
			Repo: "https://github.com/tamnd/arxiv-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "papers", List: true,
		URIType: "paper", Summary: "Search arXiv papers by keyword",
		Args: []kit.Arg{{Name: "query", Help: "search query"}}}, searchCmd)

	kit.Handle(app, kit.OpMeta{Name: "paper", Group: "papers", Single: true,
		URIType: "paper", Summary: "Fetch a single paper by arXiv ID",
		Args: []kit.Arg{{Name: "id", Help: "arXiv ID or URL (e.g. 1706.03762)"}}}, paperCmd)
}

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
	Query    string  `kit:"arg" help:"search query"`
	Category string  `kit:"flag" help:"arXiv category filter (e.g. cs.LG)"`
	Sort     string  `kit:"flag" help:"sort: relevance, date, updated"`
	Limit    int     `kit:"flag,inherit" help:"max results"`
	Client   *Client `kit:"inject"`
}

func searchCmd(ctx context.Context, in searchIn, emit func(*Paper) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	papers, err := in.Client.Search(ctx, SearchOptions{
		Query:    in.Query,
		Category: in.Category,
		Sort:     in.Sort,
		Limit:    limit,
	})
	if err != nil {
		return err
	}
	for i := range papers {
		if err := emit(&papers[i]); err != nil {
			return err
		}
	}
	return nil
}

type paperIn struct {
	ID     string  `kit:"arg" help:"arXiv ID or URL (e.g. 1706.03762)"`
	Client *Client `kit:"inject"`
}

func paperCmd(ctx context.Context, in paperIn, emit func(*Paper) error) error {
	id, err := ParsePaperID(in.ID)
	if err != nil {
		return err
	}
	p, err := in.Client.Paper(ctx, id)
	if err != nil {
		return err
	}
	return emit(&p)
}
