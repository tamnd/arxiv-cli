package arxiv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The archive is tested in two halves. The parts that turn bytes into a record
// run against the saved surfaces, and the part that writes files runs against a
// local server, because what matters there is what lands on disk rather than
// what arXiv said.

func TestTheArchiveBuildsItsRecordFromItsOwnBytes(t *testing.T) {
	body := fixture(t, "api_1706.03762.xml")
	at := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	p, err := paperFromFeed(body, "https://export.arxiv.org/api/query", at)
	if err != nil {
		t.Fatalf("paperFromFeed: %v", err)
	}
	if p.ID != "1706.03762" {
		t.Errorf("id is %q", p.ID)
	}
	if p.Title != "Attention Is All You Need" {
		t.Errorf("title is %q", p.Title)
	}
	if len(p.Authors) != 8 {
		t.Errorf("%d authors, want 8", len(p.Authors))
	}
	if p.PrimaryCategory != "cs.CL" {
		t.Errorf("primary category is %q", p.PrimaryCategory)
	}
	if !p.RetrievedAt.Equal(at) {
		t.Errorf("retrieved at %s, want the archive's own clock", p.RetrievedAt)
	}
}

func TestAFeedWithNoEntriesIsAPaperThatIsNotThere(t *testing.T) {
	if _, err := paperFromFeed([]byte(emptyFeed), "u", time.Now()); err == nil {
		t.Fatal("an empty feed came back as a paper")
	}
	if _, err := paperFromFeed([]byte("not xml at all"), "u", time.Now()); err == nil {
		t.Fatal("a body that is not a feed came back as a paper")
	}
}

// An old style id has a slash in it, and a directory named after one would be
// two directories.
func TestAnOldStyleIDIsOneDirectory(t *testing.T) {
	if got := archiveName("math/0309136"); got != "math_0309136" {
		t.Errorf("archiveName is %q", got)
	}
	if got := archiveName("1706.03762"); got != "1706.03762" {
		t.Errorf("archiveName is %q", got)
	}
}

// The submission source has no header to ask, so the first bytes are the only
// thing that says what it is.
func TestTheSourceIsNamedByWhatItActuallyIs(t *testing.T) {
	cases := []struct {
		body []byte
		want string
	}{
		{[]byte{0x1f, 0x8b, 0x08, 0x00}, ".tar.gz"},
		{[]byte("%PDF-1.5\n"), ".pdf"},
		{[]byte("who knows"), ""},
		{nil, ""},
		{[]byte{0x1f}, ""},
	}
	for _, tc := range cases {
		if got := sourceExt(tc.body); got != tc.want {
			t.Errorf("sourceExt(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

// Four reads of arxiv.org at fifteen seconds is where an archive's minute goes,
// and --files adds two more.
func TestAnArchiveCostsAMinuteAndSaysSo(t *testing.T) {
	plain := archiveCost(false)
	if want := 3*APIPlane.Pace + 4*HTMLPlane.Pace; plain != want {
		t.Errorf("an archive costs %s, want %s", plain, want)
	}
	if withFiles := archiveCost(true); withFiles-plain != 2*HTMLPlane.Pace {
		t.Errorf("--files adds %s, want two arxiv.org requests", withFiles-plain)
	}
}

// The bytes on disk are the bytes that came back, and meta.json says what they
// hash to. Everything here goes to a server on this machine.
func TestASurfaceLandsOnDiskWithItsHash(t *testing.T) {
	page := []byte("<html>the abstract page</html>")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(page)
	}))
	defer ts.Close()

	a := &archiving{c: newTestClient(t, ts), dir: t.TempDir()}
	body, err := a.get(context.Background(), SurfaceAbs, "s3-abs.html", ts.URL+"/abs/1706.03762")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != string(page) {
		t.Errorf("the body is %q", body)
	}
	onDisk, err := os.ReadFile(filepath.Join(a.dir, "s3-abs.html"))
	if err != nil {
		t.Fatalf("read the archived file: %v", err)
	}
	if string(onDisk) != string(page) {
		t.Errorf("the file holds %q", onDisk)
	}
	if len(a.out.Surfaces) != 1 {
		t.Fatalf("meta says %d surfaces", len(a.out.Surfaces))
	}
	f := a.out.Surfaces[0]
	sum := sha256.Sum256(page)
	if f.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("the hash is %q", f.SHA256)
	}
	if f.Status != http.StatusOK || f.Bytes != int64(len(page)) || f.Name != "s3-abs.html" {
		t.Errorf("the surface is %+v", f)
	}
	if a.out.Files != 1 || a.out.Bytes != int64(len(page)) {
		t.Errorf("the totals are %d files and %d bytes", a.out.Files, a.out.Bytes)
	}

	// A surface that is not there is not a failure, it is a line in Missing.
	if _, err := a.get(context.Background(), SurfaceTrackback, "s11-trackbacks.html", ts.URL+"/gone"); err == nil {
		t.Fatal("the missing page came back without an error")
	}
	if a.out.Files != 1 {
		t.Errorf("a 404 was counted as a file")
	}
	if len(a.out.Missing) != 1 || !strings.HasPrefix(a.out.Missing[0], SurfaceTrackback+":") {
		t.Errorf("missing is %v", a.out.Missing)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "s11-trackbacks.html")); !os.IsNotExist(err) {
		t.Error("a page that was not there was written anyway")
	}
}

// An archive that could be served out of the cache would be an archive of the
// cache, and the timestamp beside it would be a lie about when those bytes were
// true.
func TestAnArchiveNeverComesOutOfTheCache(t *testing.T) {
	fresh := []byte("what arxiv says today")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fresh)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	c.cache = newCache(t.TempDir(), false)
	url := ts.URL + "/abs/1706.03762"
	c.cache.put(url, []byte("what the cache was holding"))

	// The cache is warm, which fetch would use and fetchLive must not.
	if body, ok := c.cache.get(url, time.Hour); !ok || string(body) == string(fresh) {
		t.Fatalf("the cache was not warm: %q, %v", body, ok)
	}
	a := &archiving{c: c, dir: t.TempDir()}
	body, err := a.get(context.Background(), SurfaceAbs, "s3-abs.html", url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != string(fresh) {
		t.Errorf("the archive holds %q, want what the server said", body)
	}
	// And the archive did not overwrite the cache either, because a read that
	// went round the cache has no business filling it.
	if cached, _ := c.cache.get(url, time.Hour); string(cached) == string(fresh) {
		t.Error("the archive wrote its bytes into the cache")
	}
}

// A file cut off at the read cap is not a copy of anything, and meta.json says
// so rather than leaving a plausible looking PDF lying about.
func TestATruncatedFileIsFlagged(t *testing.T) {
	a := &archiving{c: newTestClientNoServer(t), dir: t.TempDir()}
	a.out.Surfaces = []ArchivedFile{{Surface: SurfaceFiles, Name: "s12-paper.pdf"}}
	a.truncated("s12-paper.pdf", make([]byte, 10))
	if a.out.Surfaces[0].Error != "" {
		t.Errorf("a small file was flagged: %q", a.out.Surfaces[0].Error)
	}
	a.truncated("s12-paper.pdf", make([]byte, maxBody))
	if !strings.Contains(a.out.Surfaces[0].Error, "truncated") {
		t.Errorf("a file at the cap says %q", a.out.Surfaces[0].Error)
	}
}

// The source is written before its extension is known, so it is renamed once
// the first bytes have said what it is, and meta.json follows the rename.
func TestTheSourceIsRenamedOnceItsBytesAreKnown(t *testing.T) {
	a := &archiving{c: newTestClientNoServer(t), dir: t.TempDir()}
	if err := a.write("s12-source", []byte{0x1f, 0x8b, 0x08, 0x00}); err != nil {
		t.Fatalf("write: %v", err)
	}
	a.out.Surfaces = []ArchivedFile{{Surface: SurfaceFiles, Name: "s12-source"}}
	a.rename("s12-source", "s12-source.tar.gz")
	if _, err := os.Stat(filepath.Join(a.dir, "s12-source.tar.gz")); err != nil {
		t.Errorf("the renamed file is not there: %v", err)
	}
	if a.out.Surfaces[0].Name != "s12-source.tar.gz" {
		t.Errorf("meta still calls it %q", a.out.Surfaces[0].Name)
	}
}

// newTestClientNoServer is a client for the tests that never make a request.
func newTestClientNoServer(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.log = nil
	return c
}
