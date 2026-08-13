// Package cli assembles the arxiv command tree on top of the arxiv library and
// the any-cli/kit framework. Every read is declared once as a kit operation in
// the arxiv domain, so the same definition drives the command line, the HTTP
// routes under `arxiv serve`, and the MCP tools under `arxiv mcp`.
package cli

import (
	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/arxiv-cli/arxiv"
)

// Build metadata, set via -ldflags at release time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// NewApp builds the kit application: identity, the arXiv defaults, and the
// operations the domain registers.
func NewApp() *kit.App {
	def := arxiv.DefaultConfig()

	app := kit.New(kit.Identity{
		Binary:  "arxiv",
		Version: Version,
		Short:   "A command line for arXiv",
		Long: `arxiv reads arXiv's public surfaces and turns preprint metadata into clean
structured records. No API key, no login, nothing to configure.

Quick start:
  arxiv search "attention" --cat cs.CL      search a category
  arxiv paper 1706.03762                    one paper by id
  arxiv author "Yann LeCun" -n 20           papers under an author name
  arxiv categories                          the category codes

arxiv is an independent tool and is not affiliated with arXiv or Cornell University.`,
		Site: "https://arxiv.org",
		Repo: "https://github.com/tamnd/arxiv-cli",
	}, kit.WithDefaults(defaults(def)))

	(arxiv.Domain{}).Register(app)
	app.AddCommand(newVersionCmd())
	return app
}

// defaults seeds the framework baseline from the arXiv defaults, so an unset
// --rate, --retries or --timeout keeps the library's own values. The domain
// owns the client factory and reads the resolved config back from there.
func defaults(def arxiv.Config) func(*kit.Config) {
	return func(c *kit.Config) {
		c.Rate = def.Rate
		c.Retries = def.Retries
		c.Timeout = def.Timeout
		c.UserAgent = userAgent()
	}
}

// userAgent is what arXiv sees. It carries the version and the repo, because
// arXiv asks that a client identify itself and a contactable one is far less
// likely to be blocked when something goes wrong.
func userAgent() string {
	return "arxiv-cli/" + Version + " (+https://github.com/tamnd/arxiv-cli)"
}
