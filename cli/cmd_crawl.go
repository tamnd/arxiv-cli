package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/arxiv"
)

// cmd_crawl.go is the other half of the escape hatch: the two commands that
// write to this machine rather than read from arXiv.
//
// Like the store commands they are hand written rather than kit operations, and
// for the same reason. A crawl runs for an hour and writes a database, and an
// archive writes a directory of files; neither is something to expose on an
// HTTP route or hand to a model as a tool.

// crawlCommands is the pair, added to the app beside the store commands.
func crawlCommands() []kit.Command {
	return []kit.Command{newCrawlCmd(), newArchiveCmd()}
}

// clientOf pulls the arXiv client kit built for this run.
func clientOf(ctx context.Context) (*arxiv.Client, error) {
	st := kit.FromContext(ctx)
	if st == nil {
		return nil, fmt.Errorf("no run state on the context")
	}
	raw, err := st.Client(ctx)
	if err != nil {
		return nil, err
	}
	c, ok := raw.(*arxiv.Client)
	if !ok || c == nil {
		return nil, fmt.Errorf("no arxiv client on this run")
	}
	return c, nil
}

// dataDir is where kit put this run's data, which is where a crawl's manifests
// and an archive's files go unless the user named somewhere else.
func dataDir(ctx context.Context) string {
	if st := kit.FromContext(ctx); st != nil {
		return st.Config.DataDir
	}
	return ""
}

// say writes a line of progress to stderr, which is where every running
// commentary in this tool goes so that stdout stays the records.
func say(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func newCrawlCmd() kit.Command {
	store := &storeFlag{}
	var (
		o        arxiv.CrawlOptions
		depth    string
		yes      bool
		manifest string
	)
	return kit.Command{
		Use:   "crawl [seed...]",
		Short: "Walk arXiv into a store, on a budget",
		Group: "graph",
		Write: true,
		Long: `Walk out from a seed and keep everything read in a store.

A seed is a paper id, a category code, or an ax:// uri. --search seeds from a
query in arXiv's own grammar instead, which is the cheapest start there is: one
request is a hundred papers.

The budget is two numbers and not one, because the planes are five times apart.
--budget is the API plane, where a request is three seconds; --html-budget is
arxiv.org, where robots.txt asks for fifteen. Fifty requests is two and a half
minutes on one and twelve and a half on the other, and a single budget would
have to be wrong about one of them.

The plan is printed before anything is read and confirmed at a terminal. --yes
skips the question, which is what a script wants.

  arxiv crawl 1706.03762 --hops 2
  arxiv crawl --search "cat:cs.CL" --max 100 --depth meta --budget 20
  arxiv crawl --resume --budget 200

Every request goes into the store's read log as it happens, and a manifest of
the run is written at the end whether the run finished, ran out of budget or was
interrupted. A crawl stopped with ctrl-c keeps everything it had already read:
the store is written as it goes, not at the end.`,
		Flags: func(f *kit.FlagSet) {
			store.bind(f)
			f.StringVar(&o.Search, "search", "", "seed from a query in arXiv's grammar, for example cat:cs.CL")
			f.IntVar(&o.Max, "max", 100, "how many results the seed search takes")
			f.IntVar(&o.Hops, "hops", 2, "how far to walk; one hop reads the seeds and stops")
			f.StringVar(&depth, "depth", "meta", "how deeply each paper is read: quick, meta, full or text")
			f.IntVar(&o.Budget, "budget", 100, "request ceiling on the api plane")
			f.IntVar(&o.HTMLBudget, "html-budget", 20, "request ceiling on the arxiv.org plane")
			f.BoolVar(&o.APIOnly, "api-only", false, "queue nothing on arxiv.org, whatever else is asked for")
			f.BoolVar(&o.Names, "names", false, "follow author names, at one search each")
			f.BoolVar(&o.Trackbacks, "trackbacks", false, "read the inbound links of the seed papers")
			f.IntVar(&o.Limit, "limit", 25, "how many papers a name search reads")
			f.BoolVar(&o.Resume, "resume", false, "carry on from what the store has already heard of and not read")
			f.BoolVar(&yes, "yes", false, "do not ask before starting")
			f.StringVar(&manifest, "manifest", "", "where to write the run manifest (default: crawls under the data directory)")
		},
		Run: func(ctx context.Context, args []string) error {
			d, err := arxiv.ParseDepth(depth)
			if err != nil {
				return errs.Usage("%s", err.Error())
			}
			o.Seeds = args
			o.Depth = d
			o.Progress = say

			c, err := clientOf(ctx)
			if err != nil {
				return err
			}
			st, err := arxiv.OpenStore(store.resolve(ctx))
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			cr, err := arxiv.NewCrawler(c, st, o)
			if err != nil {
				return err
			}
			plan, err := cr.Plan(ctx)
			if err != nil {
				return err
			}
			fmt.Fprint(os.Stderr, plan.String())
			if !yes && !confirm(ctx) {
				return errs.Usage("nothing was read")
			}

			m, runErr := cr.Run(ctx)
			if m != nil {
				dir := manifest
				if dir == "" {
					dir = filepath.Join(dataDir(ctx), "crawls")
				}
				// The manifest is written before the error is returned, because
				// the run that most needs one is the run that was interrupted.
				if path, err := m.Save(dir); err != nil {
					say("the manifest could not be written: %s", err)
				} else {
					say("%s", m)
					say("manifest %s", path)
				}
				if err := emitAll(ctx, []arxiv.Manifest{*m}); err != nil {
					return err
				}
			}
			if runErr != nil {
				return runErr
			}
			if m != nil && m.Cancelled {
				// Everything read is in the store and the manifest says where
				// it stopped, but a crawl that was interrupted did not do what
				// it was asked to, and the exit code should say so.
				return errs.New(errs.KindGeneric, "the crawl was interrupted; %s", m)
			}
			return nil
		},
	}
}

// confirm asks before a long read. At a terminal it waits for an answer; piped
// into something it does not, because there is nobody there to ask and a script
// that ran the command has already said yes.
func confirm(ctx context.Context) bool {
	st := kit.FromContext(ctx)
	if st == nil || !st.Output.IsTTY {
		return true
	}
	fmt.Fprint(os.Stderr, "start? [y/N] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func newArchiveCmd() kit.Command {
	var (
		dir   string
		files bool
	)
	return kit.Command{
		Use:   "archive <id>...",
		Short: "Write every surface of a paper to disk",
		Group: "graph",
		Write: true,
		Long: `Write one paper's surfaces to disk as arXiv served them.

Each paper gets a directory holding the raw bytes of every surface, a meta.json
saying what was fetched and what each file hashes to, and a record.json built
from those exact bytes rather than from a second read.

  s1-api.xml            the export API feed
  s2-oai-arxiv.xml      the OAI arXiv record
  s2-oai-arxivraw.xml   the OAI arXivRaw record
  s3-abs.html           the abstract page
  s9-bibtex.bib         arXiv's own BibTeX
  s10-fulltext.html     the LaTeXML rendering, when there is one
  s11-trackbacks.html   the trackback page
  s12-paper.pdf         the PDF, with --files

Nothing here is served from the cache, which is the whole point: an archive of
what the cache was holding is not an archive of what arXiv says today.

Four of these are on arxiv.org at fifteen seconds each, so one paper is about a
minute and ten papers is ten minutes.`,
		Args: kit.MinimumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.StringVar(&dir, "dir", "", "where to write (default: archive under the data directory)")
			f.BoolVar(&files, "files", false, "also fetch the PDF and the submission source")
		},
		Run: func(ctx context.Context, args []string) error {
			c, err := clientOf(ctx)
			if err != nil {
				return err
			}
			root := dir
			if root == "" {
				root = filepath.Join(dataDir(ctx), "archive")
			}
			out := make([]arxiv.Archive, 0, len(args))
			for _, ref := range args {
				a, err := c.Archive(ctx, ref, arxiv.ArchiveOptions{
					Dir:      root,
					Files:    files,
					Progress: say,
				})
				if err != nil {
					// One paper that is not there does not stop the rest, and
					// the ones already written stay written.
					say("%s could not be archived: %s", ref, err)
					if len(args) == 1 {
						return err
					}
					continue
				}
				out = append(out, a)
			}
			if len(out) == 0 {
				return errs.NoResults("nothing was archived")
			}
			return emitAll(ctx, out)
		},
	}
}
