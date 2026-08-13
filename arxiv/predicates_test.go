package arxiv

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/graph"
)

// The printed table is the table, not a copy of it that can drift.
func TestPredicateTableMatchesTheGraph(t *testing.T) {
	rows := PredicateTable()
	if len(rows) != len(graph.Predicates) {
		t.Fatalf("the command prints %d rows and the table has %d", len(rows), len(graph.Predicates))
	}
	for i, row := range rows {
		p := graph.Predicates[i]
		if row.Name != p.Name {
			t.Errorf("row %d is %q, want %q", i, row.Name, p.Name)
		}
		if row.FromText != strings.Join(p.From, ", ") || row.ToText != strings.Join(p.To, ", ") {
			t.Errorf("%s: ends read %q and %q", row.Name, row.FromText, row.ToText)
		}
		if row.SurfacesText != strings.Join(p.Surfaces, ", ") {
			t.Errorf("%s: surfaces read %q", row.Name, row.SurfacesText)
		}
	}
}

// Every predicate gets a real claim beside it, because the URI spelling is the
// thing somebody writing a query gets wrong and a description does not fix it.
func TestEveryPredicateHasAnExample(t *testing.T) {
	for _, row := range PredicateTable() {
		if row.Example == "" {
			t.Errorf("%s has no example", row.Name)
			continue
		}
		parts := strings.Fields(row.Example)
		if len(parts) < 3 {
			t.Errorf("%s: %q is not a claim", row.Name, row.Example)
			continue
		}
		if parts[1] != row.Name {
			t.Errorf("%s: the example is about %q", row.Name, parts[1])
		}
		if !strings.HasPrefix(parts[0], "ax://") || !strings.HasPrefix(parts[2], "ax://") {
			t.Errorf("%s: %q has an end that is not a node", row.Name, row.Example)
		}
	}
}

// The examples are the documentation, so they have to pass the same check a
// real edge passes. An example that would be refused is worse than none.
func TestTheExamplesWouldValidate(t *testing.T) {
	for _, row := range PredicateTable() {
		parts := strings.Fields(row.Example)
		if len(parts) < 3 || strings.Contains(row.Example, "{") {
			// The hashed kinds are written with a placeholder, because a sha256
			// in a help table teaches nobody anything.
			continue
		}
		e := graph.Edge{
			From:      parts[0],
			Predicate: parts[1],
			To:        parts[2],
			Source:    "https://arxiv.org/abs/1706.03762",
			Surface:   row.Surfaces[0],
		}
		if err := e.Validate(); err != nil {
			t.Errorf("%s: the example would be refused: %v", row.Name, err)
		}
	}
}

func TestPredicateURI(t *testing.T) {
	if got := PredicateURI(graph.Cites); got != "ax://predicate/cites" {
		t.Errorf("PredicateURI = %q", got)
	}
}
