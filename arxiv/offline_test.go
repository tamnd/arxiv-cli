package arxiv

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// offline_test.go runs the client end to end without a network.
//
// The other tests in this package parse one saved response each, which proves a
// parser and proves nothing about the read that calls it. What breaks in
// practice is the chain: a depth that quietly stops asking for a surface, a URL
// built one way and cached another, an error on the fourth request that loses
// the first three. So this serves the committed fixtures back over the real
// hostnames, through the real plane table and the real transport, and the tests
// here are about what the chain does rather than what a parser returns.
//
// The hostnames are real on purpose. Pacing is chosen by host, so a fixture
// served from 127.0.0.1 would exercise a plane that does not exist, and the
// question of which plane a surface is on is exactly the sort of thing that
// goes wrong.

// offlineRoute maps a URL onto a file in testdata.
//
// It is written as a function rather than a table because the ids move: the
// abstract page is asked for without a version and the rendering is asked for
// with one, and a table would have to repeat every id in both forms.
//
// ranged separates the two things that happen at a file URL. s10 reads the
// rendering of /html/1706.03762v7 and s12 asks the same URL for its first byte
// to learn how big it is, and the saved answers are a page and a header block.
func offlineRoute(u *url.URL, ranged bool) string {
	slug := func(s string) string {
		return strings.ReplaceAll(strings.Trim(s, "/"), "/", "_")
	}
	q := u.Query()
	switch u.Host {
	case "export.arxiv.org":
		// One id per fixture. A batch of two would need bytes arXiv served for
		// that batch, and this package has none saved.
		if ids := q.Get("id_list"); ids != "" && !strings.Contains(ids, ",") {
			return "api_" + slug(strings.TrimSuffix(ids, versionSuffix(ids))) + ".xml"
		}
	case "oaipmh.arxiv.org":
		id := strings.TrimPrefix(q.Get("identifier"), "oai:arXiv.org:")
		switch q.Get("metadataPrefix") {
		case FormatArxiv:
			return "oai_arxiv_" + slug(id) + ".xml"
		case FormatArxivRaw:
			return "oai_raw_" + slug(id) + ".xml"
		case FormatOAIDC:
			return "oai_dc_" + slug(id) + ".xml"
		}
	case Host:
		path := strings.Trim(u.Path, "/")
		kind, rest, ok := strings.Cut(path, "/")
		if !ok {
			return ""
		}
		if ranged {
			return "probe_" + kind + "_" + slug(rest) + ".txt"
		}
		switch kind {
		case "abs":
			return "abs_" + slug(rest) + ".html"
		case "html":
			return "html_" + slug(rest) + ".html"
		case "bibtex":
			return "bibtex_" + slug(rest) + ".bib"
		case "tb":
			return "tb_" + slug(rest) + ".html"
		}
	}
	return ""
}

// versionSuffix is the "v7" on the end of a reference, or empty.
func versionSuffix(ref string) string {
	i := strings.LastIndex(ref, "v")
	if i <= 0 {
		return ""
	}
	if _, err := strconv.Atoi(ref[i+1:]); err != nil {
		return ""
	}
	return ref[i:]
}

// offline answers every request out of testdata, and remembers what was asked.
type offline struct {
	t *testing.T

	mu     sync.Mutex
	asked  []string
	missed []string
}

func (o *offline) RoundTrip(req *http.Request) (*http.Response, error) {
	name := offlineRoute(req.URL, req.Header.Get("Range") != "")
	o.mu.Lock()
	o.asked = append(o.asked, req.URL.String())
	if name == "" {
		o.missed = append(o.missed, req.URL.String())
	}
	o.mu.Unlock()

	if name == "" {
		return notFound(req), nil
	}
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if os.IsNotExist(err) {
		o.mu.Lock()
		o.missed = append(o.missed, req.URL.String())
		o.mu.Unlock()
		return notFound(req), nil
	}
	if err != nil {
		return nil, err
	}
	// A .txt under a file URL is a saved header block rather than a body: what
	// s12 reads is the headers, and committing two megabytes of PDF to test a
	// content-range would be committing two megabytes of PDF.
	if strings.HasPrefix(name, "probe_") {
		return replayHeaders(o.t, req, body), nil
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        http.Header{"Content-Type": {contentTypeOf(name)}},
		Request:       req,
	}, nil
}

func notFound(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusNotFound,
		Body:          io.NopCloser(strings.NewReader("not found")),
		ContentLength: 9,
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Request:       req,
	}
}

func contentTypeOf(name string) string {
	switch filepath.Ext(name) {
	case ".xml":
		return "application/xml"
	case ".html":
		return "text/html; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

// replayHeaders turns a saved header block back into a response.
//
// The file is what curl -D wrote, first line and all, so what the test replays
// is the header arXiv sent rather than a header somebody typed from memory.
func replayHeaders(t *testing.T, req *http.Request, saved []byte) *http.Response {
	t.Helper()
	resp := &http.Response{Header: http.Header{}, Request: req, Body: io.NopCloser(strings.NewReader("%"))}
	s := bufio.NewScanner(bytes.NewReader(saved))
	for first := true; s.Scan(); first = false {
		line := strings.TrimRight(s.Text(), "\r")
		if line == "" {
			continue
		}
		if first {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				t.Fatalf("saved header block starts with %q", line)
			}
			code, err := strconv.Atoi(fields[1])
			if err != nil {
				t.Fatalf("saved header block starts with %q", line)
			}
			resp.StatusCode = code
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		resp.Header.Add(key, strings.TrimSpace(value))
	}
	resp.ContentLength, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return resp
}

// newOfflineClient is a client that reads arXiv's real URLs out of testdata.
//
// The limiters are replaced rather than removed. A client with no limiter for a
// plane refuses to fetch, and that refusal is what policy_test.go is about, so
// the paces go to zero and the rule stays.
func newOfflineClient(t *testing.T) (*Client, *offline) {
	t.Helper()
	c, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	served := &offline{t: t}
	c.httpClient = &http.Client{Transport: served}
	c.limiters = map[string]*limiter{
		APIPlane.Name:  newLimiter(0),
		HTMLPlane.Name: newLimiter(0),
	}
	c.now = func() time.Time { return testTime }
	return c, served
}

// reads collects what actually went out to arXiv, which is not the same as what
// was asked for: a cache hit asks for nothing.
func (o *offline) reads(c *Client) *[]Read {
	var got []Read
	c.Watch(func(r Read) { got = append(got, r) })
	return &got
}

// TestDepthCostsTheRequestsItSays is the promise on the depth table, checked
// against the requests that leave the client.
//
// Depth.Requests and Depth.PlaneRequests are what `arxiv paper` prints as an
// estimate before a long read and what a crawl budgets both planes from. They
// were written by hand from the code, so nothing stopped them drifting the day
// a surface moved between depths. Now something does.
func TestDepthCostsTheRequestsItSays(t *testing.T) {
	for _, depth := range Depths {
		t.Run(string(depth), func(t *testing.T) {
			c, served := newOfflineClient(t)
			got := served.reads(c)

			p, err := c.PaperAt(context.Background(), "1706.03762", PaperOptions{Depth: depth})
			if err != nil {
				t.Fatalf("read at depth %s: %v", depth, err)
			}
			if len(served.missed) > 0 {
				t.Fatalf("no fixture for %s, so this depth was measured against a 404", served.missed)
			}
			if len(*got) != depth.Requests() {
				t.Errorf("depth %s made %d requests and Requests() says %d: %s",
					depth, len(*got), depth.Requests(), strings.Join(served.asked, "\n"))
			}

			wantAPI, wantHTML := depth.PlaneRequests()
			var api, html int
			for _, r := range *got {
				switch r.Plane {
				case APIPlane.Name:
					api++
				case HTMLPlane.Name:
					html++
				default:
					t.Errorf("a request went out on plane %q", r.Plane)
				}
			}
			if api != wantAPI || html != wantHTML {
				t.Errorf("depth %s split %d api and %d html, PlaneRequests() says %d and %d",
					depth, api, html, wantAPI, wantHTML)
			}
			if p.Depth != string(depth) {
				t.Errorf("the record says depth %q", p.Depth)
			}
		})
	}
}

// Each depth reads the surfaces the surface table says it does, and a paper
// carries the ones it was actually read from.
func TestDepthReachesTheSurfacesItPromises(t *testing.T) {
	want := map[Depth][]string{
		DepthQuick: {SurfaceAPI},
		DepthMeta:  {SurfaceAPI, SurfaceOAI},
		DepthFull:  {SurfaceAPI, SurfaceOAI, SurfaceAbs},
		DepthText:  {SurfaceAPI, SurfaceOAI, SurfaceAbs, SurfaceFullText},
	}
	for _, depth := range Depths {
		c, _ := newOfflineClient(t)
		p, err := c.PaperAt(context.Background(), "1706.03762", PaperOptions{Depth: depth})
		if err != nil {
			t.Fatalf("read at depth %s: %v", depth, err)
		}
		if strings.Join(p.Surfaces, " ") != strings.Join(want[depth], " ") {
			t.Errorf("depth %s read %v, want %v", depth, p.Surfaces, want[depth])
		}
		// One surface can answer twice. OAI is asked for arXiv and for
		// arXivRaw, which is one surface and two URLs, so what has to hold is
		// that every URL is kept and not that the two lists are the same
		// length.
		if len(p.Sources) != depth.Requests() {
			t.Errorf("depth %s made %d requests and kept %d URLs: %v",
				depth, depth.Requests(), len(p.Sources), p.Sources)
		}
	}
}

// A paper arXiv never rendered costs one request less at --depth text, and says
// so rather than returning an empty section list.
//
// hep-th/9711200 is from 1997 and there is no LaTeXML rendering of it. The
// abstract page is what says so, which is why the saving only appears at a
// depth that has already read the abstract page.
func TestTextDepthSkipsAPaperWithNoRendering(t *testing.T) {
	c, served := newOfflineClient(t)
	got := served.reads(c)

	p, err := c.PaperAt(context.Background(), "hep-th/9711200", PaperOptions{Depth: DepthText})
	if err != nil {
		t.Fatalf("read hep-th/9711200: %v", err)
	}
	if p.HasHTML {
		t.Fatal("the fixture now claims a rendering, so this test is checking nothing")
	}
	if len(*got) != 4 {
		t.Errorf("made %d requests, want four: the rendering is not there to ask for\n%s",
			len(*got), strings.Join(served.asked, "\n"))
	}
	if len(p.Sections) != 0 {
		t.Errorf("got %d sections off a paper with no rendering", len(p.Sections))
	}
	var said bool
	for _, m := range p.Missed {
		if strings.Contains(m, "no LaTeXML rendering") {
			said = true
		}
	}
	if !said {
		t.Errorf("missed says %v, and none of it says why there is no full text", p.Missed)
	}
}

// The whole point of the cache is that a second read of the same paper asks
// arXiv for nothing. A cache hit is not a request, so it does not reach the
// watcher and does not count against a crawl budget.
func TestASecondReadAsksArxivForNothing(t *testing.T) {
	c, served := newOfflineClient(t)
	c.cache = newCache(t.TempDir(), false)
	got := served.reads(c)

	for i := range 2 {
		if _, err := c.PaperAt(context.Background(), "1706.03762", PaperOptions{Depth: DepthFull}); err != nil {
			t.Fatalf("read %d: %v", i+1, err)
		}
	}
	if len(*got) != DepthFull.Requests() {
		t.Errorf("two reads cost %d requests, want the %d of the first one\n%s",
			len(*got), DepthFull.Requests(), strings.Join(served.asked, "\n"))
	}
}

// s12 measures a file by asking for its first byte and reading the headers.
//
// The saved header block is the one arXiv sent for the PDF of 1706.03762v7, and
// it is worth a test because every field in a measured file comes out of a
// header that arXiv is free to stop sending: the size is in Content-Range and
// not in Content-Length, the version is in the filename and not in the URL, and
// the ETag is a sha256 rather than an opaque string.
func TestMeasuringAFileReadsTheHeadersArxivSent(t *testing.T) {
	c, served := newOfflineClient(t)
	got := served.reads(c)

	files, err := c.Files(context.Background(), "1706.03762", true)
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	if len(served.missed) > 0 {
		t.Fatalf("no fixture for %s", served.missed)
	}
	var pdf *File
	for i := range files {
		if files[i].Kind == KindPDF {
			pdf = &files[i]
		}
	}
	if pdf == nil {
		t.Fatal("no pdf in the file list")
	}
	if pdf.SizeBytes != 2215244 {
		t.Errorf("size is %d, want the 2215244 in the content-range", pdf.SizeBytes)
	}
	if pdf.SizeFrom != SizeFromServer {
		t.Errorf("size came from %q", pdf.SizeFrom)
	}
	if pdf.Filename != "1706.03762v7.pdf" {
		t.Errorf("filename is %q, and the version in it is the point", pdf.Filename)
	}
	if pdf.ContentType != "application/pdf" {
		t.Errorf("content type is %q", pdf.ContentType)
	}
	if !pdf.Resumable {
		t.Error("arXiv answered 206 and the file says it cannot be resumed")
	}
	if len(*got) == 0 {
		t.Error("measuring a file made no request")
	}
}

// s9 is passed through unchanged. arXiv's BibTeX is what every bibliography for
// a paper already says, and a byte we improve on is a byte that disagrees.
func TestBibTeXIsArxivsOwnBytes(t *testing.T) {
	c, _ := newOfflineClient(t)
	entry, err := c.BibTeX(context.Background(), []string{"1706.03762"}, false)
	if err != nil {
		t.Fatalf("bibtex: %v", err)
	}
	saved := strings.TrimSpace(string(fixture(t, "bibtex_1706.03762.bib")))
	if strings.TrimSpace(entry) != saved {
		t.Errorf("the entry is not the bytes arXiv served:\n%s", entry)
	}
	if !strings.Contains(entry, "@misc{vaswani2023attentionneed") {
		t.Errorf("the entry lost its key:\n%s", entry)
	}
}

// A surface that answers 404 is not a failed read. OAI's earliest datestamp is
// 2005 and a handful of old records are simply not in it, and the record built
// from the surfaces that did answer is the answer.
func TestAMissingSurfaceDoesNotLoseTheRecord(t *testing.T) {
	c, served := newOfflineClient(t)
	p, err := c.PaperAt(context.Background(), "1207.7214", PaperOptions{Depth: DepthText})
	if err != nil {
		t.Fatalf("read 1207.7214: %v", err)
	}
	// There is no saved rendering for this paper, so the read that asks for one
	// gets the 404 this test is about.
	if len(served.missed) == 0 {
		t.Fatal("every surface answered, so this test is checking nothing")
	}
	if p.Title == "" || len(p.Versions) == 0 {
		t.Errorf("the record came back empty: %+v", p)
	}
	if !contains(p.Surfaces, SurfaceAbs) {
		t.Errorf("surfaces are %v, and the abstract page answered", p.Surfaces)
	}
}

// TestEverySurfaceHasCommittedBytes is the fixture suite made checkable.
//
// Doc 01 names twelve surfaces and the suite is supposed to cover all of them
// offline. Counting them by eye is how a surface ends up with a live test and
// nothing else, so the ledger is counted here instead: every id in the surface
// table has to appear in a row, and the two files that ship inside the binary
// count as committed bytes the same way.
func TestEverySurfaceHasCommittedBytes(t *testing.T) {
	covered := map[string]string{
		// s7 ships with the binary rather than sitting in testdata, because the
		// tool needs the taxonomy on a first run with no network. Same bytes,
		// same parser, different directory.
		SurfaceTaxonomy: "arxiv/embedded/taxonomy.html",
	}
	for _, c := range captures(t) {
		if c.Surface == "-" {
			continue
		}
		covered[c.Surface] = c.File
	}
	for _, s := range surfaceRows {
		if covered[s.ID] == "" {
			t.Errorf("%s (%s) has no committed bytes, so nothing tests it without a network",
				s.ID, SurfaceNames[s.ID])
		}
	}
	if len(covered) != len(surfaceRows) {
		t.Errorf("%d surfaces have fixtures and the table has %d rows", len(covered), len(surfaceRows))
	}
}

// The offline suite is only worth having if it is offline. This walks the
// fixtures the router can serve and checks each one is a file, so a route
// pointing at bytes nobody committed fails here rather than as a 404 inside a
// test that then explains something else.
func TestTheOfflineRouterPointsAtRealFiles(t *testing.T) {
	for _, raw := range []string{
		apiBase + "?id_list=1706.03762v7&max_results=1",
		oaiURL("GetRecord", "oai:arXiv.org:1706.03762", FormatArxiv),
		oaiURL("GetRecord", "oai:arXiv.org:1706.03762", FormatArxivRaw),
		oaiURL("GetRecord", "oai:arXiv.org:1706.03762", FormatOAIDC),
		absURL("1706.03762"),
		htmlURL("1706.03762", 7),
		bibtexBase + "1706.03762",
		pdfBase + "1706.03762v7",
		trackbackBase + "2401.00001",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		name := offlineRoute(u, strings.Contains(raw, "/pdf/"))
		if name == "" {
			t.Errorf("%s routes nowhere", raw)
			continue
		}
		if _, err := os.Stat(filepath.Join("testdata", name)); err != nil {
			t.Errorf("%s routes to %s: %v", raw, name, err)
		}
	}
}
