---
title: "Citing"
description: "arXiv's own BibTeX, six other styles, and the two things this tool refuses to guess."
weight: 50
---

There are two ways to cite, and they are different on purpose.

## arXiv's own entry

```bash
arxiv bibtex 1706.03762
arxiv bibtex 1706.03762 1207.7214 >> refs.bib
```

The bytes are arXiv's, from `arxiv.org/bibtex/<id>`, passed through unchanged.

That is the point.
Everybody who quotes arXiv quotes this string, and an entry rebuilt from the record would disagree with every bibliography that took the served one.

It is on the fifteen second plane, so it costs fifteen seconds a paper the first time and nothing after that.

## An entry built from the record

```bash
arxiv bibtex 1706.03762 --local
```

This is a different entry and it is meant to be.

- It dates the paper by its first submission rather than by its latest version, so "Attention Is All You Need" comes out as 2017 where arXiv's own entry says 2023.
- It writes `@article` with the journal reference for a published paper, which arXiv never does.
- It puts the DOI in the `doi` field, where arXiv puts a URL.

Pick whichever matches what your bibliography already contains.

## The other six styles

```bash
arxiv cite 1706.03762 -s apa
arxiv cite 1706.03762 -s mla
arxiv cite 1706.03762 -s chicago
arxiv cite 1706.03762 -s ris
arxiv cite 1706.03762 -s csl-json
arxiv cite 1706.03762 -s text
```

Every style is built here and none of them is fetched, so this is two requests on the three second plane whatever style was asked for.

The read is at `meta` depth because that is where the structured author names live, and a citation that cannot write "Vaswani, A." is not a citation.

`csl-json` is the one worth knowing about.
It feeds every reference manager, it comes out as one array however many papers were asked for, and because it is built from the record rather than from the BibTeX it carries the abstract, the categories and the version that BibTeX drops.

```bash
arxiv cite 1706.03762 1207.7214 -s csl-json > refs.json
```

## Two things no style here does

The title is printed as arXiv holds it, so APA does not get its sentence case.
Lowercasing "Standard Model Higgs boson" correctly means knowing which words are names, and a tool that guesses gets it wrong on exactly the papers where it matters.

A name arXiv only published as one string is printed as that string rather than split into a surname and initials.
That split is wrong for "van der Waals" and wrong for "The ATLAS Collaboration", and a wrong split in a bibliography is quietly wrong forever.

Both of these are cases where doing less is the correct answer, and both are cheap for a person to fix and expensive for a tool to get right.

The output of `bibtex` and `cite` is text and not a record, because a `.bib` file is text.
