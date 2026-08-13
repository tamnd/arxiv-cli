package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// This file is s6, the announcement feed. It is one day of announcements for
// one category, on the API plane, and it carries the one field nothing else
// publishes: whether an item is a new paper, a cross list, or a replacement.
//
// A reader who cannot tell those apart is being handed noise. The cs.CL feed
// read on 2026-08-13 had 139 items, and 47 of them were replacements of papers
// somebody following the feed had already seen.

// rssBase is the feed root. The feed host is on the API plane, so this is a
// three second read rather than a fifteen second one.
const rssBase = "https://rss.arxiv.org/rss/"

// The four announce types arXiv publishes. An item with a value not on this
// list keeps it: arXiv adding a fifth is a thing that can happen, and folding
// an unknown value into "new" would turn a fact into a wrong fact.
const (
	AnnounceNew          = "new"
	AnnounceCross        = "cross"
	AnnounceReplace      = "replace"
	AnnounceReplaceCross = "replace-cross"
)

// AnnounceTypes is the set, in the order the footer counts them.
var AnnounceTypes = []string{AnnounceNew, AnnounceCross, AnnounceReplace, AnnounceReplaceCross}

// Announcement is one item off the feed.
type Announcement struct {
	Envelope

	// PaperID is canonical, with the version in its own field.
	PaperID string `json:"paper_id" kit:"id" table:"id"`
	// AnnounceType is arXiv's own string, and it is the reason this record
	// type exists, so it sits next to the id rather than at the end.
	AnnounceType string `json:"announce_type" table:"type"`
	Title        string `json:"title" table:"title,truncate"`
	// Version comes off the guid, which is the only element on the feed that
	// carries one.
	Version  int      `json:"version,omitzero" table:"-"`
	Abstract string   `json:"abstract,omitempty" table:"-"`
	Authors  []string `json:"authors,omitempty" table:"-"`
	// Categories are the item's own categories, primary first, which is not
	// the same as the feed it came out of: a cross list is announced in a feed
	// it is not primarily in.
	Categories []string `json:"categories,omitempty" table:"-"`
	License    string   `json:"license,omitempty" table:"-"`
	// DOI is the publisher's, present only on a published paper. The arXiv
	// issued DOI is a function of the id and is on the paper record.
	DOI string `json:"doi,omitempty" table:"-"`
	// Announced is the day this item was announced, which the feed gives to
	// the day and not the minute.
	Announced time.Time `json:"announced,omitzero" table:"-"`
	// Feed is the category whose feed this came from.
	Feed string `json:"feed" table:"-"`
	URL  string `json:"url,omitempty" table:"-,url"`
}

// FeedOptions is one feed read.
type FeedOptions struct {
	// Category is the feed to read.
	Category string
	// Types filters by announce type. Empty means every type.
	Types []string
}

// ─── the feed ────────────────────────────────────────────────────────────────

// rssFeed is the channel as it comes off the wire.
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
		// PubDate is the announcement this feed carries, and it is what the
		// cache TTL is computed from rather than a fixed number.
		PubDate       string `xml:"pubDate"`
		LastBuildDate string `xml:"lastBuildDate"`
		// SkipDays is arXiv saying it does not announce at the weekend, which
		// is a real fact about arXiv and not a hint.
		SkipDays struct {
			Days []string `xml:"day"`
		} `xml:"skipDays"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

// rssItem is one entry, kept in the shape the feed has it.
type rssItem struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
	// Description is the announce line and the abstract glued together with a
	// newline.
	Description string `xml:"description"`
	// GUID is the OAI identifier with the version on it.
	GUID       string   `xml:"guid"`
	Categories []string `xml:"category"`
	PubDate    string   `xml:"pubDate"`
	// AnnounceType is in arXiv's own namespace, and Creator and Rights are
	// Dublin Core. Go matches the local name and the namespace URI, and the
	// prefix in the document is not what it goes on.
	AnnounceType string `xml:"http://arxiv.org/schemas/atom announce_type"`
	DOI          string `xml:"http://arxiv.org/schemas/atom DOI"`
	Creator      string `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Rights       string `xml:"http://purl.org/dc/elements/1.1/ rights"`
}

// feedURL is where a category's feed lives.
func feedURL(category string) string { return rssBase + category }

// maxFeedTTL is the longest a feed can be current for: Friday's announcement
// stands until Monday's, which is three days, and the extra day is slack.
const maxFeedTTL = 4 * 24 * time.Hour

// Announcements reads one category's feed.
//
// The filter is applied here rather than by the caller so the counts in Types
// describe the feed and not the filtered slice, which is the number worth
// printing.
func (c *Client) Announcements(ctx context.Context, o FeedOptions) ([]Announcement, error) {
	category := strings.TrimSpace(o.Category)
	if category == "" {
		return nil, errs.Usage("give a category code, such as cs.CL")
	}
	if err := checkCategories([]string{category}); err != nil {
		return nil, err
	}
	types, err := normaliseTypes(o.Types)
	if err != nil {
		return nil, err
	}

	u := feedURL(category)
	feed, err := c.getFeed(ctx, u)
	if err != nil {
		return nil, err
	}
	if len(feed.Channel.Items) == 0 {
		// arXiv answers a category it does not know with an empty feed rather
		// than a 404, and the code was checked above, so an empty feed here is
		// a day with no announcement rather than a typo.
		c.logf(0, "arxiv: the %s feed is empty, which is what a day with no announcement looks like", category)
	}

	at := c.now()
	announced := parseFeedDate(feed.Channel.PubDate)
	out := make([]Announcement, 0, len(feed.Channel.Items))
	counts := map[string]int{}
	for _, item := range feed.Channel.Items {
		a := itemToAnnouncement(item, category, u, at, announced)
		counts[a.AnnounceType]++
		c.noteUnknownCategories(a.Categories...)
		if len(types) > 0 && !types[a.AnnounceType] {
			continue
		}
		out = append(out, a)
	}
	c.notice("%s", feedSummary(len(feed.Channel.Items), counts))
	return out, nil
}

// normaliseTypes lowercases the --type values and refuses an empty one.
//
// An unknown value is not refused. arXiv owns this vocabulary and can add to
// it, and refusing a type this build has not heard of would make the tool
// useless for the very announcement that mattered.
func normaliseTypes(types []string) (map[string]bool, error) {
	if len(types) == 0 {
		return nil, nil
	}
	out := map[string]bool{}
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			return nil, errs.Usage("--type wants one of %s", strings.Join(AnnounceTypes, ", "))
		}
		out[t] = true
	}
	return out, nil
}

// feedSummary is the line printed under the table.
//
// It counts the whole feed rather than what survived --type, because "56 of
// 139 items are new" is the fact somebody filtering wants, and the length of
// the filtered list is already on their screen.
func feedSummary(total int, counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for _, t := range AnnounceTypes {
		if counts[t] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[t], t))
		}
	}
	// A type arXiv has added since this build is counted too, under its own
	// name, so it shows up as itself rather than vanishing from the total.
	for t, n := range counts {
		if !contains(AnnounceTypes, t) {
			parts = append(parts, fmt.Sprintf("%d %s", n, t))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d items", total)
	}
	return fmt.Sprintf("%d items: %s", total, strings.Join(parts, ", "))
}

// getFeed reads the feed, using the feed's own pubDate as the cache lifetime.
//
// The lifetime cannot be known before the copy is read, because it is inside
// it, so the cached copy is read first under the longest a feed can be current
// for and kept only if it says it still is. That is a file read rather than a
// request, and it is what stops a second `arxiv new cs.CL` in an afternoon
// from costing anything while still never serving yesterday's papers today.
func (c *Client) getFeed(ctx context.Context, u string) (*rssFeed, error) {
	if body, ok := c.cache.get(u, maxFeedTTL); ok {
		if feed, err := parseFeed(body); err == nil && feedCurrent(feed, c.now()) {
			c.logf(2, "cache hit %s, and its pubDate says it is still the current announcement", u)
			return feed, nil
		}
	}
	c.logf(1, "GET %s", u)
	// Zero as the lifetime skips the cache read that just happened and leaves
	// the write in place, so the copy on disk is the one the check above reads
	// next time.
	resp, err := c.fetch(ctx, u, 0)
	if err != nil {
		return nil, err
	}
	return parseFeed(resp.Body)
}

// parseFeed decodes the channel.
func parseFeed(body []byte) (*rssFeed, error) {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("decode arxiv feed: %w", err)
	}
	return &feed, nil
}

// feedCurrent reports whether a feed is still the current announcement.
//
// The feed is rebuilt once per announcement day, and skipDays says which days
// there is no announcement on, so the answer is "until the next announcement
// day comes round", which on a Friday means three days and on a Tuesday means
// one. A feed with no pubDate is not trusted at all.
func feedCurrent(feed *rssFeed, now time.Time) bool {
	pub := parseFeedDate(feed.Channel.PubDate)
	if pub.IsZero() {
		return false
	}
	return now.Before(nextBuild(pub, feed.Channel.SkipDays.Days))
}

// nextBuild is when the announcement after pub is due: the next day that is not
// a skip day, at the same time.
func nextBuild(pub time.Time, skip []string) time.Time {
	skipped := map[time.Weekday]bool{}
	for _, d := range skip {
		for _, w := range []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
			time.Thursday, time.Friday, time.Saturday} {
			if strings.EqualFold(strings.TrimSpace(d), w.String()) {
				skipped[w] = true
			}
		}
	}
	next := pub.AddDate(0, 0, 1)
	// Seven is the whole week, so this cannot spin even if arXiv ever says it
	// skips every day of it.
	for i := 0; i < 7 && skipped[next.Weekday()]; i++ {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// parseFeedDate reads an RFC 1123 date with a numeric zone, which is what both
// the channel and the items carry.
func parseFeedDate(s string) time.Time {
	s = cleanText(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// itemToAnnouncement maps one item onto the record.
func itemToAnnouncement(item rssItem, category, source string, at, feedDate time.Time) Announcement {
	a := Announcement{
		Envelope: Envelope{
			Kind:        "announcement",
			RetrievedAt: at.UTC(),
		},
		Title:        cleanText(item.Title),
		AnnounceType: strings.ToLower(cleanText(item.AnnounceType)),
		License:      cleanText(item.Rights),
		DOI:          cleanText(item.DOI),
		Feed:         category,
		URL:          strings.TrimSpace(item.Link),
		Announced:    feedDate,
	}
	a.addSurface(SurfaceRSS, source)

	// The guid is the OAI identifier with the version on it, and it is the only
	// element on the feed that carries a version at all.
	if id, err := axid.Parse(cleanText(item.GUID)); err == nil {
		a.PaperID = id.Canonical
		a.Version = id.Version
		if a.URL == "" {
			a.URL = id.AbsURL()
		}
	} else if id, err := axid.Parse(strings.TrimSpace(item.Link)); err == nil {
		a.PaperID = id.Canonical
	}

	a.Abstract = feedAbstract(item.Description)
	for _, name := range strings.Split(item.Creator, ",") {
		if name = cleanText(name); name != "" {
			a.Authors = append(a.Authors, name)
		}
	}
	for _, code := range item.Categories {
		if code = cleanText(code); code != "" && !contains(a.Categories, code) {
			a.Categories = append(a.Categories, code)
		}
	}
	if d := parseFeedDate(item.PubDate); !d.IsZero() {
		a.Announced = d
	}
	if a.Announced.IsZero() {
		a.Announced = at.UTC()
	}
	a.setVia("announce_type", SurfaceRSS)
	a.setVia("announced", SurfaceRSS)
	return a
}

// feedAbstract pulls the abstract out of the description.
//
// The description is the announce line and the abstract glued together, so the
// split is on the "Abstract: " marker rather than on a fixed prefix length: the
// announce line's length depends on the id and the type, and an item with no
// marker keeps its whole description rather than losing it to an off by one.
func feedAbstract(description string) string {
	if _, rest, ok := strings.Cut(description, "Abstract: "); ok {
		return cleanText(rest)
	}
	return cleanText(description)
}
