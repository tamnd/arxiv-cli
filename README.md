# arxiv

A command line for arXiv.

`arxiv` reads arXiv's public surfaces and turns preprint metadata into clean structured records.
One pure Go binary, no API key, no login, nothing to run alongside it.

It reads twelve surfaces rather than one.
The export API answers searches, OAI-PMH carries the version history and the submitter, the abstract page carries the category names, the LaTeXML rendering carries the affiliations, and nine other places carry the rest.
Every record says which surfaces it was built from, which one answered for each field, and what the read did not look at.

`arxiv` is an independent tool and is not affiliated with arXiv or Cornell University.

## Install

```bash
go install github.com/tamnd/arxiv-cli/cmd/arxiv@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/tamnd/arxiv-cli/releases), or run the container image:

```bash
docker run --rm ghcr.io/tamnd/arxiv:latest --help
```

## Quick start

```bash
arxiv search "attention" --cat cs.CL          # search a category
arxiv paper 1706.03762                        # one paper by id
arxiv paper 1706.03762 --depth full           # four surfaces instead of one
arxiv author "Yann LeCun" -n 20               # papers under an author name
arxiv list cs.CL 2026-01                      # a month of a category
arxiv new cs.CL                               # today's announcements
arxiv categories                              # the category codes
```

Every command prints a table when it is talking to a terminal and NDJSON when it is talking to a pipe.
`-o json`, `-o csv`, `-o markdown` and the rest are there when you want to be explicit.

## What a record carries

A paper is not nine fields.
It is the id in both styles, the version and whether it is the latest, both DOIs, the comment, the journal reference, the report number, the MSC and ACM classes, the structured author names, the categories with their full names, the version history with sizes and source types, the licence, the submitter, and the capabilities that say whether a rendering or a source archive exists at all.

```bash
arxiv fields                        # every field, where it comes from, what it costs
arxiv fields --depth quick          # what one request gets you
arxiv surfaces                      # the twelve places, and what each is uniquely good for
arxiv routes                        # every URL this tool will ever request
arxiv grammar                       # the query language, with examples that run
```

None of those five make a request.
They answer the questions you have before you decide to pay for a read.

## Depth

`--depth` is the knob on cost.

| Depth | Requests | What it adds |
| --- | --- | --- |
| `quick` | 1 | the export API alone |
| `meta` | 2 | the report number, the classes, the licence, structured names |
| `full` | 4 | the version history, the submitter, the category names, the capabilities |
| `text` | 5 | affiliations, the section tree, the bibliography |

`quick` and `meta` stay on the fast plane.
`full` and `text` cross onto arxiv.org, where the pace is fifteen seconds a request because that is what arXiv's robots.txt asks for.
`arxiv planes` prints both, with the measurement date.

## Search

```bash
arxiv search "attention is all you need"
arxiv search --title "attention" --cat cs.CL --from 2024-01
arxiv search --author "Vaswani, Ashish" --sort submitted
arxiv count "cat:cs.CL AND ti:transformer"
```

Quote a phrase or you get a list of terms.
`ti:"attention is all you need"` returns tens of results and the unquoted form returns hundreds of thousands, which is the single most common way to get a query wrong.

Seven fields are indexed only by arXiv's search UI and have no API prefix at all: MSC class, ACM class, DOI, ORCID, licence, author identifier and full text.
Passing any of `--msc-class`, `--acm-class`, `--doi`, `--orcid`, `--license`, `--author-id` or `--full-text` routes the whole query onto the slow plane, and the tool says so before it starts.

The export API caps any one query at 10,000 results.
`--all` walks past that by slicing the query on submission date, which is the one timestamp that never moves.

## The graph

Every read can be turned into claims: subject, predicate, object, with the surface that supports each one.

```bash
arxiv edges 1706.03762               # the claims one read asserts
arxiv graph 1706.03762 --depth 2     # walk out from a reference
arxiv rdf 1706.03762                 # Dublin Core and schema.org
arxiv predicates                     # the twenty predicates, with what may be at each end
```

Reads can be teed into a store and queried later.

```bash
arxiv crawl --search "cat:cs.CL" --db papers.db --budget 200
arxiv query "select id, title from papers where primary_category = 'cs.CL'" --db papers.db
arxiv export --db papers.db -o csv
arxiv db stats --db papers.db
```

`--db` works on any command, so a normal read can fill a store as a side effect.
The crawl budget is two numbers rather than one, because a request costs three seconds on one plane and fifteen on the other, and a single number would let the slow plane eat the whole run.

## Serving

```bash
arxiv serve --addr :8080     # every operation over HTTP, NDJSON out
arxiv mcp                    # every operation as an MCP tool, over stdio
```

The command line, the HTTP routes and the MCP tools are the same operations registered once.
A command that exists on one exists on all three.

## Pacing and politeness

The tool keeps two paces because arXiv publishes two.
The API hosts get three seconds between requests and arxiv.org gets fifteen, which is the `Crawl-delay` in its robots.txt.
A 429 backs off and retries rather than failing.

Three routes sit on paths robots.txt disallows: the search UI, the trackback pages and the source archive.
The tool never follows any of them on its own.
Each is requested only when a command names it, which is a browser request made from a command line rather than a crawl.
`arxiv routes` prints all sixteen routes with the verdict against each.

## Development

```
cmd/arxiv/   thin main
cli/         the command tree and the store commands
arxiv/       the library: the client, the surfaces, the model
pkg/axid/    arXiv identifiers, both styles
pkg/graph/   nodes, predicates, the ax: URI space
pkg/rdf/     the RDF writer
pkg/latexml/ the LaTeXML rendering parser
docs/        the documentation site
```

```bash
make build      # ./bin/arxiv
make test       # go test ./...
make vet        # go vet ./...
```

The tests run against saved copies of real arXiv responses, captured with the URL and the date they came from.
There is a second suite behind the `live` build tag that talks to arXiv for real, for the assertions a fixture cannot make.

```bash
go test ./...
go test ./arxiv -tags live
```

## Releasing

Push a version tag and GitHub Actions runs GoReleaser, which builds the archives, the Linux packages, the multi-arch GHCR image, checksums, SBOMs and a cosign signature.

```bash
git tag v0.2.0
git push --tags
```

The Homebrew and Scoop steps self-disable until their tokens exist, so a release works with no extra secrets.

## License

Apache-2.0.
See [LICENSE](LICENSE).
