package arxiv

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// entry is one thing to put in a test archive.
type entry struct {
	name string
	body string
	kind byte
	link string
}

// tarball writes a gzipped tar under t.TempDir and hands back its path. The
// archives here are built rather than saved, because the cases worth testing
// are the ones arXiv does not serve: an entry that escapes the directory, a
// symlink, an absolute path.
func tarball(t *testing.T, name string, entries []entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: kind, Linkname: e.link}
		if kind == tar.TypeDir {
			h.Mode = 0o755
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, closer := range []func() error{tw.Close, zw.Close, f.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// gzipped writes a single gzipped file, which is how arXiv serves a submission
// that is one TeX file.
func gzipped(t *testing.T, name, inner, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	zw.Name = inner
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []func() error{zw.Close, f.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestExtractUnpacksASourceArchive(t *testing.T) {
	archive := tarball(t, "arXiv-2401.00001v1.tar.gz", []entry{
		{name: "paper.tex", body: `\documentclass{article}`},
		{name: "figures/", kind: tar.TypeDir},
		{name: "figures/plot.pdf", body: "%PDF-1.4"},
	})
	dir := filepath.Join(t.TempDir(), "out")

	n, err := extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("extracted %d files, want the 2 regular ones", n)
	}
	body, err := os.ReadFile(filepath.Join(dir, "figures", "plot.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "%PDF-1.4" {
		t.Errorf("plot.pdf holds %q", body)
	}
}

// This is the one that matters. An archive with ../ in an entry name is how a
// download writes over something it was never asked to touch, so the extract
// refuses the whole archive rather than skipping the entry and carrying on.
func TestExtractRefusesToWriteOutsideTheDirectory(t *testing.T) {
	cases := map[string]string{
		"a parent":       "../escaped.tex",
		"a longer climb": "figures/../../escaped.tex",
		"an absolute":    "/tmp/escaped.tex",
	}
	for name, entryName := range cases {
		t.Run(name, func(t *testing.T) {
			archive := tarball(t, "evil.tar.gz", []entry{
				{name: "paper.tex", body: "ok"},
				{name: entryName, body: "not ok"},
			})
			root := t.TempDir()
			dir := filepath.Join(root, "out")

			if _, err := extract(archive, dir); err == nil {
				t.Fatalf("%s was accepted", entryName)
			}
			outside := filepath.Join(root, "escaped.tex")
			if _, err := os.Stat(outside); err == nil {
				t.Errorf("%s was written", outside)
			}
			if _, err := os.Stat("/tmp/escaped.tex"); err == nil {
				t.Errorf("/tmp/escaped.tex was written")
			}
		})
	}
}

// A symlink is the other way out of the directory, and nothing in a LaTeX
// source needs one, so it is skipped rather than followed.
func TestExtractSkipsSymlinks(t *testing.T) {
	archive := tarball(t, "links.tar.gz", []entry{
		{name: "paper.tex", body: "ok"},
		{name: "keys", kind: tar.TypeSymlink, link: "/etc/passwd"},
	})
	dir := filepath.Join(t.TempDir(), "out")

	n, err := extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("extracted %d files, want just paper.tex", n)
	}
	if _, err := os.Lstat(filepath.Join(dir, "keys")); err == nil {
		t.Error("the symlink was written")
	}
}

// A submission of one file is a bare gzip and not a tar, and arXiv says which
// on a page rather than in a header, so extract works it out by trying.
func TestExtractFallsBackToASingleGzippedFile(t *testing.T) {
	archive := gzipped(t, "arXiv-2401.00002v1.tar.gz", "ms.tex", `\documentclass{revtex4}`)
	dir := filepath.Join(t.TempDir(), "out")

	n, err := extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("extracted %d files, want 1", n)
	}
	body, err := os.ReadFile(filepath.Join(dir, "ms.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `\documentclass{revtex4}` {
		t.Errorf("ms.tex holds %q", body)
	}
}

// The gzip header carries the original name where there is one. Where there is
// not, the archive's own name without the suffix is the next best thing.
func TestGunzipOneNamesTheFileWhenTheHeaderDoesNot(t *testing.T) {
	archive := gzipped(t, "2401.00003v1.gz", "", "one file")
	root := t.TempDir()

	if _, err := gunzipOne(archive, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "2401.00003v1")); err != nil {
		t.Errorf("nothing at 2401.00003v1: %v", err)
	}
}

// A gzip header naming ../escaped is the same escape by another route.
func TestGunzipOneRefusesAnEscapingName(t *testing.T) {
	archive := gzipped(t, "evil.gz", "../escaped.tex", "not ok")
	root := t.TempDir()

	if _, err := gunzipOne(archive, filepath.Join(root, "out")); err == nil {
		t.Fatal("../escaped.tex was accepted")
	}
}

func TestSafeJoin(t *testing.T) {
	root := "/tmp/papers/2401.00001v1"
	ok := map[string]string{
		"paper.tex":            root + "/paper.tex",
		"figures/plot.pdf":     root + "/figures/plot.pdf",
		"figures/../paper.tex": root + "/paper.tex",
		"./paper.tex":          root + "/paper.tex",
	}
	for in, want := range ok {
		got, err := safeJoin(root, in)
		if err != nil {
			t.Errorf("safeJoin(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("safeJoin(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{"", "../escaped", "../../etc/passwd", "figures/../../escaped", "/etc/passwd"}
	for _, in := range bad {
		if got, err := safeJoin(root, in); err == nil {
			t.Errorf("safeJoin(%q) = %q, want an error", in, got)
		}
	}
}

func TestExtractDir(t *testing.T) {
	cases := map[string]string{
		"/p/arXiv-1706.03762v7.tar.gz": "/p/arXiv-1706.03762v7",
		"/p/2401.00001v1.tgz":          "/p/2401.00001v1",
		"/p/2401.00001v1.gz":           "/p/2401.00001v1",
		// Nothing to strip, so the directory has to be named some other way or
		// it would be the archive's own path.
		"/p/2401.00001v1": "/p/2401.00001v1.src",
	}
	for in, want := range cases {
		if got := extractDir(in); got != want {
			t.Errorf("extractDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// The old style id has a slash in it, which cannot be in a filename, so it
// becomes an underscore the same way arXiv writes it.
func TestLocalName(t *testing.T) {
	cases := []struct {
		ref  string
		kind string
		want string
	}{
		{"1706.03762v7", KindPDF, "1706.03762v7.pdf"},
		{"1706.03762", KindPDF, "1706.03762.pdf"},
		{"1706.03762v7", KindSource, "1706.03762v7.tar.gz"},
		{"1706.03762v7", KindHTML, "1706.03762v7.html"},
		{"hep-th/9711200v3", KindPDF, "hep-th_9711200v3.pdf"},
	}
	for _, c := range cases {
		if got := localName(mustID(t, c.ref), c.kind); got != c.want {
			t.Errorf("localName(%s, %s) = %q, want %q", c.ref, c.kind, got, c.want)
		}
	}
}

func TestVersionOf(t *testing.T) {
	cases := map[string]int{
		"1706.03762v7.pdf":           7,
		"arXiv-1706.03762v12.tar.gz": 12,
		"hep-th_9711200v3.pdf":       3,
		"1706.03762.pdf":             0,
		"paper.pdf":                  0,
	}
	for in, want := range cases {
		if got := versionOf(in); got != want {
			t.Errorf("versionOf(%q) = %d, want %d", in, got, want)
		}
	}
}

// arXiv resolves a bare id to its latest version and names the file with the
// version in it, so a run that asked for 2401.00001 leaves 2401.00001v1.pdf
// behind. Looking only for the derived name would fetch the same megabytes
// again on every run.
func TestAlreadyThereFindsTheVersionedFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "2401.00001v1.pdf"), "old")
	write(t, filepath.Join(dir, "2401.00001v2.pdf"), "newer")

	id := mustID(t, "2401.00001")
	path := filepath.Join(dir, localName(id, KindPDF))
	have, size, ok := alreadyThere(path, id, KindPDF, false)
	if !ok {
		t.Fatal("the file already on disk was not found")
	}
	if filepath.Base(have) != "2401.00001v2.pdf" {
		t.Errorf("found %q, want the highest version", filepath.Base(have))
	}
	if size != int64(len("newer")) {
		t.Errorf("size = %d", size)
	}
}

// A reference that names a version and a caller that names a destination are
// both being specific, and being specific deserves an exact answer rather than
// whatever else is lying about in the directory.
func TestAlreadyThereIsExactWhenTheCallerWasSpecific(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "2401.00001v1.pdf"), "old")

	versioned := mustID(t, "2401.00001v7")
	path := filepath.Join(dir, localName(versioned, KindPDF))
	if have, _, ok := alreadyThere(path, versioned, KindPDF, false); ok {
		t.Errorf("asking for v7 was answered with %q", have)
	}

	bare := mustID(t, "2401.00001")
	named := filepath.Join(dir, "mine.pdf")
	if have, _, ok := alreadyThere(named, bare, KindPDF, true); ok {
		t.Errorf("--out mine.pdf was answered with %q", have)
	}
}

func TestAlreadyThereTakesTheExactPathFirst(t *testing.T) {
	dir := t.TempDir()
	exact := filepath.Join(dir, "2401.00001.pdf")
	write(t, exact, "exactly this")
	write(t, filepath.Join(dir, "2401.00001v9.pdf"), "not this")

	id := mustID(t, "2401.00001")
	have, size, ok := alreadyThere(exact, id, KindPDF, false)
	if !ok || have != exact {
		t.Fatalf("alreadyThere = %q, %v, want %q", have, ok, exact)
	}
	if size != int64(len("exactly this")) {
		t.Errorf("size = %d", size)
	}
}

// A directory with the name the download wants is not a download that already
// happened.
func TestAlreadyThereIgnoresADirectory(t *testing.T) {
	dir := t.TempDir()
	id := mustID(t, "2401.00001v1")
	path := filepath.Join(dir, localName(id, KindPDF))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := alreadyThere(path, id, KindPDF, false); ok {
		t.Error("a directory was taken for a finished download")
	}
}

// A 404 on a file has three different meanings and the message says which,
// because "404" is true and useless.
func TestMissingFileSaysWhy(t *testing.T) {
	cases := map[string]string{
		KindHTML:   "December 2023",
		KindSource: "submitted as a PDF",
		KindPDF:    "does not exist",
	}
	for kind, want := range cases {
		err := missingFile(kind, "2401.00001", "https://arxiv.org/pdf/2401.00001")
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v does not mention %q", kind, err, want)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
