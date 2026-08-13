package rdf

import (
	"encoding/json"
	"io"
	"sort"
)

// JSON-LD, which is the format somebody will actually load.
//
// The context is written inline rather than linked, so a consumer needs nothing
// from the network to read the file. A remote context is a dependency on a URL
// staying up, and half of them do not.
//
// Provenance is named graphs, one per source, rather than quoted triples.
// JSON-LD has had named graphs since 1.0 and has no quoted triples at all, so
// this is the format's own answer to the question rather than a translation of
// N-Triples. A statement two surfaces both assert appears in both their graphs,
// which is the quad model saying they agree, and it is still one assertion
// rather than two copies of a fact.
//
// The graph gets its own IRI, <source>#claims, rather than borrowing the page's
// address. The page is a page. What we derived from reading it is not the same
// resource, and giving them one name is how a dataset ends up asserting that
// arXiv's HTML has an author.

// writeJSONLD builds the whole document and writes it once.
//
// Ordering: maps come out of encoding/json with their keys sorted, so the only
// thing this has to fix is the arrays, and they are sorted by the same key the
// statement list is. That is where byte stability comes from here.
func writeJSONLD(w io.Writer, stmts []Statement, prov bool) error {
	doc := map[string]any{"@context": context(stmts, prov)}

	if !prov {
		doc["@graph"] = nodes(stmts)
		return encode(w, doc)
	}

	// Everything with a source goes in that source's graph, everything without
	// one stays in the default graph: an inferred type claims nothing about
	// where it came from and should not be filed under a page that never said
	// it.
	bySource := map[string][]Statement{}
	var plain []Statement
	for _, s := range stmts {
		if len(s.Sources) == 0 {
			plain = append(plain, s)
			continue
		}
		for _, src := range s.Sources {
			bySource[src] = append(bySource[src], s)
		}
	}
	sources := make([]string, 0, len(bySource))
	for src := range bySource {
		sources = append(sources, src)
	}
	sort.Strings(sources)

	graphs := make([]any, 0, len(sources)+len(plain))
	graphs = append(graphs, nodes(plain)...)
	for _, src := range sources {
		graphs = append(graphs, map[string]any{
			"@id":    src + "#claims",
			"@graph": nodes(bySource[src]),
			// prov:wasDerivedFrom on the graph itself says what the whole
			// graph came from, so a reader who ignores the #claims convention
			// still finds the URL.
			short(PROVDerivedFrom): map[string]any{"@id": src},
		})
	}
	doc["@graph"] = graphs
	return encode(w, doc)
}

// nodes groups statements into one JSON object per subject.
func nodes(stmts []Statement) []any {
	order := []string{}
	byID := map[string]map[string]any{}
	for _, s := range stmts {
		id := string(s.Subject)
		node, ok := byID[id]
		if !ok {
			node = map[string]any{"@id": id}
			byID[id] = node
			order = append(order, id)
		}
		key, value := short(s.Predicate), jsonValue(s.Object)
		if s.Predicate == RDFType {
			// @type takes the class name as a string, not a node object.
			key, value = "@type", short(s.Object.(IRI))
		}
		switch existing := node[key].(type) {
		case nil:
			node[key] = value
		case []any:
			node[key] = append(existing, value)
		default:
			node[key] = []any{existing, value}
		}
	}
	sort.Strings(order)
	out := make([]any, 0, len(order))
	for _, id := range order {
		node := byID[id]
		for k, v := range node {
			if list, ok := v.([]any); ok {
				sortValues(list)
				node[k] = list
			}
		}
		out = append(out, node)
	}
	return out
}

// jsonValue is one object end.
func jsonValue(t Term) any {
	switch v := t.(type) {
	case IRI:
		return map[string]any{"@id": string(v)}
	case Literal:
		switch {
		case v.Lang != "":
			return map[string]any{"@value": v.Value, "@language": v.Lang}
		case v.Datatype != "":
			return map[string]any{"@value": v.Value, "@type": short(v.Datatype)}
		}
		return v.Value
	}
	return nil
}

// sortValues puts an array of object ends in a fixed order.
func sortValues(list []any) {
	sort.Slice(list, func(i, j int) bool { return valueKey(list[i]) < valueKey(list[j]) })
}

func valueKey(v any) string {
	switch t := v.(type) {
	case string:
		return "1" + t
	case map[string]any:
		if id, ok := t["@id"].(string); ok {
			return "0" + id
		}
		val, _ := t["@value"].(string)
		lang, _ := t["@language"].(string)
		typ, _ := t["@type"].(string)
		return "2" + val + "\x00" + lang + "\x00" + typ
	}
	return "3"
}

// context declares the prefixes this document uses and the two keywords that
// need help: a term whose value is an IRI has to say so, or a consumer reads
// schema:author as the string "https://arxiv.org/a/vaswani_a_1".
func context(stmts []Statement, prov bool) map[string]any {
	ctx := map[string]any{}
	for _, p := range usedPrefixes(stmts, prov) {
		ctx[p.Prefix] = p.IRI
	}
	pointsAt, holds := map[string]bool{}, map[string]bool{}
	for _, s := range stmts {
		if s.Predicate == RDFType {
			continue
		}
		name := short(s.Predicate)
		if _, ok := s.Object.(IRI); ok {
			pointsAt[name] = true
		} else {
			holds[name] = true
		}
	}
	for name := range pointsAt {
		// Only where every object is an IRI. A term with one of each would be
		// coerced the wrong way for the literals, and a date written as an
		// identifier is worse than a term nobody declared.
		if !holds[name] {
			ctx[name] = map[string]any{"@type": "@id"}
		}
	}
	if prov {
		ctx[short(PROVDerivedFrom)] = map[string]any{"@type": "@id"}
	}
	return ctx
}

// short is the prefixed name where there is one and the full IRI where there is
// not, which is what a JSON key has to be either way.
func short(i IRI) string {
	if s, ok := Short(i); ok {
		return s
	}
	return string(i)
}

// encode writes the document with HTML escaping off, because a title with an
// ampersand in it should read as an ampersand and not as &.
func encode(w io.Writer, doc map[string]any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
