---
title: "Surfaces"
description: "The twelve places arXiv publishes, and what each one is uniquely good for."
weight: 10
---

arXiv publishes the same paper in twelve different places, and they do not carry the same fields.

`arxiv` reads all twelve and merges the results.
The record that comes back says which ones answered and which one stands behind each field.

```bash
arxiv surfaces
```

That prints the table below from the values the code uses, so it cannot drift out of step with the tool.

| Id | Surface | Plane | Only place you can get |
| --- | --- | --- | --- |
| s1 | the export API | api | search results, and a result count |
| s2 | OAI-PMH | api | the version history with sizes and source types, the submitter, the report number, the licence |
| s3 | the abstract page | html | the category names in full, the file list, whether a rendering exists |
| s4 | the category listing | html | arXiv's own idea of a month, which is announcement order rather than submission order |
| s5 | the search UI | html | the seven fields the export API has no prefix for |
| s6 | the announcement feed | api | the announce type: new, replaced or cross listed |
| s7 | the category taxonomy | html | the group and archive a category sits under, with arXiv's own description |
| s8 | the author identifier page | html | an ORCID against an arXiv author id |
| s9 | the BibTeX entry | html | nothing new, which is the point: it is arXiv's own entry to compare against |
| s10 | the LaTeXML full text | html | affiliations, the section tree, the bibliography |
| s11 | the trackback page | html | who linked to a paper from outside arXiv |
| s12 | the files | html | the bytes, and the real size rather than a rounded one |

## Why twelve and not one

The export API is a good surface and it is missing things.

It has no report number, no MSC or ACM class, no licence and no structured author names, all of which OAI-PMH publishes.
It has no version history at all, so a paper at v7 looks the same as a paper at v1 except for the id.
It publishes category codes and never the names.
It cannot search by DOI, ORCID, MSC class, ACM class, licence, author identifier or full text, because it has no prefix for any of them.

Reading only the export API means silently returning a subset and calling it the paper.

## What a via map is for

Four surfaces carry a title and they agree.
Four carry a comment and they agree.
The submission date is the interesting one: the export API's `published` is the v1 submission, and OAI-PMH's `created` is the current version's date, which on a revised paper is years later.

That is a real disagreement between two surfaces about what looks like the same field, so the record names the surface behind the value that is standing.

```bash
arxiv paper 1706.03762 --depth full -o json | jq '.[0].via'
```

`first_submitted` always reads `s1`, and the tool never fills it from OAI, because doing so would put a 2023 date on a 2017 paper.

## The three gated routes

arxiv.org's `robots.txt` disallows `/search`, `/tb` and `/src`.

`arxiv` never follows any of them on its own.
Each is requested only when a command names it: a search that passes one of the seven search UI only flags, `arxiv trackbacks`, or `arxiv download --kind source` on one paper at a time.
That is a browser request made from a command line rather than a crawl.

```bash
arxiv routes
```

prints all sixteen routes the tool can build, with the robots verdict against each.
The four on the API hosts read "not covered" rather than "allowed", because arxiv.org's `robots.txt` says nothing about `export.arxiv.org`, `oaipmh.arxiv.org` or `rss.arxiv.org`, and claiming otherwise would be a statement about a file nobody read.
