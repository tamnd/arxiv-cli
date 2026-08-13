package rdf

import (
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// Naming a node to the outside world.
//
// An ax:// URI is this tool's key for something and it is nobody else's, so
// every node has to be named again on the way out. The rule is one sentence: if
// the node has an address in the world, use the address; if it does not, mint
// one and say so by the namespace it is minted in.
//
// A paper is https://arxiv.org/abs/1706.03762, which is what a person would
// paste into a browser and what every other dataset on the planet calls that
// paper. A name is not anything: ax://name/aidan-n-gomez is a normalised
// author string, there is no page for it, and pointing it at an arXiv URL would
// be claiming arXiv has a person page it does not have. So it is minted under
// this tool's own identifier namespace, where it is obvious whose claim it is.

const absBase = "https://arxiv.org/abs/"
const authorBase = "https://arxiv.org/a/"
const orcidBase = "https://orcid.org/"
const doiBase = "https://doi.org/"

// fileBase is where each kind of file lives.
var fileBase = map[string]string{
	"pdf":    "https://arxiv.org/pdf/",
	"html":   "https://arxiv.org/html/",
	"source": "https://arxiv.org/src/",
}

// NodeIRI turns an ax:// URI into the IRI it is written as, or the empty string
// if the URI is not one of ours.
func NodeIRI(uri string) IRI {
	kind, ok := graph.KindOf(uri)
	if !ok {
		return ""
	}
	rest := strings.TrimPrefix(uri, graph.Scheme)
	_, rest, _ = strings.Cut(rest, "/")
	switch kind {
	case graph.KindPaper:
		// A version is a fragment on the paper here and a v7 on the end of the
		// id out there, which is the same thing said two ways.
		id, frag := cutFragment(rest)
		if strings.HasPrefix(frag, "v") {
			return IRI(absBase + id + frag)
		}
		return IRI(absBase + id)
	case graph.KindAuthor:
		return IRI(authorBase + rest)
	case graph.KindORCID:
		return IRI(orcidBase + rest)
	case graph.KindDOI:
		return IRI(doiBase + rest)
	case graph.KindLicense:
		return licenseIRI(rest)
	case graph.KindFile:
		return fileIRI(rest)
	default:
		return Mint(kind, rest)
	}
}

// Mint names a node that has no address in the world.
func Mint(kind, rest string) IRI { return IRI(NSID + kind + "/" + escape(rest)) }

// Minted says whether an IRI is one this tool made up, which is what decides
// whether a label may be hung on it. Naming somebody else's resource is not
// ours to do.
func Minted(i IRI) bool { return strings.HasPrefix(string(i), NSID) }

// fileIRI is the URL arXiv serves the bytes at.
//
// ax://file/1706.03762#v7.pdf is https://arxiv.org/pdf/1706.03762v7, and a kind
// nobody has taught this function is minted rather than guessed at, because a
// guessed file URL is a 404 wearing a fact's clothes.
func fileIRI(rest string) IRI {
	id, frag := cutFragment(rest)
	version, kind := "", frag
	if v, k, ok := strings.Cut(frag, "."); ok && strings.HasPrefix(v, "v") {
		version, kind = v, k
	}
	base, ok := fileBase[kind]
	if !ok {
		return Mint(graph.KindFile, rest)
	}
	return IRI(base + id + version)
}

// licenseIRI puts back the URL graph.LicenseSlug folded down.
//
// The slug exists so a query can say cc-by-4.0 without a lookup table, and the
// URL exists because that is the licence's actual name and what dc:rights holds
// on the OAI record. The families arXiv uses are the two below; anything else
// is minted rather than reassembled from a guess about where the slashes went.
func licenseIRI(slug string) IRI {
	code, version, ok := cutLastVersion(slug)
	if !ok {
		return Mint(graph.KindLicense, slug)
	}
	switch {
	case strings.HasPrefix(code, "cc-"):
		name := strings.TrimPrefix(code, "cc-")
		if name == "zero" || name == "mark" {
			return IRI("https://creativecommons.org/publicdomain/" + name + "/" + version + "/")
		}
		return IRI("https://creativecommons.org/licenses/" + name + "/" + version + "/")
	case strings.HasPrefix(code, "arxiv-"):
		// arXiv serves its own licences over http and the URL on the record is
		// the http one, so writing https here would be a second IRI for one
		// licence and the two would never join.
		return IRI("http://arxiv.org/licenses/" + strings.TrimPrefix(code, "arxiv-") + "/" + version + "/")
	}
	return Mint(graph.KindLicense, slug)
}

// cutLastVersion splits cc-by-nc-sa-4.0 into cc-by-nc-sa and 4.0.
func cutLastVersion(slug string) (string, string, bool) {
	i := strings.LastIndex(slug, "-")
	if i < 0 {
		return "", "", false
	}
	version := slug[i+1:]
	dot := strings.Index(version, ".")
	if dot <= 0 || dot == len(version)-1 {
		return "", "", false
	}
	for j := 0; j < len(version); j++ {
		if c := version[j]; (c < '0' || c > '9') && c != '.' {
			return "", "", false
		}
	}
	return slug[:i], version, true
}

// cutFragment splits a URI's fragment off, which is a version on a paper and a
// kind on a file.
func cutFragment(rest string) (string, string) {
	id, frag, ok := strings.Cut(rest, "#")
	if !ok {
		return rest, ""
	}
	return id, frag
}

// escape percent encodes the characters that would end an IRI early or start a
// fragment inside one. Slugs and category codes come through untouched, which
// is the whole point of minting them readably.
func escape(s string) string {
	const bad = "#?<>\"{}|\\^`% "
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || strings.IndexByte(bad, c) >= 0 {
			b.WriteString("%")
			b.WriteByte(hexDigit(c >> 4))
			b.WriteByte(hexDigit(c & 0xf))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + n - 10
}
