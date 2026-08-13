package arxiv

import (
	"strings"
	"testing"
	"time"
)

// publishedRecord is 1207.7214, the ATLAS Higgs paper, read the way `arxiv
// cite` reads it: the export API for the dates and the ids, OAI for the journal
// reference and the publisher DOI.
//
// It is the awkward one on purpose. There is one author and the author is a
// collaboration, the title has "Standard Model" in it, and it is the case where
// a locally rendered entry differs from arXiv's own in every way it can.
func publishedRecord(t *testing.T) Paper {
	t.Helper()
	p := paperFixture(t, "api_1207.7214.xml")
	mergeOAIArxiv(&p, oaiFixture(t, "oai_arxiv_1207.7214.xml"), "https://oaipmh.arxiv.org/oai")
	return p
}

func TestRenderBibTeXOfAPreprint(t *testing.T) {
	got := renderBibTeX(fullRecord(t))
	want := `@misc{vaswani2017attentionallyouneed,
      title={Attention Is All You Need},
      author={Ashish Vaswani and Noam Shazeer and Niki Parmar and Jakob Uszkoreit and Llion Jones and Aidan N. Gomez and Lukasz Kaiser and Illia Polosukhin},
      year={2017},
      eprint={1706.03762},
      archivePrefix={arXiv},
      primaryClass={cs.CL},
      doi={10.48550/arXiv.1706.03762},
      url={https://arxiv.org/abs/1706.03762v7},
}`
	if got != want {
		t.Errorf("entry:\n%s\n\nwant:\n%s", got, want)
	}
}

// TestRenderBibTeXOfAPublishedPaper is the reason --local exists. arXiv's own
// entry for this paper is @misc with no journal in it, and the journal is the
// thing a bibliography wants.
func TestRenderBibTeXOfAPublishedPaper(t *testing.T) {
	got := renderBibTeX(publishedRecord(t))
	if !strings.HasPrefix(got, "@article{") {
		t.Errorf("a published paper came out as %.20q, want @article", got)
	}
	for _, want := range []string{
		"      journal={Phys.Lett. B716 (2012) 1-29},\n",
		"      year={2012},\n",
		"      doi={10.1016/j.physletb.2012.08.020},\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("entry is missing %q:\n%s", want, got)
		}
	}
	// The doi field holds a DOI. arXiv writes a URL there, which no BibTeX
	// style can turn back into a DOI.
	if strings.Contains(got, "doi={https://") {
		t.Errorf("the doi field holds a URL:\n%s", got)
	}
}

// TestBibTeXYearIsTheFirstSubmission is the difference people notice. arXiv
// dates this paper 2023, because v7 was posted in 2023, and every citation of
// it in the literature says 2017.
func TestBibTeXYearIsTheFirstSubmission(t *testing.T) {
	p := fullRecord(t)
	if got := p.FirstSubmitted.Year(); got != 2017 {
		t.Fatalf("the fixture says %d, so this test is checking the wrong thing", got)
	}
	if !strings.Contains(renderBibTeX(p), "year={2017}") {
		t.Errorf("the entry does not carry the first submission year:\n%s", renderBibTeX(p))
	}
}

func TestBibTeXLeavesOutWhatItDoesNotKnow(t *testing.T) {
	got := renderBibTeX(Paper{ID: "2401.00001", Title: "A paper"})
	want := `@misc{240100001paper,
      title={A paper},
      eprint={2401.00001},
      archivePrefix={arXiv},
      url={https://arxiv.org/abs/2401.00001},
}`
	if got != want {
		t.Errorf("entry:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestCiteKey(t *testing.T) {
	cases := []struct {
		name  string
		paper Paper
		want  string
	}{
		{
			"four title words, small words dropped",
			Paper{
				Authors:        []Author{{Name: "Ashish Vaswani", Keyname: "Vaswani", Forenames: "Ashish"}},
				Title:          "Attention Is All You Need",
				FirstSubmitted: time.Date(2017, 6, 12, 0, 0, 0, 0, time.UTC),
			},
			"vaswani2017attentionallyouneed",
		},
		{
			"a collaboration keeps its whole name",
			Paper{
				Authors:        []Author{{Name: "The ATLAS Collaboration"}},
				Title:          "Observation of a new particle",
				FirstSubmitted: time.Date(2012, 7, 31, 0, 0, 0, 0, time.UTC),
			},
			"theatlascollaboration2012observationnewparticle",
		},
		{
			"punctuation and case go",
			Paper{
				Authors:        []Author{{Keyname: "van der Waals", Name: "J. D. van der Waals"}},
				Title:          "On the Continuity of the Gaseous and Liquid States",
				FirstSubmitted: time.Date(1873, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			"vanderwaals1873continuitygaseousliquidstates",
		},
		{
			"a paper with nothing on it falls back to the id",
			Paper{ID: "hep-th/9711200"},
			"hepth9711200",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CiteKey(tc.paper); got != tc.want {
				t.Errorf("CiteKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPaperURL(t *testing.T) {
	cases := []struct {
		name  string
		paper Paper
		want  string
	}{
		{"the record's own url wins", Paper{ID: "1706.03762", Version: 7, URL: "https://arxiv.org/abs/1706.03762v7"}, "https://arxiv.org/abs/1706.03762v7"},
		{"a version with no url is built", Paper{ID: "1706.03762", Version: 3}, "https://arxiv.org/abs/1706.03762v3"},
		{"no version at all is the bare page", Paper{ID: "hep-th/9711200"}, "https://arxiv.org/abs/hep-th/9711200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paperURL(tc.paper); got != tc.want {
				t.Errorf("paperURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLetters(t *testing.T) {
	cases := map[string]string{
		"The ATLAS Collaboration": "theatlascollaboration",
		"hep-th/9711200":          "hepth9711200",
		"Schrödinger":             "schrödinger",
		"---":                     "",
	}
	for in, want := range cases {
		if got := letters(in); got != want {
			t.Errorf("letters(%q) = %q, want %q", in, got, want)
		}
	}
}
