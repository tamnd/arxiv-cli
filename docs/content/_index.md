---
title: "arxiv"
description: "A command line for arXiv."
heroTitle: "arxiv, from the command line"
heroLead: "Read arXiv's public surfaces and turn preprint metadata into clean structured records. One pure Go binary, no API key, output that pipes into the rest of your tools."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

`arxiv` reads arXiv and turns preprint metadata into clean structured records.

It reads twelve surfaces rather than one.
The export API answers searches, OAI-PMH carries the version history and the submitter, the abstract page carries the category names, the LaTeXML rendering carries the affiliations, and eight other places carry the rest.
Every record says which surfaces it was built from, which one answered for each field, and what the read did not look at.

```bash
arxiv search "attention" --cat cs.CL     # search a category
arxiv paper 1706.03762 --depth full      # four surfaces instead of one
arxiv fields                             # every field, and what it costs
```

There is no API key, no login and nothing to configure.
All twelve surfaces answer an anonymous request with nothing but a user agent, and the three that `robots.txt` disallows are gated by politeness rather than by a credential: they are read only for a paper somebody named, never by the crawler.

## Where to go next

- New here? Read the [introduction](/getting-started/introduction/), then the [quick start](/getting-started/quick-start/).
- Installing? See [installation](/getting-started/installation/).
- Want to understand the design? The [concepts](/concepts/) pages cover the surfaces, the two planes, depth, the record model and the graph.
- Want to get something done? The [guides](/guides/) are built around jobs rather than commands.
- Need every flag? The [CLI reference](/reference/cli/) is the full surface.

`arxiv` is an independent tool and is not affiliated with arXiv or Cornell University.
