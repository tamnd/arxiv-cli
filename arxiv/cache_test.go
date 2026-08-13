package arxiv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestCache(t *testing.T) (*cache, *clock) {
	t.Helper()
	c := newCache(t.TempDir(), false)
	clk := newClock()
	// An entry's age is the file's modification time, which the filesystem
	// stamps with the real clock. So the fake one has to start where the real one
	// is and then only ever move forward from there.
	clk.t = time.Now()
	c.now = clk.now
	return c, clk
}

func TestCacheRoundTrip(t *testing.T) {
	c, _ := newTestCache(t)
	key := "https://export.arxiv.org/api/query?id_list=1706.03762"
	c.put(key, []byte(sampleFeed))

	body, ok := c.get(key, time.Hour)
	if !ok {
		t.Fatal("the body just written is not in the cache")
	}
	if string(body) != sampleFeed {
		t.Error("the cached body is not the body written")
	}
	if _, ok := c.get(key+"&max_results=2", time.Hour); ok {
		t.Error("a different URL hit the same entry")
	}
}

// TestCacheExpires is the whole of the invalidation policy. arXiv sends no
// ETag, no Last-Modified and no Cache-Control on any surface probed on
// 2026-08-13, so age is the only thing there is to go on.
func TestCacheExpires(t *testing.T) {
	c, clk := newTestCache(t)
	key := "https://export.arxiv.org/api/query?id_list=1706.03762"
	c.put(key, []byte(sampleFeed))

	clk.t = clk.t.Add(TTLSearch - time.Minute)
	if _, ok := c.get(key, TTLSearch); !ok {
		t.Error("an entry inside its ttl was treated as stale")
	}
	clk.t = clk.t.Add(2 * time.Minute)
	if _, ok := c.get(key, TTLSearch); ok {
		t.Error("an entry past its ttl was served")
	}
	// The same bytes are still fresh under a longer ttl, because the ttl belongs
	// to the read rather than to the entry.
	if _, ok := c.get(key, TTLPaper); !ok {
		t.Error("an entry inside the paper ttl was treated as stale")
	}
}

func TestCacheDisabled(t *testing.T) {
	dir := t.TempDir()
	c := newCache(dir, true)
	c.put("k", []byte("v"))
	if _, ok := c.get("k", time.Hour); ok {
		t.Error("a disabled cache served an entry")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a disabled cache wrote %d entries", len(entries))
	}
}

// TestCacheWithoutDir covers the caller who never set one. No directory means no
// cache, and that is a slower tool rather than a broken one.
func TestCacheWithoutDir(t *testing.T) {
	c := newCache("", false)
	c.put("k", []byte("v"))
	if _, ok := c.get("k", time.Hour); ok {
		t.Error("a cache with no directory served an entry")
	}
}

// TestCacheZeroTTL is how a read opts out. A search with --no-cache and a fetch
// that must be fresh both come through here.
func TestCacheZeroTTL(t *testing.T) {
	c, _ := newTestCache(t)
	c.put("k", []byte("v"))
	if _, ok := c.get("k", 0); ok {
		t.Error("a zero ttl served an entry")
	}
}

// TestCacheFanout checks the layout, because a crawl of a whole category would
// otherwise put a hundred thousand files in one directory.
func TestCacheFanout(t *testing.T) {
	c, _ := newTestCache(t)
	p := c.path("https://arxiv.org/abs/1706.03762")
	rel, err := filepath.Rel(c.dir, p)
	if err != nil {
		t.Fatal(err)
	}
	dir, name := filepath.Split(rel)
	dir = filepath.Clean(dir)
	if len(dir) != 2 {
		t.Errorf("fanout directory %q is not two characters", dir)
	}
	if len(name) != 62 {
		t.Errorf("entry name %q is %d characters, want 62", name, len(name))
	}
}

// TestCacheSurvivesAGarbageEntry checks an unreadable file reads as a miss. A
// corrupt cache costs a request, which is the thing the cache was avoiding
// rather than the thing it was required for.
func TestCacheSurvivesAGarbageEntry(t *testing.T) {
	c, _ := newTestCache(t)
	p := c.path("k")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.get("k", time.Hour); ok {
		t.Error("a directory in the cache path was served as a body")
	}
}

// TestTTLsAreOrdered pins the shape of the policy rather than each number: a
// rendered page never changes, a paper rarely does, a search result moves as
// papers are announced.
func TestTTLsAreOrdered(t *testing.T) {
	if !(TTLSearch < TTLPaper && TTLPaper < TTLTaxonomy && TTLTaxonomy < TTLRendered) {
		t.Errorf("ttls are out of order: search %s, paper %s, taxonomy %s, rendered %s",
			TTLSearch, TTLPaper, TTLTaxonomy, TTLRendered)
	}
	if TTLFeed != TTLSearch {
		t.Errorf("feed ttl %s and search ttl %s should match; both track announcements",
			TTLFeed, TTLSearch)
	}
}
