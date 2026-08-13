package arxiv

import (
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// ParsePaperID extracts a bare arXiv id from any of the nine ways a paper gets
// referred to: a bare id in either style, a versioned id, the arXiv: citation
// form, any arxiv.org URL, an OAI identifier, and the arXiv DOI.
//
// The parsing lives in pkg/axid, which is importable on its own and makes no
// network call. This wrapper stays because the client wants the bare id and
// nothing else, and because the version is never part of it.
func ParsePaperID(s string) (string, error) {
	id, err := axid.Parse(s)
	if err != nil {
		return "", err
	}
	return id.Canonical, nil
}
