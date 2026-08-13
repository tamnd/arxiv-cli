package arxiv

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// s11 is the only inbound link data arXiv publishes.
//
// Everything else in this tool runs outward from a paper: its authors, its
// categories, the papers it cites. This runs inward, from an external page to
// the paper, and that is a genuine edge and not a derived one. Doc 04 section 2
// has it as linked_by, pointing from the page to the paper.
//
// robots.txt disallows /tb, /tb-recent and /trackback. So this is only ever
// reached by an explicit request for a named paper or for the recent feed, and
// the crawler is forbidden from touching it. That is enforced in code rather
// than left to good manners.

const (
	trackbackBase   = "https://" + Host + "/tb/"
	trackbackRecent = trackbackBase + "recent"
)

// TTLTrackback is a day. A trackback arrives when somebody blogs about a
// paper, which is not something that happens twice in an afternoon.
const TTLTrackback = 24 * time.Hour

// Trackback is one external page linking to one paper.
type Trackback struct {
	Envelope

	// PaperID is the paper that was linked to, canonical.
	PaperID string `json:"paper_id" kit:"id" table:"paper"`
	// PaperTitle is only on the recent feed, which prints the title of every
	// paper a post links to. A per paper page does not repeat it.
	PaperTitle string `json:"paper_title,omitempty" table:"-"`
	// Title is the external page's own title, as the blog wrote it.
	Title string `json:"title" table:"title,truncate"`
	// BlogName is the site the page is on.
	BlogName string `json:"blog_name,omitempty" table:"blog"`
	// BlogURL is the site's own address. arXiv stores the string
	// "INVALID-URL" for every trackback measured on 2026-08-14, so this is
	// almost always empty and the field says so by being absent rather than by
	// carrying arXiv's placeholder.
	BlogURL string `json:"blog_url,omitempty" table:"-"`
	// URL is arXiv's redirect, which is what the page links through. It is not
	// the external address: reaching that means following the redirect, which
	// is a request on the fifteen second plane, so it happens only when asked.
	URL string `json:"url" table:"-,url"`
	// TargetURL is where the redirect goes, filled by --resolve.
	TargetURL string `json:"target_url,omitempty" table:"-"`
	// TrackbackID is arXiv's own number for this ping, off the redirect path.
	TrackbackID string `json:"trackback_id,omitempty" table:"-"`
	// PostedAt is when the ping arrived, to the second.
	//
	// The per paper page gives a full timestamp. The recent feed gives a day
	// and nothing finer, so on those rows this is zero and PostedDate carries
	// the day. Writing midnight into a timestamp would be a specific false
	// claim where a date is a vague true one, which is the same rule
	// announced_month follows on a paper.
	PostedAt time.Time `json:"posted_at,omitzero" table:"posted,time"`
	// PostedDate is the day, always set, as YYYY-MM-DD.
	PostedDate string `json:"posted_date,omitempty" table:"-"`
}

// TrackbackURI is the node for one ping, so two reads of the same trackback
// land on the same node.
func TrackbackURI(id string) string { return "ax://trackback/" + id }

// trackbackTimeLayout is the page's own format.
//
// It is RFC 1123 with a zone name of UTC, which time.RFC1123 does not accept
// because that layout wants MST and Go matches the abbreviation's length. The
// day of the month is not zero padded either, so "5 Dec" and "30 Nov" both
// appear and the layout has to use 2 rather than 02.
const trackbackTimeLayout = "Mon, 2 Jan 2006 15:04:05 MST"

// redirectIDRe pulls the ping's number out of /tb/redirect/1845295/8591d4034.
// The second component is a signature and is not an identifier for anything.
var redirectIDRe = regexp.MustCompile(`/tb/redirect/(\d+)/`)

// absIDRe reads a paper id out of an /abs link on the recent feed.
var absIDRe = regexp.MustCompile(`/abs/(.+)$`)

// Trackbacks reads the pings recorded for one paper.
//
// An empty page is not an error. "No external page has linked to this paper"
// is a true answer to the question, and it is the answer for most papers.
func (c *Client) Trackbacks(ctx context.Context, ref string) ([]Trackback, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return nil, err
	}
	u := trackbackBase + id.Canonical
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLTrackback)
	if err != nil {
		if errs.KindOf(err) == errs.KindNotFound {
			// arXiv answers 404 with "Article identifier not recognized" for a
			// well formed id it has no paper for, which is a different answer
			// from the page that says there are no pings.
			return nil, errs.NotFound("arxiv has no paper %s, so it has no trackbacks for it", id.Canonical)
		}
		return nil, err
	}
	return parseTrackbacks(resp.Body, id.Canonical, u, c.now().UTC())
}

// parseTrackbacks reads one paper's page.
func parseTrackbacks(body []byte, paperID, source string, at time.Time) ([]Trackback, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, errs.Wrap(errs.KindGeneric, err, "parse %s", source)
	}
	out := make([]Trackback, 0, 8)
	doc.Find("h3.trackback-title").Each(func(_ int, h *goquery.Selection) {
		tb := Trackback{
			Envelope: Envelope{Kind: "trackback", RetrievedAt: at},
			PaperID:  paperID,
		}
		link := h.Find("a").First()
		tb.Title = squash(link.Text())
		if href, ok := link.Attr("href"); ok {
			tb.URL = absoluteTrackbackURL(href)
			tb.TrackbackID = trackbackIDOf(href)
		}
		readTrackbackSource(&tb, h.NextFiltered("p.trackback-source"))
		tb.addSurface(SurfaceTrackback, source)
		out = append(out, tb)
	})
	return out, nil
}

// RecentTrackbacks reads the site-wide feed, which is the same data the other
// way round: a post, and the papers it links.
//
// One record per pair, because a post that links three papers is three claims
// and flattening them into one row would make the third invisible to anything
// that joins on a paper id.
//
// views is what arXiv's own form offers, 25, 50, 100 or 200.
func (c *Client) RecentTrackbacks(ctx context.Context, views int) ([]Trackback, error) {
	if views == 0 {
		views = 25
	}
	if !recentViews[views] {
		return nil, errs.Usage("--views takes 25, 50, 100 or 200, which is what arxiv's own form offers")
	}
	u := fmt.Sprintf("%s?views=%d", trackbackRecent, views)
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLSearch)
	if err != nil {
		return nil, err
	}
	return parseRecentTrackbacks(resp.Body, u, c.now().UTC())
}

// parseRecentTrackbacks reads the site-wide feed.
func parseRecentTrackbacks(body []byte, source string, at time.Time) ([]Trackback, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, errs.Wrap(errs.KindGeneric, err, "parse %s", source)
	}
	out := make([]Trackback, 0, 32)
	// The feed is a column per day: a heading with the date, and beside it the
	// posts that arrived on it. A second post on the same day gets a column of
	// its own with the heading left blank, so the date has to be carried
	// forward rather than read off each row. Twenty five posts came back under
	// twenty one headings on 2026-08-14, and reading the heading per row would
	// have silently dropped the date from four of them.
	posted := ""
	doc.Find("div.columns").Each(func(_ int, col *goquery.Selection) {
		if day := squash(col.Find("h3.bold-divided-header").First().Text()); day != "" {
			posted = parseTrackbackDay(day)
		}
		if posted == "" {
			return
		}
		col.Find("h3.trackback-title").Each(func(_ int, h *goquery.Selection) {
			base := Trackback{
				Envelope:   Envelope{Kind: "trackback", RetrievedAt: at},
				PostedDate: posted,
			}
			link := h.Find("a").First()
			base.Title = squash(link.Text())
			if href, ok := link.Attr("href"); ok {
				base.URL = absoluteTrackbackURL(href)
				base.TrackbackID = trackbackIDOf(href)
			}
			readTrackbackSource(&base, h.NextFiltered("p.trackback-source"))
			base.Missed = []string{"the recent feed gives the day a ping arrived and not the time of day"}
			base.addSurface(SurfaceTrackback, source)

			// The papers are in the list that follows the source line. The
			// markup wraps a heading inside a paragraph, which the HTML parser
			// unwraps, so the list is a following sibling of the heading and
			// not a child of anything. It is taken from the siblings before the
			// next heading so a post with no list cannot borrow the next one's.
			list := h.NextUntil("h3.trackback-title").Filter("ul")
			list.Find("li").Each(func(_ int, li *goquery.Selection) {
				tb := base
				href, _ := li.Find("a").First().Attr("href")
				// The row is a title in a span the markup never closes, so the
				// id's own link is inside it and comes out on the end of the
				// title text as "Title [2608.02757]".
				title := squash(li.Text())
				if i := strings.LastIndex(title, " ["); i > 0 {
					title = title[:i]
				}
				tb.PaperTitle = title
				m := absIDRe.FindStringSubmatch(href)
				if m == nil {
					return
				}
				id, err := axid.Parse(m[1])
				if err != nil {
					return
				}
				tb.PaperID = id.Canonical
				out = append(out, tb)
			})
		})
	})
	return out, nil
}

// recentViews is the set arXiv's form offers. Anything else is a number the
// page will quietly ignore, and a flag that is quietly ignored is worse than
// one that is refused.
var recentViews = map[int]bool{25: true, 50: true, 100: true, 200: true}

// invalidURL is what arXiv writes where a blog's own address should be.
const invalidURL = "INVALID-URL"

// readTrackbackSource fills the blog name, the blog URL and the timestamp from
// the source line, which reads:
//
//	[    Of Particular Significance@
//	    INVALID-URL
//	]    <small>trackback posted Mon, 5 Dec 2022 13:13:00 UTC</small>
func readTrackbackSource(tb *Trackback, sel *goquery.Selection) {
	if sel.Length() == 0 {
		return
	}
	stamp := squash(sel.Find("small").Text())
	if rest, ok := strings.CutPrefix(stamp, "trackback posted "); ok {
		if t, err := time.Parse(trackbackTimeLayout, rest); err == nil {
			tb.PostedAt = t.UTC()
			tb.PostedDate = t.UTC().Format("2006-01-02")
		}
	}

	// The small element holds the timestamp, so what is left of the paragraph
	// is the bracketed source.
	clone := sel.Clone()
	clone.Find("small").Remove()
	text := squash(clone.Text())
	text = strings.TrimSuffix(strings.TrimPrefix(text, "["), "]")
	text = strings.TrimSpace(text)
	name, rawURL := text, ""
	if i := strings.LastIndex(text, "@"); i >= 0 {
		name = strings.TrimSpace(text[:i])
		rawURL = strings.TrimSpace(text[i+1:])
	}
	// arXiv stores a placeholder rather than a URL for every trackback probed
	// on 2026-08-14, and the placeholder is not an address. An absent field
	// says that better than a field holding the word INVALID-URL.
	//
	// A few old rows are the placeholder and nothing else, with no name and no
	// separator, and those have no blog name either.
	if name != invalidURL {
		tb.BlogName = name
	}
	if rawURL != "" && rawURL != invalidURL {
		tb.BlogURL = rawURL
	}
}

// Resolve follows the arXiv redirect to the page that sent the ping.
//
// It is one request per trackback on the fifteen second plane, which is why it
// is opt-in and why the caller is told what it will cost before it starts. A
// paper with a hundred and twenty trackbacks, and 1706.03762 has that many, is
// half an hour.
func (c *Client) Resolve(ctx context.Context, tbs []Trackback) error {
	for i := range tbs {
		if tbs[i].URL == "" || tbs[i].TargetURL != "" {
			continue
		}
		target, err := c.redirectTarget(ctx, tbs[i].URL)
		if err != nil {
			// One dead redirect does not fail the read. The trackback is still
			// a real claim, it just does not say where it points.
			c.logf(1, "resolve %s: %v", tbs[i].URL, err)
			tbs[i].Missed = append(tbs[i].Missed,
				fmt.Sprintf("arxiv did not say where %s goes", tbs[i].URL))
			continue
		}
		tbs[i].TargetURL = target
		tbs[i].setVia("target_url", SurfaceTrackback)
	}
	return nil
}

// redirectTarget asks for a redirect and reads the Location without following
// it, so the external site is never contacted. This tool talks to arXiv.
func (c *Client) redirectTarget(ctx context.Context, rawURL string) (string, error) {
	if body, ok := c.cache.get(rawURL, TTLTrackback); ok {
		c.logf(2, "cache hit %s", rawURL)
		return string(body), nil
	}
	_, lim, err := c.planeFor(Host)
	if err != nil {
		return "", err
	}
	if err := lim.wait(ctx); err != nil {
		return "", ctxErr(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", errs.Wrap(errs.KindGeneric, err, "build request")
	}
	req.Header.Set("User-Agent", c.userAgent)
	c.logf(1, "HEAD %s", rawURL)

	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctxErr(ctx.Err())
		}
		return "", errs.Wrap(errs.KindNetwork, err, "head %s", rawURL)
	}
	defer func() { _ = resp.Body.Close() }()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errs.New(errs.KindGeneric, "arxiv answered %d with no location for %s", resp.StatusCode, rawURL)
	}
	c.cache.put(rawURL, []byte(loc))
	return loc, nil
}

// absoluteTrackbackURL turns the page's /tb/redirect/... into a full URL.
func absoluteTrackbackURL(href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	return "https://" + Host + href
}

// trackbackIDOf reads arXiv's number for a ping off its redirect path.
func trackbackIDOf(href string) string {
	if m := redirectIDRe.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}

// parseTrackbackDay turns the recent feed's heading, "August 13, 2026", into a
// date. An unparseable heading is returned as arXiv wrote it rather than
// dropped, because a date this tool cannot read is still a date a reader can.
func parseTrackbackDay(s string) string {
	if t, err := time.Parse("January 2, 2006", s); err == nil {
		return t.Format("2006-01-02")
	}
	return s
}

// squash collapses the page's indentation into single spaces. The trackback
// markup is written across lines inside its elements, so raw text comes out
// with runs of newlines and spaces in the middle of a title.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }
