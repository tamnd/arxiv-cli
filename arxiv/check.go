package arxiv

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/arxiv-cli/pkg/graph"
	"github.com/tamnd/arxiv-cli/pkg/rdf"
)

// check.go holds our Dublin Core up against arXiv's own.
//
// Every mapping is somebody's opinion until it is compared with something. Two
// of arXiv's surfaces publish bibliographic metadata in a vocabulary that is not
// arXiv's: the OAI-PMH oai_dc record is Dublin Core, and the abstract page's
// Highwire citation_* tags are the tags Google Scholar reads. Both cover the
// same handful of facts this tool writes as dc: terms, so both can be read back
// and lined up beside what we wrote.
//
// The point is not that the three agree. It is that where they do not, the
// disagreement is on the screen with a name on it, instead of being decided
// quietly inside a mapping function. dc:date is the one that matters: oai_dc
// gives two dates and says nothing about which is the submission, doc 03 section
// 2.5 decided which one wins, and this is where that decision stays visible.

// CheckRow is one Dublin Core element as the three sources have it.
type CheckRow struct {
	Predicate string `json:"predicate" kit:"id" table:"predicate"`
	Ours      string `json:"arxiv_cli" table:"arxiv-cli"`
	OAIDC     string `json:"oai_dc" table:"oai_dc"`
	Citation  string `json:"citation" table:"citation_*"`
	Agree     string `json:"agree" table:"agree"`
	// Note says what the verdict leaves out: which side was silent, or why two
	// spellings of the same fact are the same fact.
	Note string `json:"note,omitempty" table:"-"`
	// The values behind the three cells, whole and unjoined, because a table cell
	// is truncated to fit and a comparison somebody is arguing with should not
	// end in an ellipsis.
	OursValues []string `json:"arxiv_cli_values,omitempty" table:"-"`
	DCValues   []string `json:"oai_dc_values,omitempty" table:"-"`
	CitValues  []string `json:"citation_values,omitempty" table:"-"`
}

// The verdicts. They are words rather than a boolean because two of the four
// interesting cases are neither yes nor no.
const (
	// AgreeYes is every side that spoke saying the same thing.
	AgreeYes = "yes"
	// AgreeNormalised is the same thing in different spellings, which is the
	// usual answer for names and dates.
	AgreeNormalised = "yes, after normalising"
	// AgreePartial is one side holding more than another, with nothing between
	// them contradicted.
	AgreePartial = "partial"
	// AgreeNo is a contradiction, or one side silent where another spoke.
	AgreeNo = "no"
	// AgreeAlone is this tool saying something nobody else says, which is not a
	// disagreement.
	AgreeAlone = "only arxiv-cli"
)

// Check reads a paper, reads arXiv's own Dublin Core for it, and lines the two
// up with the abstract page's citation tags.
//
// The read is at depth full, because the citation tags are on the abstract page
// and there is no comparing against a surface that was not fetched. That is four
// requests and one of them is on the fifteen second plane, so this is not a
// command to run over a list.
func (c *Client) Check(ctx context.Context, ref string) ([]CheckRow, error) {
	p, edges, err := c.edgesWithPaper(ctx, ref, EdgeOptions{Depth: DepthFull})
	if err != nil {
		return nil, err
	}
	doc := rdf.New()
	AddPaper(doc, p)
	AddClaims(doc, edges)

	id := p.ID
	dc, err := c.getOAIDC(ctx, id)
	if err != nil {
		return nil, err
	}
	tags, err := c.citationTags(ctx, id)
	if err != nil {
		return nil, err
	}
	return compare(doc, rdf.NodeIRI(graph.Paper(id)), dc, tags), nil
}

// getOAIDC fetches the Dublin Core record.
func (c *Client) getOAIDC(ctx context.Context, id string) (oaiDC, error) {
	rec, _, err := c.getOAI(ctx, id, FormatOAIDC)
	if err != nil {
		return oaiDC{}, err
	}
	return rec.Metadata.DC, nil
}

// citationTags reads every citation_* meta tag off the abstract page.
//
// It parses the page again rather than reusing absPage, because absPage is the
// tags after they were merged into the model and this needs them as arXiv wrote
// them. The bytes are in the cache from the read above, so it costs nothing.
func (c *Client) citationTags(ctx context.Context, id string) (map[string][]string, error) {
	resp, err := c.fetch(ctx, absURL(id), TTLPaper)
	if err != nil {
		return nil, err
	}
	return citationTagsFrom(resp.Body)
}

// citationTagsFrom is the parser on its own, so a saved page can be read the
// same way the live one is.
func citationTagsFrom(body []byte) (map[string][]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse abstract page: %w", err)
	}
	out := map[string][]string{}
	doc.Find(`meta[name^="citation_"]`).Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.AttrOr("name", ""))
		if value := cleanText(s.AttrOr("content", "")); name != "" && value != "" {
			out[name] = append(out[name], value)
		}
	})
	return out, nil
}

// element is one row of the comparison: a Dublin Core term, the terms this tool
// writes for it, and the citation tags that carry the same fact.
type element struct {
	Name string
	// Ours are the terms to collect off the paper. dc:identifier is the one that
	// differs: we write schema:identifier, because a DOI is an identifier and
	// arXiv puts four unlike things in dc:identifier.
	Ours []rdf.IRI
	Tags []string
	// Norm folds two spellings of one fact together. Nil means compare as read.
	Norm func(string) string
	// Note is what the reader needs to know about the row before arguing with
	// the verdict.
	Note string
}

// elements is the comparison, in the order the table prints.
var elements = []element{
	{Name: "dc:title", Ours: []rdf.IRI{rdf.DCTitle}, Tags: []string{"citation_title"}, Norm: normText},
	{
		Name: "dc:creator", Ours: []rdf.IRI{rdf.DCCreator}, Tags: []string{"citation_author"}, Norm: normName,
		Note: "three surfaces, three name formats, compared as given name then surname",
	},
	{
		Name: "dc:date", Ours: []rdf.IRI{rdf.DCDate}, Tags: []string{"citation_date", "citation_online_date"}, Norm: normDate,
		Note: "oai_dc gives the submission and the last update without saying which is which, and this tool writes the submission",
	},
	{
		Name: "dc:subject", Ours: []rdf.IRI{rdf.DCSubject}, Tags: nil, Norm: normText,
		Note: "oai_dc names the category and the code is nowhere in it, so this compares the names",
	},
	{
		Name: "dc:description", Ours: []rdf.IRI{rdf.DCDescription}, Tags: []string{"citation_abstract"}, Norm: normText,
		Note: "oai_dc writes the abstract and the comment into the same element",
	},
	{
		Name: "dc:identifier", Ours: []rdf.IRI{rdf.SchemaIdentifier}, Tags: []string{"citation_doi"}, Norm: normIdent,
		Note: "oai_dc puts the abs URL, the journal reference and the DOI in one element, and this tool names the paper by its abs URL rather than repeating it as an identifier",
	},
	{
		Name: "dc:rights", Ours: []rdf.IRI{rdf.DCRights}, Tags: nil, Norm: normIdent,
		Note: "the licence is in the arXiv format's own license element, and arXiv does not write it into oai_dc at all",
	},
	{
		Name: "dc:type", Ours: []rdf.IRI{rdf.RDFType}, Tags: nil, Norm: normType,
		Note: "arXiv says text, which is a DCMI type; the classes this tool writes are inferred and carry no provenance for that reason",
	},
}

// compare builds the table.
func compare(d *rdf.Doc, paper rdf.IRI, dc oaiDC, tags map[string][]string) []CheckRow {
	names := labels(d)
	out := make([]CheckRow, 0, len(elements))
	for _, e := range elements {
		ours := ourValues(d, names, paper, e.Ours)
		theirs := dcValues(dc, e.Name)
		cited := tagValues(tags, e.Tags)
		verdict, note := agree(ours, theirs, cited, e.Norm)
		if note == "" {
			note = e.Note
		} else if e.Note != "" {
			note += "; " + e.Note
		}
		out = append(out, CheckRow{
			Predicate:  e.Name,
			Ours:       cellText(ours),
			OAIDC:      cellText(theirs),
			Citation:   cellText(cited),
			Agree:      verdict,
			Note:       note,
			OursValues: ours,
			DCValues:   theirs,
			CitValues:  cited,
		})
	}
	return out
}

// ourValues collects what this tool wrote about the paper under one term.
//
// An object that is an IRI is reported by the name the document gives it, which
// is what makes the row comparable: oai_dc says Computation and Language and we
// point at a category node, and the two are the same fact only once the node is
// read as its label.
func ourValues(d *rdf.Doc, names map[rdf.IRI]string, paper rdf.IRI, terms []rdf.IRI) []string {
	want := map[rdf.IRI]bool{}
	for _, t := range terms {
		want[t] = true
	}
	var out []string
	for _, s := range d.Statements() {
		if s.Subject != paper || !want[s.Predicate] {
			continue
		}
		switch o := s.Object.(type) {
		case rdf.Literal:
			out = append(out, o.Value)
		case rdf.IRI:
			if name, ok := names[o]; ok {
				out = append(out, name)
				continue
			}
			out = append(out, string(o))
		}
	}
	return tidy(out)
}

// labels is what the document calls each node it named.
func labels(d *rdf.Doc) map[rdf.IRI]string {
	out := map[rdf.IRI]string{}
	for _, s := range d.Statements() {
		lit, ok := s.Object.(rdf.Literal)
		if !ok {
			continue
		}
		switch s.Predicate {
		case rdf.SKOSPrefLabel, rdf.SchemaName:
			out[s.Subject] = lit.Value
		case rdf.RDFSLabel:
			if _, taken := out[s.Subject]; !taken {
				out[s.Subject] = lit.Value
			}
		}
	}
	return out
}

// dcValues pulls one element out of the Dublin Core record.
func dcValues(dc oaiDC, name string) []string {
	switch name {
	case "dc:title":
		return tidy(dc.Titles)
	case "dc:creator":
		return tidy(dc.Creators)
	case "dc:subject":
		return tidy(dc.Subjects)
	case "dc:description":
		return tidy(dc.Descriptions)
	case "dc:date":
		return tidy(dc.Dates)
	case "dc:type":
		return tidy(dc.Types)
	case "dc:identifier":
		return tidy(dc.Identifiers)
	case "dc:rights":
		return tidy(dc.Rights)
	}
	return nil
}

// tagValues pulls the citation tags that carry one element.
func tagValues(tags map[string][]string, names []string) []string {
	var out []string
	for _, n := range names {
		out = append(out, tags[n]...)
	}
	return tidy(out)
}

// agree is the verdict and whatever the verdict alone does not say.
//
// A side that said nothing is left out of the comparison rather than counted as
// a disagreement, because a surface that does not carry a fact has not
// contradicted anybody. Which sides were silent goes in the note.
func agree(ours, dc, cit []string, norm func(string) string) (string, string) {
	if len(ours) == 0 && len(dc) == 0 && len(cit) == 0 {
		return AgreeNo, "nobody said anything, on any of the three"
	}
	silent := []string{}
	if len(dc) == 0 {
		silent = append(silent, "oai_dc")
	}
	if len(cit) == 0 {
		silent = append(silent, "citation_*")
	}
	note := ""
	if len(silent) > 0 {
		note = strings.Join(silent, " and ") + " say nothing here"
	}
	if len(dc) == 0 && len(cit) == 0 {
		return AgreeAlone, note
	}
	if len(ours) == 0 {
		return AgreeNo, "arxiv-cli writes nothing under this term"
	}

	verdict := AgreeYes
	normalised := false
	for _, side := range [][]string{dc, cit} {
		if len(side) == 0 {
			continue
		}
		v, n := match(ours, side, norm)
		normalised = normalised || n
		verdict = worse(verdict, v)
	}
	if verdict == AgreeYes && normalised {
		verdict = AgreeNormalised
	}
	return verdict, note
}

// match compares our values against one other side's.
//
// It reports the verdict and whether normalising was what made them agree,
// because "the same string" and "the same fact spelled two ways" are worth
// telling apart: the second is a code path that can be wrong on a name with a
// particle in it.
func match(ours, theirs []string, norm func(string) string) (string, bool) {
	if same(ours, theirs) {
		return AgreeYes, false
	}
	a, b := fold(ours, norm), fold(theirs, norm)
	if same(a, b) {
		return AgreeYes, true
	}
	shared := 0
	for _, v := range a {
		if contains(b, v) {
			shared++
		}
	}
	if shared == 0 {
		return AgreeNo, false
	}
	return AgreePartial, false
}

// worse keeps the least agreeable of two verdicts, so a row that agrees with one
// side and contradicts the other reads as a contradiction.
func worse(a, b string) string {
	rank := map[string]int{AgreeYes: 0, AgreeNormalised: 1, AgreePartial: 2, AgreeNo: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// fold normalises and sorts a side for comparison.
func fold(in []string, norm func(string) string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if norm != nil {
			v = norm(v)
		}
		if v != "" && !contains(out, v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func same(a, b []string) bool {
	x, y := fold(a, nil), fold(b, nil)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// tidy cleans and deduplicates a side, keeping the order arXiv gave.
func tidy(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = cleanText(v); v != "" && !contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// cellText is the printed form of one side.
//
// It is elided, because one of the terms compared is dc:description and an
// abstract in a table cell makes the other seven rows unreadable. Nothing is
// lost: the whole values are on the record and -o json prints them.
func cellText(values []string) string {
	if len(values) == 0 {
		return "absent"
	}
	joined := strings.Join(values, ", ")
	const width = 56
	if r := []rune(joined); len(r) > width {
		return strings.TrimSpace(string(r[:width-3])) + "..."
	}
	return joined
}

// ─── the normalisers ───

// normText is case and whitespace, which is all two surfaces ever differ by on a
// title or an abstract.
func normText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// normName turns every name format arXiv publishes into given name then
// surname.
//
// The comma is the surface's own split, so it is trusted where it appears and
// nothing is guessed where it does not: a value with no comma is a collaboration
// or a mononym and stays as it is.
func normName(s string) string {
	s = cleanText(s)
	if surname, given, ok := strings.Cut(s, ","); ok {
		surname, given = strings.TrimSpace(surname), strings.TrimSpace(given)
		if surname != "" && given != "" {
			s = given + " " + surname
		}
	}
	return normText(s)
}

// normDate reads every spelling of a day the three surfaces use, and reads a
// timestamp as the day it falls on, because a day is what two surfaces can be
// compared on.
func normDate(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "T "); i > 0 {
		s = s[:i]
	}
	return strings.ReplaceAll(s, "/", "-")
}

// normIdent folds the spellings of an identifier that mean the same resource:
// the scheme, which arXiv writes both ways in the same record, and the doi
// prefix, which is a DOI whether it is written as a URL or not.
func normIdent(s string) string {
	s = strings.ToLower(cleanText(s))
	for _, prefix := range []string{"https://", "http://", "doi:", "doi.org/", "dx.doi.org/", "www."} {
		s = strings.TrimPrefix(s, prefix)
	}
	if i := strings.Index(s, "doi.org/"); i >= 0 {
		s = s[i+len("doi.org/"):]
	}
	return strings.TrimSuffix(s, "/")
}

// normType compares a class by its local name, so schema:ScholarlyArticle and
// text are compared as words rather than as a URL against a word.
func normType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexAny(s, "/#"); i >= 0 {
		s = s[i+1:]
	}
	return s
}
