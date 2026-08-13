//go:build live

// These tests talk to arxiv.org and doi.org. They are behind a build tag and
// never run in CI, because they exist to notice arXiv changing something rather
// than to guard a build. Run them by hand:
//
//	go test ./pkg/axid -tags live -v
//
// They assert shapes and rules, not values, and a refusal is a skip rather than
// a failure so a rate limit does not read as a regression.
package axid

import (
	"net/http"
	"testing"
	"time"
)

// htmlPace is the crawl delay arxiv.org asks for in robots.txt. These tests
// make a handful of requests and they wait between them.
const htmlPace = 15 * time.Second

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// TestLiveDOIFormula checks the claim that the arXiv DOI is a formula rather
// than something to scrape, for both id styles. The day the prefix changes is
// the day this fails.
func TestLiveDOIFormula(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect}

	for i, ref := range []string{"1706.03762", "hep-th/9711200"} {
		if i > 0 {
			time.Sleep(htmlPace)
		}
		id, err := Parse(ref)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Get("https://doi.org/" + id.DOI())
		if err != nil {
			t.Skipf("doi.org is not answering: %v", err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
			t.Errorf("%s: doi.org returned %d, want a redirect", id.DOI(), resp.StatusCode)
			continue
		}
		if got, want := resp.Header.Get("Location"), "https://arxiv.org/abs/"+id.Canonical; got != want {
			t.Errorf("%s resolved to %q, want %q", id.DOI(), got, want)
		}
	}
}

// TestLiveSubjectClassRedirect checks the rule that makes Canonical drop the
// subject class: arXiv itself treats the class-qualified path as an alias.
func TestLiveSubjectClassRedirect(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect}

	resp, err := client.Get("https://arxiv.org/abs/math.GT/0309136")
	if err != nil {
		t.Skipf("arxiv.org is not answering: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		t.Skip("arxiv.org is rate limiting; the HTML plane wants 15s between requests")
	}
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301; arXiv may have stopped canonicalizing the subject class", resp.StatusCode)
	}
	id, err := Parse("math.GT/0309136")
	if err != nil {
		t.Fatal(err)
	}
	// arXiv sends a relative Location here, so compare the path rather than
	// the whole URL.
	if got, want := resp.Header.Get("Location"), "/abs/"+id.Canonical; got != want {
		t.Errorf("redirect to %q, want %q", got, want)
	}
}
