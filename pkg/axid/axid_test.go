package axid

import (
	"sort"
	"testing"
)

// TestShapes walks the reference shapes from spec 3006 doc 04 section 1.
// Every one of them names a real paper, so the expectations here can be checked
// against arxiv.org by hand when one of them looks wrong.
func TestShapes(t *testing.T) {
	tests := []struct {
		shape     string
		in        string
		canonical string
		style     Style
		archive   string
		class     string
		year      int
		month     int
		seq       string
		version   int
	}{
		{
			shape: "new style, five digit", in: "2401.00001",
			canonical: "2401.00001", style: StyleNew, year: 2024, month: 1, seq: "00001",
		},
		{
			shape: "new style, four digit", in: "0704.0001",
			canonical: "0704.0001", style: StyleNew, year: 2007, month: 4, seq: "0001",
		},
		{
			shape: "versioned", in: "1706.03762v7",
			canonical: "1706.03762", style: StyleNew, year: 2017, month: 6, seq: "03762", version: 7,
		},
		{
			shape: "old style", in: "hep-th/9711200",
			canonical: "hep-th/9711200", style: StyleOld, archive: "hep-th", year: 1997, month: 11, seq: "200",
		},
		{
			shape: "old style with subject class", in: "math.GT/0309136",
			canonical: "math/0309136", style: StyleOld, archive: "math", class: "GT", year: 2003, month: 9, seq: "136",
		},
		{
			shape: "citation form", in: "arXiv:1706.03762v7",
			canonical: "1706.03762", style: StyleNew, year: 2017, month: 6, seq: "03762", version: 7,
		},
		{
			shape: "abs URL", in: "https://arxiv.org/abs/1706.03762",
			canonical: "1706.03762", style: StyleNew, year: 2017, month: 6, seq: "03762",
		},
		{
			shape: "OAI identifier", in: "oai:arXiv.org:1706.03762",
			canonical: "1706.03762", style: StyleNew, year: 2017, month: 6, seq: "03762",
		},
		{
			shape: "DataCite DOI", in: "10.48550/arXiv.1706.03762",
			canonical: "1706.03762", style: StyleNew, year: 2017, month: 6, seq: "03762",
		},
	}

	for _, tt := range tests {
		t.Run(tt.shape, func(t *testing.T) {
			id, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if id.Canonical != tt.canonical {
				t.Errorf("Canonical = %q, want %q", id.Canonical, tt.canonical)
			}
			if id.Style != tt.style {
				t.Errorf("Style = %q, want %q", id.Style, tt.style)
			}
			if id.Archive != tt.archive {
				t.Errorf("Archive = %q, want %q", id.Archive, tt.archive)
			}
			if id.Class != tt.class {
				t.Errorf("Class = %q, want %q", id.Class, tt.class)
			}
			if id.Year != tt.year || id.Month != tt.month {
				t.Errorf("submitted = %04d-%02d, want %04d-%02d", id.Year, id.Month, tt.year, tt.month)
			}
			if id.Sequence != tt.seq {
				t.Errorf("Sequence = %q, want %q", id.Sequence, tt.seq)
			}
			if id.Version != tt.version {
				t.Errorf("Version = %d, want %d", id.Version, tt.version)
			}
			if id.Input != tt.in {
				t.Errorf("Input = %q, want %q", id.Input, tt.in)
			}
		})
	}
}

// TestWrappers covers the ways a reference gets dressed up on its way into the
// tool: every arXiv route, both DOI wrappers, and mixed case prefixes.
func TestWrappers(t *testing.T) {
	want := "1706.03762"
	for _, in := range []string{
		"https://arxiv.org/abs/1706.03762v7",
		"http://arxiv.org/abs/1706.03762",
		"https://arxiv.org/pdf/1706.03762",
		"https://arxiv.org/pdf/1706.03762v7.pdf",
		"https://arxiv.org/html/1706.03762v7",
		"https://arxiv.org/format/1706.03762",
		"https://arxiv.org/src/1706.03762",
		"https://arxiv.org/e-print/1706.03762",
		"https://arxiv.org/tb/1706.03762",
		"https://export.arxiv.org/abs/1706.03762",
		"https://www.arxiv.org/abs/1706.03762",
		"https://arxiv.org/abs/1706.03762?context=cs",
		"https://arxiv.org/abs/1706.03762#comments",
		"https://doi.org/10.48550/arXiv.1706.03762",
		"https://dx.doi.org/10.48550/arXiv.1706.03762",
		"doi:10.48550/arXiv.1706.03762",
		"10.48550/arxiv.1706.03762",
		"arxiv:1706.03762",
		"arXiv:1706.03762",
		"  1706.03762  ",
	} {
		id, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if id.Canonical != want {
			t.Errorf("Parse(%q).Canonical = %q, want %q", in, id.Canonical, want)
		}
	}
}

// TestOldStyleURL checks that the slash inside an old-style id survives a URL,
// which is the case that breaks a naive split on "/".
func TestOldStyleURL(t *testing.T) {
	for _, in := range []string{
		"https://arxiv.org/abs/hep-th/9711200",
		"https://arxiv.org/abs/hep-th/9711200v3",
		"oai:arXiv.org:hep-th/9711200",
		"https://doi.org/10.48550/arXiv.hep-th/9711200",
	} {
		id, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if id.Canonical != "hep-th/9711200" {
			t.Errorf("Parse(%q).Canonical = %q, want hep-th/9711200", in, id.Canonical)
		}
	}
}

// TestSubjectClassIsNotPartOfTheID pins arXiv's own canonicalization. Every
// expectation here was read off arxiv.org and the export API on 2026-08-13:
// /abs/math.GT/0309136 301s to /abs/math/0309136, and id_list=math.GT/0309136
// returns an empty feed while id_list=math/0309136 returns the paper.
func TestSubjectClassIsNotPartOfTheID(t *testing.T) {
	tests := []struct {
		in        string
		canonical string
		archive   string
		class     string
		category  string
	}{
		{"math.GT/0309136", "math/0309136", "math", "GT", "math.GT"},
		{"cond-mat.supr-con/9910001", "cond-mat/9910001", "cond-mat", "supr-con", "cond-mat.supr-con"},
		{"https://arxiv.org/abs/math.DG/0211159v1", "math/0211159", "math", "DG", "math.DG"},
	}
	for _, tt := range tests {
		id, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		if id.Canonical != tt.canonical {
			t.Errorf("Parse(%q).Canonical = %q, want %q", tt.in, id.Canonical, tt.canonical)
		}
		if id.Archive != tt.archive || id.Class != tt.class {
			t.Errorf("Parse(%q) archive/class = %q/%q, want %q/%q", tt.in, id.Archive, id.Class, tt.archive, tt.class)
		}
		cat, ok := id.Category()
		if !ok || cat != tt.category {
			t.Errorf("Parse(%q).Category() = %q, %v, want %q, true", tt.in, cat, ok, tt.category)
		}
	}
}

// TestArchiveCategory covers the three ways an old-style id does or does not
// name a category. The retired mappings were read off the primary_category the
// export API returns for a paper in each archive, checked on 2026-08-13.
func TestArchiveCategory(t *testing.T) {
	named := map[string]string{
		// archives that never had subject classes, so they are categories
		"hep-th/9711200":   "hep-th",
		"gr-qc/9310026":    "gr-qc",
		"quant-ph/9508027": "quant-ph",
		"math-ph/0203010":  "math-ph",
		// retired archives, folded into the modern taxonomy
		"alg-geom/9503001": "math.AG",
		"cmp-lg/9503001":   "cs.CL",
		"supr-con/9503001": "cond-mat.supr-con",
		"chao-dyn/9503001": "nlin.CD",
		"q-alg/9503001":    "math.QA",
		"dg-ga/9411001":    "math.DG",
		"funct-an/9411001": "math.FA",
		"solv-int/9411001": "nlin.SI",
		"mtrl-th/9411001":  "cond-mat.mtrl-sci",
		"chem-ph/9411001":  "physics.chem-ph",
		"acc-phys/9411001": "physics.acc-ph",
		"comp-gas/9411001": "nlin.CG",
		"patt-sol/9411001": "nlin.PS",
		"adap-org/9411001": "nlin.AO",
		"ao-sci/9503001":   "physics.ao-ph",
		"bayes-an/9506001": "physics.data-an",
		"atom-ph/9601001":  "physics.atom-ph",
		"plasm-ph/9505001": "physics.plasm-ph",
	}
	for in, want := range named {
		id, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		got, ok := id.Category()
		if !ok || got != want {
			t.Errorf("Parse(%q).Category() = %q, %v, want %q, true", in, got, ok, want)
		}
	}

	// Archives that do have subject classes say nothing when the reference did
	// not carry one, and a new-style id never says anything.
	for _, in := range []string{
		"math/0211159",
		"cond-mat/9910001",
		"astro-ph/0005004",
		"cs/0501001",
		"physics/0004057",
		"1706.03762",
	} {
		id, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got, ok := id.Category(); ok {
			t.Errorf("Parse(%q).Category() = %q, true, want no category", in, got)
		}
	}
}

// TestZeroPaddingIsSignificant is the one that stops a wrong-paper bug.
// arXiv widened the sequence from four digits to five in January 2015, so the
// widths are not interchangeable and a short reference must not be repadded.
func TestZeroPaddingIsSignificant(t *testing.T) {
	if _, err := Parse("2401.0001"); err == nil {
		t.Error("Parse(2401.0001) succeeded; a four digit sequence is not valid in 2024")
	}
	if _, err := Parse("0704.00001"); err == nil {
		t.Error("Parse(0704.00001) succeeded; a five digit sequence is not valid in 2007")
	}
	// The boundary months either side of the change, which are both real.
	if _, err := Parse("1412.6980"); err != nil {
		t.Errorf("Parse(1412.6980): %v", err)
	}
	if _, err := Parse("1501.00001"); err != nil {
		t.Errorf("Parse(1501.00001): %v", err)
	}
}

func TestRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"not an id",
		"1706",
		"1706.037",       // sequence too short
		"1706.0376234",   // sequence too long
		"1713.03762",     // month 13
		"hep-th/9713200", // month 13
		"hep-th/8811200", // before arXiv existed
		"hep-th/0811200", // after the old scheme was retired
		"HEP-TH/9711200", // archive is lowercase
		"1706.03762v",    // dangling version
		"https://example.com/abs/1706.03762",
		"https://arxiv.org/list/cs.CL/2401",
		"10.1038/nature14539", // a real DOI, but not an arXiv one
	} {
		if id, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %q, want an error", in, id.Canonical)
		}
	}
}

// TestDerived checks the values that come out of an id with no request. The
// DOI and the OAI identifier are formulas, and the DOI was checked live against
// arxiv.org and doi.org on 2026-08-13 for both styles.
func TestDerived(t *testing.T) {
	new7, err := Parse("arXiv:1706.03762v7")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := new7.DOI(), "10.48550/arXiv.1706.03762"; got != want {
		t.Errorf("DOI = %q, want %q", got, want)
	}
	if got, want := new7.OAI(), "oai:arXiv.org:1706.03762"; got != want {
		t.Errorf("OAI = %q, want %q", got, want)
	}
	if got, want := new7.AbsURL(), "https://arxiv.org/abs/1706.03762v7"; got != want {
		t.Errorf("AbsURL = %q, want %q", got, want)
	}
	if got, want := new7.PDFURL(), "https://arxiv.org/pdf/1706.03762v7"; got != want {
		t.Errorf("PDFURL = %q, want %q", got, want)
	}
	if got, want := new7.URI(), "ax://paper/1706.03762#v7"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := new7.Cite(), "arXiv:1706.03762v7"; got != want {
		t.Errorf("Cite = %q, want %q", got, want)
	}
	if got, want := new7.Submitted(), "2017-06"; got != want {
		t.Errorf("Submitted = %q, want %q", got, want)
	}
	if _, ok := new7.Category(); ok {
		t.Error("Category() said a new style id names its category; it does not")
	}

	old, err := Parse("hep-th/9711200")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := old.DOI(), "10.48550/arXiv.hep-th/9711200"; got != want {
		t.Errorf("DOI = %q, want %q", got, want)
	}
	if got, want := old.URI(), "ax://paper/hep-th/9711200"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := old.Submitted(), "1997-11"; got != want {
		t.Errorf("Submitted = %q, want %q", got, want)
	}
	cat, ok := old.Category()
	if !ok || cat != "hep-th" {
		t.Errorf("Category() = %q, %v, want hep-th, true", cat, ok)
	}
}

// TestRoundTrip runs real ids from both eras through the URI space, the DOI,
// the citation form and every URL route, and checks each one parses back to
// where it started.
func TestRoundTrip(t *testing.T) {
	ids := []string{
		"1706.03762",     // Attention Is All You Need
		"1412.6980",      // Adam
		"2401.00001",     // first of January 2024
		"0704.0001",      // first of the new scheme
		"1207.7214",      // ATLAS Higgs
		"hep-th/9711200", // Maldacena
		"cond-mat/9910001",
		"astro-ph/0005004",
		"gr-qc/9310026",
	}
	for _, want := range ids {
		id, err := Parse(want)
		if err != nil {
			t.Fatalf("Parse(%q): %v", want, err)
		}
		forms := []string{
			id.Canonical,
			id.Cite(),
			id.DOI(),
			id.OAI(),
			id.AbsURL(),
			id.PDFURL(),
			id.URI(),
			"https://doi.org/" + id.DOI(),
			"https://arxiv.org/html/" + id.Canonical,
		}
		if got := id.URI(); got != "ax://paper/"+want {
			t.Errorf("URI = %q, want ax://paper/%s", got, want)
		}
		for _, form := range forms {
			back, err := Parse(form)
			if err != nil {
				t.Errorf("Parse(%q): %v", form, err)
				continue
			}
			if back.Canonical != want {
				t.Errorf("Parse(%q).Canonical = %q, want %q", form, back.Canonical, want)
			}
		}
	}
}

// TestURIParsesBack is the round trip for this tool's own node names. Every
// record prints one, and a record that names something the same tool cannot
// read back is a dead end.
func TestURIParsesBack(t *testing.T) {
	cases := []struct {
		uri       string
		canonical string
		version   int
	}{
		{"ax://paper/1706.03762", "1706.03762", 0},
		{"ax://paper/1706.03762#v7", "1706.03762", 7},
		{"ax://paper/hep-th/9711200", "hep-th/9711200", 0},
		{"ax://paper/hep-th/9711200#v3", "hep-th/9711200", 3},
		{"ax://paper/cond-mat.supr-con/9910001", "cond-mat/9910001", 0},
	}
	for _, tc := range cases {
		id, err := Parse(tc.uri)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.uri, err)
			continue
		}
		if id.Canonical != tc.canonical {
			t.Errorf("Parse(%q).Canonical = %q, want %q", tc.uri, id.Canonical, tc.canonical)
		}
		if id.Version != tc.version {
			t.Errorf("Parse(%q).Version = %d, want %d", tc.uri, id.Version, tc.version)
		}
	}

	// The other node kinds are not papers, and reading one as a paper would
	// invent a paper out of an author or a category.
	for _, bad := range []string{
		"ax://name/john-baez",
		"ax://category/cs.CL",
		"ax://author/baez_j_1",
		"ax://paper/",
		"ax://paper/not-an-id",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted something that is not a paper", bad)
		}
	}
}

// TestVersionIsNotPartOfTheID pins the rule that the canonical id never carries
// a version, so two references to different versions of one paper land on one
// node.
func TestVersionIsNotPartOfTheID(t *testing.T) {
	v1, err := Parse("1706.03762v1")
	if err != nil {
		t.Fatal(err)
	}
	v7, err := Parse("https://arxiv.org/abs/1706.03762v7")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Canonical != v7.Canonical {
		t.Errorf("v1 and v7 gave different ids: %q and %q", v1.Canonical, v7.Canonical)
	}
	if v1.Version != 1 || v7.Version != 7 {
		t.Errorf("versions = %d and %d, want 1 and 7", v1.Version, v7.Version)
	}
	if v1.Versioned() != "1706.03762v1" {
		t.Errorf("Versioned() = %q, want 1706.03762v1", v1.Versioned())
	}
	bare, err := Parse("1706.03762")
	if err != nil {
		t.Fatal(err)
	}
	if bare.Version != 0 {
		t.Errorf("Version = %d for a reference that named none, want 0", bare.Version)
	}
	if bare.Versioned() != bare.Canonical {
		t.Errorf("Versioned() = %q, want the bare id %q", bare.Versioned(), bare.Canonical)
	}
}

// TestSortKey checks that old-style ids sort against new-style ones by date,
// which sorting the canonical strings would get wrong.
func TestSortKey(t *testing.T) {
	in := []string{"1706.03762", "hep-th/9711200", "2401.00001", "0704.0001", "gr-qc/9310026"}
	want := []string{"gr-qc/9310026", "hep-th/9711200", "0704.0001", "1706.03762", "2401.00001"}

	got := make([]ID, 0, len(in))
	for _, s := range in {
		id, err := Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].SortKey() < got[j].SortKey() })

	for i := range want {
		if got[i].Canonical != want[i] {
			t.Errorf("position %d = %q, want %q", i, got[i].Canonical, want[i])
		}
	}
}

func TestValid(t *testing.T) {
	if !Valid("1706.03762") {
		t.Error("Valid(1706.03762) = false")
	}
	if Valid("nonsense") {
		t.Error("Valid(nonsense) = true")
	}
}
