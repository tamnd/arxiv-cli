---
title: "Output"
description: "Formats, column selection and templates, all the same on every command."
weight: 30
---

Every command renders through one formatter, so the same flags work everywhere.

The default is `auto`: a table when stdout is a terminal, NDJSON when it is a pipe.

```bash
arxiv surfaces            # a table, because this is a terminal
arxiv surfaces | jq .     # NDJSON, because this is a pipe
```

You reach for `-o` only when you want something other than that.

## Formats

| Format | What it is | Good for |
| --- | --- | --- |
| `table` | aligned columns with a box | reading on a terminal |
| `markdown` | a pipe table | pasting into an issue or a README |
| `list` | one block per record, key and value | a record with long fields |
| `jsonl` | one JSON object per line | piping, streaming, `jq` |
| `json` | a single JSON array | loading a whole result at once |
| `csv` | comma separated with a header | spreadsheets |
| `tsv` | tab separated | `cut` and `awk` |
| `url` | the URL column alone | feeding other commands |
| `raw` | the bytes as served | response bodies and file contents |

`jsonl` and `json` carry every field.
The table formats carry the columns the command chose to print, which is fewer, because a terminal is 100 columns wide and a paper record is not.

So if a field is missing from a table it is worth looking at the same command in `-o json` before concluding it was not read.

## Choosing columns

`--fields` takes the column names as the table prints them.

```bash
arxiv surfaces --fields id,surface
arxiv surfaces -o markdown        # to see what the column names are
```

The names are the header row, not the JSON keys, and the two are not always the same.
`arxiv surfaces` prints `surface` where the JSON says `name`.

A name that does not match a column gives an empty column rather than an error, so check the spelling against the header if a column comes back blank.

`--no-header` drops the header row, which is what a downstream tool that expects bare rows wants.

## Templates

`--template` takes a Go text template applied per record, and the keys are the JSON keys, lower case.

```bash
arxiv planes --template '{{.name}} paces at {{.pace}}'
arxiv search "attention" --cat cs.CL -n 5 --template '{{.id}}  {{.title}}'
```

Lower case matters.
`{{.Name}}` renders as `<no value>` rather than failing, because that is what text/template does with a key it cannot find.

## Piping

NDJSON in a pipe is the whole point of the default.

```bash
arxiv search "transformer" --cat cs.LG -n 50 | jq -r '.id + "  " + .title'
arxiv list cs.CL 2026-01 --all | jq -r 'select(.categories | index("cs.LG")) | .id'
arxiv search --cat cs.CL --from 2026-01 --all > cs-cl.jsonl
```

Records stream as they are read rather than buffering to the end, so a long walk prints its first result in seconds and you can kill it once you have what you wanted.

That is also why a walk interrupted with ctrl-c leaves usable output.

## Text that is text

`arxiv bibtex` and `arxiv cite` write text and not records, because a `.bib` file is text and a citation is text.

They emit no records at all, so `-o`, `--fields` and `--template` have nothing to act on.
When you want a citation as data, ask for it as data: `arxiv cite -s csl-json` is one JSON array however many papers you gave it.
