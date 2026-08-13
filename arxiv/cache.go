package arxiv

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

// TTLs, from spec 3006 doc 02 section 6.
//
// They are all time-based because there is nothing else to go on. arXiv sends
// no ETag, no Last-Modified and no Cache-Control on any surface that was
// probed on 2026-08-13, so there is no validator to send back and a conditional
// request is not available. A cached entry is trusted until it is old, and that
// is the whole of the policy.
const (
	// TTLPaper is one day. A paper's metadata changes when a new version is
	// announced, which is rare and never urgent.
	TTLPaper = 24 * time.Hour
	// TTLSearch is fifteen minutes. Results move as papers are announced.
	TTLSearch = 15 * time.Minute
	// TTLFeed is fifteen minutes, for the RSS surface.
	TTLFeed = 15 * time.Minute
	// TTLTaxonomy is a week. The category list changed twice in a decade.
	TTLTaxonomy = 7 * 24 * time.Hour
	// TTLListing is a day for a month that has ended.
	TTLListing = 24 * time.Hour
	// TTLRendered is thirty days, for full text and BibTeX. A rendered version
	// of a paper never changes; a new version gets a new key.
	TTLRendered = 30 * 24 * time.Hour
)

// cache is a flat file cache under the kit's cache directory.
//
// It is keyed on the full request URL because that is what identifies a
// response, and it stores the body alone with the file's modification time as
// the fetch time. A missing or unreadable cache is not an error: it means a
// request, which is the thing the cache was avoiding rather than the thing it
// was required for.
type cache struct {
	dir     string
	disable bool
	now     func() time.Time
}

func newCache(dir string, disable bool) *cache {
	return &cache{dir: dir, disable: disable, now: time.Now}
}

// path is where a URL's body lives. The name is the hash rather than the URL
// because a search URL is longer than most filesystems allow in one component.
func (c *cache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	// Two levels of fanout, so a big crawl does not put a hundred thousand
	// files in one directory.
	return filepath.Join(c.dir, h[:2], h[2:])
}

// get returns the cached body for key when there is one and it is younger than
// ttl. A zero or negative ttl means do not cache this at all.
func (c *cache) get(key string, ttl time.Duration) ([]byte, bool) {
	if c == nil || c.disable || c.dir == "" || ttl <= 0 {
		return nil, false
	}
	p := c.path(key)
	info, err := os.Stat(p)
	if err != nil {
		return nil, false
	}
	if c.now().Sub(info.ModTime()) > ttl {
		return nil, false
	}
	body, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return body, true
}

// put writes a body. It is best effort: a cache that cannot be written is a
// slower tool, not a broken one.
func (c *cache) put(key string, body []byte) {
	if c == nil || c.disable || c.dir == "" || len(body) == 0 {
		return
	}
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	// Write to a temporary file and rename, so a cancelled run cannot leave a
	// half-written body that later reads as a complete one.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
	}
}
