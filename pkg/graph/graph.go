// Package graph names things.
//
// Every node in the store has a URI in the ax:// space, and this package is the
// only place those URIs are built. That matters more than it sounds: a node kind
// with two spellings is two nodes, and the day somebody writes one of them by
// hand in a parser is the day half the edges stop joining.
//
// The rules are doc 04 sections 1 and 2 of spec 3006, and the ones worth
// repeating here are:
//
//   - A name is not a person. ax://name/john-baez is "papers whose author string
//     normalises to john baez", which may be one person or three. ax://author/baez_j_1
//     is the person who registered that identifier with arXiv. They are different
//     claims, so they are different spaces, joined only when arXiv says so.
//   - A name is normalised and a URL is hashed. Normalising is a lossy join and
//     that is the point of it, because two spellings of a name should land
//     together. Two URLs that differ have no useful join, only exact identity.
//   - A version is a fragment and not a node, because a paper's title, authors
//     and categories barely move across versions and seven nodes would be six
//     copies plus a diff.
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Scheme is the URI scheme every node lives under.
const Scheme = "ax://"

// The node kinds. A URI's kind is the segment after the scheme, so these are
// both the kind names and the first path segment.
const (
	KindPaper     = "paper"
	KindAuthor    = "author"
	KindName      = "name"
	KindORCID     = "orcid"
	KindCategory  = "category"
	KindArchive   = "archive"
	KindGroup     = "group"
	KindSet       = "set"
	KindJournal   = "journal"
	KindDOI       = "doi"
	KindExternal  = "external"
	KindLicense   = "license"
	KindFile      = "file"
	KindTrackback = "trackback"
)

// Kinds is every node kind, in the order doc 04 section 2 lists them.
var Kinds = []string{
	KindPaper, KindAuthor, KindName, KindORCID, KindCategory, KindArchive,
	KindGroup, KindSet, KindJournal, KindDOI, KindExternal, KindLicense,
	KindFile, KindTrackback,
}

// Paper names a paper, always without the version.
//
// The old style slash stays in the path. ax://paper/hep-th/9711200 is a legal
// URI and escaping the slash to %2F would give a key nobody can read and two
// keys for one paper the first time somebody forgets to escape. The parse rule
// is fixed instead: after ax://paper/, everything to the end is the id.
func Paper(id string) string { return Scheme + KindPaper + "/" + id }

// Version names one version of a paper, as a fragment on the paper.
func Version(id string, n int) string { return fmt.Sprintf("%s#v%d", Paper(id), n) }

// Author names a person who registered an arXiv author identifier.
func Author(id string) string { return Scheme + KindAuthor + "/" + id }

// Name names an author string, which is not the same claim as a person.
func Name(name string) string { return Scheme + KindName + "/" + NormalizeName(name) }

// ORCID names an ORCID, which is the identifier that survives a name change and
// the only one shared with the world outside arXiv.
func ORCID(orcid string) string { return Scheme + KindORCID + "/" + strings.TrimSpace(orcid) }

// Category names a category. hep-th is both a category and an archive, so it
// gets a node in both spaces, which looks odd until you try to remove it and
// find half the physics archives fall out of the tree.
func Category(code string) string { return Scheme + KindCategory + "/" + code }

// Archive names an archive.
func Archive(code string) string { return Scheme + KindArchive + "/" + code }

// Group names a top level group in the taxonomy.
func Group(name string) string { return Scheme + KindGroup + "/" + slug(name) }

// Set names an OAI set by its setSpec.
func Set(spec string) string { return Scheme + KindSet + "/" + spec }

// Journal names a journal reference by the hash of its normalised form.
//
// This is the least trustworthy node kind in the store and it is hashed rather
// than slugged so nobody reads it as a title. Two references that normalise the
// same are one node, which is the whole point, and two that do not are two nodes
// even when a human can see they are the same journal.
func Journal(ref string) string {
	n := NormalizeJournal(ref)
	if n == "" {
		return ""
	}
	return Scheme + KindJournal + "/" + Hash(n)
}

// DOI names a DOI. The case is folded because DOIs are case insensitive by
// specification and publishers do not agree on which case to print.
func DOI(doi string) string {
	d := strings.TrimSpace(doi)
	d = strings.TrimPrefix(d, "doi:")
	d = strings.TrimPrefix(d, "https://doi.org/")
	d = strings.TrimPrefix(d, "http://dx.doi.org/")
	if d == "" {
		return ""
	}
	return Scheme + KindDOI + "/" + strings.ToLower(d)
}

// External names a page somewhere that is not arXiv, by the hash of its URL.
//
// Hashed rather than embedded, because a URL in a URI needs escaping, and a URL
// that is escaped two different ways is two nodes for one page.
func External(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}
	return Scheme + KindExternal + "/" + Hash(canonicalURL(u))
}

// License names a license. arXiv publishes a URL and the well known ones get a
// readable slug, because ax://license/cc-by-4.0 is a node somebody can query for
// without looking anything up.
func License(rawURL string) string {
	s := LicenseSlug(rawURL)
	if s == "" {
		return ""
	}
	return Scheme + KindLicense + "/" + s
}

// File names one downloadable artifact of one version of one paper.
//
// A file is its own space rather than a fragment on the paper, because a file
// belongs to a version and the fragment space already belongs to versions.
// ax://paper/1706.03762#file/pdf cannot say which version's bytes it means, and
// v1 and v7 are different bytes at different URLs.
func File(id string, version int, kind string) string {
	if version > 0 {
		return fmt.Sprintf("%s%s/%s#v%d.%s", Scheme, KindFile, id, version, kind)
	}
	return Scheme + KindFile + "/" + id + "#" + kind
}

// Trackback names one ping by arXiv's own number for it, so two reads of the
// same ping land on the same node.
func Trackback(id string) string { return Scheme + KindTrackback + "/" + id }

// KindOf reports which space a URI is in, and whether it is a URI at all.
//
// A fragment does not change the kind. ax://paper/1706.03762#v7 is a paper node
// naming one of its versions, which is what makes has_version an edge inside the
// paper space rather than a join to somewhere else.
func KindOf(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, Scheme)
	if !ok {
		return "", false
	}
	kind, tail, ok := strings.Cut(rest, "/")
	if !ok || tail == "" {
		return "", false
	}
	for _, k := range Kinds {
		if k == kind {
			return k, true
		}
	}
	return "", false
}

// IsVersion reports whether a URI names a version rather than a paper.
func IsVersion(uri string) bool {
	kind, ok := KindOf(uri)
	if !ok || kind != KindPaper {
		return false
	}
	_, frag, ok := strings.Cut(uri, "#")
	return ok && strings.HasPrefix(frag, "v")
}

// NormalizeName folds an author string down to something two spellings can
// agree on: lowercased, accents folded, punctuation dropped, spaces to hyphens.
//
// Aidan N. Gomez becomes aidan-n-gomez. This is lossy on purpose. It is also
// exactly why a name node is not a person node: the same fold that brings two
// spellings of one person together brings two people with one name together.
func NormalizeName(name string) string { return slug(name) }

// NormalizeJournal folds a hand typed journal reference.
//
// Nature 521, 436-444 (2015) and Nature 521:436-444, 2015 are the same issue
// written by two authors who typed it themselves. Punctuation goes, whitespace
// collapses, the case folds, and a year in parentheses is dropped when the same
// year is already in the string, which is the one duplication that shows up
// again and again.
func NormalizeJournal(ref string) string {
	s := strings.TrimSpace(ref)
	if s == "" {
		return ""
	}
	if year, rest, ok := trailingYear(s); ok && strings.Contains(rest, year) {
		s = rest
	}
	return slug(s)
}

// trailingYear splits a "(2015)" off the end and hands back the year, the rest,
// and whether there was one.
func trailingYear(s string) (string, string, bool) {
	t := strings.TrimSpace(s)
	if !strings.HasSuffix(t, ")") {
		return "", s, false
	}
	i := strings.LastIndex(t, "(")
	if i < 0 {
		return "", s, false
	}
	year := strings.TrimSpace(t[i+1 : len(t)-1])
	if len(year) != 4 {
		return "", s, false
	}
	for _, r := range year {
		if r < '0' || r > '9' {
			return "", s, false
		}
	}
	return year, strings.TrimSpace(t[:i]), true
}

// LicenseSlug turns a license URL into a readable node name.
//
// The Creative Commons and arXiv URLs are named rather than hashed because they
// are the ones that come up, and a query for cc-by-4.0 should not need a lookup
// table. Anything else keeps its host and path, which is still readable and
// still unique.
func LicenseSlug(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return slug(s)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	parts := pathParts(u.Path)
	switch {
	case host == "creativecommons.org" && len(parts) >= 3 && parts[0] == "licenses":
		// /licenses/by-nc-sa/4.0/ becomes cc-by-nc-sa-4.0
		return "cc-" + parts[1] + "-" + parts[2]
	case host == "creativecommons.org" && len(parts) >= 2 && parts[0] == "publicdomain":
		// /publicdomain/zero/1.0/ becomes cc-zero-1.0
		return "cc-" + strings.Join(parts[1:], "-")
	case host == "arxiv.org" && len(parts) >= 2 && parts[0] == "licenses":
		return "arxiv-" + strings.Join(parts[1:], "-")
	}
	return slug(host + " " + strings.Join(parts, " "))
}

// pathParts splits a URL path and drops the empty segments a trailing slash
// leaves behind.
func pathParts(p string) []string {
	out := make([]string, 0, 4)
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// canonicalURL is the small amount of tidying two spellings of one address
// deserve before hashing: the scheme and host lowercased, a default port
// dropped, a trailing slash on the root dropped.
//
// It stops there. Dropping query parameters or fragments would merge pages that
// are genuinely different, and this is the one node kind with no join to lose.
func canonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	host = strings.TrimSuffix(host, ":80")
	host = strings.TrimSuffix(host, ":443")
	u.Host = host
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

// Hash is the sha256 an unslugged node is named by.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// slug lowercases, folds accents, drops punctuation and joins with hyphens.
func slug(s string) string {
	// NFD splits an accented letter into the letter and its mark, so dropping
	// the marks leaves the letter behind. Without this, Erdős and Erdos are two
	// nodes and the join everybody wants is the one that never happens.
	folded := norm.NFD.String(strings.ToLower(strings.TrimSpace(s)))
	var b strings.Builder
	gap := false
	for _, r := range folded {
		switch {
		case unicode.Is(unicode.Mn, r):
			// a combining mark, dropped with the accent it belongs to
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			gap = false
			b.WriteRune(r)
		default:
			gap = true
		}
	}
	return b.String()
}
