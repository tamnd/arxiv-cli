package arxiv

import (
	"context"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// IDInfo is everything an arXiv reference says about itself with no request at
// all. Requests is on the record rather than in the help text so the claim is
// machine readable and stays true.
type IDInfo struct {
	Input     string `json:"input"`
	Canonical string `json:"canonical" kit:"id"`
	Style     string `json:"style"`
	Archive   string `json:"archive,omitempty"`
	Class     string `json:"class,omitempty"`
	Category  string `json:"category,omitempty"`
	Submitted string `json:"submitted"`
	Sequence  string `json:"sequence"`
	Version   int    `json:"version,omitempty"`
	DOI       string `json:"doi"`
	OAI       string `json:"oai"`
	Abs       string `json:"abs"`
	PDF       string `json:"pdf"`
	URI       string `json:"uri"`
	Requests  int    `json:"requests"`
}

// describe turns a parsed reference into the record the command prints.
func describe(id axid.ID) IDInfo {
	info := IDInfo{
		Input:     id.Input,
		Canonical: id.Canonical,
		Style:     string(id.Style),
		Archive:   id.Archive,
		Class:     id.Class,
		Submitted: id.Submitted(),
		Sequence:  id.Sequence,
		Version:   id.Version,
		DOI:       id.DOI(),
		OAI:       id.OAI(),
		Abs:       id.AbsURL(),
		PDF:       id.PDFURL(),
		URI:       id.URI(),
	}
	if cat, ok := id.Category(); ok {
		info.Category = cat
	}
	return info
}

type idIn struct {
	Ref string `kit:"arg" help:"any way of writing an arXiv reference"`
}

func registerID(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "id",
		Group:   "explain",
		Single:  true,
		URIType: "paper",
		Summary: "Classify an arXiv reference without asking arXiv",
		Args:    []kit.Arg{{Name: "ref", Help: "any way of writing an arXiv reference"}},
		Long: `Classify an arXiv reference and print everything it says about itself.

Takes a bare id in either style, a versioned id, the arXiv: form journals print,
any arxiv.org URL, an oai:arXiv.org: identifier, or the arXiv DOI, and prints the
canonical id, the style, the submission month, the DOI, the OAI identifier and
the ax:// URI.

Nothing here is fetched. A well-formed id says when it was submitted and what
its DOI is; it does not say whether the paper exists, and only a request can
answer that.`,
	}, func(_ context.Context, in idIn, emit func(*IDInfo) error) error {
		id, err := axid.Parse(in.Ref)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		info := describe(id)
		return emit(&info)
	})
}
