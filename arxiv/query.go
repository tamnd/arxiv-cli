package arxiv

import (
	"fmt"
	"strings"
	"time"
)

// A Field is one of arXiv's search field prefixes.
//
// These nine are the whole set the export API accepts, all measured working on
// 2026-08-13. The search UI understands seven more that the API does not, and
// those are reached through the HTML plane instead; see doc 02 section 2.3.
type Field string

const (
	FieldAll      Field = "all" // every indexed field
	FieldTitle    Field = "ti"
	FieldAuthor   Field = "au"
	FieldAbstract Field = "abs"
	FieldComment  Field = "co"
	FieldJournal  Field = "jr"
	FieldCategory Field = "cat"
	FieldReport   Field = "rn"
	FieldID       Field = "id"
)

// Fields is every prefix the API accepts, in the order the docs list them.
var Fields = []Field{
	FieldAll, FieldTitle, FieldAuthor, FieldAbstract,
	FieldComment, FieldJournal, FieldCategory, FieldReport, FieldID,
}

// ParseField resolves a prefix, accepting both the arXiv spelling and the
// readable one a flag would use.
func ParseField(s string) (Field, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, f := range Fields {
		if string(f) == s {
			return f, true
		}
	}
	switch s {
	case "title":
		return FieldTitle, true
	case "author":
		return FieldAuthor, true
	case "abstract":
		return FieldAbstract, true
	case "comment", "comments":
		return FieldComment, true
	case "journal", "journal_ref":
		return FieldJournal, true
	case "category", "cat":
		return FieldCategory, true
	case "report", "report_no":
		return FieldReport, true
	}
	return "", false
}

// A Query is an arXiv search_query in the only form worth holding: plain text
// with real spaces and nothing escaped.
//
// There is deliberately no method here that returns an escaped string. The
// whole bug this rewrite exists to fix was a query that arrived at the encoder
// already carrying "+AND+", which the encoder then escaped to "%2B", so a two
// word search asked arXiv one nonsense question. Escaping happens once, in
// Request.Values, and nowhere else can reach it.
type Query struct {
	text string
}

// Raw wraps a query string the caller wrote by hand.
func Raw(s string) Query { return Query{text: strings.TrimSpace(s)} }

// String returns the query as arXiv reads it, unescaped.
func (q Query) String() string { return q.text }

// Empty reports whether there is anything to send.
func (q Query) Empty() bool { return strings.TrimSpace(q.text) == "" }

// Term builds one field match, quoting the value when it has whitespace in it.
//
// An unquoted multi-word value is not a phrase, it is a sequence of terms the
// index ORs together, which is why ti:attention is all you need returns several
// hundred thousand results and the quoted form returns 35.
func Term(f Field, value string) Query {
	value = strings.TrimSpace(value)
	if value == "" {
		return Query{}
	}
	if strings.ContainsAny(value, " \t") && !isQuoted(value) {
		value = `"` + value + `"`
	}
	return Query{text: string(f) + ":" + value}
}

// Phrase builds a quoted field match, whatever the value looks like.
func Phrase(f Field, value string) Query {
	value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"`))
	if value == "" {
		return Query{}
	}
	return Query{text: string(f) + `:"` + value + `"`}
}

func isQuoted(s string) bool {
	return len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)
}

// And joins queries with AND, dropping the empty ones.
func And(qs ...Query) Query { return join("AND", qs) }

// Or joins queries with OR, dropping the empty ones.
func Or(qs ...Query) Query { return join("OR", qs) }

// AndNot subtracts b from a. arXiv spells it as one word.
func AndNot(a, b Query) Query {
	if a.Empty() {
		return Query{}
	}
	if b.Empty() {
		return a
	}
	return Query{text: a.text + " ANDNOT " + b.text}
}

// Group parenthesises a query, which is how a mixed AND and OR is made to mean
// what it looks like it means.
func Group(q Query) Query {
	if q.Empty() {
		return q
	}
	if strings.HasPrefix(q.text, "(") && strings.HasSuffix(q.text, ")") {
		return q
	}
	return Query{text: "(" + q.text + ")"}
}

func join(op string, qs []Query) Query {
	parts := make([]string, 0, len(qs))
	for _, q := range qs {
		if !q.Empty() {
			parts = append(parts, q.text)
		}
	}
	if len(parts) == 0 {
		return Query{}
	}
	return Query{text: strings.Join(parts, " "+op+" ")}
}

// A DateField is one of the two timestamps arXiv will match a range against.
type DateField string

const (
	// SubmittedDate is the v1 submission time. It never changes, which is what
	// makes it the only safe sort for a long walk.
	SubmittedDate DateField = "submittedDate"
	// LastUpdatedDate is the time of the most recent version, so it moves when
	// a paper is revised.
	LastUpdatedDate DateField = "lastUpdatedDate"
)

// stampLayout is arXiv's twelve-digit timestamp. The eight-digit form is
// accepted too and the two agree, but this tool always sends twelve, because
// minute resolution is what lets the slicer cut a busy day in half.
const stampLayout = "200601021504"

// Stamp renders a time the way a range wants it.
func Stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

// A Range is a closed interval of time. Both ends are inclusive, which is
// arXiv's behaviour and not a choice made here.
type Range struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// NewRange builds a range truncated to the minute, which is the finest
// resolution arXiv will match on.
func NewRange(from, to time.Time) Range {
	return Range{From: from.UTC().Truncate(time.Minute), To: to.UTC().Truncate(time.Minute)}
}

// Valid reports whether the range holds at least one minute.
func (r Range) Valid() bool { return !r.From.After(r.To) }

// Minutes is the number of minutes in the range, counting both ends, so a
// range whose two ends are the same minute has one minute in it.
func (r Range) Minutes() int {
	if !r.Valid() {
		return 0
	}
	return int(r.To.Sub(r.From)/time.Minute) + 1
}

func (r Range) String() string { return Stamp(r.From) + " TO " + Stamp(r.To) }

// Between builds a date range clause.
func Between(f DateField, r Range) Query {
	if !r.Valid() {
		return Query{}
	}
	return Query{text: fmt.Sprintf("%s:[%s]", f, r)}
}

// A Sort is one of the three orderings the API accepts. Sending anything else
// returns a 400 that names these three, so the same three are validated here
// and a typo costs no round trip.
type Sort string

const (
	SortRelevance Sort = "relevance"
	SortSubmitted Sort = "submittedDate"
	SortUpdated   Sort = "lastUpdatedDate"
)

// Sorts is the accepted set, in the order the server names them.
var Sorts = []Sort{SortRelevance, SortSubmitted, SortUpdated}

// ParseSort resolves a sort, accepting arXiv's own spelling and the short one a
// flag would use.
func ParseSort(s string) (Sort, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "relevance":
		return SortRelevance, nil
	case "date", "submitted", "submitteddate":
		return SortSubmitted, nil
	case "updated", "lastupdated", "lastupdateddate":
		return SortUpdated, nil
	}
	return "", fmt.Errorf("sort order %q is not one of relevance, date and updated", s)
}

// Order is the direction a sort runs in.
type Order string

const (
	Ascending  Order = "ascending"
	Descending Order = "descending"
)
