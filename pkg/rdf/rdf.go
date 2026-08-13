// Package rdf writes what arxiv read in a vocabulary something else can read.
//
// It takes claims and literals in and gives N-Triples, Turtle or JSON-LD out.
// It knows about pkg/graph and about nothing else in this repository, so the
// mapping can be read, tested and argued with on its own.
//
// Three rules run through the whole package.
//
// A claim is written once. A store keeps one row per source, so a fact three
// surfaces agree on arrives here three times, and RDF has no way to say the
// same thing twice. The statement is written once and the three sources are
// annotated onto it.
//
// Every statement says where it came from. That is prov:wasDerivedFrom on a
// quoted triple in N-Triples and Turtle, and a named graph per source in
// JSON-LD, which has had named graphs since 1.0 and has no quoted triples at
// all. --no-provenance turns it off, because it roughly doubles the file.
//
// The bytes are stable. Two runs over the same input produce the same file in
// all three formats, which is what makes a diff mean something: a dump that
// reorders itself cannot be diffed, and a diff is how somebody notices arXiv
// started saying something different.
//
// There are no blank nodes anywhere. A blank node is a different node every
// time the file is written, which loses byte stability, and it stops two dumps
// of overlapping crawls from merging. Where the obvious modelling wants one, a
// real IRI is minted instead.
package rdf

import (
	"sort"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// Term is one end of a statement: an IRI or a literal.
type Term interface{ key() string }

// IRI is a name for something.
type IRI string

func (i IRI) key() string { return "<" + string(i) }

// Literal is a value, with a language tag or a datatype but never both.
type Literal struct {
	Value    string
	Lang     string
	Datatype IRI
}

func (l Literal) key() string { return "\"" + l.Value + "\x00" + l.Lang + "\x00" + string(l.Datatype) }

// Text is a plain string literal.
func Text(s string) Literal { return Literal{Value: s} }

// Typed is a literal with a datatype, which is how a date stays a date.
func Typed(s string, dt IRI) Literal { return Literal{Value: s, Datatype: dt} }

// Statement is one assertion and the URLs that made it.
type Statement struct {
	Subject   IRI
	Predicate IRI
	Object    Term
	// Sources are the pages that asserted it, deduplicated and sorted. A
	// statement with none is one this tool inferred, and rdf:type where the
	// class was inferred is the main one: arXiv did say a paper's dc:type is
	// text, it never said schema:ScholarlyArticle, so that claim cites nothing
	// rather than putting words in the endpoint's mouth.
	Sources []string
}

func (s Statement) key() string {
	return string(s.Subject) + "\x00" + string(s.Predicate) + "\x00" + s.Object.key()
}

// Doc is a set of statements being built up.
//
// It is a set rather than a list because deduplication is the point: a paper
// read at depth full has its title asserted by three surfaces, and the reader
// wants one title with three sources on it.
type Doc struct {
	byKey map[string]*Statement
	// refused counts claims that named a node this package could not turn into
	// an IRI. It should always be zero and the command prints it when it is not.
	refused int
}

// New starts an empty document.
func New() *Doc { return &Doc{byKey: map[string]*Statement{}} }

// Add asserts one statement, merging the sources if it is already there.
//
// An empty subject or predicate is dropped rather than written, because the
// caller that produced it was working from a record field that was not read,
// and a triple with a hole in it is worse than a missing triple.
func (d *Doc) Add(subject, predicate IRI, object Term, sources ...string) {
	if subject == "" || predicate == "" || object == nil {
		return
	}
	if iri, ok := object.(IRI); ok && iri == "" {
		return
	}
	if lit, ok := object.(Literal); ok && strings.TrimSpace(lit.Value) == "" {
		return
	}
	st := Statement{Subject: subject, Predicate: predicate, Object: object}
	k := st.key()
	existing, ok := d.byKey[k]
	if !ok {
		st.Sources = cleanSources(sources)
		d.byKey[k] = &st
		return
	}
	for _, s := range cleanSources(sources) {
		if !has(existing.Sources, s) {
			existing.Sources = append(existing.Sources, s)
		}
	}
	sort.Strings(existing.Sources)
}

// Type asserts a class, or several, with no provenance.
func (d *Doc) Type(subject IRI, classes ...IRI) {
	for _, c := range classes {
		d.Add(subject, RDFType, c)
	}
}

// Label hangs a human readable name on a node, which is the only thing that
// makes a minted IRI readable. The label is what arXiv printed, not a
// description of it.
func (d *Doc) Label(subject IRI, text string, sources ...string) {
	d.Add(subject, RDFSLabel, Text(text), sources...)
}

// AddEdge writes one claim from the store or from a read.
//
// It reports whether the claim could be written. False means one end named a
// node kind with no IRI, which should not happen and is counted rather than
// swallowed.
func (d *Doc) AddEdge(e graph.Edge) bool {
	from, to := NodeIRI(e.From), NodeIRI(e.To)
	if from == "" || to == "" {
		d.refused++
		return false
	}
	row, ok := Predicate(e.Predicate)
	if !ok {
		// Not in the table, so it is written under its own name rather than
		// dropped. See Unknown.
		row = Row{Terms: []IRI{Unknown(e.Predicate)}}
	}
	subject, object := from, to
	if row.Reverse {
		subject, object = to, from
	}
	for _, term := range row.Terms {
		d.Add(subject, term, object, e.Source)
	}
	d.typeOf(e.From, from)
	d.typeOf(e.To, to)
	d.note(e, row, from, to)
	return true
}

// typeOf types a node from its kind, when the kind has a class worth naming.
func (d *Doc) typeOf(uri string, iri IRI) {
	kind, ok := graph.KindOf(uri)
	if !ok {
		return
	}
	d.Type(iri, Classes(kind)...)
	if kind == graph.KindCategory || kind == graph.KindArchive || kind == graph.KindGroup {
		d.Add(iri, SKOSInScheme, Scheme)
	}
}

// note writes the claim's note as a label, on the end the table says it names.
//
// Only a minted node gets one. A label on https://doi.org/10.1038/nature14539
// would be this tool naming somebody else's resource, and the note on has_doi
// is "the publisher's", which is not what that DOI is called.
func (d *Doc) note(e graph.Edge, row Row, from, to IRI) {
	if e.Note == "" {
		return
	}
	var target IRI
	switch row.Label {
	case LabelFrom:
		target = from
	case LabelTo:
		target = to
	default:
		return
	}
	// Only a minted node gets one. Those are the nodes named by a slug or a
	// hash, so the label is the spelling the slug threw away and the only name
	// the node will ever have.
	if !Minted(target) {
		return
	}
	d.Label(target, e.Note, e.Source)
}

// Refused is how many claims named a node this package could not write.
func (d *Doc) Refused() int { return d.refused }

// Len is how many distinct statements there are.
func (d *Doc) Len() int { return len(d.byKey) }

// Statements is every statement, in a fixed order: by subject, then predicate,
// then object. This is where byte stability comes from.
func (d *Doc) Statements() []Statement {
	out := make([]Statement, 0, len(d.byKey))
	for _, s := range d.byKey {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		if out[i].Predicate != out[j].Predicate {
			// rdf:type first inside a subject, because a reader wants to know
			// what a thing is before what it says.
			ti, tj := out[i].Predicate == RDFType, out[j].Predicate == RDFType
			if ti != tj {
				return ti
			}
			return out[i].Predicate < out[j].Predicate
		}
		return out[i].Object.key() < out[j].Object.key()
	})
	return out
}

// Sources is every URL any statement was derived from, sorted. It is what the
// JSON-LD writer names its graphs after.
func (d *Doc) Sources() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range d.byKey {
		for _, src := range s.Sources {
			if !seen[src] {
				seen[src] = true
				out = append(out, src)
			}
		}
	}
	sort.Strings(out)
	return out
}

func cleanSources(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !has(out, s) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func has(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
