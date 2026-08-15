package rdf

// The vocabulary, and the table that says how an arxiv claim becomes one.
//
// Almost none of this is invented. arXiv publishes Dublin Core about every
// paper through OAI-PMH, and the abstract page carries the Highwire Press
// citation_* tags that Zotero and Google Scholar already read, so the core of
// the mapping is read off two surfaces rather than argued about. The Evidence
// column on each row says which surface, and an empty Evidence means the row is
// inferred: a term chosen because it is the one everybody else uses, not
// because arXiv said it.
//
// That distinction is the reason the table is data instead of a switch
// statement. `arxiv rdf --mapping` prints it, so somebody deciding whether to
// trust a triple can see where it came from without reading this file.

// The namespaces. dc is Dublin Core terms rather than the 1.1 elements: oai_dc
// serves the elements, but the terms namespace is the one with ranges on it and
// the two are aligned, so nothing is lost by writing the modern spelling.
const (
	NSRDF    = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	NSRDFS   = "http://www.w3.org/2000/01/rdf-schema#"
	NSXSD    = "http://www.w3.org/2001/XMLSchema#"
	NSDC     = "http://purl.org/dc/terms/"
	NSSchema = "https://schema.org/"
	NSSKOS   = "http://www.w3.org/2004/02/skos/core#"
	NSPROV   = "http://www.w3.org/ns/prov#"
	NSCITO   = "http://purl.org/spar/cito/"
	NSFABIO  = "http://purl.org/spar/fabio/"
	NSOWL    = "http://www.w3.org/2002/07/owl#"

	// NSAX is this tool's own namespace, and it is the docs site on purpose:
	// a term nobody has seen before can be looked up rather than guessed at.
	NSAX = "https://tamnd.github.io/arxiv-cli/ns#"

	// NSID is where a node with no address in the world is minted. A name, a
	// category or a journal reference is not a page anybody can fetch, so it
	// gets an identifier here instead of borrowing somebody else's.
	NSID = "https://tamnd.github.io/arxiv-cli/id/"
)

// Prefix is one namespace and the short name it is written under.
type Prefix struct {
	Prefix string
	IRI    string
}

// Prefixes is every namespace this package writes, in the order a Turtle header
// and a JSON-LD context declare them.
var Prefixes = []Prefix{
	{"rdf", NSRDF},
	{"rdfs", NSRDFS},
	{"xsd", NSXSD},
	{"dc", NSDC},
	{"schema", NSSchema},
	{"skos", NSSKOS},
	{"prov", NSPROV},
	{"cito", NSCITO},
	{"fabio", NSFABIO},
	{"owl", NSOWL},
	{"ax", NSAX},
}

// The terms themselves.
const (
	RDFType   = IRI(NSRDF + "type")
	RDFSLabel = IRI(NSRDFS + "label")

	DCTitle       = IRI(NSDC + "title")
	DCDescription = IRI(NSDC + "description")
	DCCreator     = IRI(NSDC + "creator")
	DCDate        = IRI(NSDC + "date")
	DCSubject     = IRI(NSDC + "subject")
	DCSource      = IRI(NSDC + "source")
	DCRights      = IRI(NSDC + "rights")

	SchemaName          = IRI(NSSchema + "name")
	SchemaAbstract      = IRI(NSSchema + "abstract")
	SchemaAuthor        = IRI(NSSchema + "author")
	SchemaAbout         = IRI(NSSchema + "about")
	SchemaDatePublished = IRI(NSSchema + "datePublished")
	SchemaDateModified  = IRI(NSSchema + "dateModified")
	SchemaIsPartOf      = IRI(NSSchema + "isPartOf")
	SchemaIdentifier    = IRI(NSSchema + "identifier")
	SchemaLicense       = IRI(NSSchema + "license")
	SchemaComment       = IRI(NSSchema + "comment")
	SchemaEncoding      = IRI(NSSchema + "encoding")
	SchemaContentURL    = IRI(NSSchema + "contentUrl")
	SchemaEncodingFmt   = IRI(NSSchema + "encodingFormat")
	SchemaAffiliation   = IRI(NSSchema + "affiliation")

	SKOSBroader    = IRI(NSSKOS + "broader")
	SKOSInScheme   = IRI(NSSKOS + "inScheme")
	SKOSPrefLabel  = IRI(NSSKOS + "prefLabel")
	SKOSDefinition = IRI(NSSKOS + "definition")

	PROVHadRevision   = IRI(NSPROV + "hadRevision")
	PROVWasRevisionOf = IRI(NSPROV + "wasRevisionOf")
	PROVDerivedFrom   = IRI(NSPROV + "wasDerivedFrom")

	CitoCites      = IRI(NSCITO + "cites")
	CitoIsCitedBy  = IRI(NSCITO + "isCitedBy")
	OWLSameAs      = IRI(NSOWL + "sameAs")
	XSDDate        = IRI(NSXSD + "date")
	XSDDateTime    = IRI(NSXSD + "dateTime")
	XSDBoolean     = IRI(NSXSD + "boolean")
	XSDNonNegative = IRI(NSXSD + "nonNegativeInteger")
)

// The classes.
const (
	SchemaScholarlyArticle = IRI(NSSchema + "ScholarlyArticle")
	SchemaPerson           = IRI(NSSchema + "Person")
	SchemaPeriodical       = IRI(NSSchema + "Periodical")
	SchemaMediaObject      = IRI(NSSchema + "MediaObject")
	SchemaOrganization     = IRI(NSSchema + "Organization")
	SKOSConcept            = IRI(NSSKOS + "Concept")
	SKOSConceptScheme      = IRI(NSSKOS + "ConceptScheme")
	SKOSCollection         = IRI(NSSKOS + "Collection")
	FabioPreprint          = IRI(NSFABIO + "Preprint")
)

// Scheme is the concept scheme the taxonomy hangs off, so a category is a
// concept in something rather than a concept floating on its own.
const Scheme = IRI(NSID + "scheme/taxonomy")

// LabelEnd says which end of a claim the note is a name for.
//
// It is per predicate rather than a rule, because there is no rule. On authored
// the note is the author's name and the name node is the one that needs it; on
// announced_as the note is "new" or "cross", and hanging that on cs.CL as a
// label would say the Computation and Language category is called new.
type LabelEnd int

const (
	LabelNone LabelEnd = iota
	LabelFrom
	LabelTo
)

// Row is one line of the mapping: what arxiv calls something, what RDF calls
// it, and where arXiv said so.
type Row struct {
	// What is the arxiv name: a predicate, a record field, or a node kind.
	What string `json:"what" table:"arxiv"`
	// Kind is which of those three it is.
	Kind string `json:"kind" table:"kind"`
	// Terms is what it is written as, all of them, because dc and schema.org
	// both have a word for most of this and a consumer reads one or the other.
	Terms []IRI `json:"terms" table:"-"`
	// Reverse says the claim turns round on the way out. arxiv writes name
	// authored paper because that is the direction the frontier reads in;
	// schema:author runs from the work to its author, and getting this backwards
	// produces a file that loads without complaint and says a paper wrote Ashish
	// Vaswani.
	Reverse bool `json:"reverse,omitempty" table:"-"`
	// Label is which end the note names, if either.
	Label LabelEnd `json:"-" table:"-"`
	// Evidence is the surface that says so. Empty means inferred: nothing on
	// arXiv asserts this term, it is the one the other tools in this family
	// already export into, so a store from all of them joins.
	Evidence string `json:"evidence,omitempty" table:"evidence"`
	// Written is the terms joined, for the printed table.
	Written string `json:"-" table:"rdf"`
}

// Mapping is the whole table, doc 04 section 5.
//
// The predicate rows come first because they are the graph, then the record
// fields that are literals rather than edges, then the classes.
var Mapping = []Row{
	// ─── the twenty predicates ───
	{What: "authored", Kind: "predicate", Terms: []IRI{SchemaAuthor, DCCreator}, Reverse: true, Label: LabelFrom, Evidence: "oai_dc dc:creator, citation_author"},
	{What: "identified_as", Kind: "predicate", Terms: []IRI{OWLSameAs}, Label: LabelFrom, Evidence: "s8, arXiv asserting identity"},
	{What: "has_orcid", Kind: "predicate", Terms: []IRI{OWLSameAs, SchemaIdentifier}, Evidence: "s8"},
	{What: "affiliated_with", Kind: "predicate", Terms: []IRI{SchemaAffiliation}, Label: LabelTo, Evidence: "s10"},
	{What: "primary_category", Kind: "predicate", Terms: []IRI{IRI(NSAX + "primaryCategory")}, Label: LabelTo},
	{What: "in_category", Kind: "predicate", Terms: []IRI{DCSubject, SchemaAbout}, Label: LabelTo, Evidence: "oai_dc dc:subject"},
	{What: "cross_listed", Kind: "predicate", Terms: []IRI{IRI(NSAX + "crossListed")}, Label: LabelTo},
	{What: "subcategory_of", Kind: "predicate", Terms: []IRI{SKOSBroader}, Label: LabelTo, Evidence: "s7, the taxonomy is a tree"},
	{What: "part_of_group", Kind: "predicate", Terms: []IRI{SKOSBroader}, Label: LabelTo, Evidence: "s7, the taxonomy is a tree"},
	{What: "in_set", Kind: "predicate", Terms: []IRI{IRI(NSAX + "inSet")}, Label: LabelTo, Evidence: "s2"},
	{What: "has_version", Kind: "predicate", Terms: []IRI{PROVHadRevision}},
	{What: "supersedes", Kind: "predicate", Terms: []IRI{PROVWasRevisionOf}},
	{What: "published_in", Kind: "predicate", Terms: []IRI{DCSource, SchemaIsPartOf}, Label: LabelTo, Evidence: "s1 and s2 journal-ref, oai_dc folds it into dc:identifier"},
	{What: "has_doi", Kind: "predicate", Terms: []IRI{SchemaIdentifier}, Evidence: "citation_doi"},
	{What: "licensed_under", Kind: "predicate", Terms: []IRI{DCRights, SchemaLicense}, Label: LabelTo, Evidence: "s2 license, dc:rights is where Dublin Core keeps one"},
	{What: "submitted_by", Kind: "predicate", Terms: []IRI{IRI(NSAX + "submitter")}, Reverse: true, Label: LabelFrom, Evidence: "s2 arXivRaw, s3"},
	{What: "announced_as", Kind: "predicate", Terms: []IRI{IRI(NSAX + "announcedIn")}, Evidence: "s6"},
	{What: "linked_by", Kind: "predicate", Terms: []IRI{CitoIsCitedBy}, Reverse: true, Label: LabelFrom, Evidence: "s11"},
	{What: "cites", Kind: "predicate", Terms: []IRI{CitoCites}, Label: LabelTo, Evidence: "s10, the rendered bibliography"},
	{What: "has_file", Kind: "predicate", Terms: []IRI{SchemaEncoding}, Label: LabelTo},

	// ─── the record fields that are literals ───
	{What: "title", Kind: "field", Terms: []IRI{DCTitle, SchemaName}, Evidence: "oai_dc, citation_title"},
	{What: "abstract", Kind: "field", Terms: []IRI{DCDescription, SchemaAbstract}, Evidence: "oai_dc, citation_abstract"},
	{What: "comment", Kind: "field", Terms: []IRI{SchemaComment}, Evidence: "oai_dc writes it into dc:description"},
	{What: "first_submitted", Kind: "field", Terms: []IRI{DCDate, SchemaDatePublished}, Evidence: "citation_date"},
	{What: "last_updated", Kind: "field", Terms: []IRI{SchemaDateModified}, Evidence: "s1 updated"},
	{What: "report_no", Kind: "field", Terms: []IRI{IRI(NSAX + "reportNo")}, Evidence: "s2 arXivRaw"},
	{What: "msc_class", Kind: "field", Terms: []IRI{IRI(NSAX + "mscClass")}, Evidence: "s2 arXivRaw"},
	{What: "acm_class", Kind: "field", Terms: []IRI{IRI(NSAX + "acmClass")}, Evidence: "s2 arXivRaw"},
	{What: "pdf_url", Kind: "field", Terms: []IRI{SchemaEncoding}, Evidence: "citation_pdf_url"},
	{What: "withdrawn", Kind: "field", Terms: []IRI{IRI(NSAX + "withdrawn")}, Evidence: "s2, an OAI header saying deleted, or s3 marking the newest version withdrawn"},

	// The other three records that carry a name of their own. A category's name
	// is a preferred label rather than a title, because a category is a concept
	// in a scheme and that is the word SKOS uses for it.
	{What: "category.name", Kind: "field", Terms: []IRI{SKOSPrefLabel, SchemaName}, Evidence: "s7"},
	{What: "category.description", Kind: "field", Terms: []IRI{SKOSDefinition}, Evidence: "s7"},
	{What: "set.name", Kind: "field", Terms: []IRI{SKOSPrefLabel, SchemaName}, Evidence: "s2"},
	{What: "person.name", Kind: "field", Terms: []IRI{SchemaName}, Evidence: "s8"},

	// ─── the classes ───
	{What: "paper", Kind: "type", Terms: []IRI{SchemaScholarlyArticle, FabioPreprint}},
	{What: "author", Kind: "type", Terms: []IRI{SchemaPerson}, Evidence: "s8 is a person's own page"},
	{What: "name", Kind: "type", Terms: []IRI{SchemaPerson}, Evidence: "dc:creator, citation_author"},
	{What: "orcid", Kind: "type", Terms: []IRI{SchemaPerson}, Evidence: "s8"},
	{What: "category", Kind: "type", Terms: []IRI{SKOSConcept}},
	{What: "archive", Kind: "type", Terms: []IRI{SKOSConcept}},
	{What: "group", Kind: "type", Terms: []IRI{SKOSConcept}},
	{What: "set", Kind: "type", Terms: []IRI{SKOSCollection}, Evidence: "s2, an OAI set is a collection"},
	{What: "journal", Kind: "type", Terms: []IRI{SchemaPeriodical}, Evidence: "oai_dc dc:source"},
	{What: "file", Kind: "type", Terms: []IRI{SchemaMediaObject}},
}

// index is the table looked up by kind and name, built once.
var index = func() map[string]Row {
	m := make(map[string]Row, len(Mapping))
	for i, r := range Mapping {
		r.Written = writtenTerms(r.Terms)
		Mapping[i] = r
		m[r.Kind+"/"+r.What] = r
	}
	return m
}()

// Predicate finds how a predicate is written.
func Predicate(name string) (Row, bool) {
	r, ok := index["predicate/"+name]
	return r, ok
}

// Field finds how a record field is written.
func Field(name string) (Row, bool) {
	r, ok := index["field/"+name]
	return r, ok
}

// Classes is what a node kind is typed as, which is nothing for the kinds where
// naming a class would be a guess. A DOI is an identifier for a work rather than
// a work, and an external node is a university one day and a blog the next.
func Classes(kind string) []IRI {
	r, ok := index["type/"+kind]
	if !ok {
		return nil
	}
	return r.Terms
}

// Unknown is the term for a predicate the table has not been taught.
//
// A claim arxiv can make and this file has no translation for is written under
// its own name in the ax namespace rather than dropped, because a claim lost in
// translation is lost silently and the file still looks complete.
func Unknown(predicate string) IRI { return IRI(NSAX + camel(predicate)) }

// camel turns has_orcid into hasOrcid, which is the shape every other term in
// the output has.
func camel(s string) string {
	out := make([]byte, 0, len(s))
	up := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c == '-' || c == ' ':
			up = true
		case up:
			out = append(out, upper(c))
			up = false
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}
	return c
}

// writtenTerms is the Terms column as one string, short form where there is one.
func writtenTerms(terms []IRI) string {
	out := ""
	for i, t := range terms {
		if i > 0 {
			out += ", "
		}
		if short, ok := Short(t); ok {
			out += short
			continue
		}
		out += string(t)
	}
	return out
}

// Short writes a term as prefix:local where a declared namespace covers it.
func Short(t IRI) (string, bool) {
	for _, p := range Prefixes {
		rest, ok := cutPrefix(string(t), p.IRI)
		if !ok || rest == "" || !isLocalName(rest) {
			continue
		}
		return p.Prefix + ":" + rest, true
	}
	return "", false
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return s, false
	}
	return s[len(prefix):], true
}

// isLocalName says whether a local part can be written after a prefix without
// escaping. Turtle allows more than this; the ones it does not allow are the
// ones worth writing out in full anyway.
func isLocalName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		case (c == '_' || c == '-' || c == '.') && i > 0:
		default:
			return false
		}
	}
	return s != ""
}
