---
title: "Depth"
description: "How many surfaces one read crosses, and what each level costs and buys."
weight: 30
---

`--depth` decides how many of the twelve surfaces a read of one paper crosses.

It is a knob on cost as much as on completeness.

| Depth | Requests | Planes | About | Adds |
| --- | --- | --- | --- | --- |
| `quick` | 1 | 1 api | 3s | the export API alone |
| `meta` | 2 | 2 api | 6s | the report number, the MSC and ACM classes, the licence, structured author names |
| `full` | 4 | 3 api, 1 html | 25s | the version history with sizes, the submitter, the category names, the html and source capabilities |
| `text` | 5 | 3 api, 2 html | 40s | affiliations, the licence name, the section tree, the bibliography |

`meta` is the default.

The jump that matters is from `meta` to `full`.
`quick` and `meta` stay on the api plane at three seconds a request.
`full` and `text` cross onto arxiv.org, where the pace is fifteen seconds, so a hundred papers at `--depth full` is about forty minutes rather than five.

## Which fields come at which depth

```bash
arxiv fields                  # everything
arxiv fields --depth quick    # what one request gets you
arxiv fields --depth full     # what four get you
```

The `--depth` filter nests: everything `quick` fills, `meta` fills too.

Some fields have no depth at all.
`hits`, `announced_month`, `announced` and `extra` are carried by a search result, a listing row or a feed item rather than by a paper read, because those are read a page at a time rather than a paper at a time.

## What a shallow read says about itself

A record read shallow does not pretend to be a deep one.

```bash
arxiv paper 1706.03762 -o json | jq '.[0].missed'
```

```json
[
  "the submitter, the version history and the html and source capabilities were not read; arxiv paper 1706.03762 --depth full reads them",
  "affiliations, the licence name and the section tree were not read; arxiv paper 1706.03762 --depth text reads them"
]
```

Those are sentences and not codes, because the reader is a person deciding whether to pay for a deeper read, and the answer they want is the command to run.

## The one thing depth cannot buy

`--depth text` reads arXiv's LaTeXML rendering, and arXiv has not rendered every paper.
Anything from before the rendering programme, and anything submitted as PDF only, has no rendering at all.

`has_html` on the record says whether one exists, and a `text` read of a paper without one adds a line to `missed` saying so rather than returning an empty section list that looks like a read that found nothing.

```bash
arxiv paper hep-th/9711200 --depth text -o json | jq '.[0] | .has_html, .missed'
```

## Estimating before you commit

`arxiv crawl` budgets the two planes separately for the same reason.

```bash
arxiv crawl --search "cat:cs.CL" --depth full --budget 200 --html-budget 20
```

`--budget` is the api plane and `--html-budget` is arxiv.org.
Two hundred api requests is ten minutes and twenty html requests is five, and a single combined budget would let the slow plane quietly eat the whole run.
