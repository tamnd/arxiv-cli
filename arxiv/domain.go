package arxiv

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the arxiv kit driver. It carries no state; the per-run Client is
// built by the factory Register hands kit.
//
// Every read is declared here once. That one registration gives the command
// line subcommand, the HTTP route under `arxiv serve`, and the MCP tool under
// `arxiv mcp`, all rendering the same records.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "arxiv",
		Hosts:  []string{Host, "export.arxiv.org"},
		Identity: kit.Identity{
			Binary: "arxiv",
			Short:  "A command line for arXiv papers.",
			Long: `A command line for arXiv papers.

Search arXiv and pull paper metadata, authors, and abstracts. No API key required.

  arxiv search "attention" --cat cs.CL    search a category
  arxiv paper 1706.03762                  one paper by id
  arxiv author "Yann LeCun" -n 20         papers under an author name
  arxiv categories                        the category codes`,
			Site: "https://" + Host,
			Repo: "https://github.com/tamnd/arxiv-cli",
		},
	}
}

// htmlRateFlag is the name of the second pacing flag. arXiv's two planes are
// five times apart, so one --rate cannot serve both.
const htmlRateFlag = "html-rate"

// Register installs the client factory, the extra global flag, and every
// operation onto app.
func (Domain) Register(app *kit.App) {
	// The flag binds to a variable owned by this registration rather than to
	// package state, so two apps in one process do not share a pace.
	htmlRate := HTMLPlane.Pace
	app.GlobalFlags(func(f *kit.FlagSet) {
		f.DurationVar(&htmlRate, htmlRateFlag, HTMLPlane.Pace,
			"minimum gap between arxiv.org requests (floor "+HTMLPlane.Floor.String()+")")
	})
	app.Finalize(func(c *kit.Config) {
		if c.Extra == nil {
			c.Extra = map[string]string{}
		}
		c.Extra[htmlRateFlag] = htmlRate.String()
	})
	app.SetClient(newClient)

	registerSearch(app)
	registerCount(app)
	registerList(app)
	registerNew(app)
	registerPaper(app)
	registerFullText(app)
	registerAuthor(app)
	registerCategories(app)
	registerCategory(app)
	registerSets(app)
	registerID(app)
	registerPlanes(app)
}

// newClient is the factory kit calls once per run. An unset framework flag
// leaves the library default in place.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if d, err := htmlRateOf(cfg); err != nil {
		return nil, err
	} else if d > 0 {
		c.HTMLRate = d
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	c.CacheDir = cfg.CacheDir
	c.NoCache = cfg.NoCache
	c.Verbose = cfg.Verbose
	// Stderr is the log whatever the verbosity, because the notices that ignore
	// -v need somewhere to go. logf still waits to be asked.
	c.Log = os.Stderr

	client, err := NewClient(c)
	if err != nil {
		// A pace under its floor is a flag the user typed, so it is a usage
		// error rather than a failure to build a client.
		return nil, errs.Usage("%s", err.Error())
	}
	return client, nil
}

// htmlRateOf reads the --html-rate value the finalize hook parked on the
// config. An unparseable value cannot come from the flag parser, so it can only
// come from a caller building a Config by hand, and it says so.
func htmlRateOf(cfg kit.Config) (time.Duration, error) {
	raw, ok := cfg.Extra[htmlRateFlag]
	if !ok || raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, errs.Usage("--%s %q is not a duration", htmlRateFlag, raw)
	}
	return d, nil
}

// searchIn is the flag set of `arxiv search`. Every query flag here maps to one
// clause of the grammar in spec 3006 doc 02 section 2.
//
// countIn below repeats the query half rather than embedding it, because kit
// binds the fields a struct declares and not the ones it promotes. A test
// asserts the two lists stay identical, which is the part that would otherwise
// drift.
type searchIn struct {
	Query    string   `kit:"arg" help:"what to search for, matched against every field"`
	Raw      string   `kit:"flag" help:"a query in arXiv's own grammar, sent unchanged"`
	Category []string `kit:"flag,name=cat" help:"a category code such as cs.LG, repeatable and OR'd"`
	Author   string   `kit:"flag" help:"match the author field"`
	Title    string   `kit:"flag" help:"match the title"`
	Abstract string   `kit:"flag" help:"match the abstract"`
	Comment  string   `kit:"flag" help:"match the author comment"`
	Journal  string   `kit:"flag" help:"match the journal reference"`
	Report   string   `kit:"flag" help:"match the report number"`

	ACMClass string `kit:"flag,name=acm-class" help:"match the ACM classification, search UI only"`
	MSCClass string `kit:"flag,name=msc-class" help:"match the MSC classification, search UI only"`
	DOI      string `kit:"flag,name=doi" help:"match the publisher DOI, search UI only"`
	ORCID    string `kit:"flag,name=orcid" help:"match an author ORCID, search UI only"`
	License  string `kit:"flag" help:"match the licence URI, search UI only"`
	AuthorID string `kit:"flag,name=author-id" help:"match an arXiv author identifier, search UI only"`
	FullText string `kit:"flag,name=full-text" help:"search paper bodies, which arXiv has moved off limits"`

	From        string `kit:"flag" help:"submitted on or after this date: 2026, 2026-01 or 2026-01-01"`
	To          string `kit:"flag" help:"submitted on or before this date, inclusive to the end of the period"`
	UpdatedFrom string `kit:"flag,name=updated-from" help:"last updated on or after this date"`
	UpdatedTo   string `kit:"flag,name=updated-to" help:"last updated on or before this date"`

	Sort   string  `kit:"flag" enum:"relevance,submitted,updated" help:"sort order, relevance by default and submitted under --all"`
	Order  string  `kit:"flag" default:"desc" enum:"desc,asc" help:"sort direction"`
	All    bool    `kit:"flag" help:"walk the whole result set, past the ten thousand result window"`
	Limit  int     `kit:"flag,short=n,inherit" help:"how many papers to return"`
	Client *Client `kit:"inject"`
}

// options is the library form of the flags.
func (in searchIn) options() SearchOptions {
	return SearchOptions{
		Query:       in.Query,
		Raw:         in.Raw,
		Categories:  in.Category,
		Author:      in.Author,
		Title:       in.Title,
		Abstract:    in.Abstract,
		Comment:     in.Comment,
		Journal:     in.Journal,
		Report:      in.Report,
		ACMClass:    in.ACMClass,
		MSCClass:    in.MSCClass,
		DOI:         in.DOI,
		ORCID:       in.ORCID,
		License:     in.License,
		AuthorID:    in.AuthorID,
		FullText:    in.FullText,
		From:        in.From,
		To:          in.To,
		UpdatedFrom: in.UpdatedFrom,
		UpdatedTo:   in.UpdatedTo,
		Sort:        in.Sort,
		Order:       in.Order,
		Limit:       in.Limit,
		All:         in.All,
	}
}

func registerSearch(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		URIType: "paper",
		Summary: "Search arXiv papers",
		Args:    []kit.Arg{{Name: "query", Help: "what to search for", Optional: true}},
		Long: `Search arXiv papers.

The positional query is matched against every indexed field. The field flags
are arXiv's own search prefixes under readable names, so --title attention
--author vaswani sends ti:attention AND au:vaswani, and -v prints both the
query that was built and the URL it went out on.

--cat takes a category code and repeats, and the codes are OR'd together. A
bare archive code such as hep-th or cs matches whether or not that archive was
split into categories. A code arXiv does not have is refused before any request
goes out, because arXiv answers a wrong code with zero results and no error,
which reads as an empty category rather than as a typo.

The date flags take 2026, 2026-01 or 2026-01-01. A bound names a period rather
than an instant, so --to 2026-01 means the end of January and not the start of
it.

--raw sends a query through untouched, for the parts of the grammar the flags
do not cover: parentheses, OR, ANDNOT and quoted phrases.

--all walks the whole result set. arXiv will not page past ten thousand
results, so a bigger set is cut into date slices that each fit, and the walk
runs in submission order because relevance is recomputed on every request and a
walk ordered by it would both repeat and skip papers.

Seven fields live in arXiv's search UI and not in its API: --acm-class,
--msc-class, --doi, --orcid, --license, --author-id and --full-text. Naming any
of them sends the whole query to the search UI on the fifteen second plane
instead, and -v says so. That route cannot take --cat, --raw or the two updated
date flags, and each refusal explains itself.

To learn only how many results there are, use arxiv count, which costs one
request.`,
	}, func(ctx context.Context, in searchIn, emit func(*Paper) error) error {
		if err := in.Client.SearchStream(ctx, in.options(), emit); err != nil {
			return mapErr(err)
		}
		return nil
	})
}

// countIn is searchIn without the ordering flags, which a count has no use for.
type countIn struct {
	Query    string   `kit:"arg" help:"what to search for, matched against every field"`
	Raw      string   `kit:"flag" help:"a query in arXiv's own grammar, sent unchanged"`
	Category []string `kit:"flag,name=cat" help:"a category code such as cs.LG, repeatable and OR'd"`
	Author   string   `kit:"flag" help:"match the author field"`
	Title    string   `kit:"flag" help:"match the title"`
	Abstract string   `kit:"flag" help:"match the abstract"`
	Comment  string   `kit:"flag" help:"match the author comment"`
	Journal  string   `kit:"flag" help:"match the journal reference"`
	Report   string   `kit:"flag" help:"match the report number"`

	ACMClass string `kit:"flag,name=acm-class" help:"match the ACM classification, search UI only"`
	MSCClass string `kit:"flag,name=msc-class" help:"match the MSC classification, search UI only"`
	DOI      string `kit:"flag,name=doi" help:"match the publisher DOI, search UI only"`
	ORCID    string `kit:"flag,name=orcid" help:"match an author ORCID, search UI only"`
	License  string `kit:"flag" help:"match the licence URI, search UI only"`
	AuthorID string `kit:"flag,name=author-id" help:"match an arXiv author identifier, search UI only"`
	FullText string `kit:"flag,name=full-text" help:"search paper bodies, which arXiv has moved off limits"`

	From        string `kit:"flag" help:"submitted on or after this date: 2026, 2026-01 or 2026-01-01"`
	To          string `kit:"flag" help:"submitted on or before this date, inclusive to the end of the period"`
	UpdatedFrom string `kit:"flag,name=updated-from" help:"last updated on or after this date"`
	UpdatedTo   string `kit:"flag,name=updated-to" help:"last updated on or before this date"`

	Client *Client `kit:"inject"`
}

// options is the library form of the flags. A count has no ordering, so the
// sort is left at its default and never reaches the wire in a way that matters.
func (in countIn) options() SearchOptions {
	return SearchOptions{
		Query:       in.Query,
		Raw:         in.Raw,
		Categories:  in.Category,
		Author:      in.Author,
		Title:       in.Title,
		Abstract:    in.Abstract,
		Comment:     in.Comment,
		Journal:     in.Journal,
		Report:      in.Report,
		ACMClass:    in.ACMClass,
		MSCClass:    in.MSCClass,
		DOI:         in.DOI,
		ORCID:       in.ORCID,
		License:     in.License,
		AuthorID:    in.AuthorID,
		FullText:    in.FullText,
		From:        in.From,
		To:          in.To,
		UpdatedFrom: in.UpdatedFrom,
		UpdatedTo:   in.UpdatedTo,
	}
}

func registerCount(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "count",
		Group:   "read",
		Single:  true,
		URIType: "count",
		Summary: "Count the results a query has",
		Args:    []kit.Arg{{Name: "query", Help: "what to search for", Optional: true}},
		Long: `Count the results a query has, without fetching any of them.

Takes the same flags as arxiv search. One request, and the number comes from
the opensearch:totalResults element the API puts on every feed, so this is the
cheapest way to find out whether a query is worth running.

The request asks for one result rather than none, because max_results=0 answers
500 with an internal error.`,
	}, func(ctx context.Context, in countIn, emit func(*Count) error) error {
		count, err := in.Client.CountSearch(ctx, in.options())
		if err != nil {
			return mapErr(err)
		}
		return emit(&count)
	})
}

// listIn is the flag set of `arxiv list`. --skip and --show keep arXiv's own
// names, because they are what the URL carries and what the page prints, and a
// user who has the page open should not have to translate.
type listIn struct {
	Category string  `kit:"arg" help:"a category code such as cs.CL"`
	Month    string  `kit:"arg" help:"the month to list, as 2026-01"`
	Recent   bool    `kit:"flag" help:"the last few announcement days instead of a month"`
	Skip     int     `kit:"flag" help:"how many entries to skip"`
	Show     int     `kit:"flag" help:"entries per page: 25, 50, 100, 250, 500, 1000 or 2000"`
	All      bool    `kit:"flag" help:"walk every page at the fifteen second pace"`
	Client   *Client `kit:"inject"`
}

func registerList(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "list",
		Group:   "read",
		List:    true,
		URIType: "paper",
		Summary: "Browse a category listing by month",
		Args: []kit.Arg{
			{Name: "category", Help: "a category code such as cs.CL"},
			{Name: "month", Help: "the month to list, as 2026-01", Optional: true},
		},
		Long: `Browse a category listing by month.

This is arXiv's own listing page rather than a search: it is every paper filed
under a category in one month, in arXiv's own order, which is what a person
browsing the archive sees. A search of the same category and month answers a
different question and returns a different set in a different order.

The month is 2026-01. The four digit form 2601 that older guides document is
gone, and arXiv answers it with a 404, so it is refused here with the form to
type instead. With no month and no --recent this lists the recent submissions.

Paging is --skip and --show, which are arXiv's own parameters. arXiv accepts
25, 50, 100, 250, 500, 1000 and 2000 entries a page and answers anything else
with an HTTP 400, so a --show it would refuse is refused here first.

--all walks every page. This is the fifteen second plane, so the walk says how
many requests it will make and how long that is before it starts. There is no
ten thousand result window here, which makes this the only way to read a whole
month of a busy category.

A listing row has no abstract and no submission time. Each record names what it
is missing and which command reads it.`,
	}, func(ctx context.Context, in listIn, emit func(*Paper) error) error {
		err := in.Client.ListStream(ctx, ListOptions{
			Category: in.Category,
			Month:    in.Month,
			Recent:   in.Recent,
			Skip:     in.Skip,
			Show:     in.Show,
			All:      in.All,
		}, emit)
		if err != nil {
			return mapErr(err)
		}
		return nil
	})
}

type newIn struct {
	Category string   `kit:"arg" help:"a category code such as cs.CL"`
	Type     []string `kit:"flag" help:"only this announce type, repeatable: new, cross, replace, replace-cross"`
	NewOnly  bool     `kit:"flag,name=new-only" help:"only first announcements, the same as --type new"`
	Client   *Client  `kit:"inject"`
}

func registerNew(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "new",
		Group:   "read",
		List:    true,
		URIType: "announcement",
		Summary: "Read today's announcement for a category",
		Args:    []kit.Arg{{Name: "category", Help: "a category code such as cs.CL"}},
		Long: `Read today's announcement for a category, off arXiv's RSS feed.

Every item carries an announce type, which is the one field no other arXiv
surface publishes: new is a first announcement, cross is a paper primarily in
another category, replace is a new version of a paper already announced here,
and replace-cross is a new version of a cross listed one.

That distinction is the reason this is a command rather than a feed dump. The
cs.CL feed read on 2026-08-13 had 139 items and 47 of them were replacements,
so a reader who cannot filter is being handed a third of a day's noise.
--type takes any of the four and repeats, and --new-only is the short way to
say --type new. The count under the table is of the whole feed, not of what
survived the filter.

arXiv announces on weekdays and the feed says so itself, so a read on a Sunday
returns Friday's announcement rather than nothing, and the cached copy is kept
until the feed's own pubDate says the next announcement is due.`,
	}, func(ctx context.Context, in newIn, emit func(*Announcement) error) error {
		types := in.Type
		if in.NewOnly {
			types = append(types, AnnounceNew)
		}
		items, err := in.Client.Announcements(ctx, FeedOptions{Category: in.Category, Types: types})
		if err != nil {
			return mapErr(err)
		}
		return emitAll(items, emit)
	})
}

type paperIn struct {
	ID     string  `kit:"arg" help:"an arXiv id, a versioned id, or an abs URL"`
	Depth  string  `kit:"flag" default:"meta" enum:"quick,meta,full,text" help:"how many surfaces to read"`
	Client *Client `kit:"inject"`
}

func registerPaper(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "paper",
		Group:   "read",
		Single:  true,
		URIType: "paper",
		Summary: "Fetch a single paper by arXiv id",
		Args:    []kit.Arg{{Name: "id", Help: "an arXiv id, a versioned id, or an abs URL"}},
		Long: `Fetch a single paper by its arXiv id.

The id may be bare (1706.03762), versioned (1706.03762v7), an old-style id
(hep-th/9711200), or a full URL.

--depth chooses how many of arXiv's surfaces to read. quick is one request to
the export API. meta adds the OAI record, which is where the report number, the
subject classes and the structured author names live. full adds the version
history and the abstract page, and the abstract page is on the fifteen second
plane, so it costs fifteen seconds a paper. Whatever a depth did not look at is
named in the missed field rather than left as a zero.`,
	}, func(ctx context.Context, in paperIn, emit func(*Paper) error) error {
		depth, err := ParseDepth(in.Depth)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		paper, err := in.Client.PaperAt(ctx, in.ID, PaperOptions{Depth: depth})
		if err != nil {
			return mapErr(err)
		}
		return emit(&paper)
	})
}

type fullTextIn struct {
	Ref      string  `kit:"arg" help:"an arXiv id, a versioned id, or an abs URL"`
	Sections bool    `kit:"flag" help:"the section tree with no body text, which is a table of contents"`
	Text     bool    `kit:"flag" help:"write the body to stdout as plain text in reading order"`
	Section  string  `kit:"flag" help:"one section and its children, by id, such as S3 or S3.SS1"`
	Refs     bool    `kit:"flag" help:"the bibliography instead of the body"`
	Client   *Client `kit:"inject"`
}

func registerFullText(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "fulltext",
		Group:   "read",
		Single:  true,
		URIType: "fulltext",
		Summary: "Read the LaTeXML full text of a paper",
		Args:    []kit.Arg{{Name: "ref", Help: "an arXiv id, a versioned id, or an abs URL"}},
		Long: `Read the LaTeXML rendering of a paper.

arXiv renders LaTeX submissions to HTML at arxiv.org/html/<id>v<n>, and that
rendering is the only place arXiv publishes author affiliations, a section tree
or the body of a paper at all.

The read is the abstract page and then the rendering, because has_html is on the
abstract page and it is the only honest way to know whether a rendering exists.
That is two requests on the fifteen second plane, so the command takes half a
minute the first time and nothing at all the second: a rendering never changes,
so it is cached for a month.

A paper arXiv never rendered exits 7 and says so. arXiv renders papers submitted
since December 2023 and some earlier ones, and there is no pattern to the
earlier ones worth guessing at.

Maths comes back as the LaTeX the author wrote, taken from the alttext
attribute, because a downstream reader can parse that and cannot parse rendered
MathML.`,
	}, func(ctx context.Context, in fullTextIn, emit func(*FullText) error) error {
		full, err := in.Client.FullText(ctx, in.Ref, FullTextOptions{
			Sections: in.Sections,
			Section:  in.Section,
			Refs:     in.Refs,
		})
		if err != nil {
			return mapErr(err)
		}
		// --text is the one output this tool writes as text rather than as a
		// record. A body is prose and a record of prose is a record with one
		// enormous field in it, which is worse to read and worse to pipe.
		if in.Text {
			_, err := fmt.Fprintln(textOut, full.PlainText())
			return err
		}
		return emit(&full)
	})
}

// textOut is where --text writes. It is a variable so a test can read what a
// run printed.
var textOut io.Writer = os.Stdout

type authorIn struct {
	Ref    string  `kit:"arg" help:"an author name, or an arXiv author identifier with --id"`
	ID     bool    `kit:"flag,name=id" help:"read the author identifier page instead of searching the name"`
	Limit  int     `kit:"flag,short=n,inherit" default:"10" help:"how many papers to return on a name search"`
	Client *Client `kit:"inject"`
}

func registerAuthor(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "author",
		Group:   "read",
		Single:  true,
		URIType: "author",
		Summary: "Look up an author by name or by arXiv identifier",
		Args:    []kit.Arg{{Name: "ref", Help: "an author name, or an arXiv author identifier with --id"}},
		Long: `Look up an author, either as a name or as a registered person.

Two lookups wear one name here and the record says which one ran.

Without --id the name goes to the arXiv au: field prefix, which is a string
match on text somebody typed. Two people share a name, one person publishes
under three spellings, and identified is false on the record because a name
match is not a person.

With --id it reads arxiv.org/a/<identifier>.html, which is arXiv asserting that
a registered person owns a set of papers, and it is the only surface anywhere on
arXiv that carries an ORCID. identified is true on that record.

An identifier looks like baez_j_1 and is never guessed from a name: the number
on the end is arXiv's own way of telling two people with the same surname and
initial apart. A 404 means the author never registered a page, which says
nothing about whether they have papers, and the message says so.`,
	}, func(ctx context.Context, in authorIn, emit func(*Person) error) error {
		var (
			person Person
			err    error
		)
		if in.ID {
			person, err = in.Client.AuthorByID(ctx, in.Ref)
		} else {
			person, err = in.Client.AuthorByName(ctx, in.Ref, in.Limit)
		}
		if err != nil {
			return mapErr(err)
		}
		return emit(&person)
	})
}

type categoriesIn struct {
	Group  string  `kit:"flag" help:"only categories in this group, matched loosely: physics, cs, math"`
	Search string  `kit:"flag" help:"only categories whose code, name or description contains this"`
	Client *Client `kit:"inject"`
}

func registerCategories(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "categories",
		Group:   "read",
		List:    true,
		URIType: "category",
		Summary: "List the arXiv categories",
		Long: `List the arXiv categories, with their descriptions.

All 155 of them, read from arxiv.org/category_taxonomy and joined to the 183
OAI sets, so each one carries the set spec it is harvested as. Both tables are
cached for a week and both ship with the binary, so a first run with no network
still prints the list and says which day the bundled copy was saved.

--search looks in the description as well as the code and the name, which is
what makes the list useful to somebody who does not already know that game
theory is cs.GT and econ.TH and math.OC.`,
	}, func(ctx context.Context, in categoriesIn, emit func(*Category) error) error {
		cats, err := in.Client.Categories(ctx)
		if err != nil {
			return mapErr(err)
		}
		return emitAll(filterCategories(cats, in.Group, in.Search), emit)
	})
}

// filterCategories applies --group and --search.
//
// The group match is loose because the group is spelled out on the page and
// nobody types "Electrical Engineering and Systems Science": eess, or a word of
// it, is what a person has in mind.
func filterCategories(cats []Category, group, search string) []Category {
	group = strings.ToLower(strings.TrimSpace(group))
	search = strings.ToLower(strings.TrimSpace(search))
	out := make([]Category, 0, len(cats))
	for _, c := range cats {
		if group != "" && !strings.Contains(strings.ToLower(c.Group), group) && !strings.EqualFold(c.Archive, group) &&
			!strings.HasPrefix(strings.ToLower(c.Code), group+".") {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(c.Code), search) &&
			!strings.Contains(strings.ToLower(c.Name), search) &&
			!strings.Contains(strings.ToLower(c.Description), search) {
			continue
		}
		out = append(out, c)
	}
	return out
}

type categoryIn struct {
	Code   string  `kit:"arg" help:"a category code such as cs.CL"`
	Count  bool    `kit:"flag" help:"also count the papers submitted in the last thirty days"`
	Client *Client `kit:"inject"`
}

func registerCategory(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "category",
		Group:   "read",
		Single:  true,
		URIType: "category",
		Summary: "Show one category",
		Args:    []kit.Arg{{Name: "code", Help: "a category code such as cs.CL"}},
		Long: `Show one category: its name, its group, its archive, its description and the
OAI set spec it is harvested as.

The set spec is a join and not a rewrite. cs.CL is cs:cs:CL and hep-th is
physics:hep-th with no middle segment, because hep-th is an archive that was
never split into categories.

--count adds the number of papers submitted to the category in the last thirty
days, which costs one request to the API and is a different number tomorrow.`,
	}, func(ctx context.Context, in categoryIn, emit func(*Category) error) error {
		cat, err := in.Client.Category(ctx, in.Code, in.Count)
		if err != nil {
			return mapErr(err)
		}
		return emit(&cat)
	})
}

type setsIn struct {
	Client *Client `kit:"inject"`
}

func registerSets(app *kit.App) {
	kit.Handle(app, kit.OpMeta{
		Name:    "sets",
		Group:   "read",
		List:    true,
		URIType: "set",
		Summary: "List the OAI-PMH sets",
		Long: `List the OAI-PMH sets, which are what a harvest is scoped by.

The response carries 183 sets and nine of them are listed twice, once as an
archive and once as a category, so the list here is the 174 distinct ones. Each
set names the category it harvests where there is one; an archive level set such
as physics:cond-mat has none, because it is a container for the categories under
it.`,
	}, func(ctx context.Context, in setsIn, emit func(*Set) error) error {
		sets, err := in.Client.Sets(ctx)
		if err != nil {
			return mapErr(err)
		}
		return emitAll(sets, emit)
	})
}

// emitAll streams a slice through the emit callback by pointer, so nothing is
// copied on the way out.
func emitAll[T any](records []T, emit func(*T) error) error {
	for i := range records {
		if err := emit(&records[i]); err != nil {
			return err
		}
	}
	return nil
}
