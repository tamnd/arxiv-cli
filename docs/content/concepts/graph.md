---
title: "The claim graph"
description: "Twenty predicates, what each end may be, and why they are claims and not facts."
weight: 50
---

A record is one paper.
The graph is what a record asserts about everything around it.

```bash
arxiv edges 1706.03762
```

Every row is a subject, a predicate, an object and the surface it came from.

```bash
arxiv predicates
arxiv predicates authored
```

## The twenty predicates

| Predicate | From | To |
| --- | --- | --- |
| `authored` | name, author | paper |
| `identified_as` | name | author |
| `has_orcid` | author | orcid |
| `affiliated_with` | name | external |
| `primary_category` | paper | category |
| `in_category` | paper | category |
| `cross_listed` | paper | category |
| `subcategory_of` | category | archive |
| `part_of_group` | archive | group |
| `in_set` | category | set |
| `has_version` | paper | paper |
| `supersedes` | paper | paper |
| `published_in` | paper | journal |
| `has_doi` | paper | doi |
| `licensed_under` | paper | license |
| `submitted_by` | name | paper |
| `announced_as` | paper | category |
| `linked_by` | external | paper |
| `cites` | paper | paper, doi, external |
| `has_file` | paper | file |

Each one lists the surfaces that can produce it, so a predicate tells you what it costs before you ask for it.
`cites` is only ever on `s10`, the rendering, which means a citation graph needs `--depth text`.
`linked_by` is only on `s11`, the trackback page, which is a fifteen second request that has to be asked for by name.

## Names and authors are different things

`authored` runs from a name, and `identified_as` runs from a name to a registered author.

That is not pedantry.
arXiv publishes author strings on every metadata surface, and it publishes registered author identities on exactly one, the author identifier page.
Treating "Y. LeCun" on a paper as the same node as `lecun_y_1` would be an assertion nobody made.

So the graph keeps them apart and joins them only where arXiv itself joined them.

## URIs

Every node has a stable URI.

```
ax://paper/1706.03762
ax://paper/1706.03762#v7
ax://name/ashish-vaswani
ax://author/baez_j_1
ax://category/cs.CL
ax://orcid/0000-0002-3300-2109
ax://doi/10.48550/arxiv.1706.03762
ax://external/{sha256-of-the-url}
```

Anything without an identifier of its own, an affiliation string, a journal reference, an external URL, is hashed.
That gives it a stable node without pretending arXiv issued it an id.

## Walking

`arxiv graph` follows the edges out from a seed.

```bash
arxiv graph 1706.03762 --hops 2 --budget 25
arxiv graph 1706.03762 --predicate authored --predicate in_category
arxiv graph hep-th/9711200 --trackbacks
```

One hop is the seed's own claims.
Two hops reads the papers those claims point at.
`--budget` is a request ceiling on the whole walk and it defaults to 25, because a two hop walk from a well cited paper is not a small thing.

`--names` follows author names, which is one search per name, and `--per-name` caps how many papers each of those searches reads.

## RDF

The same claims come out as RDF, in Dublin Core and schema.org.

```bash
arxiv rdf 1706.03762 --format turtle
arxiv rdf --mapping
arxiv rdf 1706.03762 --check
```

`--mapping` prints the predicate to RDF term table and writes nothing.

`--check` is the interesting one: it compares what this tool produces against arXiv's own Dublin Core and the citation meta tags on the abstract page.
A mapping that disagrees with the source is a bug in the mapping, and this is how you find it.

Provenance goes into the output by default, so every statement carries the surface it came from.
`--no-provenance` leaves it out for consumers that cannot take reified statements.

## Stores

Any read can tee into a store.

```bash
arxiv search "attention" --cat cs.CL --db papers.db
arxiv crawl --search "cat:cs.CL" --depth meta --budget 200 --db papers.db
arxiv db stats --store papers.db
arxiv query "select id, title from papers order by first_submitted desc limit 10" --store papers.db
arxiv export --store papers.db --format ndjson > papers.ndjson
arxiv export --store papers.db --claims --format csv > claims.csv
```

`--db` takes a SQLite path or a `postgres://` URL.
`arxiv query` runs read-only SQL, and `arxiv export` writes either the records or the claims table.
