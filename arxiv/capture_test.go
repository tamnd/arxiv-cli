package arxiv

import (
	"bufio"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// capture_test.go checks the fixture ledger. testdata/capture.txt says where
// every saved file came from and when, and this is what keeps it true.
//
// The reason for the ledger is that a fixture ages. arXiv changes a page, the
// saved bytes stay the same, and a test that passed for a year is testing a
// page that no longer exists. Nothing can stop that, but a row saying which URL
// and which day turns "the parser works" into "the parser worked on the bytes
// arXiv served on this date", which is a claim somebody can go and recheck.

// capture is one row of the ledger.
type capture struct {
	File    string
	Surface string
	Date    string
	From    string
}

// captures reads the ledger, skipping comments and blank lines.
func captures(t *testing.T) []capture {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "capture.txt"))
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []capture
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		text := s.Text()
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		cols := strings.Split(text, "\t")
		if len(cols) != 4 {
			t.Fatalf("line %d has %d columns, want four separated by tabs: %q", line, len(cols), text)
		}
		out = append(out, capture{File: cols[0], Surface: cols[1], Date: cols[2], From: cols[3]})
	}
	if err := s.Err(); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return out
}

// Both ways. A fixture with no row is bytes nobody can trace, and a row with no
// fixture is a ledger describing a directory that has moved on.
func TestEveryFixtureIsInTheLedger(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "capture.txt" {
			continue
		}
		onDisk[e.Name()] = true
	}

	inLedger := map[string]bool{}
	for _, c := range captures(t) {
		if inLedger[c.File] {
			t.Errorf("%s has two rows", c.File)
		}
		inLedger[c.File] = true
		if !onDisk[c.File] {
			t.Errorf("%s is in the ledger and not in testdata", c.File)
		}
	}
	var missing []string
	for name := range onDisk {
		if !inLedger[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s is in testdata and not in the ledger, so nothing says where it came from", name)
	}
}

// Every row says something checkable. The surface is one of the twelve or a
// dash for the two files that are not a surface at all, the date parses and is
// not in the future, and the source is a URL on a host this tool reads or a
// command that made the file.
func TestEveryLedgerRowSaysWhereAndWhen(t *testing.T) {
	for _, c := range captures(t) {
		if c.Surface != "-" && SurfaceNames[c.Surface] == "" {
			t.Errorf("%s claims surface %q, which is not one of the twelve", c.File, c.Surface)
		}
		day, err := time.Parse("2006-01-02", c.Date)
		if err != nil {
			t.Errorf("%s has the date %q, which is not a date: %v", c.File, c.Date, err)
			continue
		}
		if day.After(time.Now()) {
			t.Errorf("%s was captured on %s, which has not happened yet", c.File, c.Date)
		}
		if strings.HasPrefix(c.From, "go test ") {
			continue
		}
		u, err := url.Parse(c.From)
		if err != nil || u.Host == "" {
			t.Errorf("%s came from %q, which is neither a URL nor a command", c.File, c.From)
			continue
		}
		if _, ok := PlaneFor(u.Host); !ok {
			t.Errorf("%s came from %s, which is not a host this tool reads", c.File, u.Host)
		}
	}
}

// The ledger is sorted, because the point of it is reading a diff when a fixture
// is replaced, and a file that grows at the bottom hides which row moved.
func TestTheLedgerIsSorted(t *testing.T) {
	rows := captures(t)
	for i := 1; i < len(rows); i++ {
		if rows[i-1].File >= rows[i].File {
			t.Errorf("%s comes after %s", rows[i].File, rows[i-1].File)
		}
	}
}

// The 429 body is fourteen bytes and two words. It is committed because the
// transport has to recognise a rate limit that arrives with no Retry-After and
// no JSON, and a test that invents the body proves nothing about the one arXiv
// sends.
func TestTheRateLimitBodyIsStillFourteenBytes(t *testing.T) {
	body := fixture(t, "rate_exceeded.txt")
	if len(body) != 14 {
		t.Errorf("the saved 429 body is %d bytes, want fourteen", len(body))
	}
	if !bytes.Equal(body, []byte("Rate exceeded.")) {
		t.Errorf("the saved 429 body is %q", body)
	}
}
