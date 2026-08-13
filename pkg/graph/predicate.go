package graph

import (
	"fmt"
	"sort"
	"strings"
)

// An edge is a claim: subject, predicate, object, and who said so.
//
// The source is part of the claim's identity rather than metadata on it, so two
// surfaces asserting the same edge stay two rows and a disagreement is
// queryable. There is no client column: arXiv gives one answer per URL, so the
// key is (from, predicate, to, source).
type Edge struct {
	From      string `json:"from"`
	Predicate string `json:"predicate"`
	To        string `json:"to"`
	// Source is the URL that asserted it and Surface is which of s1 to s12 that
	// URL belongs to.
	Source  string `json:"source"`
	Surface string `json:"surface"`
	// Note is a name for the object end, which is what makes the table output
	// readable before the object has been fetched.
	Note string `json:"note,omitempty"`
	// Position is the order an ordered read gave the claim. It carries the
	// author order, which is significant in most fields and load bearing in
	// physics.
	Position int `json:"position,omitempty"`
}

// Key is the identity of a claim, which is what a store's primary key is built
// from. Note and Position are labels rather than assertions and are deliberately
// not in it, so a later sighting that carries one can fill in an earlier one
// that did not.
func (e Edge) Key() string {
	return strings.Join([]string{e.From, e.Predicate, e.To, e.Source}, "\x00")
}

// The predicates. Twenty of them, and a predicate not in this table cannot be
// written.
const (
	Authored        = "authored"
	IdentifiedAs    = "identified_as"
	HasORCID        = "has_orcid"
	AffiliatedWith  = "affiliated_with"
	PrimaryCategory = "primary_category"
	InCategory      = "in_category"
	CrossListed     = "cross_listed"
	SubcategoryOf   = "subcategory_of"
	PartOfGroup     = "part_of_group"
	InSet           = "in_set"
	HasVersion      = "has_version"
	Supersedes      = "supersedes"
	PublishedIn     = "published_in"
	HasDOI          = "has_doi"
	LicensedUnder   = "licensed_under"
	SubmittedBy     = "submitted_by"
	AnnouncedAs     = "announced_as"
	LinkedBy        = "linked_by"
	Cites           = "cites"
	HasFile         = "has_file"
)

// Predicate is one row of the table: what it means, what may be at each end,
// and which surfaces are allowed to assert it.
type Predicate struct {
	Name string `json:"name"`
	// From and To are the node kinds each end may be. A predicate whose ends are
	// not checked is a predicate that will eventually point a paper at a license
	// and nobody will notice for a year.
	From []string `json:"from"`
	To   []string `json:"to"`
	// Surfaces is where a claim may come from. cites is the reason this column
	// exists: it may only ever be written from s10, because arXiv publishes no
	// citation graph and a cites row from anywhere else would be an invention.
	Surfaces []string `json:"surfaces"`
	Help     string   `json:"help"`
}

// Predicates is the whole table, in the order doc 04 section 3.1 gives it.
var Predicates = []Predicate{
	{Authored, []string{KindName, KindAuthor}, []string{KindPaper}, []string{"s1", "s2", "s3", "s4", "s5", "s6", "s8"},
		"the paper lists this name as an author, in this position"},
	{IdentifiedAs, []string{KindName}, []string{KindAuthor}, []string{"s8"},
		"arxiv's own author page ties this name to this registered person"},
	{HasORCID, []string{KindAuthor}, []string{KindORCID}, []string{"s8"},
		"the author page publishes this ORCID"},
	{AffiliatedWith, []string{KindName}, []string{KindExternal}, []string{"s10"},
		"the rendered paper gives this affiliation for this author"},
	{PrimaryCategory, []string{KindPaper}, []string{KindCategory}, []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		"the category the paper was submitted to"},
	{InCategory, []string{KindPaper}, []string{KindCategory}, []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		"every category on the paper, primary and cross listed alike"},
	{CrossListed, []string{KindPaper}, []string{KindCategory}, []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		"the categories that are not the primary one"},
	{SubcategoryOf, []string{KindCategory}, []string{KindArchive}, []string{"s7"},
		"the taxonomy puts this category under this archive"},
	{PartOfGroup, []string{KindArchive}, []string{KindGroup}, []string{"s7"},
		"the taxonomy puts this archive in this group"},
	{InSet, []string{KindCategory}, []string{KindSet}, []string{"s2"},
		"the OAI setSpec that selects this category"},
	{HasVersion, []string{KindPaper}, []string{KindPaper}, []string{"s2", "s3"},
		"the paper has this version"},
	{Supersedes, []string{KindPaper}, []string{KindPaper}, []string{"s2", "s3"},
		"this version replaced the one before it, derived from the version list"},
	{PublishedIn, []string{KindPaper}, []string{KindJournal}, []string{"s1", "s2", "s3"},
		"the journal reference the author typed in"},
	{HasDOI, []string{KindPaper}, []string{KindDOI}, []string{"s1", "s2", "s3"},
		"a DOI for this paper, arXiv's own or the publisher's"},
	{LicensedUnder, []string{KindPaper}, []string{KindLicense}, []string{"s2", "s3", "s6"},
		"the license the submitter chose"},
	{SubmittedBy, []string{KindName}, []string{KindPaper}, []string{"s2", "s3"},
		"the submitter arXivRaw and the abstract page name, who is one of the authors and often not the first"},
	{AnnouncedAs, []string{KindPaper}, []string{KindCategory}, []string{"s6"},
		"the feed announced the paper in this category, with new, cross or replace as the note"},
	{LinkedBy, []string{KindExternal}, []string{KindPaper}, []string{"s11"},
		"an external page links to this paper"},
	{Cites, []string{KindPaper}, []string{KindPaper, KindDOI, KindExternal}, []string{"s10"},
		"the rendered bibliography resolves to this, which is the only citation arXiv publishes"},
	{HasFile, []string{KindPaper}, []string{KindFile}, []string{"s3", "s12"},
		"arxiv serves this file for this paper"},
}

// byName is the table indexed, built once.
var byName = func() map[string]Predicate {
	m := make(map[string]Predicate, len(Predicates))
	for _, p := range Predicates {
		m[p.Name] = p
	}
	return m
}()

// Lookup finds a predicate by name.
func Lookup(name string) (Predicate, bool) {
	p, ok := byName[name]
	return p, ok
}

// Names is every predicate name, sorted, for a flag's enum and an error's list.
func Names() []string {
	out := make([]string, 0, len(Predicates))
	for _, p := range Predicates {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// Allows reports whether a node kind is in a list of them.
func allows(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Validate checks a claim against the table.
//
// This runs on every edge before it is written, rather than in a test only,
// because the failure it catches is silent: an edge pointing at the wrong kind
// of node joins to nothing and looks like missing data rather than like a bug.
func (e Edge) Validate() error {
	p, ok := Lookup(e.Predicate)
	if !ok {
		return fmt.Errorf("%q is not a predicate; the twenty are %s", e.Predicate, strings.Join(Names(), ", "))
	}
	from, ok := KindOf(e.From)
	if !ok {
		return fmt.Errorf("%s: %q is not an ax:// uri", e.Predicate, e.From)
	}
	to, ok := KindOf(e.To)
	if !ok {
		return fmt.Errorf("%s: %q is not an ax:// uri", e.Predicate, e.To)
	}
	if !allows(p.From, from) {
		return fmt.Errorf("%s runs from %s, not from %s", e.Predicate, strings.Join(p.From, " or "), from)
	}
	if !allows(p.To, to) {
		return fmt.Errorf("%s runs to %s, not to %s", e.Predicate, strings.Join(p.To, " or "), to)
	}
	if e.Surface != "" && !allows(p.Surfaces, e.Surface) {
		return fmt.Errorf("%s cannot be asserted by %s, only by %s", e.Predicate, e.Surface, strings.Join(p.Surfaces, ", "))
	}
	if e.Source == "" {
		return fmt.Errorf("%s from %s has no source, and a claim nobody made is not a claim", e.Predicate, e.From)
	}
	return nil
}
