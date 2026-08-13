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
func TestDomainRegisters(t *testing.T) {
	app := kit.New(kit.Identity{Binary: "arxiv", Version: "test"})
	(Domain{}).Register(app)

	// name to the group it belongs in. read means it talks to arXiv, explain
	// means it answers from what the tool already knows.
	groups := map[string]string{
		"search":     "read",
		"paper":      "read",
		"author":     "read",
		"categories": "read",
		"id":         "explain",
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
		if op.Meta().Write {
			t.Errorf("op %q is marked as a write; arXiv is read only", op.Meta().Name)
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
