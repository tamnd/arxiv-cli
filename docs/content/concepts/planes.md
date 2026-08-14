---
title: "Planes and pacing"
description: "Two hosts, two paces, and the rules the client follows so arXiv stays public."
weight: 20
---

arXiv publishes at two speeds, so `arxiv` keeps two limiters.

```bash
arxiv planes
```

| Plane | Hosts | Pace | Floor | Flag |
| --- | --- | --- | --- | --- |
| api | `export.arxiv.org`, `oaipmh.arxiv.org`, `rss.arxiv.org` | 3s | 1s | `--rate` |
| html | `arxiv.org`, `www.arxiv.org` | 15s | 15s | `--html-rate` |

Three seconds is what arXiv's terms of use ask for on the API hosts.
Fifteen seconds is what `https://arxiv.org/robots.txt` asks for with `Crawl-delay: 15`.

The html floor is a floor.
`--html-rate` can make the tool slower and cannot make it faster, and asking for less than fifteen seconds is a usage error rather than something the tool quietly does anyway.
A tool that reads a public archive for free and then hammers it is the reason archives stop being public.

## What this means for a command

The plane a request lands on follows from its host, never from the caller.

`arxiv search` and `arxiv count` are on the api plane, so a query comes back in a few seconds.
`arxiv list`, `arxiv fulltext`, `arxiv trackbacks` and `arxiv categories` are on the html plane, so each request there costs fifteen seconds.
`arxiv paper` is on the api plane at `--depth quick` and `--depth meta`, and crosses to the html plane at `--depth full` and `--depth text`.

That is the whole reason [depth](/concepts/depth/) exists.

The seven search fields the export API has no prefix for are the other place this bites.
Passing `--msc-class`, `--acm-class`, `--doi`, `--orcid`, `--license`, `--author-id` or `--full-text` routes the entire query onto the search UI, and the tool says so before it starts.

```bash
arxiv grammar --kind field
```

prints every field with the plane it lands on.

## When arXiv says no

A 429 from arxiv.org is fourteen bytes reading `Rate exceeded.` with no `Retry-After` header, and the connection after it stalls rather than answering.
It clears after about 45 seconds.

So the client backs off starting at 60 seconds, doubles to a ten minute ceiling, retries up to five times, and holds the whole plane's limiter for the duration so a second read does not trip it again immediately.
`-v` prints the backoff, so a long pause is explained rather than looking like a hang.

If the plane is still answering 429 after the fifth retry the command exits 5 with the elapsed time.

| Condition | Retries | Backoff | Exit if it never clears |
| --- | --- | --- | --- |
| network error, DNS, connection reset | 5 | 1s doubling to 30s | 8 |
| HTTP 429 | 5 | 60s doubling to 10m | 5 |
| HTTP 503 | 3 | 5s doubling | 1 |
| HTTP 400 or 500 with an error entry | none | | 2 |
| HTTP 404 | none | | 6 |

## The cache

No arXiv response carries a validator, so there is nothing to revalidate against and the cache is time based.

| What | TTL |
| --- | --- |
| a paper by id, any surface | 24h |
| a search page | 15m |
| a listing page | 24h for a past month, 15m for the current one |
| a feed | 15m |
| the taxonomy and the OAI set list | 7d |
| full text and BibTeX | 30d, because a rendered version never changes |

`--no-cache` skips both the read and the write.

A cache hit shows up under `-vv` and never in the record.
`retrieved_at` is the time of the original fetch and not the time of the cache read, because whether a byte came off a disk is not a property of the paper.
