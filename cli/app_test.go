package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// app_test.go is the test that `arxiv serve` and `arxiv mcp` have no code of
// their own. Neither surface appears anywhere in this repository: both are
// built by kit out of the operation list, so what there is to check is the
// list. Every read is registered once, every registration carries enough
// metadata to become an HTTP route and an MCP tool, and nothing hand written on
// the command line quietly shadows one.
//
// A shadow is the failure worth catching. kit derives three names from one
// operation, the command path, the route and the tool name, and a hand-written
// command with the same name wins on the command line and loses on the other
// two. That is a tool where `arxiv paper` prints one thing and the HTTP route
// of the same name answers with another, and nothing in a build or a vet run
// would say so.

// ops is the registry as the three surfaces see it.
func ops(t *testing.T) []kit.Operation {
	t.Helper()
	got := NewApp().Ops()
	if len(got) == 0 {
		t.Fatal("no operations registered, so serve answers 404 to everything and mcp lists no tools")
	}
	return got
}

// path is the name kit derives all three surface names from.
func path(m kit.OpMeta) string {
	if m.Parent != "" {
		return m.Parent + " " + m.Name
	}
	return m.Name
}

// handWritten is every command this package adds by hand, parents and
// subcommands both, by the name the command line answers to.
func handWritten() []string {
	var out []string
	var walk func(prefix string, cmds []kit.Command)
	walk = func(prefix string, cmds []kit.Command) {
		for _, c := range cmds {
			name := prefix + strings.Fields(c.Use)[0]
			out = append(out, name)
			walk(name+" ", c.Sub)
		}
	}
	walk("", storeCommands())
	walk("", crawlCommands())
	walk("", []kit.Command{newVersionCmd()})
	sort.Strings(out)
	return out
}

// The reads, by name. This is a closed set on purpose: a new read is a new HTTP
// route and a new MCP tool, and adding one should be a line in this list rather
// than something that appears on a network surface because a file was edited.
var registered = []string{
	"author",
	"bibtex",
	"categories",
	"category",
	"cite",
	"count",
	"download",
	"edges",
	"fields",
	"files",
	"fulltext",
	"grammar",
	"graph",
	"id",
	"list",
	"new",
	"paper",
	"planes",
	"predicates",
	"routes",
	"search",
	"sets",
	"surfaces",
	"trackbacks",
}

func TestEveryReadIsRegisteredExactlyOnce(t *testing.T) {
	seen := map[string]int{}
	for _, op := range ops(t) {
		seen[path(op.Meta())]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%s is registered %d times, and the second registration wins on every surface", name, n)
		}
	}
	for _, name := range registered {
		if seen[name] == 0 {
			t.Errorf("%s is in the list and is not registered, so it is on no surface at all", name)
		}
		delete(seen, name)
	}
	for name := range seen {
		t.Errorf("%s is registered and is not in the list, so a read reached serve and mcp without being written down", name)
	}
}

// The one that matters. A hand-written command and an operation with the same
// name is not a build error and not a vet finding; it is a command line that
// disagrees with its own HTTP routes.
func TestNoHandWrittenCommandShadowsAnOperation(t *testing.T) {
	isOp := map[string]bool{}
	for _, op := range ops(t) {
		isOp[path(op.Meta())] = true
	}
	for _, name := range handWritten() {
		if isOp[name] {
			t.Errorf("%s is both a hand-written command and an operation, so the command line and the network surfaces answer differently", name)
		}
	}
}

// The commands that keep something are deliberately not operations. A crawl
// runs for an hour, an archive writes a directory, a query takes SQL and the
// rest read a file on this machine, and none of those is a thing to answer an
// HTTP request with. The reads of arXiv itself are all operations, which is why
// the list above is twenty long and this one is short.
func TestTheCommandsThatTouchDiskStayOffTheNetworkSurfaces(t *testing.T) {
	want := []string{"archive", "crawl", "db", "db stats", "db vacuum", "export", "query", "rdf", "version"}
	got := handWritten()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the hand-written commands are %v, want %v", got, want)
	}
	isOp := map[string]bool{}
	for _, op := range ops(t) {
		isOp[path(op.Meta())] = true
	}
	for _, name := range want {
		if isOp[name] {
			t.Errorf("%s became an operation, which puts it on serve and mcp", name)
		}
	}
}

// Whatever a surface needs to describe an operation, every operation has. kit
// builds an OpenAPI path out of the summary and the schemas and an MCP tool out
// of the same two, so an operation missing either is a route that exists and
// cannot be read by anything.
func TestEveryOperationCanDescribeItselfToASurface(t *testing.T) {
	for _, op := range ops(t) {
		m := op.Meta()
		name := path(m)
		if m.Summary == "" {
			t.Errorf("%s has no summary, so it is an unlabelled route and an unlabelled tool", name)
		}
		if m.Group == "" {
			t.Errorf("%s has no group, so it falls out of the help", name)
		}
		if in := op.InputSchema(); in["type"] != "object" {
			t.Errorf("%s has an input schema of type %v, want object", name, in["type"])
		}
		if out := op.OutputSchema(); len(out) == 0 {
			t.Errorf("%s has no output schema, so nothing knows what it answers with", name)
		}
		if op.OutType() == nil {
			t.Errorf("%s emits no record type, so a resource URI cannot select it", name)
		}
	}
}

// A positional argument on the command line is a named parameter everywhere
// else, and the name has to be the same one. `arxiv paper 1706.03762` and a
// request naming the same field are the same call, and they stop being the same
// call the moment an argument is renamed on one side only.
func TestEveryPositionalArgumentIsAParameterToo(t *testing.T) {
	for _, op := range ops(t) {
		m := op.Meta()
		props, _ := op.InputSchema()["properties"].(map[string]any)
		for _, a := range m.Args {
			if _, ok := props[a.Name]; !ok {
				t.Errorf("%s takes %s on the command line and has no parameter of that name for serve and mcp", path(m), a.Name)
			}
		}
	}
}

// Every parameter is spelled the way a flag is spelled. kit derives a name from
// the Go field name and joins the words with an underscore, so a field called
// FullText would reach the command line as --full_text and an MCP tool as
// full_text, and the fix is a name= in the tag. This is the test that says which
// fields still need one.
func TestEveryParameterNameReadsLikeAFlag(t *testing.T) {
	for _, op := range ops(t) {
		for _, p := range op.Params() {
			if p.Kind == kit.KindInject {
				continue
			}
			if strings.Contains(p.Name, "_") || p.Name != strings.ToLower(p.Name) {
				t.Errorf("%s takes %q, which is a Go field name rather than a flag name", path(op.Meta()), p.Name)
			}
		}
	}
}

// arXiv has nothing anonymous to write to, so nothing here changes anything at
// arXiv. The single Write operation writes to this machine: `arxiv download`
// puts a PDF in a directory. The test is here so that stays the whole list.
func TestTheOnlyWriteOperationWritesToThisMachine(t *testing.T) {
	var writes []string
	for _, op := range ops(t) {
		if op.Meta().Write {
			writes = append(writes, path(op.Meta()))
		}
	}
	if len(writes) != 1 || writes[0] != "download" {
		t.Fatalf("the write operations are %v, want download and nothing else", writes)
	}
}
