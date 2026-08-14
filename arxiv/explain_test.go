package arxiv

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// explain_test.go covers the four printed tables: surfaces, routes, grammar and
// fields.
//
// None of them make a request, which is the whole point of having them, and it
// is also what makes them easy to get wrong. A table nobody fetches is a table
// nobody notices has gone stale, so every row here is checked against the thing
// it describes: the surface constants, the base URL constants, the field list
// in query.go and the model itself.

// ─── surfaces ───

func TestTheSurfaceTableHasOneRowPerSurface(t *testing.T) {
	rows := surfaceInfos()
	if len(rows) != len(SurfaceNames) {
		t.Fatalf("%d rows for %d surfaces", len(rows), len(SurfaceNames))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.ID] {
			t.Errorf("%s has two rows", r.ID)
		}
		seen[r.ID] = true
		if want := SurfaceNames[r.ID]; r.Name != want {
			t.Errorf("%s is named %q, and the constant says %q", r.ID, r.Name, want)
		}
		if r.Only == "" {
			t.Errorf("%s does not say what it is the only source of", r.ID)
		}
		if len(r.Reads) == 0 {
			t.Errorf("%s is read by no command, so either the row or the tool is wrong", r.ID)
		}
	}
	for id := range SurfaceNames {
		if !seen[id] {
			t.Errorf("%s has no row in the surface table", id)
		}
	}
}

// Every base in the table is a real URL on a real plane. A base that resolved to
// nothing would be a request with no pace in front of it, which is the one thing
// this tool promises never to do.
func TestEverySurfaceBaseIsOnAPlane(t *testing.T) {
	for _, r := range surfaceInfos() {
		u, err := url.Parse(r.Base)
		if err != nil {
			t.Errorf("%s has a base that will not parse: %v", r.ID, err)
			continue
		}
		if u.Scheme != "https" {
			t.Errorf("%s reads over %s", r.ID, u.Scheme)
		}
		if r.Plane == "" {
			t.Errorf("%s sits on no plane, so %s is not in the plane table", r.ID, u.Host)
		}
	}
}

// The three gated surfaces are the three robots.txt disallows, and no others.
// A fourth appearing here without the file changing would mean somebody wrote a
// warning to excuse a read rather than to describe one.
func TestOnlyTheDisallowedSurfacesAreGated(t *testing.T) {
	want := map[string]bool{SurfaceSearch: true, SurfaceTrackback: true, SurfaceFiles: true}
	for _, r := range surfaceInfos() {
		if (r.Gated != "") != want[r.ID] {
			t.Errorf("%s has gated %q, and robots.txt disallowing it is %v", r.ID, r.Gated, want[r.ID])
		}
	}
}

func TestASurfaceURIIsStable(t *testing.T) {
	if got := SurfaceURI(SurfaceFullText); got != "ax://surface/s10" {
		t.Errorf("SurfaceURI wrote %q", got)
	}
}

// ─── routes ───

// Every route belongs to a surface and every surface has at least one route.
// The two are not one to one: s12 is one surface and two routes, and s2 is one
// route asked three ways.
func TestEveryRouteBelongsToASurfaceAndBack(t *testing.T) {
	rows := routeInfos()
	seen := map[string]bool{}
	for _, r := range rows {
		if SurfaceNames[r.Surface] == "" {
			t.Errorf("%s names surface %q, which is not one", r.Route, r.Surface)
		}
		seen[r.Surface] = true
		if r.Why == "" {
			t.Errorf("%s does not say what it is for", r.Route)
		}
		if r.Plane == "" {
			t.Errorf("%s sits on no plane", r.Route)
		}
		if r.Method != "GET" && r.Method != "HEAD, GET" {
			t.Errorf("%s is a %s, and this tool only reads", r.Route, r.Method)
		}
	}
	for id := range SurfaceNames {
		if !seen[id] {
			t.Errorf("%s is a surface with no route, so nothing can be read from it", id)
		}
	}
}

// A route starts with the base its surface declares, which is what keeps the
// two tables from drifting apart. s12 is the exception the check names, because
// the surface row can only carry one base and the surface has two routes.
func TestEveryRouteStartsWithItsSurfaceBase(t *testing.T) {
	bases := map[string]string{}
	for _, s := range surfaceRows {
		bases[s.ID] = s.Base
	}
	for _, r := range routeInfos() {
		if r.Surface == SurfaceFiles && strings.HasPrefix(r.Route, srcBase) {
			continue
		}
		if !strings.HasPrefix(r.Route, bases[r.Surface]) {
			t.Errorf("%s is filed under %s, whose base is %s", r.Route, r.Surface, bases[r.Surface])
		}
	}
}

// The robots verdict, on the paths it is actually about. The three disallowed
// routes each say what has to happen before they are read, because a route the
// tool will not follow on its own is a different promise from one it will.
func TestTheRobotsVerdictMatchesTheFile(t *testing.T) {
	for _, c := range []struct {
		route string
		want  string
	}{
		{absBase + "1706.03762", "allowed"},
		{listBase + "cs.CL/2026-01", "allowed"},
		{taxonomyURL, "allowed"},
		{s5Base + "?searchtype=all", "disallowed"},
		{trackbackBase + "1706.03762", "disallowed"},
		{srcBase + "1706.03762", "disallowed"},
		{apiBase + "?id_list=1706.03762", "not covered"},
		{oaiBase + "?verb=ListSets", "not covered"},
		{rssBase + "cs.CL", "not covered"},
	} {
		if got := robotsVerdict(c.route); got != c.want {
			t.Errorf("%s reads %q, want %q", c.route, got, c.want)
		}
	}

	for _, r := range routeInfos() {
		if r.Allowed == "disallowed" && r.Asked == "" {
			t.Errorf("%s is disallowed and does not say what has to be asked for first", r.Route)
		}
		if r.Allowed != "disallowed" && r.Asked != "" && r.Surface != SurfaceTrackback {
			t.Errorf("%s is %s and still gated on %q", r.Route, r.Allowed, r.Asked)
		}
	}
}

// ─── grammar ───

// Every prefix the query builder can emit has a row. A tenth field added to the
// constants with no row here would be a field nobody could find out about.
func TestTheGrammarCoversEveryAPIPrefix(t *testing.T) {
	rows := grammarInfos()
	have := map[string]GrammarInfo{}
	for _, r := range rows {
		if _, dup := have[r.Token]; dup {
			t.Errorf("%s has two rows", r.Token)
		}
		have[r.Token] = r
	}
	for _, prefix := range apiPrefixes() {
		row, ok := have[prefix]
		if !ok {
			t.Errorf("%s is a field in query.go and has no row in the grammar", prefix)
			continue
		}
		if row.Kind != "field" {
			t.Errorf("%s is filed as a %s", prefix, row.Kind)
		}
		if row.Plane != APIPlane.Name {
			t.Errorf("%s is on the %s plane, and the export API answers for it", prefix, row.Plane)
		}
	}
}

// The seven the export API has no prefix for all route onto the slow plane, and
// nothing else does. That is the fact the plane column exists to publish: one
// of these flags turns a three second query into a fifteen second one.
func TestTheSearchOnlyFieldsAreTheOnesOnTheHTMLPlane(t *testing.T) {
	slow := map[string]bool{}
	for _, r := range grammarInfos() {
		if r.Plane == HTMLPlane.Name {
			slow[r.Flag] = true
		}
	}
	if len(slow) != len(s5Only) {
		t.Fatalf("%d rows on the html plane and %d search only fields", len(slow), len(s5Only))
	}
	for _, flag := range s5Only {
		if !slow[flag] {
			t.Errorf("%s is a search only field and its row is not on the html plane", flag)
		}
	}
}

func TestEveryGrammarRowIsAnswered(t *testing.T) {
	kinds := map[string]bool{"field": true, "operator": true, "range": true, "rule": true}
	for _, r := range grammarInfos() {
		if !kinds[r.Kind] {
			t.Errorf("%s is a %q, which is not one of the four kinds", r.Token, r.Kind)
		}
		if r.Means == "" {
			t.Errorf("%s does not say what it means", r.Token)
		}
		if r.Example == "" {
			t.Errorf("%s has no example, and the examples are why this table exists", r.Token)
		}
	}
}

// The operators are spelled the way arXiv spells them. ANDNOT is one word and
// AND NOT is a syntax error, which is the sort of thing a table gets wrong once
// and then nobody checks again.
func TestTheOperatorsAreSpelledArxivsWay(t *testing.T) {
	q := AndNot(Term(FieldCategory, "cs.CL"), Term(FieldCategory, "cs.LG"))
	if !strings.Contains(q.String(), "ANDNOT") {
		t.Errorf("the builder writes %q", q)
	}
	var tokens []string
	for _, r := range grammarInfos() {
		if r.Kind == "operator" {
			tokens = append(tokens, r.Token)
		}
	}
	for _, want := range []string{"AND", "OR", "ANDNOT"} {
		if !contains(tokens, want) {
			t.Errorf("the grammar lists %v and not %s", tokens, want)
		}
	}
}

// ─── fields ───

// The census and the model name the same set, both ways. This is the test the
// whole file exists for: a field added to Paper with no row here is a field the
// tool returns and cannot explain, and a row for a field that was deleted is a
// promise the tool no longer keeps.
func TestTheFieldCensusMatchesTheModel(t *testing.T) {
	model := paperFieldTypes()
	if len(model) < 40 {
		t.Fatalf("reflection found %d fields on Paper, so it is looking at the wrong thing", len(model))
	}
	rows := fieldInfos()
	have := map[string]bool{}
	for _, r := range rows {
		if have[r.Field] {
			t.Errorf("%s has two rows", r.Field)
		}
		have[r.Field] = true
		if _, ok := model[r.Field]; !ok {
			t.Errorf("%s has a row and is not a field on Paper", r.Field)
		}
	}
	for name := range model {
		if !have[name] {
			t.Errorf("%s is a field on Paper with no row in the census", name)
		}
	}
}

// Every row is filled in and every surface it cites is a real one.
func TestEveryFieldRowIsAnswered(t *testing.T) {
	for _, r := range fieldInfos() {
		if r.Type == "" {
			t.Errorf("%s has no type, so reflection did not find it", r.Field)
		}
		if r.Group == "" {
			t.Errorf("%s is in no group", r.Field)
		}
		if r.Note == "" {
			t.Errorf("%s says nothing about itself", r.Field)
		}
		if r.SurfacesText == "" {
			t.Errorf("%s says nothing about where it comes from", r.Field)
		}
		for _, id := range r.Surfaces {
			if SurfaceNames[id] == "" {
				t.Errorf("%s cites %q, which is not a surface", r.Field, id)
			}
		}
		if r.Depth == "" {
			continue
		}
		if _, err := ParseDepth(r.Depth); err != nil {
			t.Errorf("%s is filled at depth %q: %v", r.Field, r.Depth, err)
		}
	}
}

// A field the census says arrives at meta really is on a record read to meta,
// and one it says needs full really is absent below it. Checked against the
// saved records rather than against the merge code, because the merge code is
// the thing being described.
func TestTheCensusAgreesWithARealRecord(t *testing.T) {
	// The Higgs paper, because it is the one in the suite that carries a report
	// number. The Attention paper has none, and a field an author never filled
	// in is absent at every depth, which would prove nothing about the cost.
	quick := paperFixture(t, "api_1207.7214.xml")
	full := higgsRecord(t)

	present := func(p Paper) map[string]bool {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}

	inQuick, inFull := present(quick), present(full)

	// Every key a real record carries has a row. This is the same claim as the
	// reflection test made from the other end, and it is worth making twice:
	// reflection sees the struct and this sees what a consumer actually gets.
	for _, m := range []map[string]bool{inQuick, inFull} {
		for name := range m {
			found := false
			for _, r := range fieldRows {
				if r.Field == name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("a real record carries %s and the census does not list it", name)
			}
		}
	}

	// The four this paper proves. Each is a field the census says costs more
	// than quick, and each is genuinely absent from the quick read.
	for _, name := range []string{"report_no", "license", "submitter", "versions"} {
		if inQuick[name] {
			t.Errorf("%s is on a quick record, and the census says it costs more", name)
		}
		if !inFull[name] {
			t.Errorf("%s is not on a full record, and the census says full fills it", name)
		}
	}
}

// The depth filter is a subset chain: everything quick fills, meta fills too.
func TestTheDepthFilterNests(t *testing.T) {
	at := func(d Depth) map[string]bool {
		out := map[string]bool{}
		for _, r := range fieldInfos() {
			if r.Depth != "" && d.AtLeast(Depth(r.Depth)) {
				out[r.Field] = true
			}
		}
		return out
	}
	prev := at(DepthQuick)
	if len(prev) == 0 {
		t.Fatal("a quick read fills nothing, which cannot be right")
	}
	for _, d := range Depths[1:] {
		next := at(d)
		if len(next) <= len(prev) {
			t.Errorf("%s fills %d fields and the depth below it fills %d", d, len(next), len(prev))
		}
		for name := range prev {
			if !next[name] {
				t.Errorf("%s fills %s and %s does not, so the depths do not nest", Depths[0], name, d)
			}
		}
		prev = next
	}
}
