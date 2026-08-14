---
title: "Reading a paper"
description: "One paper through all four depths, plus its files, its full text and its claims."
weight: 20
---

`arxiv paper` takes any reference arXiv would recognise.

```bash
arxiv paper 1706.03762
arxiv paper arXiv:1706.03762v7
arxiv paper hep-th/9711200
arxiv paper https://arxiv.org/abs/1706.03762
```

To see how a reference was understood without spending a request, use `arxiv id`.

```bash
arxiv id "arXiv:1706.03762v7"
```

That parses the id, the version and the style locally and asks arXiv nothing.

## Depth by depth

The default is `--depth meta`, which is two requests on the api plane.

```bash
arxiv paper 1706.03762 --depth quick   # the export API alone
arxiv paper 1706.03762 --depth meta    # adds the report number, MSC and ACM classes, licence
arxiv paper 1706.03762 --depth full    # adds versions, submitter, category names, capabilities
arxiv paper 1706.03762 --depth text    # adds affiliations, the section tree, the bibliography
```

`full` and `text` cross onto arxiv.org at fifteen seconds a request, so they are a different order of cost.
See [depth](/concepts/depth/) for the numbers.

What a read did not look at is on the record.

```bash
arxiv paper 1706.03762 -o json | jq '.[0].missed'
```

## The version history

A paper at v7 and a paper at v1 look identical on the export API, because it publishes no version history at all.

```bash
arxiv paper 1706.03762 --depth full -o json | jq '.[0].versions'
```

Each version carries its date, its size and its source type, out of arXivRaw and the abstract page together.

Pinning a version in the reference reads that version.

```bash
arxiv paper 1706.03762v1
```

## Files

```bash
arxiv files 1706.03762
```

Which files exist is already on the paper record as two capability flags off the abstract page, so the list itself costs nothing beyond the paper read.

A PDF is always there.
HTML is there for papers submitted since December 2023 and for some earlier ones.
Source is there unless the paper was submitted as a PDF.

The size on a source file is arXiv's own figure out of the submission history, and it is labelled as such because it is not the number of bytes that will arrive.
The version table says 1,102 KB for `1706.03762v7` and the archive served is 1,150,988 bytes.
There is no figure at all for the PDF anywhere in the metadata.

```bash
arxiv files 1706.03762 --measure
```

`--measure` asks arxiv.org about each file and fills in the real size, the filename arXiv would give it, the content type and arXiv's sha256.
It is one request per file on the fifteen second plane, and it is a one byte range request rather than a HEAD, because a HEAD comes back through the CDN with no content-length on it.

## Downloading

```bash
arxiv download 1706.03762                        # the PDF
arxiv download 1706.03762 --kind source --extract
```

A file that is already there is not fetched again, and `--force` says to fetch it anyway.
A download in progress is a `.part` file, which is what tells a truncated PDF apart from a whole one, and `--resume` continues it with a range request.

`--extract` unpacks a source archive into a directory named after it.
An entry that would write outside that directory is refused rather than skipped, and a symlink is skipped because nothing in a LaTeX source needs one.

`robots.txt` disallows `/src` and `/e-print`, and arXiv's data policy says full content harvesting is not permitted except by arrangement.
So this is one paper at a time, at the fifteen second pace, for a paper somebody named.
There is no `--all` and there is not going to be one.

## Full text

```bash
arxiv fulltext 1706.03762
```

arXiv renders LaTeX submissions to HTML, and that rendering is the only place arXiv publishes author affiliations, a section tree, or the body of a paper at all.

The read is the abstract page and then the rendering, because `has_html` is on the abstract page and it is the only honest way to know whether a rendering exists.
That is two requests on the fifteen second plane, so the command takes half a minute the first time and nothing at all the second: a rendering never changes, so it is cached for a month.

A paper arXiv never rendered exits 7 and says so.

Maths comes back as the LaTeX the author wrote, out of the `alttext` attribute, because a downstream reader can parse that and cannot parse rendered MathML.

## What the paper claims

```bash
arxiv edges 1706.03762
arxiv edges 1706.03762 --predicate authored
arxiv edges 1706.03762 --depth text --predicate cites
```

Each row is a subject, a predicate, an object and the surface behind it.
See [the claim graph](/concepts/graph/).
