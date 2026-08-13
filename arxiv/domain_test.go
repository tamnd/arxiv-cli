package arxiv

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "arxiv" {
		t.Errorf("Scheme = %q, want arxiv", info.Scheme)
	}
	found := false
	for _, h := range info.Hosts {
		if h == Host {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Hosts = %v, want to contain %s", info.Hosts, Host)
	}
	if info.Identity.Binary != "arxiv" {
		t.Errorf("Identity.Binary = %q, want arxiv", info.Identity.Binary)
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	domains := h.Domains()
	found := false
	for _, d := range domains {
		if d == "arxiv" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("arxiv domain not registered; got %v", domains)
	}
}

// TestDomainRegisters walks the app the binary builds and checks that every
// read the tool advertises is there, in the read group, and not marked as a
// write. arXiv is a read-only surface and nothing here should ever change that.
//
// download is the one op that carries the write flag, and it is not a write to
// arXiv. It is the only op that leaves a file behind, which is what an agent
// asking "does this change anything" wants to be told.
func TestDomainRegisters(t *testing.T) {
	app := kit.New(kit.Identity{Binary: "arxiv", Version: "test"})
	(Domain{}).Register(app)

	// name to the group it belongs in. read means it talks to arXiv, explain
	// means it answers from what the tool already knows.
	groups := map[string]string{
		"search":     "read",
		"count":      "read",
		"list":       "read",
		"new":        "read",
		"paper":      "read",
		"fulltext":   "read",
		"author":     "read",
		"categories": "read",
		"category":   "read",
		"sets":       "read",
		"trackbacks": "read",
		"files":      "read",
		"download":   "read",
		"bibtex":     "cite",
		"cite":       "cite",
		"id":         "explain",
		"planes":     "explain",
	}
	want := map[string]bool{}
	for name := range groups {
		want[name] = false
	}
	for _, op := range app.Ops() {
		group, ok := groups[op.Meta().Name]
		if !ok {
			t.Errorf("unexpected op %q", op.Meta().Name)
			continue
		}
		want[op.Meta().Name] = true
		if op.Meta().Group != group {
			t.Errorf("op %q is in group %q, want %q", op.Meta().Name, op.Meta().Group, group)
		}
		if op.Meta().Write && op.Meta().Name != "download" {
			t.Errorf("op %q is marked as a write; arXiv is read only", op.Meta().Name)
		}
		if op.Meta().Name == "download" && !op.Meta().Write {
			t.Error("download is not marked as a write, so nothing warns that it writes a file")
		}
		if op.Meta().Summary == "" {
			t.Errorf("op %q has no summary", op.Meta().Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("op %q was not registered", name)
		}
	}
}
