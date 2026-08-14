---
title: "CLI"
description: "Every command, grouped the way the help groups them, with the flags that matter."
weight: 10
---

```
arxiv <command> [subcommand] [--flags]
```

`arxiv <command> --help` is the authority on any command's flags.
Every command's long help says what it costs and which plane it lands on, so it is worth reading once even for a command you think you know.

## Read

| Command | What it does |
| --- | --- |
| `search [query]` | Search arXiv papers |
| `count [query]` | Count the results a query has, in one request |
| `paper <id>` | Fetch a single paper by arXiv id |
| `author <ref>` | Look up an author by name, or by identifier with `--id` |
| `list <category> [month]` | Browse a category listing by month |
| `new <category>` | Read today's announcement for a category |
| `categories` | List the arXiv categories |
| `category <code>` | Show one category |
| `sets` | List the OAI-PMH sets |
| `files <ref>` | What arXiv serves for a paper |
| `fulltext <ref>` | Read the LaTeXML full text |
| `download <ref>` | Fetch a PDF, HTML or source to a file |
| `trackbacks [ref]` | Inbound links, the external pages that link to a paper |

## Cite

| Command | What it does |
| --- | --- |
| `bibtex [refs...]` | arXiv's own BibTeX entry, or `--local` to build one from the record |
| `cite [refs...]` | bibtex, apa, mla, chicago, ris, csl-json or text, with `-s` |

## Graph

| Command | What it does |
| --- | --- |
| `edges <ref>` | The claims one read asserts |
| `graph <ref>` | Walk the claim graph out from a reference |
| `rdf [ref...]` | Write claims as RDF, in Dublin Core and schema.org |
| `crawl [seed...]` | Walk arXiv into a store, on a budget |
| `query <sql>` | Run read-only SQL over a store |
| `export` | Write a store as JSON, NDJSON or CSV |
| `archive <id>...` | Write every surface of a paper to disk |
| `db stats` | What is in the store, counted three ways |
| `db vacuum` | Compact the store |

## Explain

These six make no network request at all.

| Command | What it does |
| --- | --- |
| `planes` | The pace this tool keeps and why |
| `surfaces` | The twelve places it reads and what each is for |
| `routes` | Every URL it will ever request, with the robots verdict |
| `grammar` | arXiv's query language, with examples that work |
| `fields` | Every field on a paper, where it comes from and what it costs |
| `predicates [name]` | The twenty predicates, with what may be at each end |

`id <ref>` also asks arXiv nothing: it parses a reference locally and prints the canonical id, the style, the DOI, the OAI identifier and the URLs.

## Serve

| Command | What it does |
| --- | --- |
| `serve` | Serve the operations over HTTP as NDJSON |
| `mcp` | Run as an MCP server over stdio |
| `version` | Print version information |
| `completion <shell>` | Shell completion for bash, zsh, fish or powershell |

## Flags every command has

| Flag | Meaning |
| --- | --- |
| `-o`, `--output` | `auto`, `table`, `markdown`, `list`, `json`, `jsonl`, `csv`, `tsv`, `url`, `raw` |
| `--fields` | Comma separated columns to show |
| `--no-header` | Omit the header row |
| `--template` | A Go template applied per record |
| `-n`, `--limit` | Stop after N records, 0 for no limit |
| `--db` | Tee every record into a store |
| `--data-dir` | Override the data directory |
| `--profile` | Named profile to load |
| `--rate` | Minimum gap between api plane requests |
| `--html-rate` | Minimum gap between arxiv.org requests, floor 15s |
| `--retries` | Retry attempts on rate limit or 5xx, -1 for the built-in default |
| `--timeout` | Per request timeout |
| `--no-cache` | Bypass the on-disk caches |
| `--dry-run` | Print actions, do not perform them |
| `--color` | `auto`, `always` or `never` |
| `-v`, `--verbose` | Increase verbosity, repeatable |
| `-q`, `--quiet` | Suppress progress output |

## Flags worth knowing per command

`--depth` is on `paper`, `edges`, `rdf`, `graph` and `crawl`, and takes `quick`, `meta`, `full` or `text`.

`--all` is on `search` and `list`, and means walk the whole set.
On `search` that gets past the ten thousand result window by slicing on date.
On `list` it pages through a month at the fifteen second pace and says how long that will take before it starts.

`--budget` is on `crawl` and `graph`.
On `crawl` it is paired with `--html-budget`, because the two planes are five times apart.

`--store` is on `query`, `export`, `rdf --from-store` and `db`, and defaults to `arxiv.db` under the data directory.
