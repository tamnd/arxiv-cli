package graph

import (
	"strings"
	"testing"
)

// The old style slash stays in the path, because escaping it gives a key nobody
// can read and two keys for one paper the first time somebody forgets to escape.
func TestPaper(t *testing.T) {
	cases := map[string]string{
		"1706.03762":      "ax://paper/1706.03762",
		"hep-th/9711200":  "ax://paper/hep-th/9711200",
		"math.GT/0309136": "ax://paper/math.GT/0309136",
	}
	for in, want := range cases {
		if got := Paper(in); got != want {
			t.Errorf("Paper(%q) = %q, want %q", in, got, want)
		}
	}
}

// A version is a fragment on the paper and not a node of its own, so it is still
// in the paper space and still parses as one.
func TestVersionIsAFragment(t *testing.T) {
	got := Version("1706.03762", 7)
	if got != "ax://paper/1706.03762#v7" {
		t.Fatalf("Version = %q", got)
	}
	kind, ok := KindOf(got)
	if !ok || kind != KindPaper {
		t.Errorf("KindOf(%q) = %q, %v, want paper", got, kind, ok)
	}
	if !IsVersion(got) {
		t.Error("a version fragment is not recognised as one")
	}
	if IsVersion(Paper("1706.03762")) {
		t.Error("a bare paper was taken for a version")
	}
	if IsVersion(File("1706.03762", 7, "pdf")) {
		t.Error("a file was taken for a version")
	}
}

// This is the rule the whole package exists for. A name and a person are
// different claims, so they are different spaces and one never turns into the
// other by accident.
func TestANameIsNotAPerson(t *testing.T) {
	name := Name("John C. Baez")
	author := Author("baez_j_1")
	if name == author {
		t.Fatal("a name and an author landed on the same node")
	}
	if !strings.HasPrefix(name, "ax://name/") {
		t.Errorf("name uri = %q", name)
	}
	if !strings.HasPrefix(author, "ax://author/") {
		t.Errorf("author uri = %q", author)
	}
}

// Normalising is lossy and meant to be: two spellings of one name land
// together. It is also why two people with one name land together, which is the
// reason a name node is never treated as a person.
func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Aidan N. Gomez":     "aidan-n-gomez",
		"aidan n gomez":      "aidan-n-gomez",
		"  Ashish Vaswani  ": "ashish-vaswani",
		"Paul Erdős":         "paul-erdos",
		"Paul Erdos":         "paul-erdos",
		"O'Brien, Seán":      "o-brien-sean",
		"Łukasz Kaiser":      "łukasz-kaiser",
		"":                   "",
	}
	for in, want := range cases {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two authors typing the same journal issue by hand is the case this has to
// survive, and it is admitted as approximate everywhere it is used.
func TestNormalizeJournal(t *testing.T) {
	same := []string{
		"Nature 521, 436-444 (2015)",
		"Nature 521:436-444, 2015",
		"nature 521 436 444 2015",
	}
	first := NormalizeJournal(same[0])
	if first == "" {
		t.Fatal("the first form normalised to nothing")
	}
	for _, s := range same[1:] {
		if got := NormalizeJournal(s); got != first {
			t.Errorf("NormalizeJournal(%q) = %q, want %q", s, got, first)
		}
	}
	if NormalizeJournal("  ") != "" {
		t.Error("whitespace normalised to something")
	}
}

// A trailing year in parentheses is only dropped when the same year is already
// in the string. A reference whose only year is the one in brackets keeps it,
// because dropping it would merge two years of one journal.
func TestNormalizeJournalKeepsTheOnlyYear(t *testing.T) {
	a := NormalizeJournal("J. Phys. A 42 (2009)")
	b := NormalizeJournal("J. Phys. A 42 (2010)")
	if a == b {
		t.Fatalf("two years of one journal normalised to the same string, %q", a)
	}
	if !strings.Contains(a, "2009") {
		t.Errorf("the only year was dropped: %q", a)
	}
}

// A journal node is a hash, so nobody reads it as a title and nobody is tempted
// to parse it back.
func TestJournalIsHashed(t *testing.T) {
	got := Journal("Nature 521, 436-444 (2015)")
	rest, ok := strings.CutPrefix(got, "ax://journal/")
	if !ok {
		t.Fatalf("Journal = %q", got)
	}
	if len(rest) != 64 {
		t.Errorf("the hash is %d characters, want a sha256", len(rest))
	}
	if Journal("") != "" {
		t.Error("an empty reference produced a node")
	}
}

// Two URLs that differ have no useful join, only exact identity, so the address
// is hashed. The small amount of tidying done first is the part two spellings of
// one address deserve.
func TestExternalHashesTheURL(t *testing.T) {
	same := []string{
		"https://example.org/a-post",
		"HTTPS://Example.ORG/a-post",
		"https://example.org:443/a-post",
	}
	first := External(same[0])
	for _, s := range same[1:] {
		if got := External(s); got != first {
			t.Errorf("External(%q) = %q, want %q", s, got, first)
		}
	}
	// A query string is a different page and stays a different node.
	if External("https://example.org/a-post?page=2") == first {
		t.Error("a query string was dropped, merging two pages")
	}
	if External("") != "" {
		t.Error("an empty url produced a node")
	}
}

// The licenses that come up get a readable name, because a query for cc-by-4.0
// should not need a lookup table.
func TestLicenseSlug(t *testing.T) {
	cases := map[string]string{
		"http://creativecommons.org/licenses/by/4.0/":         "cc-by-4.0",
		"https://creativecommons.org/licenses/by-nc-sa/4.0/":  "cc-by-nc-sa-4.0",
		"http://creativecommons.org/publicdomain/zero/1.0/":   "cc-zero-1.0",
		"http://arxiv.org/licenses/nonexclusive-distrib/1.0/": "arxiv-nonexclusive-distrib-1.0",
		"http://example.org/some/other/license":               "example-org-some-other-license",
		"":                                                    "",
	}
	for in, want := range cases {
		if got := LicenseSlug(in); got != want {
			t.Errorf("LicenseSlug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := License("http://creativecommons.org/licenses/by/4.0/"); got != "ax://license/cc-by-4.0" {
		t.Errorf("License = %q", got)
	}
}

// A DOI is case insensitive by specification and publishers do not agree on
// which case to print, so the node folds it and strips whichever wrapper the
// reference came in.
func TestDOI(t *testing.T) {
	want := "ax://doi/10.1038/nature14539"
	for _, in := range []string{
		"10.1038/nature14539",
		"10.1038/Nature14539",
		"doi:10.1038/nature14539",
		"https://doi.org/10.1038/nature14539",
	} {
		if got := DOI(in); got != want {
			t.Errorf("DOI(%q) = %q, want %q", in, got, want)
		}
	}
	if DOI("") != "" {
		t.Error("an empty doi produced a node")
	}
}

// A file belongs to a version, so the version is in the node and v1 and v7 are
// two nodes rather than one.
func TestFile(t *testing.T) {
	if got := File("1706.03762", 7, "pdf"); got != "ax://file/1706.03762#v7.pdf" {
		t.Errorf("File = %q", got)
	}
	if File("1706.03762", 1, "pdf") == File("1706.03762", 7, "pdf") {
		t.Error("two versions of one file landed on the same node")
	}
	if got := File("1706.03762", 0, "source"); got != "ax://file/1706.03762#source" {
		t.Errorf("File with no version = %q", got)
	}
}

func TestKindOf(t *testing.T) {
	cases := map[string]string{
		"ax://paper/1706.03762":          KindPaper,
		"ax://paper/hep-th/9711200":      KindPaper,
		"ax://paper/1706.03762#v7":       KindPaper,
		"ax://author/baez_j_1":           KindAuthor,
		"ax://name/john-baez":            KindName,
		"ax://orcid/0000-0002-3300-2109": KindORCID,
		"ax://category/cs.CL":            KindCategory,
		"ax://archive/hep-th":            KindArchive,
		"ax://group/computer-science":    KindGroup,
		"ax://set/cs":                    KindSet,
		"ax://doi/10.1038/nature14539":   KindDOI,
		"ax://license/cc-by-4.0":         KindLicense,
		"ax://file/1706.03762#v7.pdf":    KindFile,
		"ax://trackback/1845295":         KindTrackback,
	}
	for in, want := range cases {
		got, ok := KindOf(in)
		if !ok {
			t.Errorf("KindOf(%q) said it is not a uri", in)
			continue
		}
		if got != want {
			t.Errorf("KindOf(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{"", "1706.03762", "https://arxiv.org/abs/1706.03762", "ax://", "ax://paper", "ax://paper/", "ax://nonsense/x"}
	for _, in := range bad {
		if got, ok := KindOf(in); ok {
			t.Errorf("KindOf(%q) = %q, want not a uri", in, got)
		}
	}
}

// hep-th is a category and an archive both, and it gets a node in each space.
// This looks odd until you try to remove it and find half the physics archives
// fall out of the tree.
func TestAnArchiveIsAlsoACategory(t *testing.T) {
	if Category("hep-th") == Archive("hep-th") {
		t.Fatal("the two nodes are the same, so the tree has nowhere to hang")
	}
	if got, _ := KindOf(Category("hep-th")); got != KindCategory {
		t.Errorf("the category node is a %s", got)
	}
	if got, _ := KindOf(Archive("hep-th")); got != KindArchive {
		t.Errorf("the archive node is a %s", got)
	}
}
