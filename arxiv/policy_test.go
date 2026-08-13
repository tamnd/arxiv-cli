package arxiv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
