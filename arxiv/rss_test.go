package arxiv

import (
	"strings"
	"testing"
	"time"
)

// The feed fixture is the real cs.CL announcement of 2026-08-13: 139 items,
// all four announce types, seven of them with a publisher DOI.

func feedFixture(t *testing.T) *rssFeed {
	t.Helper()
	feed, err := parseFeed(fixture(t, "rss_cs.CL.xml"))
	if err != nil {
		t.Fatalf("parse feed: %v", err)
	}
	return feed
}

func TestParseFeed(t *testing.T) {
	feed := feedFixture(t)
	if feed.Channel.Title != "cs.CL updates on arXiv.org" {
		t.Errorf("Title: got %q", feed.Channel.Title)
	}
	if len(feed.Channel.Items) != 139 {
		t.Fatalf("%d items, want 139", len(feed.Channel.Items))
	}
	// arXiv says out loud that it does not announce at the weekend, and that
	// is what stops a Sunday read from looking like a day with no papers.
	days := feed.Channel.SkipDays.Days
	if len(days) != 2 || !contains(days, "Saturday") || !contains(days, "Sunday") {
		t.Errorf("skipDays: got %v", days)
	}

	counts := map[string]int{}
	for _, item := range feed.Channel.Items {
		counts[item.AnnounceType]++
	}
	for _, tc := range []struct {
		kind string
		want int
	}{
		{AnnounceNew, 56},
		{AnnounceCross, 36},
		{AnnounceReplace, 32},
		{AnnounceReplaceCross, 15},
	} {
		if counts[tc.kind] != tc.want {
			t.Errorf("%s: got %d, want %d", tc.kind, counts[tc.kind], tc.want)
		}
	}
	// 47 of the 139 are replacements, which is the number the whole command
	// exists for.
	if counts[AnnounceReplace]+counts[AnnounceReplaceCross] != 47 {
		t.Errorf("replacements: got %d, want 47", counts[AnnounceReplace]+counts[AnnounceReplaceCross])
	}
}

func TestItemToAnnouncement(t *testing.T) {
	feed := feedFixture(t)
	source := feedURL("cs.CL")
	pub := parseFeedDate(feed.Channel.PubDate)
	a := itemToAnnouncement(feed.Channel.Items[0], "cs.CL", source, testTime, pub)

	if a.Kind != "announcement" || a.PaperID != "2608.11232" {
		t.Errorf("identity: got %+v", a)
	}
	// The version is on the guid and nowhere else on the item. The link is
	// bare and the title says nothing about it.
	if a.Version != 1 {
		t.Errorf("Version: got %d, want 1 off the guid", a.Version)
	}
	if a.AnnounceType != AnnounceNew {
		t.Errorf("AnnounceType: got %q", a.AnnounceType)
	}
	if !strings.HasPrefix(a.Title, "Backtrader-Bench:") {
		t.Errorf("Title: got %q", a.Title)
	}
	// The description is the announce line and the abstract glued together,
	// and the announce line is not part of the abstract.
	if strings.Contains(a.Abstract, "Announce Type") || !strings.HasPrefix(a.Abstract, "Evaluating LLM coding agents") {
		t.Errorf("Abstract: got %q", a.Abstract[:min(80, len(a.Abstract))])
	}
	if len(a.Authors) != 2 || a.Authors[0] != "Ruoxi Zhao" || a.Authors[1] != "Maziar Raissi" {
		t.Errorf("Authors: got %v", a.Authors)
	}
	// A cross listed item is announced in a feed it is not primarily in, so
	// the item's own categories are kept and are not the feed's name.
	if len(a.Categories) != 2 || a.Categories[0] != "cs.CL" || a.Categories[1] != "cs.AI" {
		t.Errorf("Categories: got %v", a.Categories)
	}
	if a.License != "http://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("License: got %q", a.License)
	}
	if a.Feed != "cs.CL" || a.URL != "https://arxiv.org/abs/2608.11232" {
		t.Errorf("feed and url: got %q %q", a.Feed, a.URL)
	}
	want := time.Date(2026, time.August, 13, 4, 0, 0, 0, time.UTC)
	if !a.Announced.Equal(want) {
		t.Errorf("Announced: got %s, want %s", a.Announced, want)
	}
	if a.Via["announce_type"] != SurfaceRSS || len(a.Surfaces) != 1 || a.Surfaces[0] != SurfaceRSS {
		t.Errorf("envelope: got %v %v", a.Via, a.Surfaces)
	}
}

// TestFeedReplacementKeepsItsVersion is the case the announce type exists for:
// the third version of a paper from February, announced again in August.
func TestFeedReplacementKeepsItsVersion(t *testing.T) {
	feed := feedFixture(t)
	for _, item := range feed.Channel.Items {
		if item.AnnounceType != AnnounceReplace {
			continue
		}
		a := itemToAnnouncement(item, "cs.CL", feedURL("cs.CL"), testTime, time.Time{})
		if a.Version < 2 {
			t.Errorf("%s is a replacement at version %d", a.PaperID, a.Version)
		}
		return
	}
	t.Error("no replacement on the feed")
}

// TestFeedPublisherDOI: seven of the 139 carry one, and it is the publisher's
// and not the arXiv issued one.
func TestFeedPublisherDOI(t *testing.T) {
	feed := feedFixture(t)
	var withDOI int
	for _, item := range feed.Channel.Items {
		a := itemToAnnouncement(item, "cs.CL", feedURL("cs.CL"), testTime, time.Time{})
		if a.DOI == "" {
			continue
		}
		withDOI++
		if strings.Contains(a.DOI, "arXiv") {
			t.Errorf("%s carries the arXiv DOI in the publisher field: %q", a.PaperID, a.DOI)
		}
	}
	if withDOI != 7 {
		t.Errorf("%d items carry a publisher DOI, want 7", withDOI)
	}
}

func TestEveryItemParses(t *testing.T) {
	for _, item := range feedFixture(t).Channel.Items {
		a := itemToAnnouncement(item, "cs.CL", feedURL("cs.CL"), testTime, time.Time{})
		if a.PaperID == "" || a.Title == "" || a.AnnounceType == "" || len(a.Authors) == 0 {
			t.Errorf("%+v is missing something every item has", a)
		}
		if a.Abstract == "" {
			t.Errorf("%s has no abstract", a.PaperID)
		}
		if !contains(AnnounceTypes, a.AnnounceType) {
			t.Errorf("%s has announce type %q, which is not one this build knows", a.PaperID, a.AnnounceType)
		}
	}
}

func TestFeedAbstract(t *testing.T) {
	got := feedAbstract("arXiv:2608.11232v1 Announce Type: new \nAbstract: Evaluating LLM coding agents")
	if got != "Evaluating LLM coding agents" {
		t.Errorf("got %q", got)
	}
	// An item with no marker keeps its whole description rather than losing
	// the front of it to a fixed prefix length.
	if got := feedAbstract("no marker here"); got != "no marker here" {
		t.Errorf("got %q", got)
	}
}

func TestFeedSummary(t *testing.T) {
	counts := map[string]int{AnnounceNew: 56, AnnounceCross: 36, AnnounceReplace: 32, AnnounceReplaceCross: 15}
	want := "139 items: 56 new, 36 cross, 32 replace, 15 replace-cross"
	if got := feedSummary(139, counts); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A type arXiv adds after this build is counted under its own name rather
	// than disappearing out of the total.
	counts["withdrawn"] = 2
	if got := feedSummary(141, counts); !strings.Contains(got, "2 withdrawn") {
		t.Errorf("got %q", got)
	}
	if got := feedSummary(0, map[string]int{}); got != "0 items" {
		t.Errorf("got %q", got)
	}
}

// TestFeedCurrent is the cache rule: the feed says when it was built and which
// days there is no announcement on, so how long a copy is good for comes off
// the copy itself rather than off a number somebody picked.
func TestFeedCurrent(t *testing.T) {
	feed := feedFixture(t)
	pub := parseFeedDate(feed.Channel.PubDate) // Thursday 13 August 2026, 04:00 UTC

	if !feedCurrent(feed, pub.Add(12*time.Hour)) {
		t.Error("the same afternoon should still be the current announcement")
	}
	if feedCurrent(feed, pub.Add(25*time.Hour)) {
		t.Error("the next announcement is out, so the copy is not current")
	}

	// Friday's feed stands until Monday, because arXiv says it skips the
	// weekend and it does.
	friday := feed
	friday.Channel.PubDate = "Fri, 14 Aug 2026 00:00:00 -0400"
	pub = parseFeedDate(friday.Channel.PubDate)
	if !feedCurrent(friday, pub.Add(48*time.Hour)) {
		t.Error("a Sunday read of Friday's feed should be current, not empty")
	}
	if feedCurrent(friday, pub.Add(73*time.Hour)) {
		t.Error("Monday's announcement is out, so Friday's copy is stale")
	}

	// A feed with no build date is not trusted, because the whole rule rests
	// on it.
	feed.Channel.PubDate = ""
	if feedCurrent(feed, testTime) {
		t.Error("a feed with no pubDate was treated as current")
	}
}

func TestNextBuild(t *testing.T) {
	weekend := []string{"Sunday", "Saturday"}
	thu := time.Date(2026, time.August, 13, 4, 0, 0, 0, time.UTC)
	if got := nextBuild(thu, weekend); !got.Equal(thu.AddDate(0, 0, 1)) {
		t.Errorf("Thursday: got %s", got)
	}
	fri := thu.AddDate(0, 0, 1)
	if got := nextBuild(fri, weekend); !got.Equal(fri.AddDate(0, 0, 3)) {
		t.Errorf("Friday: got %s, want the Monday", got)
	}
	// No skipDays at all means tomorrow, whatever day that is.
	if got := nextBuild(fri, nil); !got.Equal(fri.AddDate(0, 0, 1)) {
		t.Errorf("no skip days: got %s", got)
	}
}

func TestParseFeedDate(t *testing.T) {
	got := parseFeedDate("Thu, 13 Aug 2026 00:00:00 -0400")
	want := time.Date(2026, time.August, 13, 4, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
	if !parseFeedDate("").IsZero() || !parseFeedDate("yesterday").IsZero() {
		t.Error("an unreadable date should be absent rather than the zero of the epoch")
	}
}

func TestNormaliseTypes(t *testing.T) {
	got, err := normaliseTypes([]string{"New", " replace-cross "})
	if err != nil {
		t.Fatal(err)
	}
	if !got[AnnounceNew] || !got[AnnounceReplaceCross] || len(got) != 2 {
		t.Errorf("got %v", got)
	}
	// An unknown type is passed through rather than refused: arXiv owns this
	// vocabulary and a build from last year must not be the reason a new type
	// cannot be asked for.
	got, err = normaliseTypes([]string{"withdrawn"})
	if err != nil || !got["withdrawn"] {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := normaliseTypes([]string{" "}); err == nil {
		t.Error("an empty --type was accepted")
	}
	if got, _ := normaliseTypes(nil); got != nil {
		t.Errorf("no filter should be no filter, got %v", got)
	}
}

func TestFeedURL(t *testing.T) {
	if got := feedURL("cs.CL"); got != "https://rss.arxiv.org/rss/cs.CL" {
		t.Errorf("got %s", got)
	}
	// The feed host is on the API plane, which is why this is a three second
	// read and the listing is a fifteen second one.
	plane, ok := PlaneFor("rss.arxiv.org")
	if !ok || plane.Name != APIPlane.Name {
		t.Errorf("the feed host is on the %q plane", plane.Name)
	}
}
