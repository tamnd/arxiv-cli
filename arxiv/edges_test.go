package arxiv

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// The extractors are tested against the same saved surfaces the record tests
// use, because a claim is only worth checking against what arXiv actually
// published. A hand written record would prove the extractor handles the shape
// its author had in mind and nothing else.

// edgesOf indexes a claim set by predicate, which is how most of these tests
// want to look at it.
func edgesOf(edges []graph.Edge) map[string][]graph.Edge {
	out := map[string][]graph.Edge{}
	for _, e := range edges {
		out[e.Predicate] = append(out[e.Predicate], e)
	}
	return out
}

// one is the single claim with this predicate, and a failure otherwise.
func one(t *testing.T, edges []graph.Edge, predicate string) graph.Edge {
	t.Helper()
	var found []graph.Edge
	for _, e := range edges {
		if e.Predicate == predicate {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: got %d claims, want one", predicate, len(found))
	}
	return found[0]
}

// categoryFixture builds a category the way the client builds one: the taxonomy
// page for the tree and the OAI set list for the set spec, with the surface each
// field came from stamped on it.
func categoryFixture(t *testing.T, code string) Category {
	t.Helper()
	cats := embeddedTaxonomy(t)
	sets, _ := embeddedSets(t)
	joinSets(cats, sets)
	for _, c := range cats {
		if c.Code != code {
			continue
		}
		c.addSurface(SurfaceTaxonomy, taxonomyURL)
		if c.SetSpec != "" {
			c.addSurface(SurfaceOAI, setsURL)
		}
		return c
	}
	t.Fatalf("%s is not in the taxonomy", code)
	return Category{}
}

// personFixture is the identifier page as a person record, built the way
// AuthorByID builds one.
func personFixture(t *testing.T) Person {
	t.Helper()
	page := authorFixture(t)
	u := authorURL("baez_j_1")
	p := Person{
		Envelope:   Envelope{Kind: "author", RetrievedAt: testTime},
		Name:       page.Name,
		Mode:       "identifier page",
		Identified: true,
		ArxivID:    "baez_j_1",
		ORCID:      page.ORCID,
		URI:        AuthorURI("baez_j_1"),
	}
	p.addSurface(SurfaceAuthorID, u)
	for _, row := range page.Rows {
		p.Papers = append(p.Papers, listToPaper(row, SurfaceAuthorID, u, testTime))
	}
	return p
}

// fullTextFixture is the rendering with a real bibliography: 46 entries, 21 of
// them linked to an abs page.
func fullTextFixture(t *testing.T) FullText {
	t.Helper()
	doc := renderingFixture(t, "html_2601.00086v3.html")
	p := Paper{ID: "2601.00086", Version: 3, HasHTML: true}
	return fullTextFrom(doc, p, "https://arxiv.org/html/2601.00086v3", testTime)
}

func TestEdgesOfPaperAtDepthFull(t *testing.T) {
	edges := EdgesOfPaper(fullRecord(t))
	by := edgesOf(edges)

	for _, tc := range []struct {
		predicate string
		want      int
	}{
		{graph.Authored, 8},
		{graph.PrimaryCategory, 1},
		{graph.InCategory, 2},
		{graph.CrossListed, 1},
		{graph.HasVersion, 7},
		{graph.Supersedes, 6},
		{graph.HasDOI, 1},
		{graph.LicensedUnder, 1},
		{graph.SubmittedBy, 1},
		{graph.HasFile, 3},
	} {
		if got := len(by[tc.predicate]); got != tc.want {
			t.Errorf("%s: got %d claims, want %d", tc.predicate, got, tc.want)
		}
	}

	// 1706.03762 was never published in a journal and has no publisher DOI, so
	// the two claims that would need one are absent rather than empty.
	if len(by[graph.PublishedIn]) != 0 {
		t.Errorf("published_in: got %d claims for a paper with no journal reference", len(by[graph.PublishedIn]))
	}
	// cites lives on the rendering, and a metadata read has none of it.
	if len(by[graph.Cites]) != 0 {
		t.Errorf("cites: got %d claims from a metadata read", len(by[graph.Cites]))
	}

	// The submitter is one of the eight authors and is not the first, which is
	// why it is its own claim rather than a position on authored.
	by8 := one(t, edges, graph.SubmittedBy)
	if by8.From != graph.Name("Llion Jones") {
		t.Errorf("submitted_by: got %q", by8.From)
	}
	if by8.Surface != SurfaceOAI {
		t.Errorf("submitted_by came from %q, and only arXivRaw and the abstract page carry a submitter", by8.Surface)
	}

	doi := one(t, edges, graph.HasDOI)
	if doi.To != graph.DOI("10.48550/arXiv.1706.03762") {
		t.Errorf("has_doi: got %q", doi.To)
	}
	// The note says how the claim was made, because arXiv's own DOI is a
	// formula on the id rather than a field somebody read.
	if !strings.Contains(doi.Note, "computed from the id") {
		t.Errorf("has_doi note: got %q", doi.Note)
	}

	primary := one(t, edges, graph.PrimaryCategory)
	if primary.To != graph.Category("cs.CL") || primary.Note != "Computation and Language" {
		t.Errorf("primary_category: got %q note %q", primary.To, primary.Note)
	}
}

// The version history is the one claim set derived rather than read. supersedes
// is the order of the list and costs no request of its own, so a seven version
// paper carries six of them and the first version supersedes nothing.
func TestVersionsAndSupersedes(t *testing.T) {
	edges := EdgesOfPaper(fullRecord(t))
	by := edgesOf(edges)

	for i, e := range by[graph.HasVersion] {
		if e.Position != i+1 {
			t.Errorf("has_version %d: position %d", i, e.Position)
		}
		if e.To != graph.Version("1706.03762", i+1) {
			t.Errorf("has_version %d: got %q", i, e.To)
		}
		if e.Note == "" {
			t.Errorf("has_version %d has no date on it", i)
		}
	}
	for _, e := range by[graph.Supersedes] {
		if e.From == graph.Version("1706.03762", 1) {
			t.Error("v1 supersedes something, and there was nothing before it")
		}
	}
	if got := by[graph.Supersedes][0]; got.From != graph.Version("1706.03762", 2) ||
		got.To != graph.Version("1706.03762", 1) {
		t.Errorf("the first supersedes claim is %q to %q", got.From, got.To)
	}
}

// authored runs from the name to the paper, not the other way round. The query
// worth answering is everything a name touched, and a claim indexed from the
// name answers it without a scan.
func TestAuthoredRunsFromTheName(t *testing.T) {
	edges := EdgesOfPaper(fullRecord(t))
	paper := graph.Paper("1706.03762")

	var authored []graph.Edge
	for _, e := range edges {
		if e.Predicate == graph.Authored {
			authored = append(authored, e)
		}
	}
	if len(authored) != 8 {
		t.Fatalf("authored: got %d claims, want 8", len(authored))
	}
	for i, e := range authored {
		if e.To != paper {
			t.Errorf("authored %d points at %q, want the paper", i, e.To)
		}
		if kind, _ := graph.KindOf(e.From); kind != graph.KindName {
			t.Errorf("authored %d runs from a %s, want a name", i, kind)
		}
		// The order is significant in most fields and load bearing in physics,
		// so it is on the claim rather than left to the row order of whatever
		// stored it.
		if e.Position != i+1 {
			t.Errorf("authored %d: position %d", i, e.Position)
		}
		// The note keeps the spelling the slug normalised away.
		if e.Note == "" || graph.Name(e.Note) != e.From {
			t.Errorf("authored %d: note %q does not spell out %q", i, e.Note, e.From)
		}
	}
	if authored[0].From != graph.Name("Ashish Vaswani") {
		t.Errorf("the first author is %q", authored[0].From)
	}
	if authored[7].Note != "Illia Polosukhin" {
		t.Errorf("the eighth author is %q", authored[7].Note)
	}
}

// linked_by points inward: the external page is the subject, because a
// trackback is somebody else's page linking here. The other way round would say
// the paper cites the blog, which is backwards and would corrupt any merge with
// a real citation set.
func TestLinkedByPointsInward(t *testing.T) {
	tbs := trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200")
	if len(tbs) == 0 {
		t.Fatal("the fixture has no trackbacks, so this test is checking nothing")
	}
	paper := graph.Paper("hep-th/9711200")

	for _, tb := range tbs {
		edges := EdgesOfTrackback(tb)
		if len(edges) != 1 {
			t.Fatalf("%q: got %d claims, want one", tb.Title, len(edges))
		}
		e := edges[0]
		if e.To != paper {
			t.Errorf("%q: linked_by points at %q, want the paper", tb.Title, e.To)
		}
		if kind, _ := graph.KindOf(e.From); kind != graph.KindExternal {
			t.Errorf("%q: linked_by runs from a %s, want an external page", tb.Title, kind)
		}
		if e.Surface != SurfaceTrackback {
			t.Errorf("%q: surface %q", tb.Title, e.Surface)
		}
	}
}

// An unresolved ping names arXiv's redirect, because that is the only address
// arXiv publishes, and the note says so rather than letting the redirect pass
// for the page itself.
func TestTrackbackSaysWhenTheURLIsARedirect(t *testing.T) {
	tb := trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200")[0]
	if tb.TargetURL != "" {
		t.Fatal("the fixture is resolved, so the unresolved path is untested")
	}
	e := EdgesOfTrackback(tb)[0]
	if !strings.Contains(e.Note, "unresolved") {
		t.Errorf("note: got %q, want it to admit the URL is arXiv's redirect", e.Note)
	}

	tb.TargetURL = "https://example.org/a-post"
	e = EdgesOfTrackback(tb)[0]
	if e.From != graph.External("https://example.org/a-post") {
		t.Errorf("a resolved ping names %q, want the page itself", e.From)
	}
	if strings.Contains(e.Note, "unresolved") {
		t.Errorf("a resolved ping still says unresolved: %q", e.Note)
	}
}

// The feed is the only surface that says which category a paper was announced
// in and whether it was new, a cross list or a replacement, and that is a
// different fact from the categories on the paper.
func TestEdgesOfAnnouncement(t *testing.T) {
	feed := feedFixture(t)
	a := itemToAnnouncement(feed.Channel.Items[0], "cs.CL", feedURL("cs.CL"), testTime, testTime)
	edges := EdgesOfAnnouncement(a)

	announced := one(t, edges, graph.AnnouncedAs)
	if announced.To != graph.Category("cs.CL") {
		t.Errorf("announced_as: got %q", announced.To)
	}
	if announced.Note != a.AnnounceType || announced.Note == "" {
		t.Errorf("announced_as note: got %q, want the announce type", announced.Note)
	}
	for _, e := range edges {
		if e.Surface != SurfaceRSS {
			t.Errorf("%s came from %q, and this record only read the feed", e.Predicate, e.Surface)
		}
	}
	by := edgesOf(edges)
	if len(by[graph.Authored]) != len(a.Authors) {
		t.Errorf("authored: got %d claims for %d authors", len(by[graph.Authored]), len(a.Authors))
	}
	// The first category on a feed item is the primary one, and the rest are
	// cross lists.
	if len(by[graph.PrimaryCategory]) != 1 {
		t.Errorf("primary_category: got %d claims", len(by[graph.PrimaryCategory]))
	}
	if len(by[graph.InCategory]) != len(a.Categories) {
		t.Errorf("in_category: got %d claims for %d categories", len(by[graph.InCategory]), len(a.Categories))
	}
}

// The identifier page is the only surface that joins a name to a person, and
// that join is the one claim on arXiv a string match cannot make.
func TestEdgesOfPerson(t *testing.T) {
	p := personFixture(t)
	edges := EdgesOfPerson(p)
	author := graph.Author("baez_j_1")

	id := one(t, edges, graph.IdentifiedAs)
	if id.From != graph.Name("John Baez") || id.To != author {
		t.Errorf("identified_as: got %q to %q", id.From, id.To)
	}
	orcid := one(t, edges, graph.HasORCID)
	if orcid.To != graph.ORCID("0000-0002-0609-9836") {
		t.Errorf("has_orcid: got %q", orcid.To)
	}
	by := edgesOf(edges)
	if len(by[graph.Authored]) != 125 {
		t.Errorf("authored: got %d claims for a page listing 125 papers", len(by[graph.Authored]))
	}
	for _, e := range by[graph.Authored] {
		if e.From != author {
			t.Errorf("authored runs from %q, and this page knows the person rather than the string", e.From)
		}
	}
}

// A name search matched strings and nothing else. The claims belong to the
// papers it found, and the search itself has none of its own to make.
func TestANameSearchClaimsNothing(t *testing.T) {
	p := Person{Name: "John Baez", Mode: "name search"}
	if edges := EdgesOfPerson(p); len(edges) != 0 {
		t.Errorf("a name search made %d claims", len(edges))
	}
}

func TestEdgesOfCategory(t *testing.T) {
	edges := EdgesOfCategory(categoryFixture(t, "cs.CL"))

	sub := one(t, edges, graph.SubcategoryOf)
	if sub.From != graph.Category("cs.CL") || sub.To != graph.Archive("cs") {
		t.Errorf("subcategory_of: got %q to %q", sub.From, sub.To)
	}
	group := one(t, edges, graph.PartOfGroup)
	if group.From != graph.Archive("cs") || group.To != graph.Group("Computer Science") {
		t.Errorf("part_of_group: got %q to %q", group.From, group.To)
	}
	set := one(t, edges, graph.InSet)
	if set.To != graph.Set("cs:cs:CL") {
		t.Errorf("in_set: got %q", set.To)
	}
	// The set spec came off OAI and the tree came off the taxonomy page, so
	// saying s7 for both would misattribute the one field s7 does not publish.
	if set.Surface != SurfaceOAI || sub.Surface != SurfaceTaxonomy {
		t.Errorf("surfaces: in_set %q, subcategory_of %q", set.Surface, sub.Surface)
	}
}

// hep-th is an archive that is also a category, so it is its own parent. That
// looks wrong until it is removed and half the physics archives fall out of the
// tree.
func TestAnArchiveIsItsOwnParent(t *testing.T) {
	sub := one(t, EdgesOfCategory(categoryFixture(t, "hep-th")), graph.SubcategoryOf)
	if sub.From != graph.Category("hep-th") || sub.To != graph.Archive("hep-th") {
		t.Errorf("subcategory_of: got %q to %q", sub.From, sub.To)
	}
}

// cites comes out of the rendered bibliography and nowhere else, because arXiv
// publishes no citation graph.
func TestCitesComesOffTheBibliography(t *testing.T) {
	full := fullTextFixture(t)
	edges, cover := EdgesOfFullText(full)
	by := edgesOf(edges)

	if cover.Entries != 46 {
		t.Fatalf("coverage counted %d entries, the bibliography has 46", cover.Entries)
	}
	if cover.Resolved != len(by[graph.Cites]) {
		t.Errorf("coverage says %d resolved and there are %d cites claims", cover.Resolved, len(by[graph.Cites]))
	}
	// 21 entries link to an abs page. The rest are read out of the citation
	// string, which is where most bibliographies keep the identifier.
	if cover.Resolved < 21 {
		t.Errorf("resolved %d of 46, and 21 of them are linked outright", cover.Resolved)
	}
	if cover.Resolved > cover.Entries {
		t.Errorf("resolved %d of %d entries", cover.Resolved, cover.Entries)
	}
	for _, e := range by[graph.Cites] {
		if e.From != graph.Paper("2601.00086") {
			t.Errorf("cites runs from %q", e.From)
		}
		if e.Surface != SurfaceFullText {
			t.Errorf("cites came from %q, and only the rendering may assert it", e.Surface)
		}
		if e.Note == "" {
			t.Error("a cites claim with no note reads as a column of hashes")
		}
	}
}

// A citation set that looks complete and is not is worse than one that admits
// what it missed, so the fraction is printed rather than inferred.
func TestCoverageSaysWhatItMissed(t *testing.T) {
	got := Coverage{Entries: 40, Resolved: 22}.String()
	for _, want := range []string{"22 of 40", "55%"} {
		if !strings.Contains(got, want) {
			t.Errorf("coverage reads %q, want %q in it", got, want)
		}
	}
	empty := Coverage{}.String()
	if strings.Contains(empty, "%") {
		t.Errorf("an empty bibliography reads %q, and there is no fraction to print", empty)
	}
}

// A prose bibliography has nothing structured in it, so there are no claims and
// the coverage says so rather than reporting nought per cent of nothing.
func TestAProseBibliographyResolvesToNothing(t *testing.T) {
	doc := renderingFixture(t, "html_2401.00001v1.html")
	full := fullTextFrom(doc, sectorPaper(), "https://arxiv.org/html/2401.00001v1", testTime)
	edges, cover := EdgesOfFullText(full)

	if cover.Entries != 0 || cover.Resolved != 0 {
		t.Errorf("coverage: got %+v", cover)
	}
	for _, e := range edges {
		if e.Predicate == graph.Cites {
			t.Errorf("a cites claim off a page with no bibliography: %q", e.To)
		}
	}
	// The rendering is the only surface that carries an affiliation, so this is
	// what it is worth reading for when the bibliography is prose.
	if len(edgesOf(edges)[graph.AffiliatedWith]) != 2 {
		t.Errorf("affiliated_with: got %d claims for two authors at one university", len(edges))
	}
}

func TestCitedNode(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  Reference
		want string
	}{
		{"a linked arXiv id wins", Reference{ArxivID: "2309.16609", DOI: "10.1000/x"}, graph.Paper("2309.16609")},
		{"a doi when there is no arXiv id", Reference{DOI: "10.1038/nature14539"}, graph.DOI("10.1038/nature14539")},
		{"an id in the citation string", Reference{Text: "arXiv preprint arXiv:1607.06450"}, graph.Paper("1607.06450")},
		{"the abs form of it", Reference{Text: "CoRR, abs/1409.0473"}, graph.Paper("1409.0473")},
		{"an old style id", Reference{Text: "arXiv:hep-th/9711200"}, graph.Paper("hep-th/9711200")},
		{"a versioned id loses the version", Reference{Text: "arXiv:1706.03762v5"}, graph.Paper("1706.03762")},
		{"a doi written out", Reference{Text: "Nature 521, doi:10.1038/nature14539, 2015"}, graph.DOI("10.1038/nature14539")},
		{"a link and nothing else", Reference{Text: "A blog post", Links: []string{"https://example.org/post"}},
			graph.External("https://example.org/post")},
		// A bare number in a page range is not a citation, and a wrong edge is
		// worse than a missing one.
		{"a page range is not an id", Reference{Text: "Journal of Things, 1607.06450, 2016"}, ""},
		{"nothing at all", Reference{Text: "R. Smith, personal communication"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := citedNode(tc.ref); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCitedNote(t *testing.T) {
	if got := citedNote(Reference{Label: "Bai et al.", Text: "the whole sentence", Title: "Qwen technical report"}); got != "Qwen technical report" {
		t.Errorf("got %q, want the title", got)
	}
	if got := citedNote(Reference{Label: "Bai et al.", Text: "the whole sentence"}); got != "the whole sentence" {
		t.Errorf("got %q, want the citation string", got)
	}
	if got := citedNote(Reference{Label: "Bai et al."}); got != "Bai et al." {
		t.Errorf("got %q, want the label", got)
	}
}

// Nothing outside the table can be written, and nothing inside it may be
// asserted by a surface that does not publish it. This runs every extractor
// over every fixture and checks that not one claim was refused, because a
// refusal in the field is a bug that looks like missing data.
func TestNothingIsRefused(t *testing.T) {
	var s edgeSet
	s.addAll(EdgesOfPaper(fullRecord(t)))
	s.addAll(EdgesOfPaper(realPaper(t)))
	s.addAll(EdgesOfPaper(paperFixture(t, "api_1207.7214.xml")))
	s.addAll(EdgesOfPerson(personFixture(t)))
	s.addAll(EdgesOfCategory(categoryFixture(t, "cs.CL")))
	s.addAll(EdgesOfCategory(categoryFixture(t, "hep-th")))
	for _, tb := range trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200") {
		s.addAll(EdgesOfTrackback(tb))
	}
	feed := feedFixture(t)
	for _, item := range feed.Channel.Items {
		s.addAll(EdgesOfAnnouncement(itemToAnnouncement(item, "cs.CL", feedURL("cs.CL"), testTime, testTime)))
	}
	edges, _ := EdgesOfFullText(fullTextFixture(t))
	s.addAll(edges)

	for _, r := range s.refused {
		t.Errorf("a claim was refused: %s", r)
	}
	if len(s.out) < 1000 {
		t.Fatalf("only %d claims came out of the fixtures, so this is not checking much", len(s.out))
	}
	for _, e := range s.out {
		if _, ok := graph.Lookup(e.Predicate); !ok {
			t.Errorf("%q is not in the table", e.Predicate)
		}
		if e.Source == "" {
			t.Errorf("%s from %s has no source, and a claim nobody made is not a claim", e.Predicate, e.From)
		}
		if e.Surface == "" {
			t.Errorf("%s from %s does not say which surface asserted it", e.Predicate, e.From)
		}
	}
}

// Every predicate in the table has a parser that emits it. A predicate nothing
// writes is either a missing extractor or a row that should not be there, and
// both are worth failing over.
func TestEveryPredicateIsEmittedBySomething(t *testing.T) {
	seen := map[string]bool{}
	note := func(edges []graph.Edge) {
		for _, e := range edges {
			seen[e.Predicate] = true
		}
	}
	note(EdgesOfPaper(fullRecord(t)))
	note(EdgesOfPerson(personFixture(t)))
	note(EdgesOfCategory(categoryFixture(t, "cs.CL")))
	for _, tb := range trackbacksFixture(t, "tb_hep-th_9711200.html", "hep-th/9711200") {
		note(EdgesOfTrackback(tb))
	}
	feed := feedFixture(t)
	note(EdgesOfAnnouncement(itemToAnnouncement(feed.Channel.Items[0], "cs.CL", feedURL("cs.CL"), testTime, testTime)))
	edges, _ := EdgesOfFullText(fullTextFixture(t))
	note(edges)
	// published_in needs a paper that was published, which 1706.03762 was not.
	note(EdgesOfPaper(paperFixture(t, "api_1207.7214.xml")))

	for _, p := range graph.Predicates {
		if !seen[p.Name] {
			t.Errorf("nothing emits %s, so it is either a missing parser or a row that should not be in the table", p.Name)
		}
	}
}

// Two surfaces asserting the same claim stay two rows, because the source is
// part of a claim's identity rather than metadata on it. The same surface
// saying it twice is one row.
func TestTheSameClaimTwiceIsOneRow(t *testing.T) {
	var s edgeSet
	e := graph.Edge{
		From: graph.Paper("1706.03762"), Predicate: graph.InCategory, To: graph.Category("cs.CL"),
		Source: apiBase, Surface: SurfaceAPI,
	}
	s.add(e)
	s.add(e)
	if len(s.out) != 1 {
		t.Fatalf("got %d rows for one claim said twice", len(s.out))
	}
	other := e
	other.Source = absURL("1706.03762")
	other.Surface = SurfaceAbs
	s.add(other)
	if len(s.out) != 2 {
		t.Errorf("got %d rows for one claim from two surfaces, and a disagreement has to stay queryable", len(s.out))
	}
}

// An end that could not be named is not an error. A paper with no journal
// reference has nothing to say rather than something wrong to say.
func TestAnEmptyEndIsDroppedQuietly(t *testing.T) {
	var s edgeSet
	s.add(graph.Edge{From: graph.Paper("1706.03762"), Predicate: graph.PublishedIn, To: "", Source: apiBase})
	if len(s.out) != 0 || len(s.refused) != 0 {
		t.Errorf("out %d, refused %v", len(s.out), s.refused)
	}
}

// A claim pointing at the wrong kind of node joins to nothing and looks like
// missing data rather than like a bug, so it is refused and said out loud.
func TestABackwardsClaimIsRefused(t *testing.T) {
	var s edgeSet
	s.add(graph.Edge{
		From: graph.Paper("1706.03762"), Predicate: graph.Authored, To: graph.Name("Ashish Vaswani"),
		Source: apiBase, Surface: SurfaceAPI,
	})
	if len(s.out) != 0 {
		t.Error("a backwards authored claim was written")
	}
	if len(s.refused) != 1 || !strings.Contains(s.refused[0], "authored runs from") {
		t.Errorf("refused: got %v", s.refused)
	}
}

// cites may only ever be asserted by the rendering, because arXiv publishes no
// citation graph and a cites row from anywhere else would be an invention.
func TestCitesFromAnywhereElseIsRefused(t *testing.T) {
	var s edgeSet
	s.add(graph.Edge{
		From: graph.Paper("1706.03762"), Predicate: graph.Cites, To: graph.Paper("1409.0473"),
		Source: apiBase, Surface: SurfaceAPI,
	})
	if len(s.out) != 0 {
		t.Error("a cites claim was written off the export API, which publishes no citations")
	}
	if len(s.refused) != 1 {
		t.Errorf("refused: got %v", s.refused)
	}
}

func TestSurfaceOfURL(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{apiBase + "?id_list=1706.03762", SurfaceAPI},
		{oaiBase + "?verb=GetRecord", SurfaceOAI},
		{absURL("1706.03762"), SurfaceAbs},
		{listBase + "cs.CL/2026-01", SurfaceList},
		{s5Base + "?query=attention", SurfaceSearch},
		{rssBase + "cs.CL", SurfaceRSS},
		{taxonomyURL, SurfaceTaxonomy},
		{authorURL("baez_j_1"), SurfaceAuthorID},
		{htmlBase + "1706.03762v7", SurfaceFullText},
		{trackbackBase + "1706.03762", SurfaceTrackback},
		{pdfBase + "1706.03762", SurfaceFiles},
		{"https://example.org/", ""},
		{"snapshot:2026-08-13", ""},
	} {
		if got := surfaceOfURL(tc.url); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.url, got, tc.want)
		}
	}
}

// The envelope keeps surfaces and sources as two lists appended together, and
// they stop lining up the moment OAI is read twice for its two formats. Matching
// on the URL is what makes the attribution right anyway.
func TestSourceOfMatchesOnTheURL(t *testing.T) {
	p := fullRecord(t)
	if len(p.Surfaces) != 3 || len(p.Sources) != 4 {
		t.Fatalf("the fixture reads %d surfaces over %d requests, so the lists no longer disagree",
			len(p.Surfaces), len(p.Sources))
	}
	if got := sourceOf(p.Envelope, SurfaceAbs); got != absURL("1706.03762") {
		t.Errorf("the abstract page source is %q", got)
	}
	// The last OAI read is arXivRaw, which is the one carrying the version
	// table and the submitter.
	if got := sourceOf(p.Envelope, SurfaceOAI); !strings.Contains(got, "arXivRaw") {
		t.Errorf("the OAI source is %q, want the arXivRaw read", got)
	}
	if got := sourceOf(p.Envelope, SurfaceFullText); got != "" {
		t.Errorf("a metadata read claims a rendering at %q", got)
	}
}

// The taxonomy falls back to the bundled snapshot when the network is down, and
// the snapshot is not a URL. A claim still has a source, and it says what it is.
func TestSourceOfKeepsASnapshot(t *testing.T) {
	var e Envelope
	e.addSurface(SurfaceTaxonomy, "snapshot:2026-08-13")
	if got := sourceOf(e, SurfaceTaxonomy); got != "snapshot:2026-08-13" {
		t.Errorf("got %q, want the snapshot", got)
	}
}

func TestViaOr(t *testing.T) {
	var e Envelope
	e.setVia("authors", SurfaceAuthorID)
	if got := viaOr(e, "authors", SurfaceAPI); got != SurfaceAuthorID {
		t.Errorf("got %q, want the surface the record recorded", got)
	}
	if got := viaOr(e, "license", SurfaceOAI); got != SurfaceOAI {
		t.Errorf("got %q, want the fallback", got)
	}
}
