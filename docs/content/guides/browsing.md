---
title: "Browsing and following"
description: "Listings, announcement feeds, authors, categories and inbound links."
weight: 30
---

Search answers a question.
Browsing is what you do when you do not have one yet.

## A month of a category

```bash
arxiv list cs.CL 2026-01
arxiv list cs.CL --recent
arxiv list cs.CL 2026-01 --skip 100 --show 100
```

This is arXiv's own listing page rather than a search: every paper filed under a category in one month, in arXiv's own order, which is what a person browsing the archive sees.
A search of the same category and month answers a different question and returns a different set in a different order.

The month is `2026-01`.
The four digit form `2601` that older guides document is gone, arXiv answers it with a 404, so it is refused here with the form to type instead.

Paging is `--skip` and `--show`, which are arXiv's own parameters.
arXiv accepts 25, 50, 100, 250, 500, 1000 and 2000 entries a page and answers anything else with an HTTP 400, so a `--show` it would refuse is refused here first.

```bash
arxiv list cs.CL 2026-01 --all -o jsonl > cs-cl-jan.jsonl
```

`--all` walks every page.
This is the fifteen second plane, so the walk says how many requests it will make and how long that is before it starts.

There is no ten thousand result window on a listing, which makes this the only way to read a whole month of a busy category.

A listing row has no abstract and no submission time.
Each record names what it is missing and which command reads it, rather than handing back a half filled paper that looks whole.

## Today's announcements

```bash
arxiv new cs.CL
arxiv new cs.CL --new-only
arxiv new cs.CL --type new --type cross
```

Every item carries an announce type, which is the one field no other arXiv surface publishes.
`new` is a first announcement, `cross` is a paper primarily in another category, `replace` is a new version of a paper already announced here, and `replace-cross` is a new version of a cross listed one.

That distinction is why this is a command and not a feed dump.
The cs.CL feed read on 2026-08-13 had 139 items and 47 of them were replacements, so a reader who cannot filter is being handed a third of a day's noise.

The count under the table is of the whole feed and not of what survived the filter.

arXiv announces on weekdays and the feed says so itself, so a read on a Sunday returns Friday's announcement rather than nothing.

## Authors

```bash
arxiv author "Yann LeCun" -n 20
arxiv author baez_j_1 --id
```

Two lookups wear one name here, and the record says which one ran.

Without `--id` the name goes to arXiv's `au:` prefix, which is a string match on text somebody typed.
Two people share a name, one person publishes under three spellings, and `identified` is false on the record because a name match is not a person.

With `--id` it reads the author identifier page, which is arXiv asserting that a registered person owns a set of papers, and it is the only surface anywhere on arXiv that carries an ORCID.
`identified` is true on that record.

An identifier looks like `baez_j_1` and is never guessed from a name.
The number on the end is arXiv's own way of telling two people with the same surname and initial apart, and guessing it would attribute papers to the wrong person.

A 404 means the author never registered a page, which says nothing about whether they have papers, and the message says so.

## Categories

```bash
arxiv categories
arxiv categories --group physics
arxiv category cs.CL
arxiv sets
```

`arxiv categories` reads arXiv's taxonomy page, so the codes come with their names, their archive and their group.
The export API publishes codes and never names, which is why this is a separate surface.

`arxiv sets` lists the OAI-PMH sets, which is the same idea from the harvesting side.

## Inbound links

```bash
arxiv trackbacks hep-th/9711200
arxiv trackbacks --recent
arxiv trackbacks hep-th/9711200 --resolve
```

A trackback is a blog telling arXiv that it linked to a paper.
It is the only inbound link data arXiv publishes and the only edge in this whole tool that points at a paper rather than away from it.

A paper with no trackbacks is not an error.
Most papers have none, "no external page has linked to this" is a true answer, and it comes back as an empty list and exit 0.
A paper arXiv has never heard of is a different answer and exits 6.

`--recent` reads the site wide feed instead, which is the same data the other way round: recent posts and the papers each one links.
A post linking three papers comes out as three records, because a post that links three papers is three claims.

The `url` on a record is arXiv's redirect and not the blog's own address, because that is what the page publishes.
`--resolve` follows each redirect to find where it goes, which is one request per trackback on the fifteen second plane, so a paper with a hundred trackbacks takes half an hour.
The redirect is followed as far as arXiv's answer and no further: the external site is never contacted.

`robots.txt` disallows `/tb`, so this is only ever read for a paper somebody named, and the crawler never touches it.
