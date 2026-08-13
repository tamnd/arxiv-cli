package arxiv

import (
	"strings"
	"testing"
)

// The two tables under embedded/ are the real pages, saved 2026-08-13, and they
// are what these tests read. Testing the bundled copy and shipping a different
// one would leave the thing the binary actually uses unchecked.

func embeddedTaxonomy(t *testing.T) []Category {
	t.Helper()
	body, err := embedded.ReadFile("embedded/taxonomy.html")
	if err != nil {
		t.Fatal(err)
	}
	cats, err := parseTaxonomy(body)
	if err != nil {
		t.Fatal(err)
	}
	return cats
}

func embeddedSets(t *testing.T) ([]Set, int) {
	t.Helper()
	body, err := embedded.ReadFile("embedded/sets.xml")
	if err != nil {
		t.Fatal(err)
	}
	sets, raw, err := parseSets(body)
	if err != nil {
		t.Fatal(err)
	}
	return sets, raw
}

func TestParseTaxonomy(t *testing.T) {
	cats := embeddedTaxonomy(t)
	if len(cats) != 155 {
		t.Errorf("%d categories, want 155", len(cats))
	}
	groups := map[string]int{}
	for _, c := range cats {
		groups[c.Group]++
		if c.Code == "" || c.Name == "" || c.Group == "" {
			t.Errorf("%+v is missing a code, a name or a group", c)
		}
		if strings.Contains(c.Name, "(") || strings.Contains(c.Name, ")") {
			t.Errorf("%s kept the brackets in its name: %q", c.Code, c.Name)
		}
	}
	if len(groups) != 8 {
		t.Errorf("%d groups, want 8: %v", len(groups), groups)
	}

	byCode := map[string]Category{}
	for _, c := range cats {
		byCode[c.Code] = c
	}
	for _, tc := range []struct {
		code, name, group, archive string
		isArchive                  bool
	}{
		{"cs.CL", "Computation and Language", "Computer Science", "cs", false},
		{"hep-th", "High Energy Physics - Theory", "Physics", "hep-th", true},
		{"quant-ph", "Quantum Physics", "Physics", "quant-ph", true},
		{"cond-mat.supr-con", "Superconductivity", "Physics", "cond-mat", false},
		{"econ.EM", "Econometrics", "Economics", "econ", false},
		{"eess.SP", "Signal Processing", "Electrical Engineering and Systems Science", "eess", false},
	} {
		got, ok := byCode[tc.code]
		if !ok {
			t.Errorf("%s is not in the table", tc.code)
			continue
		}
		if got.Name != tc.name || got.Group != tc.group || got.Archive != tc.archive || got.IsArchive != tc.isArchive {
			t.Errorf("%s = %+v, want name %q group %q archive %q is_archive %v",
				tc.code, got, tc.name, tc.group, tc.archive, tc.isArchive)
		}
		if got.Description == "" {
			t.Errorf("%s has no description, which is the reason to read this page at all", tc.code)
		}
	}

	// The page's own legend row is an h4 with "Category Name" in it, and it is
	// not a category.
	for _, c := range cats {
		if strings.Contains(c.Code, " ") {
			t.Errorf("%q parsed as a code and it has a space in it", c.Code)
		}
	}
}

// TestArchivesAreTheNineUnsplitOnes pins the nine codes that are an archive and
// a category at the same time, because they are the ones every join and every
// set spec has to treat differently.
func TestArchivesAreTheNineUnsplitOnes(t *testing.T) {
	var archives []string
	for _, c := range embeddedTaxonomy(t) {
		if c.IsArchive {
			archives = append(archives, c.Code)
		}
	}
	want := []string{"gr-qc", "hep-ex", "hep-lat", "hep-ph", "hep-th", "math-ph", "nucl-ex", "nucl-th", "quant-ph"}
	if len(archives) != len(want) {
		t.Fatalf("%d unsplit archives, want %d: %v", len(archives), len(want), archives)
	}
	got := strings.Join(archives, " ")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s is not marked as an unsplit archive", w)
		}
	}
}

func TestParseSets(t *testing.T) {
	sets, raw := embeddedSets(t)
	if raw != 183 {
		t.Errorf("the response carried %d sets, want 183", raw)
	}
	// Nine specs are listed twice, once as an archive and once as a category.
	if len(sets) != 174 {
		t.Errorf("%d distinct sets, want 174", len(sets))
	}
	seen := map[string]bool{}
	for _, s := range sets {
		if seen[s.SetSpec] {
			t.Errorf("%s came back twice", s.SetSpec)
		}
		seen[s.SetSpec] = true
		if s.Name == "" {
			t.Errorf("%s has no name", s.SetSpec)
		}
	}
	if !seen["cs:cs:CL"] || !seen["physics:hep-th"] || !seen["physics:cond-mat:supr-con"] {
		t.Error("a set spec that is known to exist is missing")
	}
}

// TestJoinSets is the one that matters: the set spec is a join and not a string
// rewrite, and every category has to find its set.
func TestJoinSets(t *testing.T) {
	cats := embeddedTaxonomy(t)
	sets, _ := embeddedSets(t)
	joinSets(cats, sets)

	specs := map[string]string{}
	for _, c := range cats {
		if c.SetSpec == "" {
			t.Errorf("%s found no set", c.Code)
			continue
		}
		specs[c.Code] = c.SetSpec
	}
	for code, want := range map[string]string{
		"cs.CL":             "cs:cs:CL",
		"hep-th":            "physics:hep-th",
		"quant-ph":          "physics:quant-ph",
		"nlin.AO":           "physics:nlin:AO",
		"econ.EM":           "econ:econ:EM",
		"astro-ph.HE":       "physics:astro-ph:HE",
		"math.NT":           "math:math:NT",
		"stat.ML":           "stat:stat:ML",
		"cond-mat.supr-con": "physics:cond-mat:supr-con",
	} {
		if got := specs[code]; got != want {
			t.Errorf("%s is set %q, want %q", code, got, want)
		}
	}

	// A container set harvests no single category, and there are eleven of them
	// plus the eight group sets.
	var containers, leaves int
	for _, s := range sets {
		if s.Category == "" {
			containers++
			continue
		}
		leaves++
	}
	if leaves != 155 {
		t.Errorf("%d sets name a category, want 155", leaves)
	}
	if containers != 19 {
		t.Errorf("%d sets are containers, want 19: 8 groups and 11 archives", containers)
	}
}

func TestCheckCategories(t *testing.T) {
	for _, code := range []string{"cs.CL", "hep-th", "cs", "physics", "cond-mat.supr-con"} {
		if err := checkCategories([]string{code}); err != nil {
			t.Errorf("%s was refused: %v", code, err)
		}
	}
	for _, tc := range []struct{ code, want string }{
		{"cs.NOPE", "cs.NE"},
		{"cs.lg", "cs.LG"},
		{"nope.NOPE", ""},
	} {
		err := checkCategories([]string{tc.code})
		if err == nil {
			t.Errorf("%s was accepted", tc.code)
			continue
		}
		if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s says %q, want a suggestion of %s", tc.code, err, tc.want)
		}
		if tc.want == "" && strings.Contains(err.Error(), "closest") {
			t.Errorf("%s got a suggestion it should not have: %q", tc.code, err)
		}
	}
}

// TestBadCategoryStopsBeforeTheRequest is the point of the check: arXiv answers
// a wrong code with HTTP 200 and zero results, so the refusal has to happen
// here or not at all.
func TestBadCategoryStopsBeforeTheRequest(t *testing.T) {
	_, err := buildSearch(SearchOptions{Categories: []string{"cs.NOPE"}})
	if err == nil {
		t.Fatal("a query with a category that does not exist was built anyway")
	}
	if !strings.Contains(err.Error(), "cs.NOPE") {
		t.Errorf("the message does not name the code: %v", err)
	}
}

func TestFilterCategories(t *testing.T) {
	cats := embeddedTaxonomy(t)

	if got := filterCategories(cats, "physics", ""); len(got) < 50 {
		t.Errorf("the physics group has %d categories, which is too few", len(got))
	}
	// A group can be named by its archive code as well as its spelled out name.
	byCode := filterCategories(cats, "eess", "")
	byName := filterCategories(cats, "Electrical Engineering", "")
	if len(byCode) != len(byName) || len(byCode) == 0 {
		t.Errorf("eess matched %d and the spelled out name matched %d", len(byCode), len(byName))
	}

	// The search reads descriptions, which is the whole reason it exists.
	got := filterCategories(cats, "", "game theory")
	var codes []string
	for _, c := range got {
		codes = append(codes, c.Code)
	}
	joined := strings.Join(codes, " ")
	for _, want := range []string{"cs.GT", "econ.TH", "math.OC"} {
		if !strings.Contains(joined, want) {
			t.Errorf("a search for game theory did not find %s, it found %v", want, codes)
		}
	}
}

func TestSnapshotKnowsEveryCode(t *testing.T) {
	known := knownCodes()
	if len(known) == 0 {
		t.Fatal("the bundled taxonomy did not parse, so every category check would be skipped")
	}
	for _, c := range embeddedTaxonomy(t) {
		if !known[c.Code] {
			t.Errorf("%s is in the table and not in the check", c.Code)
		}
	}
	// Archive codes are legal too, because --cat cs is a documented query.
	for _, code := range []string{"cs", "math", "physics", "astro-ph", "cond-mat", "nlin"} {
		if !known[code] {
			t.Errorf("%s is an archive and the check does not know it", code)
		}
	}
	if len(snapshotCategories()) != 155 {
		t.Errorf("the snapshot holds %d categories, want 155", len(snapshotCategories()))
	}
}
