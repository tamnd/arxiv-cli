package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// oaiBase is the real OAI-PMH endpoint.
//
// The documented base is https://export.arxiv.org/oai2 and it redirects twice
// to get here, so this points at the destination and keeps redirect following
// on. That way a working client costs one request instead of three, and it
// still survives the host moving again.
const oaiBase = "https://oaipmh.arxiv.org/oai"

// The two metadata formats worth reading. oai_dc carries less than s1 does and
// arXivOld is superseded, so neither is fetched.
const (
	// FormatArxiv gives structured author names and the bibliographic classes,
	// which appear on no other surface.
	FormatArxiv = "arXiv"
	// FormatArxivRaw gives the version history and the submitter.
	FormatArxivRaw = "arXivRaw"
)

// oaiURL builds a GetRecord request for one paper.
func oaiURL(verb, identifier, prefix string) string {
	v := url.Values{}
	v.Set("verb", verb)
	if identifier != "" {
		v.Set("identifier", identifier)
	}
	if prefix != "" {
		v.Set("metadataPrefix", prefix)
	}
	return oaiBase + "?" + v.Encode()
}

// ─── the wire types ───

type oaiResponse struct {
	XMLName   xml.Name   `xml:"OAI-PMH"`
	Error     oaiError   `xml:"error"`
	GetRecord oaiRecords `xml:"GetRecord"`
}

type oaiError struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type oaiRecords struct {
	Record oaiRecord `xml:"record"`
}

type oaiRecord struct {
	Header   oaiHeader   `xml:"header"`
	Metadata oaiMetadata `xml:"metadata"`
}

type oaiHeader struct {
	// Status is "deleted" on a withdrawn paper. arXiv's deletedRecord policy is
	// persistent, so a withdrawn paper comes back with a header and no
	// metadata rather than vanishing.
	Status     string   `xml:"status,attr"`
	Identifier string   `xml:"identifier"`
	Datestamp  string   `xml:"datestamp"`
	SetSpecs   []string `xml:"setSpec"`
}

type oaiMetadata struct {
	Arxiv    oaiArxiv    `xml:"arXiv"`
	ArxivRaw oaiArxivRaw `xml:"arXivRaw"`
}

// oaiArxiv is the arXiv metadata format.
type oaiArxiv struct {
	ID         string      `xml:"id"`
	Created    string      `xml:"created"`
	Updated    string      `xml:"updated"`
	Authors    []oaiAuthor `xml:"authors>author"`
	Title      string      `xml:"title"`
	Categories string      `xml:"categories"`
	Comments   string      `xml:"comments"`
	ReportNo   string      `xml:"report-no"`
	JournalRef string      `xml:"journal-ref"`
	DOI        string      `xml:"doi"`
	MSCClass   string      `xml:"msc-class"`
	ACMClass   string      `xml:"acm-class"`
	License    string      `xml:"license"`
	Abstract   string      `xml:"abstract"`
	Proxy      string      `xml:"proxy"`
}

type oaiAuthor struct {
	Keyname     string `xml:"keyname"`
	Forenames   string `xml:"forenames"`
	Suffix      string `xml:"suffix"`
	Affiliation string `xml:"affiliation"`
}

// oaiArxivRaw is the arXivRaw metadata format, which is the authoritative
// version history and the reason s2 is not optional.
type oaiArxivRaw struct {
	ID         string       `xml:"id"`
	Submitter  string       `xml:"submitter"`
	Versions   []oaiVersion `xml:"version"`
	Title      string       `xml:"title"`
	Authors    string       `xml:"authors"`
	Categories string       `xml:"categories"`
	Comments   string       `xml:"comments"`
	ReportNo   string       `xml:"report-no"`
	JournalRef string       `xml:"journal-ref"`
	DOI        string       `xml:"doi"`
	MSCClass   string       `xml:"msc-class"`
	ACMClass   string       `xml:"acm-class"`
	License    string       `xml:"license"`
	Abstract   string       `xml:"abstract"`
}

type oaiVersion struct {
	Version    string `xml:"version,attr"`
	Date       string `xml:"date"`
	Size       string `xml:"size"`
	SourceType string `xml:"source_type"`
}

// ─── fetching ───

// getOAI fetches one GetRecord response and decodes it.
func (c *Client) getOAI(ctx context.Context, id, prefix string) (*oaiRecord, string, error) {
	u := oaiURL("GetRecord", "oai:arXiv.org:"+id, prefix)
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLPaper)
	if err != nil {
		return nil, u, err
	}
	var out oaiResponse
	if err := xml.Unmarshal(resp.Body, &out); err != nil {
		return nil, u, fmt.Errorf("decode oai response: %w", err)
	}
	// OAI reports its own errors inside a 200, the way s1 does, so the body has
	// to be checked even on success.
	if code := strings.TrimSpace(out.Error.Code); code != "" {
		if code == "idDoesNotExist" {
			return nil, u, fmt.Errorf("%s: %w", id, ErrNotFound)
		}
		return nil, u, fmt.Errorf("oai %s: %s", code, cleanText(out.Error.Message))
	}
	rec := out.GetRecord.Record
	if rec.Header.Identifier == "" {
		return nil, u, fmt.Errorf("%s: %w", id, ErrNotFound)
	}
	return &rec, u, nil
}

// ─── merging ───

// mergeOAIArxiv folds the arXiv metadata format into a paper.
//
// Everything s1 already answered for is left alone, except where s2 knows more:
// the author names are structured here and nowhere else, so they replace the
// display-only list rather than sitting beside it.
func mergeOAIArxiv(p *Paper, rec *oaiRecord, source string) {
	p.addSurface(SurfaceOAI, source)
	m := rec.Metadata.Arxiv

	mergeOAIHeader(p, rec)

	if p.Title == "" {
		p.Title = cleanText(m.Title)
		p.setVia("title", SurfaceOAI)
	}
	if p.Abstract == "" {
		p.Abstract = cleanText(m.Abstract)
		p.setVia("abstract", SurfaceOAI)
	}
	if p.Comment == "" {
		if v := cleanText(m.Comments); v != "" {
			p.Comment = v
			p.setVia("comment", SurfaceOAI)
		}
	}
	if p.JournalRef == "" {
		if v := cleanText(m.JournalRef); v != "" {
			p.JournalRef = v
			p.setVia("journal_ref", SurfaceOAI)
		}
	}
	if p.PublisherDOI == "" {
		if v := cleanText(m.DOI); v != "" {
			p.PublisherDOI = v
			p.setVia("publisher_doi", SurfaceOAI)
		}
	}
	// These four are not on s1 at all, so in practice this is where they get
	// filled. They are still guarded, so via always names the surface that
	// supplied the value standing in the field.
	if p.ReportNo == "" {
		if v := cleanText(m.ReportNo); v != "" {
			p.ReportNo = v
			p.setVia("report_no", SurfaceOAI)
		}
	}
	if len(p.MSCClass) == 0 {
		if v := splitClasses(m.MSCClass); len(v) > 0 {
			p.MSCClass = v
			p.setVia("msc_class", SurfaceOAI)
		}
	}
	if len(p.ACMClass) == 0 {
		if v := splitClasses(m.ACMClass); len(v) > 0 {
			p.ACMClass = v
			p.setVia("acm_class", SurfaceOAI)
		}
	}
	if p.License == "" {
		if v := cleanText(m.License); v != "" {
			p.License = v
			p.setVia("license", SurfaceOAI)
		}
	}
	if cats := strings.Fields(m.Categories); len(cats) > 0 && len(p.Categories) == 0 {
		p.Categories = primaryFirst(cats, cats[0])
		p.PrimaryCategory = cats[0]
		p.CrossLists = crossLists(p.Categories, p.PrimaryCategory)
		p.setVia("categories", SurfaceOAI)
	}

	// The structured names are the whole reason to read this format. They are
	// only taken when the surface actually split them, so a keyname-only entry
	// like "The ATLAS Collaboration" stays one name and is not turned into a
	// forename and a surname it does not have.
	if authors := oaiAuthors(m.Authors); len(authors) > 0 {
		p.Authors = mergeAuthors(p.Authors, authors)
		p.AuthorLine = authorLine(p.Authors)
		p.setVia("authors", SurfaceOAI)
	}

	// created is deliberately not read. It was measured against s1's published
	// on two papers and was the current version's date both times, so filling
	// first_submitted from it would put a 2023 date on a 2017 paper.
}

// mergeOAIRaw folds the arXivRaw format in, which is the version history.
func mergeOAIRaw(p *Paper, rec *oaiRecord, source string) {
	p.addSurface(SurfaceOAI, source)
	m := rec.Metadata.ArxivRaw

	mergeOAIHeader(p, rec)

	if v := cleanText(m.Submitter); v != "" {
		p.Submitter = v
		p.setVia("submitter", SurfaceOAI)
	}
	if versions := rawVersions(m.Versions); len(versions) > 0 {
		p.Versions = versions
		p.setVia("versions", SurfaceOAI)
		last := versions[len(versions)-1]
		if p.Version == 0 {
			p.Version = last.Version
		}
		p.IsLatest = p.Version == last.Version
	}
	if p.License == "" {
		if v := cleanText(m.License); v != "" {
			p.License = v
			p.setVia("license", SurfaceOAI)
		}
	}
	if p.Comment == "" {
		if v := cleanText(m.Comments); v != "" {
			p.Comment = v
			p.setVia("comment", SurfaceOAI)
		}
	}
	if p.ReportNo == "" {
		if v := cleanText(m.ReportNo); v != "" {
			p.ReportNo = v
			p.setVia("report_no", SurfaceOAI)
		}
	}
}

// mergeOAIHeader takes the two facts that live on the header rather than in
// either metadata format.
func mergeOAIHeader(p *Paper, rec *oaiRecord) {
	if rec.Header.Status == "deleted" {
		p.Withdrawn = true
		p.setVia("withdrawn", SurfaceOAI)
	}
	if t, ok := parseOAIDate(rec.Header.Datestamp); ok {
		p.OAIDatestamp = t
		p.setVia("oai_datestamp", SurfaceOAI)
	}
	if p.OAIID == "" {
		p.OAIID = strings.TrimSpace(rec.Header.Identifier)
	}
}

// oaiAuthors turns the structured author elements into the model's authors.
func oaiAuthors(list []oaiAuthor) []Author {
	out := make([]Author, 0, len(list))
	for _, a := range list {
		keyname := cleanText(a.Keyname)
		forenames := cleanText(a.Forenames)
		if suffix := cleanText(a.Suffix); suffix != "" {
			keyname = strings.TrimSpace(keyname + " " + suffix)
		}
		if keyname == "" && forenames == "" {
			continue
		}
		name := strings.TrimSpace(forenames + " " + keyname)
		author := Author{Name: name, Keyname: keyname, Via: SurfaceOAI}
		// A keyname with no forenames is a collaboration or a mononym, not a
		// surname, so it is not claimed as one.
		if forenames != "" {
			author.Forenames = forenames
		} else {
			author.Keyname = ""
		}
		author.Affiliation = cleanText(a.Affiliation)
		out = append(out, author)
	}
	return out
}

// mergeAuthors keeps the richer of two author lists.
//
// The lists come from different surfaces and they agree on order but not on
// shape, so the merge is positional: the structured entry wins on the name
// parts and the display entry keeps whatever it had that the other lacks.
func mergeAuthors(have, structured []Author) []Author {
	if len(have) != len(structured) {
		// Different lengths mean the two surfaces disagree about who the
		// authors are, which is not something to resolve by guessing. The
		// structured list is the one with more in it, so it wins whole.
		return structured
	}
	out := make([]Author, len(have))
	for i := range have {
		out[i] = structured[i]
		if out[i].Affiliation == "" {
			out[i].Affiliation = have[i].Affiliation
		}
		if out[i].ORCID == "" {
			out[i].ORCID = have[i].ORCID
		}
	}
	return out
}

// rawVersions parses the version history.
func rawVersions(list []oaiVersion) []Version {
	out := make([]Version, 0, len(list))
	for _, v := range list {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(v.Version), "v"))
		if err != nil {
			continue
		}
		ver := Version{Version: n, Via: SurfaceOAI}
		if t, ok := parseRawDate(v.Date); ok {
			ver.Date = t
		}
		if size, ok := parseKilobytes(v.Size); ok {
			ver.SizeBytes = size
		}
		if letter := strings.TrimSpace(v.SourceType); letter != "" {
			ver.SourceType = letter
			// An unrecognised letter leaves the interpretation absent rather
			// than guessing, because arXiv is free to add one.
			if kind, ok := sourceKinds[letter]; ok {
				ver.SourceKind = kind
			}
		}
		out = append(out, ver)
	}
	sortVersions(out)
	return out
}

// rawDateLayouts are what arXivRaw's date element looks like.
//
// It is RFC 1123 with a GMT zone, and the day of the month is sometimes zero
// padded and sometimes not, so both spellings are tried.
var rawDateLayouts = []string{
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04:05 -0700",
}

// parseRawDate reads a submission history timestamp in UTC.
func parseRawDate(s string) (time.Time, bool) {
	s = cleanText(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range rawDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// parseOAIDate reads the header datestamp, which is a day and nothing finer.
func parseOAIDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// parseKilobytes normalises a size from either surface's spelling.
//
// arXivRaw writes "1102kb" and the abstract page writes "1,102 KB". Both mean
// kilobytes, so both are multiplied by 1024. The two differ by one on some
// versions, which is rounding on arXiv's side and not something to reconcile.
func parseKilobytes(s string) (int64, bool) {
	s = strings.ToLower(cleanText(s))
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(s, "kb")), " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n * 1024, true
}
