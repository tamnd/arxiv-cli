---
title: "Quick start"
description: "Run your first arxiv command."
weight: 30
---

Once `arxiv` is on your `PATH`:

```bash
arxiv search "attention" --cat cs.CL
```

That prints a table if you are at a terminal and NDJSON if you are piping.
No flag needed either way.

## One paper

```bash
arxiv paper 1706.03762
```

One request, and you get the id in both styles, the version, both DOIs, the title, the abstract, the comment, the journal reference, the authors and the categories.

```bash
arxiv paper 1706.03762 --depth full
```

Four requests, and you also get the version history with sizes and source types, the submitter, the licence, the report number, the subject classes and the full category names.
It takes about twenty seconds, because the fourth request is on arxiv.org and the tool waits fifteen seconds before touching that host.
See [depth](/concepts/depth/) for what each level costs and buys.

## Look before you fetch

Five commands answer from tables the binary carries, with no request at all.

```bash
arxiv fields                  # every field on a paper, where it comes from, what it costs
arxiv fields --depth quick    # what one request actually gets you
arxiv surfaces                # the twelve places this tool reads
arxiv routes                  # every URL it will ever request
arxiv grammar                 # the query language, with examples that run
arxiv planes                  # the pace it keeps, and why
```

`arxiv id` is the sixth, and it classifies a reference before a request goes out.

```bash
arxiv id hep-th/9711200
arxiv id 10.48550/arXiv.1706.03762
arxiv id https://arxiv.org/abs/2401.00001v3
```

## Browsing

```bash
arxiv categories                  # every category code and name
arxiv category cs.CL              # one category, with its group and archive
arxiv list cs.CL 2026-01          # a month of a category
arxiv new cs.CL                   # today's announcements, with the announce type
```

## Piping

Every command prints NDJSON to a pipe, so `jq` works with no flag.

```bash
arxiv search "transformer" --cat cs.LG -n 50 | jq -r '.id + "  " + .title'
arxiv search "diffusion" --from 2026-01 --fields id,title,submitted -o csv > papers.csv
```

`--fields` takes a comma separated allowlist and works with every format.

## Next

- [Searching](/guides/searching/) covers the query grammar, the 10,000 result window and how to walk past it.
- [Reading a paper](/guides/reading-a-paper/) covers depth, the full text and the files.
- [The CLI reference](/reference/cli/) is every command and flag.
