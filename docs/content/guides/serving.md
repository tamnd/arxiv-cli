---
title: "Serving"
description: "The same operations over HTTP and MCP, from one definition."
weight: 60
---

Every read is registered once and shows up in three places: as a CLI subcommand, as an HTTP route, and as an MCP tool.

That is not three implementations kept in step by hand.
It is one registration, which is why a command cannot exist on the CLI and be missing from the server.

## HTTP

```bash
arxiv serve --addr :8080
```

```bash
curl -s localhost:8080/healthz
curl -s "localhost:8080/v1/planes"
curl -s "localhost:8080/v1/id/1706.03762"
curl -s "localhost:8080/v1/search?query=attention&cat=cs.CL&limit=5"
curl -s "localhost:8080/v1/paper/1706.03762?depth=full"
```

Responses are NDJSON, one record per line, so a long read streams instead of buffering.

Flags become query parameters, and a command's positional argument becomes a path segment.

The full route list is at `/v1/openapi.json`, generated from the same registration.

```bash
curl -s localhost:8080/v1/openapi.json | jq -r '.paths | keys[]'
```

```
/v1/author      /v1/edges      /v1/id         /v1/planes      /v1/sets
/v1/bibtex      /v1/fields     /v1/list       /v1/predicates  /v1/surfaces
/v1/categories  /v1/files      /v1/new        /v1/routes      /v1/trackbacks
/v1/category    /v1/fulltext   /v1/paper      /v1/search
/v1/cite        /v1/grammar    /v1/count      /v1/download
/v1/edges       /v1/graph
```

Writes are off unless you ask for them.

```bash
arxiv serve --allow-writes
```

## The server does not lift the pacing

A request to `/v1/paper/1706.03762?depth=full` crosses onto arxiv.org and waits fifteen seconds between requests, exactly as the CLI does.

The limiter is per plane and per process, so ten concurrent HTTP clients share one queue rather than each getting their own.
Putting a server in front of arXiv does not create permission to go faster, and a server that let it would be a much easier way to get an IP blocked than the CLI ever was.

Six commands make no request at all, so they answer instantly and are the right things to hit from a health check or a UI: `planes`, `surfaces`, `routes`, `grammar`, `fields`, `predicates`, plus `id`.

## MCP

```bash
arxiv mcp
```

That speaks MCP over stdio, with the same operations as tools.

For Claude Code:

```bash
claude mcp add arxiv -- arxiv mcp
```

For any client that takes a JSON config:

```json
{
  "mcpServers": {
    "arxiv": {
      "command": "arxiv",
      "args": ["mcp"]
    }
  }
}
```

Each tool carries the same summary and the same flag help the CLI shows, so a model reading the tool list gets the same warnings a person does, including which depth crosses onto the slow plane.

The no-request commands are worth pointing a model at first.
`arxiv grammar` teaches it the query language before it spends a request guessing, and `arxiv fields` tells it which depth a field it wants actually needs.
