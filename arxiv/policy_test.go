package arxiv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// policy_test.go is the proof that the claims in doc 00 are facts rather than
// intentions. It reads the module as source, because the interesting failures
// here are things nobody would write on purpose: a credential arriving with a
// dependency, a request that skips the limiter, a shell out that started as a
// debugging line.

// goFiles walks the module and hands every Go file to fn as a parsed tree.
//
// Test files are included. A token in a test is still a token, and a request
// that skips the limiter in a test proves nothing about the client anyway.
func goFiles(t *testing.T, fn func(path string, fset *token.FileSet, file *ast.File)) {
	t.Helper()
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "docs", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		fn(strings.TrimPrefix(path, root+"/"), fset, file)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
}

// TestNoCredentialLiterals is the one that would be embarrassing to fail.
//
// This tool reads arXiv anonymously and has nothing to authenticate with, so a
// header name like Authorization has no business appearing in a string anywhere.
// Comments are not searched: doc 00 explains at length that there is no key,
// and a test that failed on the word in a sentence would be read as noise and
// then switched off.
func TestNoCredentialLiterals(t *testing.T) {
	// Spelled in halves so that this test does not fail on itself. The words
	// are the point, and a file that has to name them to look for them would
	// otherwise be the only place in the module that trips the check.
	banned := []string{
		"author" + "ization",
		"api" + "_key",
		"api" + "key",
		"access" + "_token",
		"coo" + "kie",
		"bear" + "er ",
	}
	goFiles(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text := strings.ToLower(lit.Value)
			for _, word := range banned {
				if strings.Contains(text, word) {
					t.Errorf("%s:%d has the literal %s, and this tool has nothing to authenticate with",
						path, fset.Position(lit.Pos()).Line, lit.Value)
				}
			}
			return true
		})
	})
}

// TestNothingShellsOut keeps the library to Go. A tool that reads a web site
// has no reason to start a process, and the one place in the series that ever
// wanted to was an editor launcher, which this tool does not have either.
func TestNothingShellsOut(t *testing.T) {
	goFiles(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "exec" {
				return true
			}
			t.Errorf("%s:%d calls exec.%s", path, fset.Position(sel.Pos()).Line, sel.Sel.Name)
			return true
		})
	})
}

// The three functions in the package that hand a request to net/http. Every one
// of them either waits on a limiter first or is called by exactly one function
// that does, and the comment on each says which.
var requestSites = map[string]string{
	// try makes the attempt. It does not wait, because fetchLive waits once per
	// attempt and then goes through try, and pacing inside try as well would
	// add a gap to a backoff that was already measured.
	"try": "fetchLive waits, and try is the attempt it retries",
	// requestWith is the streaming path: files, downloads, anything that wants
	// headers or a body it does not hold in memory.
	"requestWith": "waits",
	// redirectTarget follows one trackback link with HEAD, to see where it
	// lands without pulling down the page at the other end.
	"redirectTarget": "waits",
	// The one deliberately unpaced request in the module, and it is a test
	// rather than a code path. It goes fast on purpose to make arxiv.org answer
	// 429 so the backoff can be tested against the real thing, and it is behind
	// both the live build tag and -provoke-rate-limit. It uses a bare
	// http.Client rather than this package's, because this package's refuses to
	// go under the fifteen second floor and that refusal is what everything
	// else relies on.
	"TestLiveRateLimitBacksOffAndCompletes": "provokes a 429 on purpose, behind a tag and a flag",
}

// TestEveryRequestGoesThroughTheLimiter is the pacing promise made structural.
//
// Rather than trusting that every read remembered, the test finds every call to
// an http.Client's Do in the package and insists it is one of three known
// functions. Adding a fourth request path is then a decision somebody makes on
// purpose, with this list as the place to argue for it.
func TestEveryRequestGoesThroughTheLimiter(t *testing.T) {
	found := map[string]bool{}
	waits := map[string]bool{}
	goFiles(t, func(path string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(path, "arxiv/") {
			return
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Do":
					// An http.Client's Do takes the request and nothing else,
					// which is what separates it from this package's own Do,
					// the one that takes a context and a Request value.
					if len(call.Args) != 1 {
						return true
					}
					arg, ok := call.Args[0].(*ast.Ident)
					if !ok || arg.Name != "req" {
						return true
					}
					found[fn.Name.Name] = true
					if _, known := requestSites[fn.Name.Name]; !known {
						t.Errorf("%s:%d issues a request from %s, which is not one of the paced entry points",
							path, fset.Position(call.Pos()).Line, fn.Name.Name)
					}
				case "wait":
					waits[fn.Name.Name] = true
				}
				return true
			})
		}
	})

	for name := range requestSites {
		if !found[name] {
			t.Errorf("%s no longer makes a request, so this list is out of date", name)
		}
	}
	for name, why := range requestSites {
		if why == "waits" && !waits[name] {
			t.Errorf("%s makes a request and no longer waits on a limiter", name)
		}
	}
	if !waits["fetchLive"] {
		t.Error("fetchLive no longer waits on a limiter, and it is what paces try")
	}
}

// TestNoWriteRoute holds the read-only claim at the HTTP level. arXiv has
// nothing anonymous to write to, so every request is a GET or a HEAD, and a
// POST appearing anywhere is worth stopping on rather than reviewing later.
func TestNoWriteRoute(t *testing.T) {
	goFiles(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "http" {
				return true
			}
			switch sel.Sel.Name {
			case "MethodPost", "MethodPut", "MethodPatch", "MethodDelete":
				t.Errorf("%s:%d names %s, and there is nothing on arXiv to write to",
					path, fset.Position(sel.Pos()).Line, sel.Sel.Name)
			}
			return true
		})
	})
}

// TestNoDirectCommandFrameworks keeps the command tree on one framework.
//
// The whole series runs on any-cli/kit, and a tool that half-migrated is a tool
// where `arxiv serve` and `arxiv mcp` need a second implementation of every
// command. cobra and pflag stay in the module graph because kit uses them, so
// the check is on the direct requires rather than on the whole graph.
func TestNoDirectCommandFrameworks(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, banned := range []string{
		"github.com/spf13/cobra",
		"github.com/charmbracelet/fang",
	} {
		for _, line := range strings.Split(string(mod), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, banned) {
				continue
			}
			if strings.Contains(line, "// indirect") {
				continue
			}
			t.Errorf("%s is a direct requirement; the command tree belongs to any-cli/kit", banned)
		}
	}
}

// TestGoVersionPinned holds the toolchain the spec asks for. A patch version
// rather than a minor one, so a build reproduces.
func TestGoVersionPinned(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(mod), "\ngo 1.26.5\n") {
		t.Error("go.mod should pin go 1.26.5")
	}
}
