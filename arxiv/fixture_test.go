package arxiv

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures under testdata are real responses, saved verbatim on the days
// named in their responseDate elements. They are not hand written, because a
// hand written sample only proves the parser handles what its author already
// knew about.

// fixture reads a saved response.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// atomFixture decodes a saved export API response and returns its first entry.
func atomFixture(t *testing.T, name string) atomEntry {
	t.Helper()
	var feed atomFeed
	if err := xml.Unmarshal(fixture(t, name), &feed); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if len(feed.Entries) == 0 {
		t.Fatalf("%s has no entries", name)
	}
	return feed.Entries[0]
}

// oaiFixture decodes a saved OAI-PMH GetRecord response.
func oaiFixture(t *testing.T, name string) *oaiRecord {
	t.Helper()
	var resp oaiResponse
	if err := xml.Unmarshal(fixture(t, name), &resp); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if code := resp.Error.Code; code != "" {
		t.Fatalf("%s carries an oai error: %s", name, code)
	}
	rec := resp.GetRecord.Record
	if rec.Header.Identifier == "" {
		t.Fatalf("%s has no record header", name)
	}
	return &rec
}

// paperFixture builds a record from a saved export API response, which is the
// starting point every deeper surface merges into.
func paperFixture(t *testing.T, name string) Paper {
	t.Helper()
	return entryToPaper(atomFixture(t, name), "https://export.arxiv.org/api/query", testTime)
}
