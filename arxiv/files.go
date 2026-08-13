package arxiv

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// s12 is the bytes themselves: the PDF, the LaTeXML rendering and the
// submission source.
//
// It is the one surface with no metadata on it at all. Everything a file record
// says beyond its URL comes either from the paper's version table or from
// asking the server, and the two disagree, which is why every size says which
// one it came from.
//
// robots.txt disallows /src, /e-print, /ps, /dvi and /ftp, and arXiv's data
// policy says full content harvesting is not permitted except by arrangement.
// So this is one paper at a time, on the fifteen second plane, for a paper
// somebody named. There is no bulk mode and there is not going to be one.

const (
	pdfBase = "https://" + Host + "/pdf/"
	srcBase = "https://" + Host + "/src/"
)

// The three kinds of file arXiv serves for a paper.
const (
	KindPDF    = "pdf"
	KindHTML   = "html"
	KindSource = "source"
)

// FileKinds is the set in the order a person wants them: the one everybody
// reads, the one that is machine readable, the one that is the actual
// submission.
var FileKinds = []string{KindPDF, KindHTML, KindSource}

// TTLProbe is how long a measured size is kept. A version's bytes never change,
// because a change is a new version with a URL of its own, so this could be much
// longer. A day is enough to make a repeated read free and short enough that a
// wrong answer cached by accident does not follow anybody around.
const TTLProbe = 24 * time.Hour

// Where a size came from. They differ by more than rounding, so a record that
// carried a number without saying which one it was would be lying quietly.
const (
	// SizeFromTable is arXiv's own figure out of the submission history. It
	// describes the source and never the PDF, and on 1706.03762v7 it reads
	// 1,102 KB where the source actually served is 1,150,988 bytes.
	SizeFromTable = "table"
	// SizeFromServer is what the server said when asked, which is the number of
	// bytes that will arrive.
	SizeFromServer = "measured"
)

// File is one downloadable artifact of one version of one paper.
type File struct {
	Envelope

	// PaperID is canonical, with no version on it.
	PaperID string `json:"paper_id" kit:"id" table:"paper"`
	// Version is the version this file belongs to. A file belongs to a version
	// and not to a paper: v1 and v7 are different bytes at different URLs.
	Version int `json:"version,omitempty" table:"v"`
	// Kind is pdf, html or source.
	Kind string `json:"kind" table:"kind"`
	URL  string `json:"url" table:"-,url"`
	// Filename is what arXiv names the file in content-disposition, so a
	// download lands under the name arXiv would have given it. It is only known
	// after asking.
	Filename    string `json:"filename,omitempty" table:"filename"`
	ContentType string `json:"content_type,omitempty" table:"-"`
	// SizeBytes is the size and SizeFrom says whose number it is.
	SizeBytes int64  `json:"size_bytes,omitempty" table:"size"`
	SizeFrom  string `json:"size_from,omitempty" table:"from"`
	// ETag is arXiv's. On the PDF and the source it is a sha256 of the content,
	// so it doubles as a checksum to check a download against. On the HTML it
	// is an opaque token from the CDN and it is only good for comparing.
	ETag string `json:"etag,omitempty" table:"-"`
	// ModifiedAt is the last-modified header, which is when this version was
	// last built and not when it was submitted.
	ModifiedAt time.Time `json:"modified_at,omitzero" table:"-"`
	// Resumable is whether the server offered byte ranges, which is what
	// `arxiv download --resume` needs.
	Resumable bool `json:"resumable" table:"-"`
}

// FileURI names one file of one version, so two reads of the same bytes land on
// the same node.
func FileURI(id string, version int, kind string) string {
	if version > 0 {
		return fmt.Sprintf("ax://file/%s#v%d.%s", id, version, kind)
	}
	return "ax://file/" + id + "#" + kind
}

// Files lists what arXiv serves for a paper.
//
// The list itself is free: which files exist is already on the paper record, as
// the two capability flags the abstract page publishes. measure asks the server
// for each one, which is a request a file on the fifteen second plane and the
// only way to learn a real size, a filename or a checksum.
func (c *Client) Files(ctx context.Context, ref string, measure bool) ([]File, error) {
	p, err := c.PaperAt(ctx, ref, PaperOptions{Depth: DepthFull})
	if err != nil {
		return nil, err
	}
	files := filesOf(p, c.now().UTC())
	if !measure {
		return files, nil
	}
	for i := range files {
		if err := c.measure(ctx, &files[i]); err != nil {
			return files, err
		}
	}
	return files, nil
}

// filesOf builds the list from what the paper already knows.
func filesOf(p Paper, at time.Time) []File {
	base := Envelope{Kind: "file", RetrievedAt: at}
	versioned := p.ID
	if p.Version > 0 {
		versioned = fmt.Sprintf("%sv%d", p.ID, p.Version)
	}

	out := make([]File, 0, 3)
	add := func(kind, url string) {
		f := File{Envelope: base, PaperID: p.ID, Version: p.Version, Kind: kind, URL: url}
		// The surfaces that answered for the paper answered for this list too:
		// which files exist came off the abstract page and the size came out of
		// the version table, so the provenance travels with the record.
		for i, s := range p.Surfaces {
			source := ""
			if i < len(p.Sources) {
				source = p.Sources[i]
			}
			f.addSurface(s, source)
		}
		f.addSurface(SurfaceFiles, url)
		out = append(out, f)
	}

	// The PDF is always there. arXiv builds one from the source for every
	// paper, including the ones submitted as PDF in the first place.
	add(KindPDF, pdfBase+versioned)
	if p.HasHTML {
		u := p.HTMLURL
		if u == "" {
			u = htmlBase + versioned
		}
		add(KindHTML, u)
	}
	if p.HasSource {
		add(KindSource, srcBase+versioned)
	}

	// The version table's size is the source's, so it goes on the source and
	// nowhere else. Putting it on the PDF would be off by a factor of two on
	// the paper this was measured against.
	if size, ok := sourceSize(p); ok {
		for i := range out {
			if out[i].Kind == KindSource {
				out[i].SizeBytes = size
				out[i].SizeFrom = SizeFromTable
				out[i].setVia("size_bytes", viaOf(p, "size"))
			}
		}
	}
	return out
}

// sourceSize is the submission size for the version the record is at.
func sourceSize(p Paper) (int64, bool) {
	for _, v := range p.Versions {
		if v.Version == p.Version && v.SizeBytes > 0 {
			return v.SizeBytes, true
		}
	}
	return 0, false
}

// viaOf reports which surface answered for a field on the paper, falling back
// to OAI because that is where the version table normally comes from.
func viaOf(p Paper, field string) string {
	if s, ok := p.Via[field]; ok && s != "" {
		return s
	}
	return SurfaceOAI
}

// measure asks the server about one file.
//
// It is a one byte range request rather than a HEAD, because a HEAD of a PDF
// came back with no content-length at all through arXiv's CDN on 2026-08-14,
// while the same URL asked for bytes 0-0 answered 206 and a content-range with
// the total in it. One byte on the wire is a cheaper way to be told the truth.
func (c *Client) measure(ctx context.Context, f *File) error {
	info, err := c.probe(ctx, f.URL)
	if err != nil {
		return err
	}
	if info.Size > 0 {
		f.SizeBytes = info.Size
		f.SizeFrom = SizeFromServer
		f.setVia("size_bytes", SurfaceFiles)
	}
	f.Filename = info.Filename
	f.ContentType = info.ContentType
	f.ETag = info.ETag
	f.Resumable = info.Resumable
	if info.Modified != "" {
		if t, err := http.ParseTime(info.Modified); err == nil {
			f.ModifiedAt = t.UTC()
		}
	}
	return nil
}

// fileInfo is what the server says about a URL. It is stored as JSON in the
// cache, so the fields are exported and the header strings are kept as strings.
type fileInfo struct {
	Size        int64  `json:"size"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	ETag        string `json:"etag,omitempty"`
	Modified    string `json:"modified,omitempty"`
	Resumable   bool   `json:"resumable"`
}

// probe reads the headers for a URL, from the cache when it can.
func (c *Client) probe(ctx context.Context, rawURL string) (fileInfo, error) {
	key := "probe " + rawURL
	if body, ok := c.cache.get(key, TTLProbe); ok {
		var info fileInfo
		if err := json.Unmarshal(body, &info); err == nil {
			c.logf(2, "cache hit %s", key)
			return info, nil
		}
	}

	resp, err := c.request(ctx, rawURL, http.MethodGet, map[string]string{"Range": "bytes=0-0"})
	if err != nil {
		return fileInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
	case http.StatusNotFound:
		return fileInfo{}, errs.NotFound("arxiv has nothing at %s", rawURL)
	default:
		return fileInfo{}, errs.New(errs.KindGeneric, "arxiv returned %d for %s", resp.StatusCode, rawURL)
	}

	info := fileInfo{
		ContentType: strings.TrimSpace(resp.Header.Get("Content-Type")),
		ETag:        strings.Trim(resp.Header.Get("ETag"), `"`),
		Modified:    resp.Header.Get("Last-Modified"),
		Filename:    filenameOf(resp.Header.Get("Content-Disposition")),
		Resumable:   resp.StatusCode == http.StatusPartialContent,
	}
	switch {
	case resp.StatusCode == http.StatusPartialContent:
		info.Size = totalOf(resp.Header.Get("Content-Range"))
	default:
		info.Size = resp.ContentLength
	}
	if body, err := json.Marshal(info); err == nil {
		c.cache.put(key, body)
	}
	return info, nil
}

// filenameOf reads the name out of a content-disposition header. arXiv puts the
// version in it, "1706.03762v7.pdf", which is the name a saved file wants even
// when the URL that fetched it had no version on it.
func filenameOf(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return params["filename"]
}

// totalOf reads the size out of "bytes 0-0/2215244".
func totalOf(header string) int64 {
	_, total, ok := strings.Cut(header, "/")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// request makes one paced request and hands back the live response, for the
// reads that want headers or a stream rather than a body in memory.
//
// It shares the pacing with fetch and shares nothing else: no cache, because a
// PDF is not something to keep a copy of in a metadata cache, and no retry,
// because a caller streaming to a file has to decide what a half-written file
// means before anything is retried.
func (c *Client) request(ctx context.Context, rawURL, method string, headers map[string]string) (*http.Response, error) {
	return c.requestWith(ctx, rawURL, method, headers, c.httpClient)
}

// requestWith is request with the HTTP client named, for the one caller that
// needs a different one: a download streams to disk and cannot live under the
// per request timeout that suits a metadata read.
func (c *Client) requestWith(ctx context.Context, rawURL, method string, headers map[string]string, hc *http.Client) (*http.Response, error) {
	_, lim, err := c.planeFor(Host)
	if err != nil {
		return nil, err
	}
	if err := lim.wait(ctx); err != nil {
		return nil, ctxErr(err)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, errs.Wrap(errs.KindGeneric, err, "build request")
	}
	req.Header.Set("User-Agent", c.userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.logf(1, "%s %s", method, rawURL)

	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctxErr(ctx.Err())
		}
		return nil, errs.Wrap(errs.KindNetwork, err, "%s %s", strings.ToLower(method), rawURL)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		_ = resp.Body.Close()
		return nil, errs.RateLimited("arxiv returned 429 for %s", rawURL)
	}
	return resp, nil
}

// fileURL is the URL for one kind of file for one reference.
func fileURL(id axid.ID, kind string) (string, error) {
	switch kind {
	case KindPDF:
		return pdfBase + id.Versioned(), nil
	case KindHTML:
		return htmlBase + id.Versioned(), nil
	case KindSource:
		return srcBase + id.Versioned(), nil
	}
	return "", errs.Usage("%q is not one of %s", kind, strings.Join(FileKinds, ", "))
}
