package arxiv

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// Downloading is the one thing this tool does that leaves something behind, so
// the rules are written down where the code is.
//
// One paper at a time, on the fifteen second plane, for a paper somebody named.
// The bytes never go through the metadata cache: a cache is for records that
// are read again and again, and a PDF is written to the place the person asked
// for it and read from there.
//
// A partial download is a .part file. That is the difference between a file
// that is there and a file that is nearly there, and without it --resume has no
// way to tell a truncated PDF from a whole one.

// maxExtract caps what one source archive may unpack to. arXiv sources are
// measured in megabytes, so this is not a limit anybody will meet by accident,
// and it is the difference between a malformed archive filling a disk and a
// malformed archive being refused.
const maxExtract = 512 << 20

// DownloadOptions is what to fetch and where to put it.
type DownloadOptions struct {
	// Kind is pdf, html or source. Empty means pdf.
	Kind string
	// Dir is the directory to write into. Empty means the working directory.
	Dir string
	// Path is an explicit destination file, which overrides Dir and the name
	// arXiv would have given the file.
	Path string
	// Extract unpacks a source archive next to it.
	Extract bool
	// Resume continues a .part file left by an interrupted run.
	Resume bool
	// Force fetches again over a file that is already there.
	Force bool
}

// Download is what one download did.
type Download struct {
	Envelope

	PaperID string `json:"paper_id" kit:"id" table:"paper"`
	Version int    `json:"version,omitempty" table:"v"`
	Kind    string `json:"kind" table:"kind"`
	URL     string `json:"url" table:"-,url"`
	// Path is where the bytes are now.
	Path string `json:"path" table:"path"`
	// SizeBytes is the size of the file on disk, which is measured here rather
	// than taken from a header.
	SizeBytes int64 `json:"size_bytes" table:"size"`
	// Downloaded is how many bytes came over the wire this time, which differs
	// from the size when a download resumed and is zero when nothing was
	// fetched at all.
	Downloaded int64 `json:"downloaded" table:"-"`
	// Resumed and Skipped say which of the three things happened: a fresh
	// download, a resumed one, or a file that was already there.
	Resumed bool `json:"resumed" table:"-"`
	Skipped bool `json:"skipped" table:"-"`
	// ETag is arXiv's sha256 of the content where it gave one, so a download
	// can be checked without fetching it again.
	ETag string `json:"etag,omitempty" table:"-"`
	// ExtractedTo and ExtractedFiles describe the unpacked source.
	ExtractedTo    string `json:"extracted_to,omitempty" table:"-"`
	ExtractedFiles int    `json:"extracted_files,omitempty" table:"-"`
}

// Download fetches one file for one paper.
func (c *Client) Download(ctx context.Context, ref string, o DownloadOptions) (Download, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return Download{}, err
	}
	kind := o.Kind
	if kind == "" {
		kind = KindPDF
	}
	if o.Extract && kind != KindSource {
		return Download{}, errs.Usage("--extract unpacks a source archive, so it needs --source")
	}
	u, err := fileURL(id, kind)
	if err != nil {
		return Download{}, err
	}

	d := Download{
		Envelope: Envelope{Kind: "download", RetrievedAt: c.now().UTC()},
		PaperID:  id.Canonical,
		Version:  id.Version,
		Kind:     kind,
		URL:      u,
	}
	d.addSurface(SurfaceFiles, u)

	path := o.Path
	if path == "" {
		path = filepath.Join(o.Dir, localName(id, kind))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return d, errs.Wrap(errs.KindGeneric, err, "make the directory for %s", path)
	}

	// A file that is already there is not fetched again. Fifteen seconds and
	// somebody else's bandwidth are worth one stat call.
	if have, size, ok := alreadyThere(path, id, kind, o.Path != ""); ok && !o.Force {
		d.Path = have
		d.SizeBytes = size
		d.Skipped = true
		if v := versionOf(filepath.Base(have)); v > 0 {
			d.Version = v
		}
		c.logf(1, "%s is already there, use --force to fetch it again", have)
		if o.Extract {
			if err := c.extractInto(&d, have); err != nil {
				return d, err
			}
		}
		return d, nil
	}

	if err := c.fetchFile(ctx, &d, path, o); err != nil {
		return d, err
	}
	if o.Extract {
		if err := c.extractInto(&d, d.Path); err != nil {
			return d, err
		}
	}
	return d, nil
}

// alreadyThere finds the file this download would produce, when a previous run
// left one behind.
//
// The name is not simply the one derived from the reference. arXiv resolves an
// unversioned id to its latest version and names the file with the version in
// it, so asking for 2401.00001 leaves 2401.00001v1.pdf on disk, and a check
// that only looked for 2401.00001.pdf would fetch the same five megabytes again
// every single run.
//
// The search only happens for a reference with no version on it and no
// destination named, because both of those are the caller being specific and
// being specific deserves an exact answer.
func alreadyThere(path string, id axid.ID, kind string, named bool) (string, int64, bool) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, info.Size(), true
	}
	if named || id.Version > 0 {
		return "", 0, false
	}
	base := strings.ReplaceAll(id.Canonical, "/", "_")
	suffix := strings.TrimPrefix(localName(id, kind), base)
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), base+"v[0-9]*"+suffix))
	if err != nil {
		return "", 0, false
	}
	best, bestV := "", 0
	for _, m := range matches {
		if v := versionOf(filepath.Base(m)); v > bestV {
			best, bestV = m, v
		}
	}
	if best == "" {
		return "", 0, false
	}
	info, err := os.Stat(best)
	if err != nil {
		return "", 0, false
	}
	return best, info.Size(), true
}

// fetchFile does the transfer into path, through a .part file.
func (c *Client) fetchFile(ctx context.Context, d *Download, path string, o DownloadOptions) error {
	part := path + ".part"
	var from int64
	if o.Resume {
		if info, err := os.Stat(part); err == nil {
			from = info.Size()
		}
	} else {
		// Without --resume a leftover part file is stale, and appending to it
		// would produce a file that is the wrong length in a way nothing would
		// notice until somebody opened it.
		_ = os.Remove(part)
	}

	headers := map[string]string{}
	if from > 0 {
		headers["Range"] = fmt.Sprintf("bytes=%d-", from)
		c.logf(1, "resuming %s at %d bytes", part, from)
	}

	// A download runs for as long as it takes. The per request timeout suits a
	// metadata read and would cut a large PDF off partway through, so this uses
	// a client with no timeout and lets the context decide when to stop.
	hc := *c.httpClient
	hc.Timeout = 0

	resp, err := c.requestWith(ctx, d.URL, http.MethodGet, headers, &hc)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// The server ignored the range, so whatever is in the part file is not
		// the start of this download.
		from = 0
	case http.StatusPartialContent:
		d.Resumed = from > 0
	case http.StatusRequestedRangeNotSatisfiable:
		// The part file is already the whole thing.
		c.logf(1, "%s is already complete", part)
		return c.finish(d, part, path, resp, 0, o.Path != "")
	case http.StatusNotFound:
		return missingFile(d.Kind, d.PaperID, d.URL)
	default:
		return errs.New(errs.KindGeneric, "arxiv returned %d for %s", resp.StatusCode, d.URL)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if from > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return errs.Wrap(errs.KindGeneric, err, "open %s", part)
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		// The part file stays where it is. That is the point of it: the bytes
		// already on disk are the bytes --resume picks up from.
		if ctx.Err() != nil {
			return ctxErr(ctx.Err())
		}
		return errs.Wrap(errs.KindNetwork, copyErr, "download %s", d.URL)
	}
	if closeErr != nil {
		return errs.Wrap(errs.KindGeneric, closeErr, "write %s", part)
	}
	return c.finish(d, part, path, resp, n, o.Path != "")
}

// finish renames the part file into place, under the name arXiv gave it when
// the caller did not name one.
func (c *Client) finish(d *Download, part, path string, resp *http.Response, n int64, named bool) error {
	final := path
	if name := filenameOf(resp.Header.Get("Content-Disposition")); name != "" && !named {
		// arXiv names the file with the version it resolved to, which is the
		// name worth keeping: two downloads of the same paper a year apart are
		// two different files and they should not land on top of each other.
		if candidate := filepath.Join(filepath.Dir(path), filepath.Base(name)); candidate != path {
			final = candidate
		}
	}
	if err := os.Rename(part, final); err != nil {
		return errs.Wrap(errs.KindGeneric, err, "move %s into place", part)
	}
	info, err := os.Stat(final)
	if err != nil {
		return errs.Wrap(errs.KindGeneric, err, "stat %s", final)
	}
	d.Path = final
	d.SizeBytes = info.Size()
	d.Downloaded = n
	d.ETag = strings.Trim(resp.Header.Get("ETag"), `"`)
	if v := versionOf(filepath.Base(final)); v > 0 {
		d.Version = v
	}
	return nil
}

// extractInto unpacks a source archive next to itself, into a directory named
// after the archive.
func (c *Client) extractInto(d *Download, path string) error {
	dir := extractDir(path)
	n, err := extract(path, dir)
	if err != nil {
		return err
	}
	d.ExtractedTo = dir
	d.ExtractedFiles = n
	return nil
}

// extractDir is the archive's path without its suffix, so
// papers/1706.03762v7.tar.gz unpacks into papers/1706.03762v7.
func extractDir(path string) string {
	for _, suffix := range []string{".tar.gz", ".tgz", ".gz"} {
		if strings.HasSuffix(path, suffix) {
			return strings.TrimSuffix(path, suffix)
		}
	}
	return path + ".src"
}

// extract unpacks a gzipped archive into dir.
//
// arXiv serves a submission as a gzipped tar when it has several files and as a
// single gzipped file when it has one, and the format page says so rather than
// the headers, so both are tried in that order.
func extract(path, dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "make %s", dir)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "resolve %s", dir)
	}

	n, err := untar(path, root)
	if err == nil {
		return n, nil
	}
	if !errors.Is(err, tar.ErrHeader) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, err
	}
	// Not a tar, so it is one gzipped file and the name is the archive's
	// without the .gz on it.
	return gunzipOne(path, root)
}

// untar unpacks a gzipped tar into root, refusing anything that would write
// outside it.
func untar(path, root string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "open %s", path)
	}
	defer func() { _ = f.Close() }()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "%s is not gzipped", path)
	}
	defer func() { _ = zr.Close() }()

	tr := tar.NewReader(zr)
	var (
		count   int
		written int64
	)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if count == 0 {
				// An empty tar and a file that is not a tar are the same thing
				// as far as this is concerned: nothing was unpacked, so let the
				// single file path have a go.
				return 0, tar.ErrHeader
			}
			return count, nil
		}
		if err != nil {
			return count, err
		}

		dest, err := safeJoin(root, h.Name)
		if err != nil {
			return count, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return count, errs.Wrap(errs.KindGeneric, err, "make %s", dest)
			}
			continue
		case tar.TypeReg:
		default:
			// A symlink is the other way to write outside the directory, and
			// nothing in a LaTeX source needs one.
			continue
		}

		if written+h.Size > maxExtract {
			return count, errs.New(errs.KindGeneric,
				"%s unpacks to more than %d bytes, which is not a paper", path, int64(maxExtract))
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return count, errs.Wrap(errs.KindGeneric, err, "make %s", filepath.Dir(dest))
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return count, errs.Wrap(errs.KindGeneric, err, "write %s", dest)
		}
		n, err := io.Copy(out, io.LimitReader(tr, maxExtract))
		closeErr := out.Close()
		if err != nil {
			return count, errs.Wrap(errs.KindGeneric, err, "write %s", dest)
		}
		if closeErr != nil {
			return count, errs.Wrap(errs.KindGeneric, closeErr, "write %s", dest)
		}
		written += n
		count++
	}
}

// gunzipOne unpacks a single gzipped file into root.
func gunzipOne(path, root string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "open %s", path)
	}
	defer func() { _ = f.Close() }()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "%s is not gzipped", path)
	}
	defer func() { _ = zr.Close() }()

	// The gzip header carries the original name where the submission had one.
	name := zr.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".gz")
	}
	dest, err := safeJoin(root, name)
	if err != nil {
		return 0, err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "write %s", dest)
	}
	_, err = io.Copy(out, io.LimitReader(zr, maxExtract))
	closeErr := out.Close()
	if err != nil {
		return 0, errs.Wrap(errs.KindGeneric, err, "write %s", dest)
	}
	if closeErr != nil {
		return 0, errs.Wrap(errs.KindGeneric, closeErr, "write %s", dest)
	}
	return 1, nil
}

// safeJoin resolves an archive entry against the destination and refuses
// anything that lands outside it.
//
// This is the check that stops an archive with ../../.ssh/authorized_keys in it
// from writing there. It is done on the cleaned path rather than by looking for
// "..", because a/../../b has no leading dots and still escapes.
func safeJoin(root, name string) (string, error) {
	if name == "" {
		return "", errs.New(errs.KindGeneric, "the archive has an entry with no name")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(filepath.ToSlash(name), "/") {
		return "", errs.New(errs.KindGeneric, "the archive wants to write to %s, which is outside %s", name, root)
	}
	dest := filepath.Join(root, filepath.FromSlash(name))
	if dest != root && !strings.HasPrefix(dest, root+string(filepath.Separator)) {
		return "", errs.New(errs.KindGeneric, "the archive wants to write to %s, which is outside %s", name, root)
	}
	return dest, nil
}

// localName is what to call the file before arXiv says what it calls it. The
// old style slash cannot be in a filename, so it becomes an underscore, which
// is what arXiv does too.
func localName(id axid.ID, kind string) string {
	base := strings.ReplaceAll(id.Versioned(), "/", "_")
	switch kind {
	case KindHTML:
		return base + ".html"
	case KindSource:
		return base + ".tar.gz"
	}
	return base + ".pdf"
}

// versionOf reads a version out of a filename arXiv chose, "1706.03762v7.pdf".
func versionOf(name string) int {
	i := strings.LastIndex(name, "v")
	if i < 0 {
		return 0
	}
	rest := name[i+1:]
	if j := strings.Index(rest, "."); j >= 0 {
		rest = rest[:j]
	}
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// missingFile says which of the three reasons a file is not there, because
// "404" is true and useless.
func missingFile(kind, id, url string) error {
	switch kind {
	case KindHTML:
		return errs.NotFound("arxiv has no HTML rendering for %s; it renders HTML for papers submitted since December 2023 and for some earlier ones", id)
	case KindSource:
		return errs.NotFound("arxiv has no source for %s, which happens when the paper was submitted as a PDF", id)
	}
	return errs.NotFound("arxiv has nothing at %s, which usually means the id or the version does not exist", url)
}

// EstimateDownload is the sentence printed before a download of several files,
// so a wait is announced rather than discovered.
func EstimateDownload(n int) string {
	d := time.Duration(n) * HTMLPlane.Pace
	return fmt.Sprintf("%d files at %s apart is about %s", n, HTMLPlane.Pace, d.Round(time.Second))
}
