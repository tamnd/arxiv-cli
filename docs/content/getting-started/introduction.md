---
title: "Introduction"
description: "What arxiv is, what it reads, and why it is put together this way."
weight: 10
---

`arxiv` is a single Go binary that reads arXiv and turns preprint metadata into clean structured records.

There is nothing to sign up for, no key to paste into a config file and nothing to run alongside it.
Eleven of the twelve surfaces it reads answer an anonymous request with nothing but a user agent, and the twelfth is disallowed by robots rather than gated behind a credential.

## What makes it different

Most arXiv tooling wraps the export API and stops there.
The export API is one surface out of twelve, and there are useful fields it simply does not carry.

The version history with sizes and source types is on OAI-PMH.
The category names, as opposed to the codes, are on the abstract page.
Affiliations, the section tree and the bibliography are on the LaTeXML rendering.
The announce type, which says whether an item is new, cross listed or a replacement, is on the RSS feed and nowhere else.
Seven searchable fields, including MSC class, DOI and ORCID, are indexed only by the search UI.

`arxiv` reads all of them and merges the results into one record.
See [surfaces](/concepts/surfaces/) for the full list, or run `arxiv surfaces`.

## What a record tells you about itself

Every record carries an envelope.

```json
{
  "kind": "paper",
  "surfaces": ["s1", "s2", "s3"],
  "sources": ["https://export.arxiv.org/api/query?id_list=1706.03762", "..."],
  "retrieved_at": "2026-08-14T09:12:44Z",
  "via": {"first_submitted": "s1", "versions": "s2", "subject_names": "s3"},
  "missed": ["affiliations, the licence name and the section tree were not read; arxiv paper 1706.03762 --depth text reads them"],
  "depth": "full"
}
```

`surfaces` is what answered, `sources` is the URLs actually fetched, `via` says which surface stands behind each contested field, and `missed` says in a sentence what this read did not look at and which read would.

A field that was not read is absent, not zero.
A paper with seven versions never serialises as `"versions": []`, because an empty array and an unasked question are different things and a consumer has no way to tell them apart after the fact.

## How it is put together

- `arxiv` is the library: the client, the twelve surfaces, the record model and the merge rules.
- `pkg/axid` classifies arXiv identifiers in all their shapes, without a request.
- `pkg/graph` holds the node kinds, the predicates and the `ax:` URI space.
- `pkg/rdf` writes claims as Dublin Core and schema.org.
- `pkg/latexml` parses the rendered full text.
- `cli` is the command tree.
- `cmd/arxiv` is a thin main.

The command line, the HTTP server and the MCP tools are the same operations registered once, so a command that exists on one exists on all three.

## Scope

`arxiv` reads.
It does not submit, replace, cross list, claim authorship or send trackbacks.
There is no code path in the binary that issues a write, and a test fails the build if one appears.

Next: [install it](/getting-started/installation/), then take the [quick start](/getting-started/quick-start/).
