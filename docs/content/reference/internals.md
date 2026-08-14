---
title: "Internals"
description: "Package layout, how a command is registered, and how the tests are built."
weight: 50
---

The whole thing is Go with no cgo, so a binary is one file with nothing beside it.

## Package layout

| Package | What is in it |
| --- | --- |
| `arxiv/` | the client, the twelve surface readers, the model, and every operation |
| `cli/` | the binary's wiring, plus the few commands that are CLI shaped rather than op shaped |
| `pkg/axid` | arXiv identifiers, old style and new, parsed and canonicalised |
| `pkg/latexml` | the LaTeXML rendering reader |
| `pkg/graph` | claims, nodes and the walk |
| `pkg/rdf` | the RDF writer, Dublin Core and schema.org |
| `cmd/arxiv` | main, which is a dozen lines |

There is no `internal/`.
Everything is importable, because a package worth writing is a package somebody else might want, and hiding it behind `internal` is a decision made on their behalf without asking.

## One registration, three surfaces

A read is registered once and appears as a CLI subcommand, an HTTP route and an MCP tool.

```go
kit.Handle(app, kit.OpMeta{
	Name:    "surfaces",
	Group:   "explain",
	List:    true,
	URIType: "surface",
	Summary: "Show the twelve places this tool reads and what each is for",
	Long:    `...`,
}, func(_ context.Context, in surfacesIn, emit func(*SurfaceInfo) error) error {
	return emitAll(surfaceInfos(), emit)
})
```

The input struct's tags carry the flags, their defaults, their enums and their help, and the same tags drive the HTTP query parameters and the MCP tool schema.

So a flag cannot exist on one surface and be missing from another, and a help string cannot be right in one place and stale in the other.

Two closed lists exist to keep that honest.
`groups` in `arxiv/domain_test.go` maps every op to its group, and `registered` in `cli/app_test.go` lists every op by name.
Adding an op fails both tests until somebody adds it to both lists, which makes reaching `serve` and `mcp` a line a person wrote rather than something that happened because a file was edited.

## Tests

560 tests, and the fixtures are real bytes from arXiv rather than hand written XML.

```bash
go test ./...
gofmt -l . && go vet ./...
```

`arxiv/testdata` holds a captured response per surface, mostly for three papers chosen because they disagree with each other.

- `1706.03762`, the Attention paper: new style, many versions, no report number, rendered.
- `1207.7214`, the Higgs discovery: a report number, a journal reference, a DOI, and a thousand authors.
- `hep-th/9711200`, Maldacena: old style, from before most of the fields existed, never rendered.

A test that passes on all three is a test that has met a paper with a field filled in, a paper with it empty, and a paper from before the field existed.

There are golden files too, a whole record at `--depth full` for each of the three, so a change in what a read produces shows up as a diff rather than as a test nobody updated.

## The live suite

67 tests run against arXiv itself, behind a build tag.

```bash
go test ./arxiv -tags live -run TestLiveSomething -v -timeout 20m
```

They are the ones that catch what a fixture cannot: a limit that moved, a page arXiv restructured, a header that stopped being sent.

They are also slow by construction, because they keep the same pacing everything else does.
Twenty tests on the html plane is five minutes of waiting on purpose.

Every measured number in the source carries the date it was measured, so when a live test starts failing the comment says what the world used to look like.

## Building

```bash
make build      # into bin/, which is gitignored
make install
make test
make fmt
```

The binary goes into `bin/` rather than the repo root, because the repo root already has a directory called `arxiv` and a binary of the same name there would shadow the source package in every shell that uses `./`.

Builds are `CGO_ENABLED=0` with `-trimpath`, and the version, commit and date go in through ldflags.
