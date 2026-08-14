---
title: "Troubleshooting"
description: "The exit codes, and what each of the common failures actually means."
weight: 40
---

Almost everything here is arXiv telling you something rather than a defect.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | it worked, including a result that is genuinely empty |
| 1 | something went wrong that has no better code |
| 2 | usage, including a query arXiv itself rejected |
| 3 | no results |
| 4 | authentication needed, which nothing in this tool should ever hit |
| 5 | rate limited and it never cleared |
| 6 | not found |
| 7 | unsupported, such as a paper arXiv never rendered |
| 8 | network, DNS, connection reset or timeout |

The same code is used everywhere, and the HTTP surface maps it to a status, so a script and a caller of `arxiv serve` get the same answer for the same problem.

An empty result is exit 0.
A paper with no trackbacks is not an error, "no external page has linked to this" is a true answer, and it comes back as an empty list.

## A command sits there doing nothing

Check whether it is on the html plane.

Fifteen seconds a request is the pace `arxiv.org/robots.txt` asks for, so `arxiv fulltext` taking half a minute and `arxiv list --all` taking twenty minutes are both the tool working correctly.

```bash
arxiv planes
```

`-v` prints each request as it goes out, which is the fastest way to tell a slow plane apart from a hang.

## A pause of a minute or more

That is the 429 backoff.

arXiv answers a too-fast request with fourteen bytes reading `Rate exceeded.` and no `Retry-After` header, then stalls the next connection rather than answering it.
So the client waits 60 seconds, doubles up to a ten minute ceiling, and holds the whole plane for the duration so a second read does not trip it again immediately.

`-v` prints the backoff.
If it never clears, the command exits 5 with the elapsed time.

Nothing you pass will make this go faster.
A `--rate` below its floor is refused, and the floor exists because ignoring `robots.txt` is how a tool gets an IP blocked for everyone using it.

## A category returns nothing

Check the code.

```bash
arxiv categories --group physics
arxiv category cs.CL
```

`--cat` refuses a code arXiv does not have before any request goes out.
A query built with `--raw` goes through untouched, though, and arXiv answers a wrong code inside a raw query with zero results and no error.

That reads as an empty category rather than as a typo, which is the whole reason `--cat` exists.

## A search finds fewer papers than expected

The likely cause is quoting.

```bash
arxiv search --raw 'ti:"large language model"'   # right
arxiv search --raw 'ti:large language model'     # not the same query
```

Unquoted, that is `ti:large` and two loose terms, which returns a different set and looks like a perfectly good answer.

```bash
arxiv grammar
```

is the whole grammar with examples that work.

## A field is missing from the output

Two different things wear the same look, and the record tells them apart.

```bash
arxiv paper 1706.03762 -o json | jq '.[0].missed'
arxiv fields --depth full
```

If the field is named in `missed`, it was not read, and the line says which depth reads it.
If it is not in `missed`, the read looked and arXiv does not have it.

A field missing from a table might also just be a column the table did not print, so check `-o json` before concluding anything.

## Cannot get past ten thousand results

That is arXiv's index window and it does not move with the query, the sort or the time of day.

```bash
arxiv search --cat cs.CL --from 2026-01 --all
```

`--all` slices the query by date so each slice fits, and walks in submission order.

A listing has no such window, so `arxiv list cs.CL 2026-01 --all` is the way to read a whole month of a busy category.

## Full text says the paper was never rendered

Exit 7 means arXiv has no rendering, not that the read failed.

arXiv renders papers submitted since December 2023 and some earlier ones, and there is no pattern to the earlier ones worth guessing at.

```bash
arxiv paper <id> --depth full -o json | jq '.[0].has_html'
```

## The binary is not on your PATH

`go install` puts it in `$(go env GOPATH)/bin`, usually `~/go/bin`, and a release archive leaves it wherever you unpacked it.

See [installation](/getting-started/installation/).

## Seeing exactly what happened

```bash
arxiv paper 1706.03762 --depth full -vv
arxiv routes
arxiv paper 1706.03762 -o json | jq '.[0].sources'
```

`-v` prints requests and `-vv` adds cache hits.
`arxiv routes` lists every URL the tool can ever build, so a URL you saw can be confirmed as one of them.
And `sources` on any record is the list of URLs that record was built from, which you can open by hand.
