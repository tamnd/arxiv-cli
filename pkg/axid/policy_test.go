package axid

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoNetworkImports reads the package as source and asserts it imports
// nothing that can make a request or read a clock.
//
// The claim that axid answers with zero requests is in the package doc, in the
// help text and on every record `arxiv id` emits as "requests": 0. This is what
// keeps that claim true after somebody adds a convenience lookup.
func TestNoNetworkImports(t *testing.T) {
	banned := map[string]string{
		"net":       "a network call",
		"net/http":  "a network call",
		"net/url":   "URL parsing, which this package does by hand so a reference never gets normalised behind its back",
		"os":        "the filesystem",
		"os/exec":   "a subprocess",
		"time":      "the clock, which would make the output depend on when it ran",
		"math/rand": "randomness, which would make the output depend on the run",
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("found no Go files; this check read nothing")
	}

	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		// Test files may reach for the network; the live suite does.
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if why, bad := banned[path]; bad {
				t.Errorf("%s imports %q, which is %s", fset.Position(imp.Pos()), path, why)
			}
		}
	}
	if checked == 0 {
		t.Fatal("every Go file here was a test file; this check read nothing")
	}
}
