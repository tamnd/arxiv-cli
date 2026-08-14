---
title: "Configuration"
description: "Directories, environment variables, and the flags that change how requests go out."
weight: 20
---

There is nothing to configure to get started.

No API key, no login, no config file needed.
Everything below has a working default, and the defaults are the values this tool measured against arXiv rather than round numbers somebody picked.

## Directories

| What | Default | Override |
| --- | --- | --- |
| data | `~/.local/share/arxiv` | `ARXIV_DATA_DIR`, or `XDG_DATA_HOME`, or `--data-dir` |
| cache | `<data>/cache` | follows the data directory |
| config | `~/.config/arxiv` | `ARXIV_CONFIG_DIR`, or `XDG_CONFIG_HOME` |

The default store is `arxiv.db` under the data directory, crawl manifests go to `crawls/` and `arxiv archive` writes to `archive/`, both under the same place.

`--store` and `--dir` override those per command.

## Environment variables

There are three, and they are all about where things live.

| Variable | What it sets |
| --- | --- |
| `ARXIV_DATA_DIR` | the data directory, beating `XDG_DATA_HOME` |
| `ARXIV_CONFIG_DIR` | the config directory, beating `XDG_CONFIG_HOME` |
| `NO_COLOR` | turns colour off, the same as `--color never` |

```bash
export ARXIV_DATA_DIR=/data/arxiv
```

Nothing else reads the environment.
The pacing, the output format and the caching are flags, and a flag is what they stay.

That is fewer knobs than a tool this size usually has, and it means a command in a script does what it says on the line rather than what somebody's shell profile decided last month.

## Pacing

| Flag | Default | Floor |
| --- | --- | --- |
| `--rate` | 3s on the api plane | 1s |
| `--html-rate` | 15s on arxiv.org | 15s |

The html floor is a floor.
`--html-rate 30s` is honoured and `--html-rate 2s` is a usage error, because `arxiv.org/robots.txt` says `Crawl-delay: 15` and a tool that reads it and then ignores it is worse than a tool that never read it.

See [planes and pacing](/concepts/planes/) for what happens when arXiv says no.

## Retries and timeouts

`--retries` defaults to the built-in table, which is different per condition: five retries on a 429 with a 60 second backoff doubling to ten minutes, three on a 503, five on a network error, none on a 400 or a 404.

Setting `--retries` to a number overrides the count and leaves the backoff shape alone.
`--retries 0` turns retrying off, which is what you want in a script that would rather fail fast than sit for ten minutes.

`--timeout` is per request and not per command.
A `--depth text` read of a long paper is five requests, so a 30 second timeout gives each of them 30 seconds.

## Caching

Responses are cached on disk under the cache directory, keyed by URL, with a TTL per surface.
No arXiv response carries a validator, so there is nothing to revalidate against and the cache is time based.

`--no-cache` skips both the read and the write.

A cache hit never shows up in a record.
`retrieved_at` is the time of the original fetch, because whether a byte came off a disk is not a property of the paper.

## Profiles

`--profile <name>` comes from the shared CLI framework and this tool does not act on it yet.
It is accepted and recorded and nothing reads it back, so passing it changes nothing.

Use a shell alias for now.

## Colour and terminals

`--color auto` is the default: colour when stdout is a terminal, none when it is not.
`--color never` and `NO_COLOR` both turn it off.

Output format follows the same idea.
`auto` is a table on a terminal and NDJSON in a pipe, so `arxiv search ... | jq` works with no flag.
