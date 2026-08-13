package rdf

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// The formats, and the pieces all three of them share.

// The format names, which are also what --format takes.
const (
	FormatNT     = "nt"
	FormatTurtle = "turtle"
	FormatJSONLD = "jsonld"
)

// Formats is every format this package writes, in the order help text lists
// them: the default first.
var Formats = []string{FormatNT, FormatTurtle, FormatJSONLD}

// Options is how a document is written.
type Options struct {
	// Format is one of Formats. Empty means N-Triples, which is the default
	// because it is a line per statement and streams without holding anything.
	Format string
	// Provenance writes where each statement came from. It is on by default at
	// the call sites; the flag that turns it off is --no-provenance, because
	// dropping it is the exception and should be the thing somebody types.
	Provenance bool
}

// Write serialises a document.
func Write(w io.Writer, d *Doc, o Options) error {
	format := o.Format
	if format == "" {
		format = FormatNT
	}
	stmts := d.Statements()
	switch format {
	case FormatNT:
		return writeNT(w, stmts, o.Provenance)
	case FormatTurtle:
		return writeTurtle(w, stmts, o.Provenance)
	case FormatJSONLD:
		return writeJSONLD(w, stmts, o.Provenance)
	}
	return fmt.Errorf("%q is not one of %s", o.Format, strings.Join(Formats, ", "))
}

// ─── terms on the wire ───

// iriText writes an IRI in the angle bracket form N-Triples and Turtle share.
func iriText(i IRI) string { return "<" + escapeIRI(string(i)) + ">" }

// literalText writes a literal with its language tag or its datatype. A plain
// string gets neither, which is xsd:string by definition and does not need
// saying.
func literalText(l Literal, short bool) string {
	out := `"` + escapeString(l.Value) + `"`
	switch {
	case l.Lang != "":
		return out + "@" + l.Lang
	case l.Datatype != "":
		return out + "^^" + termName(l.Datatype, short)
	}
	return out
}

// termName is an IRI written short where a declared prefix covers it, which is
// Turtle only. N-Triples has no prefixes at all.
func termName(i IRI, short bool) string {
	if short {
		if s, ok := Short(i); ok {
			return s
		}
	}
	return iriText(i)
}

func objectText(t Term, short bool) string {
	switch v := t.(type) {
	case IRI:
		return termName(v, short)
	case Literal:
		return literalText(v, short)
	}
	return ""
}

// escapeIRI takes out the characters that would end the angle brackets early.
// Anything else, including every non-ASCII byte, is legal in an IRI and is left
// alone, so a title in Cyrillic stays readable.
func escapeIRI(s string) string {
	if !strings.ContainsAny(s, "<>\"{}|^`\\ \n\r\t") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '<', '>', '"', '{', '}', '|', '^', '`', '\\', ' ':
			fmt.Fprintf(&b, "%%%02X", r)
		case '\n', '\r', '\t':
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeString escapes a literal's value. An abstract is full of newlines and
// backslashes, because it is LaTeX, so this one gets exercised on every paper.
func escapeString(s string) string {
	if !strings.ContainsAny(s, "\\\"\n\r\t") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ─── n-triples ───

// writeNT is a line per statement and a line per source.
//
// The provenance form is the quoted triple, << s p o >> prov:wasDerivedFrom
// <url>, which is RDF 1.2 and what every parser that understands RDF-star
// reads. A parser that does not will refuse those lines and keep the
// assertions, which is the failure mode worth having.
func writeNT(w io.Writer, stmts []Statement, prov bool) error {
	bw := bufio.NewWriter(w)
	for _, s := range stmts {
		triple := iriText(s.Subject) + " " + iriText(s.Predicate) + " " + objectText(s.Object, false)
		if _, err := bw.WriteString(triple + " .\n"); err != nil {
			return err
		}
		if !prov {
			continue
		}
		for _, src := range s.Sources {
			line := "<< " + triple + " >> " + iriText(PROVDerivedFrom) + " " + iriText(IRI(src)) + " .\n"
			if _, err := bw.WriteString(line); err != nil {
				return err
			}
		}
	}
	return bw.Flush()
}

// ─── turtle ───

// writeTurtle groups by subject, which is the only reason to prefer it to
// N-Triples: a paper is one block a person can read top to bottom.
func writeTurtle(w io.Writer, stmts []Statement, prov bool) error {
	bw := bufio.NewWriter(w)
	for _, p := range usedPrefixes(stmts, prov) {
		if _, err := fmt.Fprintf(bw, "@prefix %s: <%s> .\n", p.Prefix, p.IRI); err != nil {
			return err
		}
	}
	if _, err := bw.WriteString("\n"); err != nil {
		return err
	}

	i := 0
	for i < len(stmts) {
		j := i
		for j < len(stmts) && stmts[j].Subject == stmts[i].Subject {
			j++
		}
		if err := turtleSubject(bw, stmts[i:j]); err != nil {
			return err
		}
		i = j
	}
	if !prov {
		return bw.Flush()
	}

	// The annotations go in one block at the end rather than beside the
	// statements they are about. Interleaved, they break the subject grouping
	// that is the point of the format, and a reader who does not care about
	// provenance can stop at the blank line.
	wrote := false
	for _, s := range stmts {
		for _, src := range s.Sources {
			if !wrote {
				if _, err := bw.WriteString("# where each statement came from\n\n"); err != nil {
					return err
				}
				wrote = true
			}
			triple := termName(s.Subject, true) + " " + termName(s.Predicate, true) + " " + objectText(s.Object, true)
			line := "<< " + triple + " >> " + termName(PROVDerivedFrom, true) + " " + iriText(IRI(src)) + " .\n"
			if _, err := bw.WriteString(line); err != nil {
				return err
			}
		}
	}
	return bw.Flush()
}

// turtleSubject writes one subject's block, predicates separated by semicolons
// and repeated objects by commas.
func turtleSubject(bw *bufio.Writer, stmts []Statement) error {
	if _, err := bw.WriteString(termName(stmts[0].Subject, true) + "\n"); err != nil {
		return err
	}
	i := 0
	for i < len(stmts) {
		j := i
		objects := make([]string, 0, 4)
		for j < len(stmts) && stmts[j].Predicate == stmts[i].Predicate {
			objects = append(objects, objectText(stmts[j].Object, true))
			j++
		}
		name := termName(stmts[i].Predicate, true)
		if stmts[i].Predicate == RDFType {
			// a is Turtle's own spelling of rdf:type and every example in
			// every specification uses it.
			name = "a"
		}
		end := " ;"
		if j == len(stmts) {
			end = " ."
		}
		if _, err := fmt.Fprintf(bw, "    %s %s%s\n", name, strings.Join(objects, ", "), end); err != nil {
			return err
		}
		i = j
	}
	_, err := bw.WriteString("\n")
	return err
}

// usedPrefixes is the namespaces this document actually writes, so a file with
// no citations does not declare cito.
func usedPrefixes(stmts []Statement, prov bool) []Prefix {
	used := map[string]bool{}
	mark := func(i IRI) {
		for _, p := range Prefixes {
			if strings.HasPrefix(string(i), p.IRI) {
				used[p.Prefix] = true
				return
			}
		}
	}
	for _, s := range stmts {
		mark(s.Subject)
		mark(s.Predicate)
		switch v := s.Object.(type) {
		case IRI:
			mark(v)
		case Literal:
			if v.Datatype != "" {
				mark(v.Datatype)
			}
		}
	}
	if prov {
		mark(PROVDerivedFrom)
	}
	out := make([]Prefix, 0, len(Prefixes))
	for _, p := range Prefixes {
		if used[p.Prefix] {
			out = append(out, p)
		}
	}
	return out
}
