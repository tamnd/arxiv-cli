package arxiv

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
)

// unmarshalFeed is a test helper exposed from within the package.
func unmarshalFeed(data []byte, feed *atomFeed) error {
	return xml.Unmarshal(data, feed)
}

// sampleFeed is a minimal Atom XML response with two entries.
const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"
      xmlns:arxiv="http://arxiv.org/schemas/atom">
  <opensearch:totalResults>2</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>10</opensearch:itemsPerPage>
  <entry>
    <id>http://arxiv.org/abs/1706.03762v7</id>
    <title>Attention Is All You Need</title>
    <summary>The dominant sequence transduction models are based on complex
recurrent or convolutional neural networks.</summary>
    <published>2017-06-12T17:00:00Z</published>
    <updated>2023-08-02T15:25:27Z</updated>
    <author><name>Ashish Vaswani</name></author>
    <author><name>Noam Shazeer</name></author>
    <author><name>Niki Parmar</name></author>
    <author><name>Jakob Uszkoreit</name></author>
    <link href="http://arxiv.org/abs/1706.03762v7" type="text/html" rel="alternate"/>
    <link href="http://arxiv.org/pdf/1706.03762v7" type="application/pdf" title="pdf" rel="related"/>
    <arxiv:primary_category term="cs.CL" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.CL" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.LG" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/1810.04805v2</id>
    <title>BERT: Pre-training of Deep Bidirectional Transformers</title>
    <summary>We introduce a new language representation model called BERT.</summary>
    <published>2018-10-11T00:00:00Z</published>
    <updated>2019-05-24T00:00:00Z</updated>
    <author><name>Jacob Devlin</name></author>
    <author><name>Ming-Wei Chang</name></author>
    <link href="http://arxiv.org/abs/1810.04805v2" type="text/html" rel="alternate"/>
    <link href="http://arxiv.org/pdf/1810.04805v2" type="application/pdf" title="pdf" rel="related"/>
    <arxiv:primary_category term="cs.CL" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.CL" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
</feed>`

// emptyFeed is an Atom XML response with no entries.
const emptyFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <opensearch:totalResults>0</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>10</opensearch:itemsPerPage>
</feed>`

func newTestClient(ts *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.Rate = 0 // no pacing in tests
	c := NewClient(cfg)
	c.httpClient = ts.Client()
	return c
}

func TestSearch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	feed, err := c.getXML(context.Background(), ts.URL+"/api/query?search_query=all:attention")
	if err != nil {
		t.Fatalf("getXML error: %v", err)
	}
	papers := feedToPapers(feed)
	if len(papers) != 2 {
		t.Fatalf("expected 2 papers, got %d", len(papers))
	}
	p := papers[0]
	if p.ID != "1706.03762" {
		t.Errorf("ID: got %q, want %q", p.ID, "1706.03762")
	}
	if p.Title != "Attention Is All You Need" {
		t.Errorf("Title: got %q", p.Title)
	}
	if len(p.Authors) != 4 {
		t.Errorf("Authors: got %d, want 4", len(p.Authors))
	}
	if p.Authors[0] != "Ashish Vaswani" {
		t.Errorf("Authors[0]: got %q", p.Authors[0])
	}
	if p.Category != "cs.CL" {
		t.Errorf("Category: got %q, want cs.CL", p.Category)
	}
	if p.Published != "2017-06-12" {
		t.Errorf("Published: got %q, want 2017-06-12", p.Published)
	}
	if p.URL != "https://arxiv.org/abs/1706.03762" {
		t.Errorf("URL: got %q", p.URL)
	}
	if p.PDF != "https://arxiv.org/pdf/1706.03762" {
		t.Errorf("PDF: got %q", p.PDF)
	}
}

func TestPaperFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	feed, err := c.getXML(context.Background(), ts.URL+"?id_list=1706.03762")
	if err != nil {
		t.Fatalf("getXML error: %v", err)
	}
	papers := feedToPapers(feed)
	if len(papers) == 0 {
		t.Fatal("expected at least one paper")
	}
}

func TestPaperNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(emptyFeed))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	feed, err := c.getXML(context.Background(), ts.URL+"?id_list=9999.99999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(feed.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(feed.Entries))
	}
}

func TestXMLDecode(t *testing.T) {
	var feed atomFeed
	if err := unmarshalFeed([]byte(sampleFeed), &feed); err != nil {
		t.Fatalf("xml decode error: %v", err)
	}
	if feed.Total != 2 {
		t.Errorf("Total: got %d, want 2", feed.Total)
	}
	if len(feed.Entries) != 2 {
		t.Errorf("Entries: got %d, want 2", len(feed.Entries))
	}
	e := feed.Entries[0]
	if e.PrimaryCategory.Term != "cs.CL" {
		t.Errorf("PrimaryCategory: got %q, want cs.CL", e.PrimaryCategory.Term)
	}
	if len(e.Authors) != 4 {
		t.Errorf("Authors: got %d, want 4", len(e.Authors))
	}
}

func TestCleanText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello world  ", "hello world"},
		{"line1\nline2", "line1 line2"},
		{"a  b   c", "a b c"},
		{"", ""},
	}
	for _, tc := range cases {
		got := cleanText(tc.in)
		if got != tc.want {
			t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEntryToPaper(t *testing.T) {
	e := atomEntry{
		ID:      "http://arxiv.org/abs/1706.03762v7",
		Title:   "  Attention Is All You Need  ",
		Summary: "The dominant sequence\ntransduction models.",
		Authors: []atomName{
			{Name: "Ashish Vaswani"},
			{Name: "Noam Shazeer"},
		},
		Published: "2017-06-12T17:00:00Z",
		Updated:   "2023-08-02T15:25:27Z",
		Links: []atomLink{
			{Href: "http://arxiv.org/abs/1706.03762v7", Type: "text/html"},
			{Href: "http://arxiv.org/pdf/1706.03762v7", Type: "application/pdf"},
		},
		PrimaryCategory: atomCat{Term: "cs.CL"},
	}
	p := entryToPaper(e)
	if p.ID != "1706.03762" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.Title != "Attention Is All You Need" {
		t.Errorf("Title: got %q", p.Title)
	}
	if p.Summary != "The dominant sequence transduction models." {
		t.Errorf("Summary: got %q", p.Summary)
	}
	if p.Published != "2017-06-12" {
		t.Errorf("Published: got %q", p.Published)
	}
	if p.URL != "https://arxiv.org/abs/1706.03762" {
		t.Errorf("URL: got %q", p.URL)
	}
	if p.PDF != "https://arxiv.org/pdf/1706.03762" {
		t.Errorf("PDF: got %q", p.PDF)
	}
	if p.Category != "cs.CL" {
		t.Errorf("Category: got %q", p.Category)
	}
}

func TestParseNewStyle(t *testing.T) {
	id, err := ParsePaperID("1706.03762")
	if err != nil || id != "1706.03762" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func TestParseVersioned(t *testing.T) {
	id, err := ParsePaperID("1706.03762v7")
	if err != nil || id != "1706.03762" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func TestParseLegacy(t *testing.T) {
	id, err := ParsePaperID("hep-th/9705158")
	if err != nil || id != "hep-th/9705158" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func TestParseFullURL(t *testing.T) {
	id, err := ParsePaperID("https://arxiv.org/abs/1706.03762v7")
	if err != nil || id != "1706.03762" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func TestParseExportURL(t *testing.T) {
	id, err := ParsePaperID("https://export.arxiv.org/abs/1706.03762")
	if err != nil || id != "1706.03762" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := ParsePaperID("notanid")
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := ParsePaperID("")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestStripArxivID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://arxiv.org/abs/1706.03762v7", "1706.03762"},
		{"https://arxiv.org/abs/1706.03762", "1706.03762"},
		{"1706.03762v7", "1706.03762"},
		{"1706.03762", "1706.03762"},
		{"hep-th/9705158", "hep-th/9705158"},
	}
	for _, tc := range cases {
		got := stripArxivID(tc.in)
		if got != tc.want {
			t.Errorf("stripArxivID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
