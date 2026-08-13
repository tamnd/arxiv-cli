package arxiv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-cli/pkg/latexml"
)

// archive.go writes one paper's surfaces to disk as arXiv served them. Spec
// 3006 doc 04 section 6.
//
// This is the escape hatch from every judgement the rest of the tool makes. A
// record is a reading of a page, and a reading can be wrong: a field this tool
// does not know about is a field it drops, and a page arXiv changes next year
// is a page nobody can check against afterwards. An archive is the bytes, with
// a hash and a timestamp beside each one, so a record can be argued with.
//
// Nothing here comes out of the cache. An archive served from a week old cache
// entry would be a copy of the cache rather than an archive, and the timestamp
// in meta.json would be a lie about when those bytes were true.

// ArchiveOptions is one archive.
type ArchiveOptions struct {
	// Dir is the root. Each paper gets a directory under it, named after the
	// id with the old style slash turned into an underscore.
	Dir string
	// Files adds the PDF and the submission source, which are the only surfaces
	// here measured in megabytes rather than kilobytes.
	Files bool
	// Progress is where the running commentary goes.
	Progress func(string, ...any)
}

// Archive is meta.json: what was fetched, when, how big it was and what it
// hashes to.
type Archive struct {
	ID  string `json:"id" kit:"id" table:"id"`
	Dir string `json:"dir" table:"dir,truncate"`
	// At is when the archive started, which is the timestamp the bytes are true
	// as of.
	At        time.Time      `json:"at" table:"-"`
	UserAgent string         `json:"user_agent" table:"-"`
	Surfaces  []ArchivedFile `json:"surfaces" table:"-"`
	Files     int            `json:"files" table:"files"`
	Bytes     int64          `json:"bytes" table:"bytes"`
	// Missing names the surfaces that had nothing on them, which for most
	// papers is the rendering and the trackbacks.
	Missing []string `json:"missing,omitempty" table:"-"`
}

// ArchivedFile is one surface on disk.
type ArchivedFile struct {
	Surface string    `json:"surface"`
	Name    string    `json:"name,omitempty"`
	URL     string    `json:"url"`
	Status  int       `json:"status"`
	Bytes   int64     `json:"bytes"`
	SHA256  string    `json:"sha256,omitempty"`
	At      time.Time `json:"at"`
	Error   string    `json:"error,omitempty"`
}

// String is the sentence printed when an archive finishes.
func (a Archive) String() string {
	return fmt.Sprintf("archived %s to %s: %d files, %d bytes", a.ID, a.Dir, a.Files, a.Bytes)
}

// archiving is one run's state: the client, where it is writing, and what it
// has written so far.
type archiving struct {
	c   *Client
	dir string
	out Archive
}

// Archive writes every surface of one paper to disk, and the record built from
// exactly those bytes beside them.
//
// The record is built from the archived bytes rather than read again, so
// record.json and the files next to it can never disagree. If the abstract page
// changed between the two reads, a record read separately would describe a page
// that is not in the directory.
func (c *Client) Archive(ctx context.Context, ref string, o ArchiveOptions) (Archive, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return Archive{}, err
	}
	root := o.Dir
	if root == "" {
		root = "archive"
	}
	dir := filepath.Join(root, archiveName(id.Canonical))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Archive{}, fmt.Errorf("create %s: %w", dir, err)
	}

	a := &archiving{
		c:   c,
		dir: dir,
		out: Archive{
			ID:        id.Canonical,
			Dir:       dir,
			At:        c.now(),
			UserAgent: c.userAgent,
		},
	}
	say := o.Progress
	if say == nil {
		say = c.notice
	}
	say("archiving %s to %s, which is %s of requests", id.Canonical, dir, archiveCost(o.Files).Round(time.Second))

	// s1 is the one surface an archive cannot do without: it is the cheapest,
	// it answers for every paper arXiv has, and it is what the record is built
	// on top of.
	body, err := a.get(ctx, SurfaceAPI, "s1-api.xml", apiURLFor(id))
	if err != nil {
		return a.out, err
	}
	p, err := paperFromFeed(body, apiURLFor(id), c.now())
	if err != nil {
		return a.out, err
	}
	c.noteUnknownCategories(p.Categories...)

	// s2 twice, because the two metadata formats are different records and
	// arXivRaw is the only one with the version table on it.
	for _, f := range []struct {
		prefix string
		name   string
		merge  func(*Paper, *oaiRecord, string)
	}{
		{FormatArxiv, "s2-oai-arxiv.xml", mergeOAIArxiv},
		{FormatArxivRaw, "s2-oai-arxivraw.xml", mergeOAIRaw},
	} {
		u := oaiURL("GetRecord", "oai:arXiv.org:"+p.ID, f.prefix)
		body, err := a.get(ctx, SurfaceOAI, f.name, u)
		if err != nil {
			continue
		}
		rec, err := parseOAIRecord(body, p.ID)
		if err != nil {
			a.note(f.name, err)
			continue
		}
		f.merge(&p, rec, u)
	}

	if body, err := a.get(ctx, SurfaceAbs, "s3-abs.html", absURL(p.ID)); err == nil {
		page, err := parseAbs(body)
		if err != nil {
			a.note("s3-abs.html", err)
		} else {
			mergeAbs(&p, page, absURL(p.ID))
		}
	}

	// s9 adds nothing to the record. It is archived because it is a surface and
	// an archive that skipped the surfaces with nothing new on them would not
	// be an archive of the paper, it would be an archive of this tool's opinion
	// of the paper.
	_, _ = a.get(ctx, SurfaceBibTeX, "s9-bibtex.bib", bibtexBase+p.ID)

	if p.HasHTML {
		u := p.HTMLURL
		if u == "" {
			u = htmlURL(p.ID, p.Version)
		}
		if body, err := a.get(ctx, SurfaceFullText, "s10-fulltext.html", u); err == nil {
			doc, err := latexml.Parse(body)
			if err != nil {
				a.note("s10-fulltext.html", err)
			} else {
				mergeFullText(&p, doc, u)
			}
		}
	} else {
		a.out.Missing = append(a.out.Missing, SurfaceFullText+": arXiv has no rendering of this paper")
	}

	_, _ = a.get(ctx, SurfaceTrackback, "s11-trackbacks.html", trackbackBase+p.ID)

	if o.Files {
		if body, err := a.get(ctx, SurfaceFiles, "s12-paper.pdf", pdfBase+p.ID); err == nil {
			a.truncated("s12-paper.pdf", body)
		}
		if body, err := a.get(ctx, SurfaceFiles, "s12-source"+sourceExt(nil), srcBase+p.ID); err == nil {
			// The extension depends on what came back, so the file is renamed
			// once the first two bytes are known.
			a.rename("s12-source", "s12-source"+sourceExt(body))
			a.truncated("s12-source"+sourceExt(body), body)
		}
	}

	// The record says depth text whether or not the rendering was there, which
	// is true: every surface this tool reads was read.
	annotateDepth(&p, DepthText)
	if err := a.write("record.json", jsonBytes(p)); err != nil {
		return a.out, err
	}
	if err := a.write("meta.json", jsonBytes(a.out)); err != nil {
		return a.out, err
	}
	say("%s", a.out)
	return a.out, nil
}

// get fetches one surface, writes it, and records what happened either way.
//
// A surface that is not there is not an error the archive stops for. Most
// papers have no trackbacks and most have no rendering, and an archive that
// gave up on the first 404 would never finish one.
func (a *archiving) get(ctx context.Context, surface, name, rawURL string) ([]byte, error) {
	f := ArchivedFile{Surface: surface, URL: rawURL, At: a.c.now()}
	a.c.logf(1, "GET %s", rawURL)
	resp, err := a.c.fetchLive(ctx, rawURL)
	if err != nil {
		f.Error = err.Error()
		a.out.Surfaces = append(a.out.Surfaces, f)
		a.out.Missing = append(a.out.Missing, surface+": "+err.Error())
		return nil, err
	}
	f.Status = resp.Status
	f.Bytes = int64(len(resp.Body))
	sum := sha256.Sum256(resp.Body)
	f.SHA256 = hex.EncodeToString(sum[:])
	f.Name = name
	if err := a.write(name, resp.Body); err != nil {
		f.Error = err.Error()
		a.out.Surfaces = append(a.out.Surfaces, f)
		return nil, err
	}
	a.out.Surfaces = append(a.out.Surfaces, f)
	a.out.Files++
	a.out.Bytes += f.Bytes
	return resp.Body, nil
}

// write puts one file in the archive directory.
func (a *archiving) write(name string, body []byte) error {
	path := filepath.Join(a.dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// rename fixes a name once the bytes said what they were.
func (a *archiving) rename(from, to string) {
	if from == to {
		return
	}
	if err := os.Rename(filepath.Join(a.dir, from), filepath.Join(a.dir, to)); err != nil {
		return
	}
	for i := range a.out.Surfaces {
		if a.out.Surfaces[i].Name == from {
			a.out.Surfaces[i].Name = to
		}
	}
}

// note records that a surface was fetched and could not be read. The bytes are
// still on disk, which is the point: the file is there to be looked at when the
// parser is the thing that is wrong.
func (a *archiving) note(name string, err error) {
	for i := range a.out.Surfaces {
		if a.out.Surfaces[i].Name == name {
			a.out.Surfaces[i].Error = err.Error()
		}
	}
	a.c.logf(1, "%s was archived and could not be parsed: %s", name, err)
}

// truncated flags a file that hit the read cap, because a PDF cut off at
// sixteen megabytes is not a copy of anything and an archive should say so
// rather than leave a plausible looking file lying about.
func (a *archiving) truncated(name string, body []byte) {
	if int64(len(body)) < maxBody {
		return
	}
	a.note(name, fmt.Errorf("truncated at the %d MB read cap", maxBody>>20))
}

// paperFromFeed builds the record from an archived s1 body.
func paperFromFeed(body []byte, source string, at time.Time) (Paper, error) {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return Paper{}, fmt.Errorf("decode arxiv response: %w", err)
	}
	if apiErr := errorEntry(&feed); apiErr != nil {
		return Paper{}, apiErr
	}
	if len(feed.Entries) == 0 {
		return Paper{}, ErrNotFound
	}
	return entryToPaper(feed.Entries[0], source, at), nil
}

// apiURLFor is the s1 request for one paper, version and all.
func apiURLFor(id axid.ID) string {
	u, err := Request{IDs: []string{id.Versioned()}, Max: 1}.URL()
	if err != nil {
		// The only way this fails is an empty id list, and there is one id.
		return apiBase + "?id_list=" + id.Versioned()
	}
	return u
}

// archiveName is the id as a directory name. An old style id has a slash in it,
// which would make math/0309136 two directories.
func archiveName(id string) string {
	return strings.ReplaceAll(id, "/", "_")
}

// archiveCost is what an archive costs in wall clock, which is mostly the four
// reads of arxiv.org.
func archiveCost(files bool) time.Duration {
	api, html := 3, 4
	if files {
		html += 2
	}
	return time.Duration(api)*APIPlane.Pace + time.Duration(html)*HTMLPlane.Pace
}

// sourceExt names the submission source by what it actually is. arXiv answers
// /src/ with a gzipped tar for most papers, a PDF for the ones submitted as
// one, and there is no header to ask.
func sourceExt(body []byte) string {
	switch {
	case len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b:
		return ".tar.gz"
	case len(body) >= 4 && string(body[:4]) == "%PDF":
		return ".pdf"
	}
	return ""
}

// jsonBytes is one record as a file, indented because these are read by people.
func jsonBytes(v any) []byte {
	blob, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(blob, '\n')
}
