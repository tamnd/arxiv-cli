package arxiv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// crawl.go is the walk that keeps what it read. Spec 3006 doc 04 section 3.
//
// `arxiv walk` holds a graph in memory and prints it. A crawl is the same walk
// with a store under it, and the store changes three things. It can stop and be
// picked up again, because the frontier is a query rather than a variable. It
// can be told what it spent, because every request goes into the read log. And
// it can be told what it did not do, because a node named by a claim and never
// fetched is a row in the nodes table with a null record.
//
// The budget is counted per plane and not in total, which is the one number in
// here that matters. Fifty requests is two and a half minutes on the API plane
// and twelve and a half on arxiv.org, so a single budget would either be
// generous enough to spend an afternoon or tight enough to be useless.

// CrawlOptions is one crawl.
type CrawlOptions struct {
	// Seeds are where it starts: paper references, category codes, or ax://
	// URIs. They are sighted in the store and then read off the frontier like
	// anything else, so a seed that has already been read is not read twice.
	Seeds []string
	// Search seeds from one query in arXiv's own grammar, which is by far the
	// cheapest way to start: one request is a hundred papers.
	Search string
	// Max is how many results the seed search takes.
	Max int
	// Hops is how far to walk. One hop reads the seeds and stops.
	Hops int
	// Depth is how deeply each paper is read.
	Depth Depth
	// Budget and HTMLBudget are the request ceilings, one per plane.
	Budget     int
	HTMLBudget int
	// APIOnly refuses to queue anything on the HTML plane, whatever else was
	// asked for. It is the flag to reach for on somebody else's network.
	APIOnly bool
	// Names follows author names, at one search each. Off by default because
	// eight authors a paper is eight searches a paper and most of them find
	// somebody else of the same name.
	Names bool
	// Trackbacks reads the inbound links of the seed papers, on the HTML plane.
	Trackbacks bool
	// Limit is how many papers a name search reads.
	Limit int
	// Resume takes the whole store's frontier rather than only what this run
	// reached, which is how a crawl that ran out of budget yesterday carries on.
	Resume bool
	// Progress is where the running commentary goes. Nil means the client's own
	// log.
	Progress func(string, ...any)
}

// crawl defaults. They are deliberately small: a crawl is the one command in
// this tool that can run for an hour, so the default should be the one that
// finishes while you watch it.
const (
	defaultCrawlBudget = 100
	defaultHTMLBudget  = 20
	defaultCrawlHops   = 2
	defaultCrawlLimit  = 25
	// frontierPage is how many unread nodes of a kind one hop takes. Two
	// hundred papers is one batched request, so this is a hop and not a cap on
	// the crawl.
	frontierPage = 200
)

func (o *CrawlOptions) fill() {
	if o.Hops < 1 {
		o.Hops = defaultCrawlHops
	}
	if o.Depth == "" {
		o.Depth = DepthMeta
	}
	if o.Budget <= 0 {
		o.Budget = defaultCrawlBudget
	}
	if o.HTMLBudget < 0 {
		o.HTMLBudget = 0
	}
	if o.HTMLBudget == 0 && !o.APIOnly {
		o.HTMLBudget = defaultHTMLBudget
	}
	if o.Limit <= 0 {
		o.Limit = defaultCrawlLimit
	}
	if o.Max <= 0 {
		o.Max = PageSize
	}
}

// Crawler is a client, a store and one set of options.
type Crawler struct {
	c    *Client
	st   *Store
	opts CrawlOptions

	// reached is every node this run has heard of, which is the frontier when
	// the run is not resuming.
	reached map[string]bool
	// tried is every node this run has already gone after. A withdrawn paper
	// answers 404 and stays unread, and without this the next hop would ask for
	// it again, and the hop after that.
	tried map[string]bool

	spent map[string]int
	man   *Manifest
}

// NewCrawler builds one. Nothing is read here and nothing is written.
func NewCrawler(c *Client, st *Store, o CrawlOptions) (*Crawler, error) {
	if c == nil || st == nil {
		return nil, errs.New(errs.KindGeneric, "internal error: a crawl needs a client and a store")
	}
	if st.ReadOnly() {
		return nil, errs.New(errs.KindGeneric, "the store at %s is open read only, so there is nowhere to put a crawl", st.Path())
	}
	o.fill()
	if o.APIOnly {
		o.HTMLBudget = 0
	}
	if len(o.Seeds) == 0 && o.Search == "" && !o.Resume {
		return nil, errs.Usage("a crawl needs somewhere to start: give it a paper, a category, --search, or --resume to carry on from the store")
	}
	return &Crawler{
		c:       c,
		st:      st,
		opts:    o,
		reached: map[string]bool{},
		tried:   map[string]bool{},
		spent:   map[string]int{},
	}, nil
}

// ─── the plan ────────────────────────────────────────────────────────────────

// CrawlPlan is what a crawl says it is about to do, before it does any of it.
//
// It is printed and confirmed rather than just logged, because the difference
// between a crawl that takes eight seconds and one that takes forty minutes is
// two flags, and the flags do not look different from each other.
type CrawlPlan struct {
	Store   string         `json:"store" kit:"id" table:"store,truncate"`
	Seeds   []string       `json:"seeds,omitempty" table:"-"`
	Search  string         `json:"search,omitempty" table:"-"`
	Waiting map[string]int `json:"waiting,omitempty" table:"-"`
	Hops    int            `json:"hops" table:"hops"`
	Depth   string         `json:"depth" table:"depth"`
	Budget  int            `json:"budget" table:"budget"`
	HTML    int            `json:"html_budget" table:"html"`
	// Wall is the worst case: every request in both budgets, at this client's
	// pace. A crawl that runs out of frontier stops long before it.
	Wall  time.Duration `json:"wall" table:"wall"`
	Notes []string      `json:"notes,omitempty" table:"-"`
}

// Plan describes the run without making a request. The only thing it reads is
// the store, and only to count what is already waiting.
func (cr *Crawler) Plan(ctx context.Context) (CrawlPlan, error) {
	p := CrawlPlan{
		Store:  cr.st.Path(),
		Search: cr.opts.Search,
		Hops:   cr.opts.Hops,
		Depth:  string(cr.opts.Depth),
		Budget: cr.opts.Budget,
		HTML:   cr.opts.HTMLBudget,
	}
	for _, s := range cr.opts.Seeds {
		uri, err := ResolveSeed(s)
		if err != nil {
			return p, err
		}
		p.Seeds = append(p.Seeds, uri)
	}
	p.Wall = time.Duration(p.Budget)*cr.c.Pace(APIPlane.Name) +
		time.Duration(p.HTML)*cr.c.Pace(HTMLPlane.Name)

	if cr.opts.Resume {
		p.Waiting = map[string]int{}
		for _, kind := range readableKinds {
			uris, err := cr.st.Frontier(kind, frontierPage)
			if err != nil {
				return p, err
			}
			if len(uris) > 0 {
				p.Waiting[kind] = len(uris)
			}
		}
	}

	api, html := cr.opts.Depth.PlaneRequests()
	if cr.opts.APIOnly {
		p.Notes = append(p.Notes, "--api-only, so nothing is queued on arxiv.org")
		if html > 0 {
			p.Notes = append(p.Notes, fmt.Sprintf("depth %s reads the abstract page, so it is read at depth meta instead", cr.opts.Depth))
		}
		if cr.opts.Trackbacks {
			p.Notes = append(p.Notes, "trackbacks are on the HTML plane, so they are skipped")
		}
	} else if html > 0 {
		p.Notes = append(p.Notes, fmt.Sprintf("depth %s costs %d API requests and %d on arxiv.org per paper, and arxiv.org paces at %s",
			cr.opts.Depth, api, html, cr.c.Pace(HTMLPlane.Name)))
	}
	if cr.opts.Names {
		p.Notes = append(p.Notes, "--names is on, so every author name found is one more search")
	}
	if !cr.opts.Resume {
		p.Notes = append(p.Notes, "this run follows only what its own seeds reach; --resume takes the rest of the store's frontier too")
	}
	return p, nil
}

// String is the paragraph printed before a crawl starts.
func (p CrawlPlan) String() string {
	var b strings.Builder
	from := strings.Join(p.Seeds, ", ")
	switch {
	case from != "" && p.Search != "":
		from += fmt.Sprintf(" and the search %q", p.Search)
	case from == "" && p.Search != "":
		from = fmt.Sprintf("the search %q", p.Search)
	case from == "":
		from = "the store's frontier"
	}
	fmt.Fprintf(&b, "crawl %s from %s, %d hops at depth %s\n", p.Store, from, p.Hops, p.Depth)
	fmt.Fprintf(&b, "budget %d requests on the api plane and %d on the html plane, which is at most %s\n",
		p.Budget, p.HTML, p.Wall.Round(time.Second))
	for _, kind := range sortedKeys(p.Waiting) {
		fmt.Fprintf(&b, "%d %s nodes are already waiting in the store\n", p.Waiting[kind], kind)
	}
	for _, n := range p.Notes {
		fmt.Fprintf(&b, "%s\n", n)
	}
	return b.String()
}

// ─── the manifest ────────────────────────────────────────────────────────────

// Manifest is the record of a run, written whether the run finished or not.
//
// A crawl that was interrupted is the one whose manifest matters most: it is
// the only thing that says how far it got, what it refused, and what arXiv said
// while it was going. The counts here are the run's; the store holds the rest.
type Manifest struct {
	Store      string    `json:"store" kit:"id" table:"store,truncate"`
	StartedAt  time.Time `json:"started_at" table:"-"`
	FinishedAt time.Time `json:"finished_at" table:"-"`
	Elapsed    string    `json:"elapsed" table:"elapsed"`

	Seeds  []string `json:"seeds,omitempty" table:"-"`
	Search string   `json:"search,omitempty" table:"-"`
	Hops   int      `json:"hops" table:"-"`
	Depth  string   `json:"depth" table:"depth"`

	Budget map[string]int `json:"budget" table:"-"`
	Spent  map[string]int `json:"spent" table:"-"`

	Requests int   `json:"requests" table:"requests"`
	Bytes    int64 `json:"bytes" table:"-"`

	Papers     int `json:"papers" table:"papers"`
	Categories int `json:"categories" table:"-"`
	Searches   int `json:"searches" table:"-"`
	Claims     int `json:"claims" table:"claims"`
	// Misses are nodes this run asked for and did not get back, which is mostly
	// papers that were withdrawn.
	Misses      int `json:"misses" table:"-"`
	Errors      int `json:"errors" table:"errors"`
	RateLimited int `json:"rate_limited" table:"-"`

	Refusals  []Refusal `json:"refusals,omitempty" table:"-"`
	Notes     []string  `json:"notes,omitempty" table:"-"`
	Cancelled bool      `json:"cancelled" table:"cancelled"`
}

// Refusal is one thing the budget would not pay for. It names the plane,
// because "out of budget" is a different problem on each of them.
type Refusal struct {
	What  string `json:"what"`
	Plane string `json:"plane"`
	Need  int    `json:"need"`
	Left  int    `json:"left"`
}

// Save writes the manifest into dir under a name made from its start time, and
// returns the path.
func (m *Manifest) Save(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("no directory to write the manifest into")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("crawl-%d.json", m.StartedAt.Unix()))
	blob, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(blob, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// String is the sentence printed when a crawl stops.
func (m *Manifest) String() string {
	end := "done"
	if m.Cancelled {
		end = "cancelled"
	}
	return fmt.Sprintf("%s after %s: %d requests (%d api, %d html), %d papers, %d claims, %d refused",
		end, m.Elapsed, m.Requests, m.Spent[APIPlane.Name], m.Spent[HTMLPlane.Name],
		m.Papers, m.Claims, len(m.Refusals))
}

// ─── the run ─────────────────────────────────────────────────────────────────

// Run crawls until the frontier is empty, the hops are used up, the budget is
// spent or the context is cancelled.
//
// The manifest comes back in every one of those cases, including the error
// ones, because a run that failed halfway still spent requests and still wrote
// what it read, and the caller needs to be able to say so.
func (cr *Crawler) Run(ctx context.Context) (*Manifest, error) {
	now := time.Now().UTC()
	cr.man = &Manifest{
		Store:     cr.st.Path(),
		StartedAt: now,
		Seeds:     append([]string(nil), cr.opts.Seeds...),
		Search:    cr.opts.Search,
		Hops:      cr.opts.Hops,
		Depth:     string(cr.opts.Depth),
		Budget: map[string]int{
			APIPlane.Name:  cr.opts.Budget,
			HTMLPlane.Name: cr.opts.HTMLBudget,
		},
		Spent: map[string]int{},
	}

	// Every request from here on goes into the read log and onto the plane's
	// tally, whoever made it.
	cr.c.Watch(cr.saw)
	defer cr.c.Watch(nil)
	defer cr.finish()

	if err := cr.seed(ctx); err != nil {
		if cancelled(err) {
			cr.man.Cancelled = true
			return cr.man, nil
		}
		return cr.man, err
	}

	for hop := 1; hop <= cr.opts.Hops; hop++ {
		if ctx.Err() != nil {
			cr.man.Cancelled = true
			break
		}
		if n := cr.hop(ctx, hop); n == 0 {
			cr.say("nothing left to read at hop %d", hop)
			break
		}
	}
	if ctx.Err() != nil {
		cr.man.Cancelled = true
	}
	return cr.man, nil
}

// finish stamps the manifest. It runs on the way out of Run however Run left.
func (cr *Crawler) finish() {
	cr.man.FinishedAt = time.Now().UTC()
	cr.man.Elapsed = cr.man.FinishedAt.Sub(cr.man.StartedAt).Round(time.Second).String()
	for _, plane := range Planes {
		cr.man.Spent[plane.Name] = cr.spent[plane.Name]
	}
}

// saw is the read hook: one row in the log, one on the plane's tally.
func (cr *Crawler) saw(r Read) {
	cr.spent[r.Plane]++
	cr.man.Requests++
	cr.man.Bytes += r.Bytes
	if r.Error != "" {
		cr.man.Errors++
	}
	if r.Status == 429 {
		cr.man.RateLimited++
	}
	if err := cr.st.PutRead(r); err != nil {
		cr.say("the read of %s could not be logged: %s", r.URL, err)
	}
}

// seed puts the starting points into the store and runs the seed search.
func (cr *Crawler) seed(ctx context.Context) error {
	for _, s := range cr.opts.Seeds {
		uri, err := ResolveSeed(s)
		if err != nil {
			return err
		}
		if err := cr.st.Sight(uri); err != nil {
			return err
		}
		cr.reached[uri] = true
	}
	if cr.opts.Search == "" {
		return nil
	}

	cost := (cr.opts.Max + PageSize - 1) / PageSize
	if !cr.afford(APIPlane.Name, cost, "the seed search") {
		return nil
	}
	papers, err := cr.c.Search(ctx, SearchOptions{
		Raw:   cr.opts.Search,
		Limit: cr.opts.Max,
		Sort:  string(SortSubmitted),
		Order: string(Descending),
	})
	if err != nil {
		return err
	}
	cr.man.Searches++
	cr.say("the seed search found %d papers", len(papers))
	for i := range papers {
		// A search result is a sighting and its claims, not a read. The feed
		// carries what depth quick carries, and filing it as the record would
		// take the paper off the frontier at a depth the crawl was not asked
		// for. At depth quick it is exactly the read that was asked for, so it
		// is filed and not fetched again.
		if cr.opts.Depth == DepthQuick {
			if err := cr.file(papers[i]); err != nil {
				return err
			}
			cr.man.Papers++
			cr.tried[graph.Paper(papers[i].ID)] = true
		}
		if err := cr.keep(EdgesOfPaper(papers[i])); err != nil {
			return err
		}
	}
	return nil
}

// hop reads one round of the frontier and says how many nodes it read.
//
// The order is doc 04 section 3.3: papers first, because two hundred of them
// are one request and everything else is one request each, so anything that
// went first would be spending the budget that the cheap read was going to use.
func (cr *Crawler) hop(ctx context.Context, n int) int {
	did := 0
	if uris := cr.frontier(graph.KindPaper); len(uris) > 0 {
		did += cr.readPapers(ctx, uris, n == 1)
	}
	if uris := cr.frontier(graph.KindCategory); len(uris) > 0 {
		did += cr.readCategories(ctx, uris)
	}
	if uris := cr.frontier(graph.KindName); len(uris) > 0 {
		did += cr.readNames(ctx, uris)
	}
	if did > 0 {
		cr.say("hop %d read %d nodes for %d api and %d html requests",
			n, did, cr.spent[APIPlane.Name], cr.spent[HTMLPlane.Name])
	}
	return did
}

// frontier is the unread nodes of one kind this hop should go after.
//
// Without --resume it is what this run reached, so a crawl of one paper does
// not wander off into last week's crawl. With it, the store's own frontier
// comes too, oldest sighting first.
func (cr *Crawler) frontier(kind string) []string {
	var candidates []string
	for uri := range cr.reached {
		if k, ok := graph.KindOf(uri); ok && k == kind {
			candidates = append(candidates, uri)
		}
	}
	sort.Strings(candidates)

	if cr.opts.Resume {
		stored, err := cr.st.Frontier(kind, frontierPage)
		if err != nil {
			cr.say("the store's %s frontier could not be read: %s", kind, err)
		}
		candidates = append(candidates, stored...)
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, uri := range candidates {
		if seen[uri] || cr.tried[uri] || graph.IsVersion(uri) {
			continue
		}
		seen[uri] = true
		node, err := cr.st.Node(uri)
		if err != nil {
			cr.say("the store could not be asked about %s: %s", uri, err)
			continue
		}
		if node != nil && node.Read() {
			// Already read, in this run or in a previous one. Reading a node
			// again is what a fresh store is for.
			cr.tried[uri] = true
			continue
		}
		out = append(out, uri)
		if len(out) >= frontierPage {
			break
		}
	}
	return out
}

// readPapers drains the paper queue in one batched read, which is the cheapest
// thing on arXiv by a wide margin: two hundred papers for one request.
func (cr *Crawler) readPapers(ctx context.Context, uris []string, seedHop bool) int {
	depth := cr.depth()
	refs := make([]string, 0, len(uris))
	for _, uri := range uris {
		refs = append(refs, strings.TrimPrefix(uri, graph.Scheme+graph.KindPaper+"/"))
		cr.tried[uri] = true
	}

	api, html := depth.PlaneRequests()
	apiCost := len(BatchIDs(refs)) + len(refs)*(api-1)
	htmlCost := len(refs) * html
	if !cr.afford(APIPlane.Name, apiCost, "%d papers at depth %s", len(refs), depth) {
		return 0
	}
	if htmlCost > 0 && !cr.afford(HTMLPlane.Name, htmlCost, "the arxiv.org half of %d papers at depth %s", len(refs), depth) {
		return 0
	}

	papers, err := cr.c.PapersAt(ctx, refs, PaperOptions{Depth: depth})
	if err != nil {
		if cancelled(err) {
			return 0
		}
		cr.say("reading %d papers failed: %s", len(refs), err)
		cr.man.Errors++
		return 0
	}
	cr.man.Misses += len(refs) - len(papers)

	read := 0
	for i := range papers {
		if err := cr.file(papers[i]); err != nil {
			cr.say("%s could not be stored: %s", papers[i].ID, err)
			continue
		}
		if err := cr.keep(EdgesOfPaper(papers[i])); err != nil {
			cr.say("the claims of %s could not be stored: %s", papers[i].ID, err)
		}
		cr.man.Papers++
		read++
		cr.cite(ctx, papers[i])
		if seedHop {
			cr.trackbacks(ctx, papers[i])
		}
	}
	return read
}

// cite adds the bibliography, which lives on the rendering and nowhere else.
//
// At depth text the rendering has already been fetched by the read above, so
// this is a cache hit rather than a request. With the cache off it is a real
// request, and the read log will say so: the budget is counted from what went
// out, not from what was predicted.
func (cr *Crawler) cite(ctx context.Context, p Paper) {
	if !cr.depth().AtLeast(DepthText) || !p.HasHTML {
		return
	}
	full, err := cr.c.FullText(ctx, p.ID, FullTextOptions{})
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !cancelled(err) {
			cr.say("the rendering of %s could not be read: %s", p.ID, err)
		}
		return
	}
	edges, _ := EdgesOfFullText(full)
	if err := cr.keep(edges); err != nil {
		cr.say("the citations of %s could not be stored: %s", p.ID, err)
	}
}

// trackbacks reads the inbound links of a seed paper.
//
// Seeds only, and only when asked for. It is one request on the fifteen second
// plane per paper, so a batch of two hundred would be fifty minutes and almost
// all of it would come back empty.
func (cr *Crawler) trackbacks(ctx context.Context, p Paper) {
	if !cr.opts.Trackbacks || cr.opts.APIOnly {
		return
	}
	if !cr.afford(HTMLPlane.Name, 1, "the trackbacks of %s", p.ID) {
		return
	}
	tbs, err := cr.c.Trackbacks(ctx, p.ID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !cancelled(err) {
			cr.say("the trackbacks of %s could not be read: %s", p.ID, err)
		}
		return
	}
	for _, t := range tbs {
		if err := cr.keep(EdgesOfTrackback(t)); err != nil {
			cr.say("a trackback of %s could not be stored: %s", p.ID, err)
		}
	}
}

// readCategories expands categories out of the taxonomy, which is one table for
// all of them however many there are.
func (cr *Crawler) readCategories(ctx context.Context, uris []string) int {
	if cr.opts.APIOnly {
		cr.refuse("the category taxonomy", HTMLPlane.Name, 1)
		for _, uri := range uris {
			cr.tried[uri] = true
		}
		return 0
	}
	if !cr.afford(APIPlane.Name, 1, "the OAI set list") ||
		!cr.afford(HTMLPlane.Name, 1, "the category taxonomy") {
		return 0
	}
	cats, err := cr.c.Categories(ctx)
	if err != nil {
		if !cancelled(err) {
			cr.say("the taxonomy could not be read, so the categories stay unexpanded: %s", err)
			cr.man.Errors++
		}
		return 0
	}
	want := map[string]bool{}
	for _, uri := range uris {
		want[strings.TrimPrefix(uri, graph.Scheme+graph.KindCategory+"/")] = true
		cr.tried[uri] = true
	}
	read := 0
	for _, cat := range cats {
		if !want[cat.Code] {
			continue
		}
		if err := cr.file(cat); err != nil {
			cr.say("%s could not be stored: %s", cat.Code, err)
			continue
		}
		if err := cr.keep(EdgesOfCategory(cat)); err != nil {
			cr.say("the claims of %s could not be stored: %s", cat.Code, err)
		}
		cr.man.Categories++
		read++
	}
	return read
}

// readNames follows author names, one search each.
//
// This is the expansion that explodes, so it is opt in and it is drained last.
// A name search matches strings and nothing else, so what it finds may be
// somebody else's paper, and the claims it writes say s1 exactly like the ones
// the paper made about itself. That is why a name is never filed as a person:
// the store keeps the claims and does not pretend the name was identified.
func (cr *Crawler) readNames(ctx context.Context, uris []string) int {
	if !cr.opts.Names {
		cr.say("%d author names are off the frontier; --names follows them at one search each", len(uris))
		return 0
	}
	read := 0
	for _, uri := range uris {
		name, err := cr.st.Label(uri)
		if err != nil {
			cr.say("the spelling of %s could not be read: %s", uri, err)
			continue
		}
		if name == "" {
			// Only the slug is known, and searching for a slug finds nothing.
			cr.tried[uri] = true
			continue
		}
		// A name search is a count and then the search itself, and the search
		// pages at a hundred, so a limit of two hundred is three requests and
		// not one.
		cost := 1 + (cr.opts.Limit+PageSize-1)/PageSize
		if !cr.afford(APIPlane.Name, cost, "the search for %s", name) {
			return read
		}
		person, err := cr.c.AuthorByName(ctx, name, cr.opts.Limit)
		cr.tried[uri] = true
		if err != nil {
			if cancelled(err) {
				return read
			}
			cr.say("the search for %s failed: %s", name, err)
			cr.man.Errors++
			continue
		}
		cr.man.Searches++
		for i := range person.Papers {
			if err := cr.keep(EdgesOfPaper(person.Papers[i])); err != nil {
				cr.say("the claims of %s could not be stored: %s", person.Papers[i].ID, err)
			}
		}
		read++
	}
	return read
}

// ─── the small pieces ────────────────────────────────────────────────────────

// depth is the depth this crawl actually reads at, which --api-only clamps to
// the deepest that never touches arxiv.org.
func (cr *Crawler) depth() Depth {
	if !cr.opts.APIOnly {
		return cr.opts.Depth
	}
	if _, html := cr.opts.Depth.PlaneRequests(); html > 0 {
		return DepthMeta
	}
	return cr.opts.Depth
}

// file writes a record and remembers that its node is done.
func (cr *Crawler) file(record any) error {
	uri, err := cr.st.PutRecord(record)
	if err != nil {
		return err
	}
	cr.tried[uri] = true
	cr.reached[uri] = true
	return nil
}

// keep writes claims and adds everything they named to the frontier.
func (cr *Crawler) keep(edges []graph.Edge) error {
	if len(edges) == 0 {
		return nil
	}
	n, err := cr.st.PutClaims(edges)
	if err != nil {
		return err
	}
	cr.man.Claims += n
	for _, e := range edges {
		for _, end := range []string{e.From, e.To} {
			if _, ok := graph.KindOf(end); ok && !graph.IsVersion(end) {
				cr.reached[end] = true
			}
		}
	}
	return nil
}

// afford checks a plane's budget before a read rather than before a request, so
// the crawl finishes what it started or does not start it.
func (cr *Crawler) afford(plane string, cost int, what string, args ...any) bool {
	budget := cr.man.Budget[plane]
	left := budget - cr.spent[plane]
	if cost <= left {
		return true
	}
	cr.refuse(fmt.Sprintf(what, args...), plane, cost)
	return false
}

// refuse records that the budget would not pay for something, and says so.
func (cr *Crawler) refuse(what, plane string, need int) {
	left := cr.man.Budget[plane] - cr.spent[plane]
	if left < 0 {
		left = 0
	}
	cr.man.Refusals = append(cr.man.Refusals, Refusal{What: what, Plane: plane, Need: need, Left: left})
	cr.say("the %s budget has %d of %d requests left, so %s went unread",
		plane, left, cr.man.Budget[plane], what)
}

// say is the running commentary.
func (cr *Crawler) say(format string, args ...any) {
	if cr.opts.Progress != nil {
		cr.opts.Progress(format, args...)
		return
	}
	cr.c.notice(format, args...)
}

// cancelled reports whether an error is the context being done, which is not a
// failure and should not be logged as one.
func cancelled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// seedCode is what a category code looks like: cs.CL, hep-th, q-bio.NC.
var seedCode = regexp.MustCompile(`^[a-zA-Z][a-zA-Z-]*(\.[a-zA-Z-]{2,})?$`)

// ResolveSeed turns what a person typed into the URI of a node.
//
// A paper reference, a category code and an ax:// URI are all accepted, and
// nothing else is, because a seed that was silently misread is a crawl that
// spends its whole budget somewhere the user did not ask for.
func ResolveSeed(seed string) (string, error) {
	s := strings.TrimSpace(seed)
	if s == "" {
		return "", errs.Usage("a seed cannot be empty")
	}
	if strings.HasPrefix(s, graph.Scheme) {
		if _, ok := graph.KindOf(s); !ok {
			return "", errs.Usage("%q is not a kind of node this tool knows", s)
		}
		return s, nil
	}
	if id, err := axid.Parse(s); err == nil {
		return graph.Paper(id.Canonical), nil
	}
	if seedCode.MatchString(s) {
		return graph.Category(s), nil
	}
	return "", errs.Usage("%q is not a paper id, a category code or an ax:// uri", s)
}

// sortedKeys is a map's keys in order, so a plan prints the same way twice.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
