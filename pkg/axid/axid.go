// Package axid turns any way of writing an arXiv reference into one canonical
// id, and derives from it everything that can be derived without a request.
//
// arXiv has had three id schemes: the old archive/YYMMNNN form used until March
// 2007, the new YYMM.NNNN form used from April 2007 to December 2014, and the
// same form with a five digit sequence from January 2015 on. On top of that,
// papers get cited as arXiv:1706.03762v7, linked as https://arxiv.org/abs/...,
// harvested as oai:arXiv.org:..., and minted as DOI 10.48550/arXiv.... They all
// name the same paper, so they all parse to the same ID here.
//
// Nothing in this package makes a network call. A well-formed id says which
// month a paper was submitted in and what its DOI is; it does not say whether
// the paper exists. Only a request can answer that.
package axid

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Style is which of arXiv's two id schemes a reference belongs to.
type Style string

const (
	// StyleNew is YYMM.NNNN or YYMM.NNNNN, used since April 2007.
	StyleNew Style = "new"
	// StyleOld is archive/YYMMNNN, used until March 2007.
	StyleOld Style = "old"
)

// DOIPrefix is the DataCite prefix arXiv mints its own DOIs under.
const DOIPrefix = "10.48550/arXiv."

// OAIPrefix is the namespace OAI-PMH identifiers carry.
const OAIPrefix = "oai:arXiv.org:"

// ID is a parsed arXiv reference.
//
// Canonical never carries the version. A reference that named one keeps it in
// Version, which is 0 when the reference did not name one. This is deliberate:
// a paper's title, authors and categories barely move across versions, so the
// paper is the node and the version is an attribute of the reference to it.
type ID struct {
	// Input is the reference exactly as it was passed in.
	Input string
	// Canonical is the bare id: no version, no URL, no prefix, no subject
	// class. Old-style ids keep their slash, so this is "1706.03762" or
	// "hep-th/9711200".
	//
	// Dropping the class is arXiv's own rule, not ours. Asking for
	// /abs/math.GT/0309136 gets a 301 to /abs/math/0309136, and the export
	// API's id_list returns nothing at all for the class-qualified form. Both
	// checked live on 2026-08-13.
	Canonical string
	// Style is new or old.
	Style Style
	// Archive is the old-style archive part without the subject class, so
	// "hep-th", "math" or "cond-mat". Empty for new-style ids.
	Archive string
	// Class is the subject class an old-style reference carried, so "GT" from
	// math.GT/0309136 or "supr-con" from cond-mat.supr-con/9910001. Empty when
	// the reference named none, which is most of them.
	Class string
	// Year is the four digit submission year the id encodes.
	Year int
	// Month is the submission month, 1 to 12.
	Month int
	// Sequence is the number within the month, kept exactly as written because
	// its zero padding is significant.
	Sequence string
	// Version is the version the reference named, or 0 for none.
	Version int
}

var (
	// newRe is YYMM.NNNN or YYMM.NNNNN with an optional version.
	newRe = regexp.MustCompile(`^(\d{2})(\d{2})\.(\d{4,5})(?:v(\d+))?$`)
	// oldRe is archive/YYMMNNN with an optional subject class and version.
	// The class is usually two uppercase letters (math.GT) but cond-mat and
	// physics write theirs in lowercase with hyphens (cond-mat.supr-con), so it
	// is matched by shape rather than against a list that would go stale.
	oldRe = regexp.MustCompile(`^([a-z][a-z-]*)(?:\.([A-Za-z][A-Za-z-]*))?/(\d{2})(\d{2})(\d{3})(?:v(\d+))?$`)
)

// urlPathPrefixes are the arXiv routes that carry an id in the path. They all
// mean the same paper, so a link to the PDF parses the same as a link to the
// abstract.
var urlPathPrefixes = []string{
	"/abs/", "/pdf/", "/html/", "/format/", "/src/", "/e-print/", "/ps/", "/tb/",
}

// Parse reads any of the nine ways an arXiv paper gets referred to and returns
// the canonical id.
//
// Accepted: a bare new-style id, a bare old-style id, either with a version,
// the arXiv:NNNN.NNNNN citation form journals print, any arxiv.org URL, an
// oai:arXiv.org: identifier, and the arXiv DOI with or without a doi: or
// https://doi.org/ wrapper.
func Parse(ref string) (ID, error) {
	raw := strings.TrimSpace(ref)
	if raw == "" {
		return ID{}, fmt.Errorf("empty arXiv reference")
	}

	s, err := unwrap(raw)
	if err != nil {
		return ID{}, err
	}

	id, err := parseBare(s)
	if err != nil {
		return ID{}, fmt.Errorf("cannot read %q as an arXiv id: %w", ref, err)
	}
	id.Input = raw
	return id, nil
}

// unwrap peels off whatever the reference is wrapped in and returns the bare
// id underneath. It does not validate the id itself; parseBare does that, so
// there is one place that decides what a well-formed id looks like.
func unwrap(s string) (string, error) {
	lower := strings.ToLower(s)

	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return unwrapURL(s)

	case strings.HasPrefix(lower, oaiPrefixLower):
		return s[len(OAIPrefix):], nil

	case strings.HasPrefix(lower, "doi:"):
		return unwrap(s[len("doi:"):])

	case strings.HasPrefix(lower, strings.ToLower(DOIPrefix)):
		return s[len(DOIPrefix):], nil

	case strings.HasPrefix(lower, "arxiv:"):
		return s[len("arxiv:"):], nil
	}
	return s, nil
}

// oaiPrefixLower is OAIPrefix folded once, since the prefix check runs on
// every parse and the constant is mixed case.
var oaiPrefixLower = strings.ToLower(OAIPrefix)

// unwrapURL extracts the id from an arxiv.org or doi.org URL.
func unwrapURL(s string) (string, error) {
	rest := s
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	host, path, _ := strings.Cut(rest, "/")
	path = "/" + path
	// Drop the query and the fragment; neither carries the id.
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	host = strings.ToLower(host)

	switch {
	case host == "doi.org" || host == "dx.doi.org":
		return unwrap(strings.TrimPrefix(path, "/"))

	case host == "arxiv.org" || strings.HasSuffix(host, ".arxiv.org"):
		for _, prefix := range urlPathPrefixes {
			if strings.HasPrefix(path, prefix) {
				id := strings.TrimPrefix(path, prefix)
				// The PDF route serves the same id with or without the
				// extension, and both forms show up in the wild.
				id = strings.TrimSuffix(id, ".pdf")
				return strings.TrimSuffix(id, "/"), nil
			}
		}
		return "", fmt.Errorf("no arXiv id in the path of %q", s)
	}
	return "", fmt.Errorf("the URL %q is not on arxiv.org or doi.org", s)
}

// parseBare parses a bare id, with or without a version suffix.
func parseBare(s string) (ID, error) {
	if m := newRe.FindStringSubmatch(s); m != nil {
		return parseNew(m)
	}
	if m := oldRe.FindStringSubmatch(s); m != nil {
		return parseOld(m)
	}
	return ID{}, fmt.Errorf("expected YYMM.NNNNN, archive/YYMMNNN, or an arxiv.org URL")
}

// parseNew builds an ID from a new-style match.
//
// The sequence width is not cosmetic. arXiv widened it from four digits to five
// in January 2015 because the four digit space ran out, so 2401.0001 is not a
// short way of writing 2401.00001, it is a reference to a paper that cannot
// exist. Repadding it silently would resolve to the wrong paper, so it is an
// error instead.
func parseNew(m []string) (ID, error) {
	yy, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	seq := m[3]
	year := 2000 + yy

	if month < 1 || month > 12 {
		return ID{}, fmt.Errorf("month %02d is not a month", month)
	}
	ym := year*100 + month
	switch len(seq) {
	case 4:
		if ym < 200704 || ym > 201412 {
			return ID{}, fmt.Errorf("a four digit sequence only ran from 0704 to 1412, so %02d%02d needs five digits", yy, month)
		}
	case 5:
		if ym < 201501 {
			return ID{}, fmt.Errorf("a five digit sequence started at 1501, so %02d%02d needs four digits", yy, month)
		}
	}

	id := ID{
		Canonical: fmt.Sprintf("%02d%02d.%s", yy, month, seq),
		Style:     StyleNew,
		Year:      year,
		Month:     month,
		Sequence:  seq,
	}
	if m[4] != "" {
		id.Version, _ = strconv.Atoi(m[4])
	}
	return id, nil
}

// parseOld builds an ID from an old-style match.
//
// The old scheme ran from August 1991 to March 2007, and the two digit year is
// read against that window: 91 through 99 are the 1990s, 00 through 07 are the
// 2000s. There is no ambiguity to resolve because the scheme was retired before
// the window wrapped.
func parseOld(m []string) (ID, error) {
	archive, class := m[1], m[2]
	yy, _ := strconv.Atoi(m[3])
	month, _ := strconv.Atoi(m[4])
	seq := m[5]

	if month < 1 || month > 12 {
		return ID{}, fmt.Errorf("month %02d is not a month", month)
	}
	year := 2000 + yy
	if yy >= 91 {
		year = 1900 + yy
	}
	if ym := year*100 + month; ym < 199108 || ym > 200703 {
		return ID{}, fmt.Errorf("old style ids ran from 9108 to 0703, and %02d%02d is outside that", yy, month)
	}

	id := ID{
		Canonical: fmt.Sprintf("%s/%02d%02d%s", archive, yy, month, seq),
		Style:     StyleOld,
		Archive:   archive,
		Class:     class,
		Year:      year,
		Month:     month,
		Sequence:  seq,
	}
	if m[6] != "" {
		id.Version, _ = strconv.Atoi(m[6])
	}
	return id, nil
}

// Category returns the arXiv category the id itself names, and whether it names
// one at all.
//
// Three cases, in order. A reference that carried a subject class says its
// category outright: math.GT/0309136 is math.GT. An archive that never had
// subject classes is a category on its own, so hep-th/9711200 is hep-th, and a
// retired archive is the category it was folded into, so alg-geom/9503001 is
// math.AG. Everything else, which is most old-style ids and every new-style
// one, does not encode a category and has to be asked for.
func (id ID) Category() (string, bool) {
	if id.Style != StyleOld {
		return "", false
	}
	if id.Class != "" {
		return id.Archive + "." + id.Class, true
	}
	if cat, ok := archiveCategory[id.Archive]; ok {
		return cat, true
	}
	return "", false
}

// archiveCategory maps every old-style archive that had no subject classes to
// the category it means today.
//
// The nine live ones map to themselves. The eighteen retired ones were folded
// into the modern taxonomy when arXiv reorganised, and each mapping here was
// read off the primary_category the export API returns for a paper in that
// archive, checked on 2026-08-13.
//
// This table cannot grow. The old scheme was retired in March 2007, so the set
// of archives an old-style id can name has been closed for nineteen years.
var archiveCategory = map[string]string{
	"gr-qc":    "gr-qc",
	"hep-ex":   "hep-ex",
	"hep-lat":  "hep-lat",
	"hep-ph":   "hep-ph",
	"hep-th":   "hep-th",
	"math-ph":  "math-ph",
	"nucl-ex":  "nucl-ex",
	"nucl-th":  "nucl-th",
	"quant-ph": "quant-ph",

	"acc-phys": "physics.acc-ph",
	"adap-org": "nlin.AO",
	"alg-geom": "math.AG",
	"ao-sci":   "physics.ao-ph",
	"atom-ph":  "physics.atom-ph",
	"bayes-an": "physics.data-an",
	"chao-dyn": "nlin.CD",
	"chem-ph":  "physics.chem-ph",
	"cmp-lg":   "cs.CL",
	"comp-gas": "nlin.CG",
	"dg-ga":    "math.DG",
	"funct-an": "math.FA",
	"mtrl-th":  "cond-mat.mtrl-sci",
	"patt-sol": "nlin.PS",
	"plasm-ph": "physics.plasm-ph",
	"q-alg":    "math.QA",
	"solv-int": "nlin.SI",
	"supr-con": "cond-mat.supr-con",
}

// Submitted is the month the id encodes, as YYYY-MM.
//
// This is the month arXiv assigned the number in, which is the submission month
// of v1 and not the announcement date. The two differ by up to a weekend, and
// the id encodes neither exactly, only the month.
func (id ID) Submitted() string {
	return fmt.Sprintf("%04d-%02d", id.Year, id.Month)
}

// DOI is the arXiv-issued DataCite DOI, which is a formula rather than a
// lookup: the prefix plus the canonical id, old-style slash and all.
//
// arXiv prints exactly this on the abstract page and doi.org resolves it, both
// checked live on 2026-08-13 against hep-th/9711200.
func (id ID) DOI() string { return DOIPrefix + id.Canonical }

// OAI is the OAI-PMH identifier for the paper, which is also a formula.
func (id ID) OAI() string { return OAIPrefix + id.Canonical }

// AbsURL is the canonical abstract page, with the version when one was named.
func (id ID) AbsURL() string { return "https://arxiv.org/abs/" + id.Versioned() }

// PDFURL is the canonical PDF, with the version when one was named.
func (id ID) PDFURL() string { return "https://arxiv.org/pdf/" + id.Versioned() }

// URI is the node name in the ax:// space.
//
// The old-style slash stays in the path. Escaping it to %2F would make a key
// nobody can read, and two keys for one paper the first time somebody forgot to
// escape it. The parse rule is fixed instead: after ax://paper/, everything to
// the end is the id.
func (id ID) URI() string {
	if id.Version > 0 {
		return fmt.Sprintf("ax://paper/%s#v%d", id.Canonical, id.Version)
	}
	return "ax://paper/" + id.Canonical
}

// Versioned is the canonical id with the version appended when the reference
// named one, which is the form arXiv's own routes take.
func (id ID) Versioned() string {
	if id.Version > 0 {
		return fmt.Sprintf("%sv%d", id.Canonical, id.Version)
	}
	return id.Canonical
}

// Cite is the arXiv:NNNN.NNNNN form journals print.
func (id ID) Cite() string { return "arXiv:" + id.Versioned() }

// String returns the canonical id, so an ID drops into a path or a log line.
func (id ID) String() string { return id.Canonical }

// SortKey returns a string that sorts ids by submission month and then by
// sequence within the month, across both styles.
//
// Sorting the canonical ids directly does not work: "hep-th/9711200" sorts
// after "1706.03762" because "h" beats "1", which would put a 1997 paper after
// a 2017 one. The key puts the four digit year first, and pads the sequence to
// five digits so 0704.0001 and 1501.00001 line up in the same column.
func (id ID) SortKey() string {
	return fmt.Sprintf("%04d%02d-%05s-%s", id.Year, id.Month, id.Sequence, id.Archive)
}

// Valid reports whether ref parses. It exists so callers that only need a yes
// or no do not have to discard an ID and an error to get one.
func Valid(ref string) bool {
	_, err := Parse(ref)
	return err == nil
}
