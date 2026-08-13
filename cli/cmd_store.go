package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/arxiv-cli/arxiv"
)

// cmd_store.go is the half of the tool that keeps something. The store is a
// SQLite file of nodes, claims and reads (doc 04 section 4); `arxiv query`,
// `arxiv db` and `arxiv export` read it.
//
// None of these are kit operations, so none of them are reachable over `arxiv
// serve` or `arxiv mcp`, and that is deliberate. They open a file on this
// machine and one of them takes SQL, which is a different thing to put on a
// network port than a paper lookup. Every read of arXiv itself stays an
// operation and stays on both surfaces.
//
// They still render like an operation. kit hands an escape hatch command the
// run's resolved output settings, so -o json, --fields and --no-header work
// here exactly as they do everywhere else.

// storeCommands is the whole set, added to the app in one call.
func storeCommands() []kit.Command {
	return []kit.Command{
		newQueryCmd(),
		newDBCmd(),
		newExportCmd(),
	}
}

// storeFlag binds --store to a command and resolves it at run time.
//
// An unset --store is the file under the data directory kit already resolved,
// which is beside the cache rather than in the working directory: a store is
// something to keep, and a file called arxiv.db appearing wherever the command
// was run is how a crawl gets deleted by accident.
type storeFlag struct{ path string }

func (s *storeFlag) bind(f *kit.FlagSet) {
	f.StringVar(&s.path, "store", "", "the store to read (default: arxiv.db under the data directory)")
}

func (s *storeFlag) resolve(ctx context.Context) string {
	if s.path != "" {
		return s.path
	}
	if st := kit.FromContext(ctx); st != nil {
		return arxiv.DefaultStorePath(st.Config.DataDir)
	}
	return arxiv.DefaultStorePath("")
}

// out builds the renderer for a hand written command, using the same output
// settings every operation was given.
func out(ctx context.Context) (*render.Renderer, error) {
	st := kit.FromContext(ctx)
	if st == nil {
		return nil, fmt.Errorf("no run state on the context")
	}
	return st.Renderer(os.Stdout)
}

// emitAll renders every record and flushes once, which is what makes a table a
// table rather than a row at a time.
func emitAll[T any](ctx context.Context, records []T) error {
	r, err := out(ctx)
	if err != nil {
		return err
	}
	for i := range records {
		if err := r.Emit(&records[i]); err != nil {
			return err
		}
	}
	return r.Flush()
}

func newQueryCmd() kit.Command {
	store := &storeFlag{}
	return kit.Command{
		Use:   "query <sql>",
		Short: "Run SQL over a store",
		Group: "graph",
		Long: `Run SQL over a store.

The string is handed straight to SQLite. There is no query language of arxiv's
own here on purpose: the answer to what a store says should be a query somebody
already knows how to write, and the schema is three tables a person can hold in
their head.

  nodes   uri, kind, record, first_seen, last_seen
  claims  from_uri, predicate, to_uri, source, surface, note, position, seen_at
  reads   url, surface, plane, status, bytes, at, error

The file is opened mode=ro, so a finger slip that says delete is refused by
SQLite rather than by a check in this tool. A check here would be one regular
expression away from being wrong and the database has the answer already.

  arxiv query "select predicate, count(*) c from claims group by 1 order by c desc"
  arxiv query "select uri from nodes where kind='paper' and record is null limit 20"

The second one is the frontier: every paper the store has heard of and nobody
has read.`,
		Args:  kit.ExactArgs(1),
		Flags: store.bind,
		Run: func(ctx context.Context, args []string) error {
			st, err := arxiv.OpenStoreReadOnly(store.resolve(ctx))
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			cols, rows, err := st.Query(args[0])
			if err != nil {
				// SQLite's own message names the token it choked on, which is
				// more use than anything this tool could say about it.
				return errs.Usage("%s", err.Error())
			}
			if len(rows) == 0 {
				return errs.NoResults("that query matched nothing")
			}
			r, err := out(ctx)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if err := r.Emit(queryRow(cols, row)); err != nil {
					return err
				}
			}
			return r.Flush()
		},
	}
}

// queryRow turns one SQL row into a record with the query's own column names on
// it, so `select predicate, count(*) c` prints a predicate column and a c
// column rather than col1 and col2.
func queryRow(cols []string, vals []any) render.Record {
	out := make([]string, len(cols))
	value := make(map[string]any, len(cols))
	for i, c := range cols {
		if i < len(vals) {
			out[i] = cell(vals[i])
			value[c] = vals[i]
		}
	}
	return render.Record{Cols: cols, Vals: out, Value: value}
}

// cell prints one SQL value. A blob comes back as []byte, which is what a
// record column is, and printing it as a Go byte slice would be unreadable.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func newDBCmd() kit.Command {
	stats := &storeFlag{}
	vacuum := &storeFlag{}
	return kit.Command{
		Use:   "db",
		Short: "Store maintenance and statistics",
		Group: "graph",
		Sub: []kit.Command{
			{
				Use:   "stats",
				Short: "What is in the store, counted three ways",
				Long: `Count what is in the store: nodes by kind, claims by predicate, and reads by
plane, surface and status.

The read log is the section to look at first. A crawl that spent its whole
budget on 404s and a crawl that worked look identical in the other two, and the
plane column is the one that says where the afternoon went: the HTML plane
paces at fifteen seconds, so a hundred rows of it is twenty five minutes.

Each kind of node gets a second row when some of them have not been read. That
number is the frontier, and it is the one a crawl is deciding on.`,
				Args:  kit.NoArgs,
				Flags: stats.bind,
				Run: func(ctx context.Context, args []string) error {
					st, err := arxiv.OpenStoreReadOnly(stats.resolve(ctx))
					if err != nil {
						return err
					}
					defer func() { _ = st.Close() }()
					rows, err := st.Stats()
					if err != nil {
						return err
					}
					if len(rows) == 0 {
						return errs.NoResults("the store is empty")
					}
					return emitAll(ctx, rows)
				},
			},
			{
				Use:   "vacuum",
				Short: "Compact the store",
				Write: true,
				Long: `Rebuild the file, releasing the space deleted rows left behind.

This is the one db command that opens the store for writing, and it changes
nothing about what the store says.`,
				Args:  kit.NoArgs,
				Flags: vacuum.bind,
				Run: func(ctx context.Context, args []string) error {
					path := vacuum.resolve(ctx)
					if _, err := os.Stat(path); err != nil {
						return errs.NotFound("no store at %s", path)
					}
					st, err := arxiv.OpenStore(path)
					if err != nil {
						return err
					}
					defer func() { _ = st.Close() }()
					return st.Vacuum()
				},
			},
		},
	}
}

// exportFormats are the ones this command writes today. rdf is doc 04 section 5
// and lands with `arxiv rdf`.
var exportFormats = []string{"json", "ndjson", "csv"}

// exportKinds are the node kinds that carry a record. Everything else in the
// store is a node a claim named and nothing read, and those are what `arxiv
// query` lists.
var exportKinds = []string{"paper", "category", "author", "set"}

func newExportCmd() kit.Command {
	store := &storeFlag{}
	var format, kind string
	var claims bool
	var limit int
	return kit.Command{
		Use:   "export",
		Short: "Write a store as JSON, NDJSON or CSV",
		Group: "graph",
		Long: `Write what a store holds to stdout.

By default this is the records: everything the store actually read, in URI
order, so two exports of the same store are the same bytes and a diff means
something changed. --claims writes the claims table instead, which is the graph
rather than the records.

--kind narrows it to one kind of node: paper, category, author or set. Nodes
with no record are not exported, because a node nobody read has nothing to
export; arxiv query is how to list those.

--format is the same set the global -o takes, and it is here because an export
is written to a file rather than looked at, so the format belongs to the
command and not to the terminal it happened to run in.`,
		Args: kit.NoArgs,
		Flags: func(f *kit.FlagSet) {
			store.bind(f)
			f.StringVar(&format, "format", "ndjson", "json, ndjson or csv")
			f.StringVar(&kind, "kind", "paper", "which kind of record to write: "+strings.Join(exportKinds, ", "))
			f.BoolVar(&claims, "claims", false, "write the claims table instead of the records")
			f.IntVar(&limit, "limit", 0, "stop after this many rows")
		},
		Run: func(ctx context.Context, args []string) error {
			if !contains(exportFormats, format) {
				return errs.Usage("the --format value %q is not one of %s", format, strings.Join(exportFormats, ", "))
			}
			st, err := arxiv.OpenStoreReadOnly(store.resolve(ctx))
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			r, err := exportRenderer(ctx, format)
			if err != nil {
				return err
			}
			n, err := writeExport(st, r, kind, claims, limit)
			if err != nil {
				return err
			}
			if n == 0 {
				return errs.NoResults("the store holds nothing to export")
			}
			return r.Flush()
		},
	}
}

// exportRenderer is the run's renderer with the format the command was given.
func exportRenderer(ctx context.Context, format string) (*render.Renderer, error) {
	st := kit.FromContext(ctx)
	if st == nil {
		return nil, fmt.Errorf("no run state on the context")
	}
	o := st.Output
	return render.New(render.Options{
		Format:   render.Format(format),
		IsTTY:    o.IsTTY,
		Color:    o.Color,
		Fields:   o.Fields,
		NoHeader: o.NoHeader,
		Template: o.Template,
		Width:    o.Width,
		Writer:   os.Stdout,
	})
}

// writeExport streams the rows out and says how many there were.
//
// Each kind comes back as the record it is rather than as a node row, so a CSV
// of papers has a title column. A node row would be a uri, a kind and a blob.
func writeExport(st *arxiv.Store, r *render.Renderer, kind string, claims bool, limit int) (int, error) {
	if claims {
		rows, err := st.Claims("", "", "", limit)
		if err != nil {
			return 0, err
		}
		return len(rows), emitInto(r, rows)
	}
	switch kind {
	case "paper":
		return emitRecords(st.Papers(limit))(r)
	case "category":
		return emitRecords(st.Categories(limit))(r)
	case "author":
		return emitRecords(st.People(limit))(r)
	case "set":
		return emitRecords(st.Sets(limit))(r)
	default:
		return 0, errs.Usage("the --kind value %q is not one of %s", kind, strings.Join(exportKinds, ", "))
	}
}

// emitRecords carries a typed slice and its error through the switch above, so
// each arm stays one line and the error is still checked.
func emitRecords[T any](records []T, err error) func(*render.Renderer) (int, error) {
	return func(r *render.Renderer) (int, error) {
		if err != nil {
			return 0, err
		}
		return len(records), emitInto(r, records)
	}
}

func emitInto[T any](r *render.Renderer, records []T) error {
	for i := range records {
		if err := r.Emit(&records[i]); err != nil {
			return err
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
