---
title: "Searching"
description: "The field flags, the raw grammar, the ten thousand result window, and the seven fields the API does not have."
weight: 10
---

The positional argument matches every indexed field, a word at a time, with a quoted phrase kept whole.

```bash
arxiv search "attention is all you need"
```

If what you type is already arXiv's grammar, with a field prefix or a bare `AND`, `OR` or `ANDNOT` in it, it goes out as written and the flags still compose around it.

```bash
arxiv search 'ti:"attention is all you need" AND cat:cs.CL'
arxiv search 'ti:transformer OR ti:attention' --cat cs.CL
```

The second one sends `(ti:transformer OR ti:attention) AND cat:cs.CL`.
The parentheses are added because otherwise the trailing `AND` would bind to the last term alone and quietly answer a different question.

The field flags are arXiv's own prefixes under readable names, and they AND together.

```bash
arxiv search --title attention --author vaswani
```

That sends `ti:attention AND au:vaswani`.
`-v` prints both the query that was built and the URL it went out on, which is the fastest way to learn the grammar.

| Flag | Prefix | Matches |
| --- | --- | --- |
| `--title` | `ti:` | the title |
| `--author` | `au:` | the author field |
| `--abstract` | `abs:` | the abstract |
| `--comment` | `co:` | the author comment |
| `--journal` | `jr:` | the journal reference |
| `--cat` | `cat:` | a category code |
| `--report` | `rn:` | the report number |

## Categories

`--cat` takes a code, repeats, and the codes are OR'd.

```bash
arxiv search "transformer" --cat cs.CL --cat cs.LG
```

A bare archive code such as `hep-th` or `cs` matches whether or not that archive was split into categories.

A code arXiv does not have is refused before any request goes out.
That is on purpose: arXiv answers a wrong code with zero results and no error, which reads as an empty category rather than as a typo, and a silent zero is the worst possible answer to a misspelling.

```bash
arxiv categories          # every code
arxiv category cs.CL      # one, with its group and archive
```

## Dates

The date flags take `2026`, `2026-01` or `2026-01-01`.

```bash
arxiv search "diffusion" --cat cs.CV --from 2025-01 --to 2025-06
arxiv search "diffusion" --updated-from 2026
```

A bound names a period rather than an instant, so `--to 2026-01` means the end of January and not the start of it.
That is the reading a person means when they say "up to January", and getting it the other way round silently drops a month.

## The raw grammar

`--raw` sends a query through untouched, for the parts the flags do not cover: parentheses, `OR`, `ANDNOT` and quoted phrases.
A grammar query typed as the positional argument composes with the flags; `--raw` is the one that promises nothing at all will be added to it, which is why it refuses to be combined with them.

```bash
arxiv search --raw 'abs:"large language model" ANDNOT cat:cs.CL'
arxiv search --raw '(ti:transformer OR ti:attention) AND cat:cs.CL'
```

```bash
arxiv grammar
arxiv grammar --kind operator
```

prints the whole grammar with examples that work.

One rule is worth memorising: a multi word phrase must be quoted, and the quotes have to survive your shell.
`ti:large language model` is parsed as `ti:large` AND two loose terms, which returns a different set and does not look like an error.

## Sorting and paging

```bash
arxiv search "attention" --sort submitted --order desc -n 50
```

Sort is by relevance unless you say otherwise, and by submission date under `--all`.

arXiv will not page past ten thousand results.
`--all` walks the whole set anyway by cutting the query into date slices that each fit.

```bash
arxiv search --cat cs.CL --from 2026-01 --all -o jsonl > cs-cl-2026.jsonl
```

The walk runs in submission order rather than relevance order, and that is not a preference.
Relevance is recomputed on every request, so a walk ordered by it both repeats and skips papers, and you would never know which.

A long walk says what it is about to cost before it starts, and `-vv` names each slice as it reaches it.

```console
$ arxiv search cat:cs.CL --all -n 25000 -vv
116325 results in 18 slices, 35 count requests to plan and about 1170 to walk
slice 1 of 18, 199108010000 to 200902061159, 1585 results
slice 2 of 18, 200902061200 to 201711110559, 6000 results
slice 3 of 18, 201711110600 to 202001200429, 6000 results
...
```

Twenty five thousand ids took about fifty minutes on the three second pace, most of it waiting.
arXiv answered 429 a few times along the way, and the tool held the whole plane and carried on rather than losing the walk.

To learn only how many results a query has, use `arxiv count`, which is one request.

```bash
arxiv count --raw 'cat:cs.CL AND abs:transformer'
```

## The seven search UI fields

Seven fields exist in arXiv's search UI and have no prefix in the export API.

`--acm-class`, `--msc-class`, `--doi`, `--orcid`, `--license`, `--author-id` and `--full-text`.

Naming any of them sends the whole query to the search UI on the fifteen second plane instead, and `-v` says so before it starts.

```bash
arxiv search --doi 10.1038/nature14539
arxiv search --orcid 0000-0002-3300-2109
arxiv search --msc-class 11M06
```

That route cannot take `--cat`, `--raw` or the two `--updated-` date flags, because the search UI has no equivalent for them.
Each refusal explains itself rather than quietly dropping the flag.

```bash
arxiv grammar --kind field
```

lists every field with the plane it lands on.
