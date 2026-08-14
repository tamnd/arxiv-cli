---
title: "Records"
description: "What comes back, why it carries its own provenance, and how to read absence."
weight: 40
---

Every record is one JSON object with the paper's fields and an envelope wrapped around them.

The envelope is seven fields and it is there so a record can be checked rather than trusted.

| Field | What it is |
| --- | --- |
| `kind` | the record type, set here and not read from anywhere |
| `depth` | how deeply this record was read |
| `surfaces` | the surface ids that contributed, in read order |
| `sources` | the URLs actually fetched, so the record can be rebuilt by hand |
| `retrieved_at` | when the first fetch happened, UTC |
| `via` | field name to surface id, for the fields more than one surface carries |
| `missed` | what this read did not look at, and which read would |

There is also `truncated`, set only when a result set was cut short, with the reason.

`-o json` writes an array, so pick the record out of it before looking at the envelope.

```bash
arxiv paper 1706.03762 -o json | jq '.[0] | {kind, depth, surfaces, sources}'
```

```json
{
  "kind": "paper",
  "depth": "meta",
  "surfaces": ["s1", "s2"],
  "sources": [
    "https://export.arxiv.org/api/query?id_list=1706.03762&max_results=1&sortBy=relevance&sortOrder=descending",
    "https://oaipmh.arxiv.org/oai?identifier=oai%3AarXiv.org%3A1706.03762&metadataPrefix=arXiv&verb=GetRecord"
  ]
}
```

## Why sources is a list of URLs

Because that is what makes the record falsifiable.

A tool that merges three surfaces and hands back one flat object is asking to be believed.
Printing the URLs means anyone can open them and check, and it means a record that looks wrong can be diagnosed without re-running anything.

`via` does the same job one level down.
It names the surface behind each field that more than one surface carries, so a disagreement is visible instead of averaged away.

## Absence is not zero

A field that is missing from the JSON was not read, or arXiv does not have it.
Neither of those is the same as the value being empty, and the tool never writes a zero to paper over the difference.

- No `report_no` at `--depth quick` means it was not read, because the export API has no report number at all.
- No `report_no` at `--depth full` means the paper does not have one.

`missed` closes that gap in words rather than in codes.

```bash
arxiv paper 1706.03762 -o json | jq '.[0].missed'
```

```json
[
  "the submitter, the version history and the html and source capabilities were not read; arxiv paper 1706.03762 --depth full reads them",
  "affiliations, the licence name and the section tree were not read; arxiv paper 1706.03762 --depth text reads them"
]
```

Every line names the fields that were not read and gives the command that would read them.

## Reading the field census

`arxiv fields` prints all 46 fields with their type, group, depth and surfaces.

```bash
arxiv fields --group time
arxiv fields --depth quick
arxiv fields -o csv > fields.csv
```

The `from` column reads `computed` when the field is derived here rather than read from a surface.
`doi`, `pdf_url`, `cross_lists` and `style` are all functions of the id, so there is nothing to fetch and nothing to get wrong.

That is deliberately different from an empty list meaning "we did not look".

## The dates that are not the same date

Four time fields look interchangeable and are not.

| Field | What it actually means |
| --- | --- |
| `first_submitted` | when v1 went in |
| `last_updated` | the current version's timestamp, which moves on every revision |
| `announced` | the day arXiv announced it, which is not the day it was submitted |
| `oai_datestamp` | a modification date at day granularity, what an incremental harvest resumes from |

`first_submitted` always comes from the export API and never from OAI-PMH, because OAI's `created` is the current version's date.
Filling it from OAI would put a 2023 date on a 2017 paper, and it would look completely plausible.

## Authors

`authors` is a list of objects rather than a list of strings, because the surfaces disagree about how much structure a name has.

The export API gives a display name.
OAI-PMH splits it into keyname and forenames, and carries a suffix when there is one.
The rendering at `--depth text` adds affiliations.

So an author read at `quick` has a name and nothing else, and the same author at `text` has a name, a split, and where they were working.
