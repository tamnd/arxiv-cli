package arxiv

import (
	"bytes"
	"context"
	"embed"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/any-cli/kit/errs"
)

// taxonomyURL is the page that lists every category with its description.
const taxonomyURL = "https://" + Host + "/category_taxonomy"

// snapshotDate is when the two embedded tables below were saved. It is in the
// notice a fallback prints, because "this is the bundled copy" is only useful
// with a date next to it.
const snapshotDate = "2026-08-13"

// The two tables ship with the binary so a first run with no network still
// prints the taxonomy, and so category validation costs nothing. They are the
// pages themselves rather than a generated table: the parser that reads them
// here is the parser that reads the live ones, so a snapshot that stops parsing
// is a test failure and not a silent divergence.
//
//go:embed embedded/taxonomy.html embedded/sets.xml
var embedded embed.FS

// Category is one arXiv subject class.
type Category struct {
	Envelope
	Code string `json:"code" kit:"id" table:"code"`
	Name string `json:"name" table:"name"`
	// Group is the top level heading the page files this category under, such
	// as "Computer Science" or "Physics".
	Group string `json:"group" table:"group"`
	// Archive is the code before the dot, and the code itself for an archive
	// that was never split.
	Archive     string `json:"archive" table:"-"`
	Description string `json:"description,omitempty" table:"-"`
	// SetSpec is the OAI set this category is harvested as. It is a join
	// against ListSets rather than a rewrite of the code, because the two
	// vocabularies do not line up: cs.CL is cs:cs:CL and hep-th is
	// physics:hep-th with no middle segment at all.
	SetSpec string `json:"set_spec,omitempty" table:"-"`
	// IsArchive is true for the nine archives that were never split into
	// categories, which are both an archive and a category at once. It is the
	// field that stops code from generating physics:hep-th:hep-th.
	IsArchive bool `json:"is_archive" table:"-"`
	// RecentPapers is how many papers were submitted to this category in the
	// last thirty days, and it is only filled when it was asked for, because
	// it costs a request and it is a different number tomorrow.
	RecentPapers int `json:"recent_papers,omitempty" table:"-"`
}

// Set is one OAI-PMH set.
type Set struct {
	Envelope
	SetSpec string `json:"set_spec" kit:"id" table:"set"`
	Name    string `json:"name" table:"name"`
	// Category is the category code this set harvests, where there is one. An
	// archive level set such as physics:cond-mat has none: it is a container
	// for the categories under it.
	Category string `json:"category,omitempty" table:"category"`
}

// ─── the taxonomy page ───────────────────────────────────────────────────────

// parseTaxonomy reads the category taxonomy page.
//
// The page is a list of group headings, each followed by a body of category
// rows, so the parser walks the list's own children in order and keeps the
// heading it last saw. Reading the headings and the rows separately and pairing
// them by index would work until arXiv adds one empty group.
func parseTaxonomy(body []byte) ([]Category, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse taxonomy page: %w", err)
	}
	list := doc.Find("#category_taxonomy_list")
	if list.Length() == 0 {
		return nil, fmt.Errorf("parse taxonomy page: no category list on it")
	}

	var out []Category
	var group string
	list.Children().Each(func(_ int, s *goquery.Selection) {
		if s.Is("h2") {
			group = cleanText(s.Text())
			return
		}
		s.Find("div.columns.divided").Each(func(_ int, row *goquery.Selection) {
			c, ok := parseTaxonomyRow(row)
			if !ok {
				return
			}
			c.Group = group
			out = append(out, c)
		})
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("parse taxonomy page: no categories on it")
	}
	return out, nil
}

// parseTaxonomyRow reads one row: the code and name in an h4, the description
// in the paragraph beside it.
func parseTaxonomyRow(row *goquery.Selection) (Category, bool) {
	h4 := row.Find("h4").First()
	if h4.Length() == 0 {
		return Category{}, false
	}
	name := cleanText(h4.Find("span").First().Text())
	name = strings.TrimSuffix(strings.TrimPrefix(name, "("), ")")
	// The code is what is left of the h4 once the name is taken off it.
	code := cleanText(strings.TrimSuffix(cleanText(h4.Text()), cleanText(h4.Find("span").First().Text())))
	if code == "" || strings.Contains(code, " ") {
		// The page's own legend row is an h4 too, and it says "Category Name"
		// where a code goes. A code has no spaces in it.
		return Category{}, false
	}
	c := Category{
		Code:        code,
		Name:        name,
		Description: cleanText(row.Find("p").First().Text()),
	}
	// A code with no dot is an archive that was never split, and it is its own
	// archive. quant-ph is the whole of quantum physics and there is no
	// quant-ph.something under it.
	if archive, _, ok := strings.Cut(code, "."); ok {
		c.Archive = archive
	} else {
		c.Archive = code
		c.IsArchive = true
	}
	return c, true
}

// ─── the OAI sets ────────────────────────────────────────────────────────────

// setsURL asks OAI for the whole set list, which comes back in one response.
var setsURL = oaiBase + "?verb=ListSets"

type oaiListSets struct {
	XMLName xml.Name `xml:"OAI-PMH"`
	Error   oaiError `xml:"error"`
	Sets    []struct {
		Spec string `xml:"setSpec"`
		Name string `xml:"setName"`
	} `xml:"ListSets>set"`
}

// parseSets reads ListSets and returns the sets and how many the response
// carried before deduplication.
//
// Nine specs appear twice, physics:hep-th and physics:gr-qc among them, because
// an archive that was never split is listed once as an archive and once as a
// category. They are the same set both times, so the second one is dropped and
// the raw count is returned for anyone who wants to know it happened.
func parseSets(body []byte) ([]Set, int, error) {
	var resp oaiListSets
	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode oai sets: %w", err)
	}
	if resp.Error.Code != "" {
		return nil, 0, fmt.Errorf("oai sets: %s: %s", resp.Error.Code, cleanText(resp.Error.Message))
	}
	seen := map[string]bool{}
	out := make([]Set, 0, len(resp.Sets))
	for _, s := range resp.Sets {
		spec := cleanText(s.Spec)
		if spec == "" || seen[spec] {
			continue
		}
		seen[spec] = true
		out = append(out, Set{SetSpec: spec, Name: cleanText(s.Name)})
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("decode oai sets: the response carried none")
	}
	return out, len(resp.Sets), nil
}

// joinSets fills in each category's set spec and each set's category.
//
// The rule is read off the data rather than spelled: a three segment spec
// group:archive:SUB is the category archive.SUB, and a two segment spec
// group:archive is a category only when that archive has no three segment
// children, which is exactly the archives that were never split. Everything
// left over is a container.
func joinSets(cats []Category, sets []Set) {
	leaf := map[string]int{}    // "cs:CL" style key to set index
	archive := map[string]int{} // archive code to set index
	split := map[string]bool{}  // archives that have categories under them
	for i, s := range sets {
		parts := strings.Split(s.SetSpec, ":")
		switch len(parts) {
		case 2:
			archive[parts[1]] = i
		case 3:
			leaf[parts[1]+":"+parts[2]] = i
			split[parts[1]] = true
		}
	}
	for i := range cats {
		c := &cats[i]
		idx, ok := -1, false
		if sub, has := strings.CutPrefix(c.Code, c.Archive+"."); has {
			idx, ok = leaf[c.Archive+":"+sub]
		} else if !split[c.Archive] {
			idx, ok = archive[c.Archive]
		}
		if !ok {
			continue
		}
		c.SetSpec = sets[idx].SetSpec
		sets[idx].Category = c.Code
	}
}

// ─── reading them ────────────────────────────────────────────────────────────

// Categories returns the whole taxonomy, joined to the OAI sets.
//
// It reads two surfaces on two planes and both are cached for a week, so the
// second run in a week costs nothing. A network failure falls back to the
// bundled snapshot and says so on stderr, because a taxonomy that is a few
// weeks stale is far more useful than an error: arXiv adds a category about
// once a year.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	cats, err := c.taxonomy(ctx)
	if err != nil {
		return nil, err
	}
	sets, err := c.Sets(ctx)
	if err != nil {
		// The join is the only thing the sets are needed for here, so a failure
		// costs the set spec field and nothing else.
		c.logf(0, "arxiv: could not read the OAI sets, so set_spec is missing from these categories: %s", err)
		return cats, nil
	}
	joinSets(cats, sets)
	// The set spec came from the other surface, and a record that says s7 alone
	// while carrying a field only s2 knows would be misattributing it.
	source := ""
	if len(sets) > 0 && len(sets[0].Sources) > 0 {
		source = sets[0].Sources[0]
	}
	for i := range cats {
		if cats[i].SetSpec == "" {
			continue
		}
		cats[i].addSurface(SurfaceOAI, source)
		cats[i].setVia("set_spec", SurfaceOAI)
	}
	return cats, nil
}

// Sets returns the OAI sets, joined to the taxonomy.
func (c *Client) Sets(ctx context.Context) ([]Set, error) {
	c.logf(1, "GET %s", setsURL)
	body, source, err := c.tableBody(ctx, setsURL, "embedded/sets.xml", "the OAI set list")
	if err != nil {
		return nil, err
	}
	sets, raw, err := parseSets(body)
	if err != nil {
		return nil, err
	}
	c.logf(1, "%d sets, %d after dropping the ones listed twice", raw, len(sets))
	cats, err := c.taxonomy(ctx)
	taxSource := ""
	if err == nil {
		joinSets(cats, sets)
		if len(cats) > 0 && len(cats[0].Sources) > 0 {
			taxSource = cats[0].Sources[0]
		}
	}
	at := c.now()
	for i := range sets {
		sets[i].Kind = "set"
		sets[i].RetrievedAt = at
		sets[i].addSurface(SurfaceOAI, source)
		if sets[i].Category != "" {
			sets[i].addSurface(SurfaceTaxonomy, taxSource)
			sets[i].setVia("category", SurfaceTaxonomy)
		}
	}
	return sets, nil
}

// taxonomy reads the category page once per client and remembers it, because
// both commands and the category check want the same table.
func (c *Client) taxonomy(ctx context.Context) ([]Category, error) {
	c.taxOnce.Do(func() {
		var body []byte
		var source string
		body, source, c.taxErr = c.tableBody(ctx, taxonomyURL, "embedded/taxonomy.html", "the category taxonomy")
		if c.taxErr != nil {
			return
		}
		var cats []Category
		if cats, c.taxErr = parseTaxonomy(body); c.taxErr != nil {
			return
		}
		at := c.now()
		for i := range cats {
			cats[i].Kind = "category"
			cats[i].RetrievedAt = at
			cats[i].addSurface(SurfaceTaxonomy, source)
		}
		c.tax = cats
	})
	if c.taxErr != nil {
		return nil, c.taxErr
	}
	// A copy, so a caller that filters or annotates does not edit the table the
	// next caller gets.
	out := make([]Category, len(c.tax))
	copy(out, c.tax)
	return out, nil
}

// tableBody fetches one of the two tables, falling back to the bundled copy.
//
// The source it returns is the URL when the fetch worked and the snapshot name
// when it did not, so a record built from the fallback says where it came from
// instead of naming a URL nobody read.
func (c *Client) tableBody(ctx context.Context, url, name, what string) ([]byte, string, error) {
	body, err := c.fetch(ctx, url, TTLTaxonomy)
	if err == nil {
		return body.Body, url, nil
	}
	snapshot, readErr := embedded.ReadFile(name)
	if readErr != nil {
		return nil, "", err
	}
	c.logf(0, "arxiv: could not read %s from %s (%s), using the copy saved on %s", what, url, err, snapshotDate)
	return snapshot, "snapshot:" + snapshotDate, nil
}

// ─── the offline table ───────────────────────────────────────────────────────

var (
	snapshotOnce sync.Once
	snapshotCats []Category
	snapshotCode map[string]bool
)

// knownCodes is every code a --cat may name, from the bundled snapshot.
//
// Validation reads the snapshot and never the network. A category check that
// cost a fifteen second page fetch would be a check nobody could afford to run
// before every search, and the table it is checking against changes about once
// a year.
func knownCodes() map[string]bool {
	loadSnapshot()
	return snapshotCode
}

// snapshotCategories is the bundled taxonomy, parsed once.
func snapshotCategories() []Category {
	loadSnapshot()
	return snapshotCats
}

func loadSnapshot() {
	snapshotOnce.Do(func() {
		snapshotCode = map[string]bool{}
		body, err := embedded.ReadFile("embedded/taxonomy.html")
		if err != nil {
			return
		}
		cats, err := parseTaxonomy(body)
		if err != nil {
			return
		}
		if sets, _, err := parseSets(mustEmbed("embedded/sets.xml")); err == nil {
			joinSets(cats, sets)
		}
		snapshotCats = cats
		for _, c := range cats {
			snapshotCode[c.Code] = true
			// An archive code is a legal --cat too: cat:cs matches nothing on
			// its own but cs.* does, and the query builder expands it.
			snapshotCode[c.Archive] = true
		}
	})
}

func mustEmbed(name string) []byte {
	b, _ := embedded.ReadFile(name)
	return b
}

// checkCategories refuses a category code arXiv does not have.
//
// This is the check doc 01 section 1.4 asks for. A wrong code is not an error
// at the API: cat:nope.NOPE answers HTTP 200 with zero results, which reads as
// "this category is empty today" and sends the user looking for a problem that
// is a typo. So the code is checked here, before any request goes out.
func checkCategories(codes []string) error {
	known := knownCodes()
	if len(known) == 0 {
		// The snapshot did not parse, which is a broken build rather than a
		// user error, and refusing every category over it would be worse than
		// letting the query through.
		return nil
	}
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || known[code] {
			continue
		}
		if near := nearestCode(code, known); near != "" {
			return errs.Usage("no arXiv category is called %s; the closest one is %s", code, near)
		}
		return errs.Usage("no arXiv category is called %s; run arxiv categories for the list", code)
	}
	return nil
}

// nearestCode is the closest known code to a typo, or "" when nothing is close.
//
// Case alone counts as one edit, so cs.lg suggests cs.LG, and the cutoff is
// deliberately tight: a suggestion that is not the thing the user meant is
// worse than no suggestion.
func nearestCode(code string, known map[string]bool) string {
	best, bestDist := "", 3
	for k := range known {
		d := editDistance(strings.ToLower(code), strings.ToLower(k))
		if strings.EqualFold(code, k) {
			return k
		}
		if d < bestDist || (d == bestDist && k < best) {
			best, bestDist = k, d
		}
	}
	if bestDist >= 3 {
		return ""
	}
	return best
}

// editDistance is Levenshtein over two short strings, which is all a category
// code ever is.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// noteUnknownCategories reports a category on a record that the snapshot does
// not have, once per code per run.
//
// The code is kept on the record either way. arXiv adding a category is a thing
// that happens, and a tool that drops the field because its table is a month
// old would be hiding the only evidence that the table is a month old.
func (c *Client) noteUnknownCategories(codes ...string) {
	known := knownCodes()
	if len(known) == 0 {
		return
	}
	c.unknownMu.Lock()
	defer c.unknownMu.Unlock()
	for _, code := range codes {
		if code == "" || known[code] || c.unknownCats[code] {
			continue
		}
		if c.unknownCats == nil {
			c.unknownCats = map[string]bool{}
		}
		c.unknownCats[code] = true
		c.logf(0, "arxiv: %s is not in the category table saved on %s, keeping it; run arxiv categories to see the current list", code, snapshotDate)
	}
}

// Category returns one category by code.
//
// A code that is not in the table is a usage error with the nearest match,
// which is the same answer a bad --cat gets, because the alternative is an
// empty record that reads like arXiv has nothing to say about cs.CL.
func (c *Client) Category(ctx context.Context, code string, count bool) (Category, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Category{}, errs.Usage("give a category code, such as cs.CL")
	}
	cats, err := c.Categories(ctx)
	if err != nil {
		return Category{}, err
	}
	for _, cat := range cats {
		if !strings.EqualFold(cat.Code, code) {
			continue
		}
		if count {
			if err := c.countRecent(ctx, &cat); err != nil {
				return Category{}, err
			}
		}
		return cat, nil
	}
	known := map[string]bool{}
	for _, cat := range cats {
		known[cat.Code] = true
	}
	if near := nearestCode(code, known); near != "" {
		return Category{}, errs.Usage("no arXiv category is called %s; the closest one is %s", code, near)
	}
	return Category{}, errs.NotFound("no arXiv category is called %s; run arxiv categories for the list", code)
}

// countRecent fills in RecentPapers, which is one count request on the API
// plane. The window is the last thirty days rather than the calendar month,
// because on the second of the month a calendar month is two days of data and
// looks like the category died.
func (c *Client) countRecent(ctx context.Context, cat *Category) error {
	from := c.now().AddDate(0, 0, -30).Format("2006-01-02")
	n, err := c.CountSearch(ctx, SearchOptions{Categories: []string{cat.Code}, From: from})
	if err != nil {
		return err
	}
	cat.RecentPapers = n.Total
	if len(n.Sources) > 0 {
		cat.addSurface(SurfaceAPI, n.Sources[0])
	}
	cat.setVia("recent_papers", SurfaceAPI)
	return nil
}
