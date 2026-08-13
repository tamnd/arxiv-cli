package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/tamnd/any-cli/kit"
)

// newVersionCmd prints the build, which is what a bug report should quote.
func newVersionCmd() kit.Command {
	var short bool
	return kit.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  kit.NoArgs,
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&short, "short", false, "print just the version number")
		},
		Run: func(_ context.Context, _ []string) error {
			if short {
				_, _ = fmt.Fprintln(os.Stdout, Version)
				return nil
			}
			_, _ = fmt.Fprintf(os.Stdout, "arxiv %s (commit %s, built %s, %s/%s, %s)\n",
				Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
			return nil
		},
	}
}
