package arxiv

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// realPaper is the record the file list is built from, put together the same
// way the client puts it together: the API answer with the abstract page merged
// over it.
func realPaper(t *testing.T) Paper {
	t.Helper()
	p := paperFixture(t, "api_1706.03762.xml")
	mergeAbs(&p, absFixture(t, "abs_1706.03762.html"), "https://arxiv.org/abs/1706.03762")
	return p
}

// The list costs nothing beyond the paper read, because which files exist is
// already on the abstract page. That is what makes `arxiv files` free and
// --measure the opt in.
func TestFilesOfAPaperWithEverything(t *testing.T) {
	p := realPaper(t)
	if !p.HasHTML || !p.HasSource {
		t.Fatalf("the fixture no longer offers html and source, so this test is checking nothing")
	}
	files := filesOf(p, testTime)
	if len(files) != 3 {
		t.Fatalf("got %d files, want pdf, html and source", len(files))
	}

	want := []string{KindPDF, KindHTML, KindSource}
	for i, f := range files {
		if f.Kind != want[i] {
			t.Errorf("file %d is %q, want %q", i, f.Kind, want[i])
		}
		if f.PaperID != "1706.03762" {
			t.Errorf("%s names paper %q", f.Kind, f.PaperID)
		}
		if f.Version != p.Version {
			t.Errorf("%s is at version %d, want %d", f.Kind, f.Version, p.Version)
		}
		if !strings.HasPrefix(f.URL, "https://arxiv.org/") {
			t.Errorf("%s has url %q", f.Kind, f.URL)
		}
		if !strings.Contains(f.URL, "v7") {
			t.Errorf("%s url %q has no version on it; a file belongs to a version", f.Kind, f.URL)
		}
	}
}

// Every file carries the surfaces that answered for the paper, each with the
// URL that answered, plus s12 for the bytes. A surface with no source next to
// it is provenance that cannot be followed back.
func TestFilesCarryTheProvenanceOfThePaper(t *testing.T) {
	p := realPaper(t)
	for _, f := range filesOf(p, testTime) {
		if len(f.Surfaces) != len(f.Sources) {
			t.Fatalf("%s has %d surfaces and %d sources", f.Kind, len(f.Surfaces), len(f.Sources))
		}
		seen := false
		for i, s := range f.Surfaces {
			if f.Sources[i] == "" {
				t.Errorf("%s cites %s with no url", f.Kind, s)
			}
			if s == SurfaceFiles {
				seen = true
				if f.Sources[i] != f.URL {
					t.Errorf("%s cites s12 as %q, want %q", f.Kind, f.Sources[i], f.URL)
				}
			}
		}
		if !seen {
			t.Errorf("%s does not cite %s", f.Kind, SurfaceFiles)
		}
	}
}

// arXiv's own figure is the size of the submission, so it goes on the source
// and nowhere else. On this paper the source is about 1.1 MB and the PDF is
// about 2.2 MB, so putting the table figure on the PDF would be off by a factor
// of two.
func TestFilesPutTheTableSizeOnTheSourceOnly(t *testing.T) {
	files := filesOf(realPaper(t), testTime)
	for _, f := range files {
		switch f.Kind {
		case KindSource:
			if f.SizeBytes == 0 {
				t.Error("the source has no size, but the version table gives one")
			}
			if f.SizeFrom != SizeFromTable {
				t.Errorf("the source size says it came from %q, want %q", f.SizeFrom, SizeFromTable)
			}
			if f.Via["size_bytes"] == "" {
				t.Error("the source size is not attributed to a surface")
			}
		default:
			if f.SizeBytes != 0 {
				t.Errorf("%s carries a size of %d that nobody measured", f.Kind, f.SizeBytes)
			}
			if f.SizeFrom != "" {
				t.Errorf("%s says its size came from %q", f.Kind, f.SizeFrom)
			}
		}
	}
}

// A paper submitted as a PDF has no source and most older papers have no HTML,
// so the list is what the paper says exists rather than the three URLs that
// could be formed.
func TestFilesOfAPaperWithOnlyAPDF(t *testing.T) {
	p := Paper{ID: "hep-th/9711200", Version: 3}
	files := filesOf(p, testTime)
	if len(files) != 1 || files[0].Kind != KindPDF {
		t.Fatalf("got %d files %v, want just the pdf", len(files), kindsOf(files))
	}
	if files[0].URL != "https://arxiv.org/pdf/hep-th/9711200v3" {
		t.Errorf("url = %q", files[0].URL)
	}
}

// The abstract page links its own HTML rendering, and that link wins over one
// built from the id, because arXiv is the one that knows where it put it.
func TestFilesUseTheHTMLLinkThePageGives(t *testing.T) {
	p := Paper{ID: "2401.00001", Version: 1, HasHTML: true, HTMLURL: "https://arxiv.org/html/2401.00001v1"}
	files := filesOf(p, testTime)
	if len(files) != 2 {
		t.Fatalf("got %v, want pdf and html", kindsOf(files))
	}
	if files[1].URL != "https://arxiv.org/html/2401.00001v1" {
		t.Errorf("html url = %q", files[1].URL)
	}
}

// The size on the record is the size of the version the record is at. A paper
// read at v7 that took the v1 figure would report a number for bytes nobody is
// about to download.
func TestSourceSizeIsTheSizeOfThisVersion(t *testing.T) {
	p := Paper{
		ID:      "2401.00001",
		Version: 2,
		Versions: []Version{
			{Version: 1, SizeBytes: 1000},
			{Version: 2, SizeBytes: 2000},
		},
	}
	got, ok := sourceSize(p)
	if !ok || got != 2000 {
		t.Errorf("sourceSize = %d, %v, want 2000, true", got, ok)
	}

	p.Version = 3
	if _, ok := sourceSize(p); ok {
		t.Error("a version with no row in the table reported a size")
	}
}

func TestFileURL(t *testing.T) {
	id := mustID(t, "1706.03762v7")
	cases := map[string]string{
		KindPDF:    "https://arxiv.org/pdf/1706.03762v7",
		KindHTML:   "https://arxiv.org/html/1706.03762v7",
		KindSource: "https://arxiv.org/src/1706.03762v7",
	}
	for kind, want := range cases {
		got, err := fileURL(id, kind)
		if err != nil {
			t.Fatalf("fileURL(%s): %v", kind, err)
		}
		if got != want {
			t.Errorf("fileURL(%s) = %q, want %q", kind, got, want)
		}
	}
	if _, err := fileURL(id, "ps"); err == nil {
		t.Error("a kind arxiv no longer serves was accepted")
	}
}

func TestFileURI(t *testing.T) {
	cases := []struct {
		id      string
		version int
		kind    string
		want    string
	}{
		{"1706.03762", 7, KindPDF, "ax://file/1706.03762#v7.pdf"},
		{"1706.03762", 0, KindSource, "ax://file/1706.03762#source"},
	}
	for _, c := range cases {
		if got := FileURI(c.id, c.version, c.kind); got != c.want {
			t.Errorf("FileURI(%s, %d, %s) = %q, want %q", c.id, c.version, c.kind, got, c.want)
		}
	}
}

// The total in a content-range is the only place the CDN says how big a file
// is, because a HEAD comes back without a content-length.
func TestTotalOf(t *testing.T) {
	cases := map[string]int64{
		"bytes 0-0/2215244":  2215244,
		"bytes 100-200/1000": 1000,
		"bytes 0-0/*":        0,
		"":                   0,
		"nonsense":           0,
	}
	for in, want := range cases {
		if got := totalOf(in); got != want {
			t.Errorf("totalOf(%q) = %d, want %d", in, got, want)
		}
	}
}

// arXiv puts the version in the filename it gives out, which is the name worth
// saving under even when the URL asked for had no version on it.
func TestFilenameOf(t *testing.T) {
	cases := map[string]string{
		`inline; filename="1706.03762v7.pdf"`:              "1706.03762v7.pdf",
		`attachment; filename="arXiv-1706.03762v7.tar.gz"`: "arXiv-1706.03762v7.tar.gz",
		`attachment; filename=hep-th_9711200v3.pdf`:        "hep-th_9711200v3.pdf",
		"":                                    "",
		"attachment; filename=\"unterminated": "",
	}
	for in, want := range cases {
		if got := filenameOf(in); got != want {
			t.Errorf("filenameOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustID(t *testing.T, ref string) axid.ID {
	t.Helper()
	id, err := axid.Parse(ref)
	if err != nil {
		t.Fatalf("parse %q: %v", ref, err)
	}
	return id
}

func kindsOf(files []File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Kind
	}
	return out
}
