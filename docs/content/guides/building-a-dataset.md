---
title: "Building a dataset"
description: "Crawl into a store, query it with SQL, export it, and keep every read accounted for."
weight: 40
---

Any read can tee into a store with `--db`, and `arxiv crawl` is the read that keeps going.

```bash
arxiv crawl --search "cat:cs.CL" --max 100 --depth meta --budget 20
arxiv crawl 1706.03762 --hops 2
arxiv crawl --resume --budget 200
```

A seed is a paper id, a category code, or an `ax://` uri.
`--search` seeds from a query in arXiv's own grammar instead, which is the cheapest start there is: one request is a hundred papers.

## The budget is two numbers

`--budget` is the api plane, where a request is three seconds.
`--html-budget` is arxiv.org, where `robots.txt` asks for fifteen.

Fifty requests is two and a half minutes on one and twelve and a half on the other, and a single budget would have to be wrong about one of them.

```bash
arxiv crawl --search "cat:cs.CL" --depth full --budget 200 --html-budget 20
arxiv crawl --search "cat:cs.CL" --api-only --budget 500
```

`--api-only` queues nothing on arxiv.org whatever else is asked for, which is the setting for an overnight run that must not be slow.

The plan is printed before anything is read and confirmed at a terminal.
`--yes` skips the question, which is what a script wants.

## What a crawl leaves behind

Every request goes into the store's read log as it happens, and a manifest of the run is written at the end whether the run finished, ran out of budget, or was interrupted.

A crawl stopped with ctrl-c keeps everything it had already read.
The store is written as it goes and not at the end, so there is no run that costs an hour and produces nothing.

```bash
arxiv db stats --store arxiv.db
```

Nodes by kind, claims by predicate, and reads by plane, surface and status.

The read log is the section to look at first.
A crawl that spent its whole budget on 404s and a crawl that worked look identical in the other two, and the plane column is the one that says where the afternoon went.

Each kind of node gets a second row when some of them have not been read.
That number is the frontier, and it is what `--resume` picks up.

## Querying

```bash
arxiv query "select predicate, count(*) c from claims group by 1 order by c desc"
arxiv query "select uri from nodes where kind='paper' and record is null limit 20"
```

The string goes straight to SQLite.
There is no query language of this tool's own on purpose: the answer to what a store says should be a query somebody already knows how to write, and the schema is three tables a person can hold in their head.

```
nodes   uri, kind, record, first_seen, last_seen
claims  from_uri, predicate, to_uri, source, surface, note, position, seen_at
reads   url, surface, plane, status, bytes, at, error
```

The file is opened `mode=ro`, so a finger slip that says `delete` is refused by SQLite rather than by a check in this tool.
A check here would be one regular expression away from being wrong, and the database has the answer already.

That second query is the frontier: every paper the store has heard of and nobody has read.

## Exporting

```bash
arxiv export --store arxiv.db --format ndjson > papers.ndjson
arxiv export --store arxiv.db --format json > papers.json
arxiv export --store arxiv.db --claims --format csv > claims.csv
arxiv export --store arxiv.db --kind category --format csv > categories.csv
```

`--kind` picks paper, category, author or set.
`--claims` writes the claims table instead of the records.

For RDF, go through `arxiv rdf --from-store` instead.

```bash
arxiv rdf --from-store --store arxiv.db --format turtle > arxiv.ttl
```

## Archiving one paper whole

```bash
arxiv archive 1706.03762 --files
```

That writes every surface of a paper to disk, the raw bytes as served, one file per surface.
`--files` adds the PDF and the submission source.

It is the thing to reach for when you want to be able to prove later what arXiv said today.

## Housekeeping

```bash
arxiv db vacuum --store arxiv.db
```

A store that has been crawled and re-crawled carries slack, and vacuum compacts it.
