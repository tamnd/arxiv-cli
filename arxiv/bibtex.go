package arxiv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// bibtexBase is s9, arXiv's own BibTeX. It is on the HTML plane, so it costs
// fifteen seconds a paper.
const bibtexBase = "https://" + Host + "/bibtex/"

// BibTeX returns a BibTeX entry for each reference.
//
// By default the bytes are arXiv's own, fetched from s9 and passed through
// unchanged. That is the whole point: every tool that quotes arXiv quotes this
// string, and a tool that regenerates it produces an entry that disagrees with
// everybody else's bibliography for the same paper.
//
// local renders from the record instead, which is a different entry on purpose.
// See renderBibTeX for what it does differently and why.
func (c *Client) BibTeX(ctx context.Context, refs []string, local bool) (string, error) {
	if local {
		papers, err := c.citePapers(ctx, refs)
		if err != nil {
			return "", err
		}
		out := make([]string, 0, len(papers))
		for _, p := range papers {
			out = append(out, renderBibTeX(p))
		}
		return strings.Join(out, "\n\n"), nil
	}

	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		entry, err := c.fetchBibTeX(ctx, ref)
		if err != nil {
			return "", err
		}
		out = append(out, entry)
	}
	return strings.Join(out, "\n\n"), nil
}

// fetchBibTeX reads one entry from s9.
func (c *Client) fetchBibTeX(ctx context.Context, ref string) (string, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return "", err
	}
	// s9 ignores a version suffix and always answers for the paper, so the
	// canonical id is what goes in the URL rather than what the user typed.
	u := bibtexBase + id.Canonical
	c.logf(1, "GET %s", u)
	resp, err := c.fetch(ctx, u, TTLPaper)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf("arxiv has no paper %s: %w", id.Canonical, ErrNotFound)
		}
		return "", err
	}
	// The served entry has no trailing newline and two of its lines end in a
	// space. Both are kept, because passing the bytes through means passing
	// the bytes through, and the newline is added by whoever prints it.
	return strings.TrimRight(string(resp.Body), "\n"), nil
}

// citePapers reads the papers a citation is built from.
//
// Meta depth, always. A citation needs structured author names to write
// "Vaswani, A." and only OAI publishes them, and it needs nothing from the
// abstract page, so this is two requests on the API plane and no fifteen second
// wait.
//
// The answers come back in the order the batch read returned them and go out in
// the order the user asked for them, because a bibliography is a list somebody
// wrote down in a particular order. An id that answered with nothing is an
// error rather than a gap: a citation list quietly one shorter than it should
// be is the kind of thing that gets noticed after submission.
func (c *Client) citePapers(ctx context.Context, refs []string) ([]Paper, error) {
	papers, err := c.PapersAt(ctx, refs, PaperOptions{Depth: DepthMeta})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Paper, len(papers))
	for _, p := range papers {
		byID[p.ID] = p
	}
	out := make([]Paper, 0, len(refs))
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return nil, err
		}
		p, ok := byID[id.Canonical]
		if !ok {
			return nil, fmt.Errorf("arxiv has no paper %s: %w", id.Canonical, ErrNotFound)
		}
		out = append(out, p)
	}
	return out, nil
}

// renderBibTeX builds an entry from a record.
//
// It differs from arXiv's own entry in three ways, and each one is the reason
// somebody would ask for it.
//
// The year is the year of the first submission. arXiv's entry uses the year of
// the latest version, so its entry for "Attention Is All You Need" says 2023,
// which is when v7 was posted and not when the paper appeared.
//
// A published paper comes out as @article with its journal reference in it.
// arXiv always writes @misc and never mentions the journal, even when it knows
// the DOI.
//
// The doi field is the DOI. arXiv puts a URL in it, and a URL is not what a
// BibTeX doi field takes.
func renderBibTeX(p Paper) string {
	var b strings.Builder
	kind := "misc"
	if p.JournalRef != "" {
		kind = "article"
	}
	fmt.Fprintf(&b, "@%s{%s,\n", kind, CiteKey(p))

	field := func(name, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "      %s={%s},\n", name, value)
	}
	field("title", p.Title)
	field("author", strings.Join(authorNames(p), " and "))
	field("journal", p.JournalRef)
	if !p.FirstSubmitted.IsZero() {
		field("year", fmt.Sprint(p.FirstSubmitted.Year()))
	}
	field("eprint", p.ID)
	field("archivePrefix", "arXiv")
	field("primaryClass", p.PrimaryCategory)
	doi := p.PublisherDOI
	if doi == "" {
		doi = p.DOI
	}
	field("doi", doi)
	field("url", paperURL(p))
	b.WriteString("}")
	return b.String()
}

// CiteKey is the key renderBibTeX writes.
//
// It is the first author, the year of the first submission, and up to four
// words of the title with the small words dropped. arXiv's key follows the same
// shape with a different word list, and this does not try to reproduce it: the
// years already differ, so the two keys could not match anyway, and a key that
// is nearly arXiv's would be worse than one that is plainly its own.
func CiteKey(p Paper) string {
	var b strings.Builder
	if len(p.Authors) == 0 {
		// No author, so the id leads. A key of "paper" would collide with
		// every other authorless record in the same bibliography.
		b.WriteString(letters(p.ID))
	} else {
		a := p.Authors[0]
		name := a.Keyname
		if name == "" {
			// No structured name, so the whole display string is the key part.
			// Splitting it on the last space would be a guess, and it is wrong
			// for "The ATLAS Collaboration" and for every name written surname
			// first.
			name = a.Name
		}
		b.WriteString(letters(name))
	}
	if !p.FirstSubmitted.IsZero() {
		fmt.Fprintf(&b, "%d", p.FirstSubmitted.Year())
	}
	words := 0
	for _, w := range strings.Fields(p.Title) {
		w = letters(w)
		if w == "" || keyStop[w] {
			continue
		}
		b.WriteString(w)
		if words++; words == 4 {
			break
		}
	}
	if b.Len() == 0 {
		return letters(p.ID)
	}
	return b.String()
}

// keyStop are the words a cite key skips. It is short on purpose: the job is a
// readable key, not a search index.
var keyStop = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "by": true,
	"for": true, "from": true, "in": true, "is": true, "of": true, "on": true,
	"or": true, "the": true, "to": true, "with": true,
}

// letters keeps the letters and digits of a string, lowercased.
func letters(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// authorNames is the display names in order.
func authorNames(p Paper) []string {
	out := make([]string, 0, len(p.Authors))
	for _, a := range p.Authors {
		out = append(out, a.Name)
	}
	return out
}

// paperURL is the abstract page for the version the record describes, falling
// back to the unversioned page when the record does not know its version.
func paperURL(p Paper) string {
	if p.URL != "" {
		return p.URL
	}
	if p.Version > 0 {
		return absURL(fmt.Sprintf("%sv%d", p.ID, p.Version))
	}
	return absURL(p.ID)
}
