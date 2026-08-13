package arxiv

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/arxiv-cli/pkg/graph"

	_ "modernc.org/sqlite"
)

// store.go is the graph on disk. Spec 3006 doc 04 section 4.
//
// Three tables and no more. A table per record type would make a new kind of
// thing a migration, would leave a paper nobody has fetched with nowhere to
// live, and would turn "what have I heard of and not looked at" into a query
// nobody can write. Nodes and claims replace all of it: a node is something
// with a URI, its record is the last read of it or null when nobody has read
// it, and a claim is one observation of one edge.

// Store is the SQLite file. One file, opened read-write by everything that
// writes and read-only by everything that asks.
type Store struct {
	db       *sql.DB
	path     string
	readOnly bool
	now      func() time.Time
}

// storeSchema is the whole thing. The three tables are doc 04 section 4; the
// indexes are explained where they appear.
const storeSchema = `
CREATE TABLE IF NOT EXISTS nodes (
	uri        TEXT PRIMARY KEY,
	kind       TEXT NOT NULL,
	record     JSON,
	first_seen INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL
);

-- The frontier query, which a crawl runs on every hop: everything of a kind
-- that nothing has read.
CREATE INDEX IF NOT EXISTS nodes_unread ON nodes(kind) WHERE record IS NULL;

CREATE TABLE IF NOT EXISTS claims (
	from_uri  TEXT NOT NULL,
	predicate TEXT NOT NULL,
	to_uri    TEXT NOT NULL,
	source    TEXT NOT NULL,
	surface   TEXT NOT NULL DEFAULT '',
	-- note and position are labels rather than assertions, which is why they are
	-- outside the key: a later sighting that carries one fills in an earlier one
	-- that did not, instead of becoming a second row.
	note      TEXT NOT NULL DEFAULT '',
	position  INTEGER NOT NULL DEFAULT 0,
	seen_at   INTEGER NOT NULL,
	PRIMARY KEY (from_uri, predicate, to_uri, source)
);

CREATE INDEX IF NOT EXISTS claims_to ON claims(to_uri, predicate);
CREATE INDEX IF NOT EXISTS claims_predicate ON claims(predicate);

CREATE TABLE IF NOT EXISTS reads (
	url     TEXT NOT NULL,
	surface TEXT NOT NULL DEFAULT '',
	-- plane is the arXiv specific column. The two planes are five times apart,
	-- so how much of a run went to arxiv.org is the number that explains where
	-- the afternoon went.
	plane   TEXT NOT NULL DEFAULT '',
	status  INTEGER NOT NULL DEFAULT 0,
	bytes   INTEGER NOT NULL DEFAULT 0,
	at      INTEGER NOT NULL,
	error   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS reads_at ON reads(at);
`

// storeTables is what Reset drops. It is spelled out rather than read out of
// sqlite_master, so a reset never drops a table something else put in the file.
var storeTables = []string{"nodes", "claims", "reads"}

// DefaultStorePath is where a store lives when nobody named one: under the
// data directory kit already resolved, beside the cache.
func DefaultStorePath(dataDir string) string {
	if dataDir == "" {
		return "arxiv.db"
	}
	return filepath.Join(dataDir, "arxiv.db")
}

// OpenStore opens the file read-write, creating it and its directory.
func OpenStore(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create store dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec(storeSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db, path: path, now: time.Now}, nil
}

// OpenStoreReadOnly opens the file with mode=ro, which is what `arxiv query`
// uses.
//
// The point is that a finger slip that says delete is refused by SQLite rather
// than by a check in this tool. A check here would be one regular expression
// away from being wrong, and the database has the answer already.
func OpenStoreReadOnly(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, errs.NotFound("no store at %s; arxiv crawl writes one", path)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	return &Store{db: db, path: path, readOnly: true, now: time.Now}, nil
}

// Path is the file, ReadOnly is how it was opened, and Close and Vacuum are
// what they say.
func (s *Store) Path() string   { return s.path }
func (s *Store) ReadOnly() bool { return s.readOnly }
func (s *Store) Close() error   { return s.db.Close() }

func (s *Store) Vacuum() error {
	_, err := s.db.Exec(`VACUUM`)
	return err
}

// Reset empties the store, which is what a crawl that wants to start again
// does rather than deleting a file somebody may have pointed something else at.
func (s *Store) Reset() error {
	for _, t := range storeTables {
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return fmt.Errorf("drop %s: %w", t, err)
		}
	}
	_, err := s.db.Exec(storeSchema)
	return err
}

// stamp is the second everything written in one call shares.
func (s *Store) stamp() int64 {
	if s.now == nil {
		return time.Now().Unix()
	}
	return s.now().Unix()
}

// ─── nodes ───────────────────────────────────────────────────────────────────

// StoredNode is one row of the nodes table. Record is nil on a node something
// named and nothing has read, which on any real store is most of them.
type StoredNode struct {
	URI       string          `json:"uri" kit:"id" table:"uri"`
	Kind      string          `json:"kind" table:"kind"`
	Record    json.RawMessage `json:"record,omitempty" table:"-"`
	FirstSeen time.Time       `json:"first_seen" table:"-"`
	LastSeen  time.Time       `json:"last_seen" table:"last_seen"`
}

// Read reports whether anything actually fetched this node.
func (n StoredNode) Read() bool { return len(n.Record) > 0 }

// Sight records that a claim named a node, without claiming to have read it.
//
// This is the half of the store that makes a crawl resumable, and it is why the
// store can answer what it has not looked at. One paper read names eight
// authors, three categories, a licence and a DOI, and every one of those is
// something the tool has now heard of.
func (s *Store) Sight(uri string) error {
	kind, ok := graph.KindOf(uri)
	if !ok || graph.IsVersion(uri) {
		// A version is a fragment on a paper rather than a node of its own, and
		// nothing will ever fetch one on its own.
		return nil
	}
	return s.putNode(uri, kind, nil)
}

// PutRecord writes what a read returned, under the URI the record names.
//
// It takes the record rather than a URI and a blob, so a caller cannot file a
// category under a paper's URI, and it returns the URI it used so the caller can
// say where it went.
func (s *Store) PutRecord(record any) (string, error) {
	uri, kind := recordURI(record)
	if uri == "" {
		return "", fmt.Errorf("%T names no node, so there is nowhere to put it", record)
	}
	blob, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	return uri, s.putNode(uri, kind, blob)
}

// putNode is the upsert both of those end in.
//
// A node met twice keeps the better sighting. A later sighting with no record
// does not erase a record that is there, which is what stops a search result
// naming a paper the store read last week from blanking it.
func (s *Store) putNode(uri, kind string, record []byte) error {
	now := s.stamp()
	var blob any
	if len(record) > 0 {
		blob = string(record)
	}
	_, err := s.db.Exec(`
		INSERT INTO nodes (uri, kind, record, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(uri) DO UPDATE SET
			kind       = CASE WHEN excluded.kind <> '' THEN excluded.kind ELSE nodes.kind END,
			record     = COALESCE(excluded.record, nodes.record),
			first_seen = MIN(nodes.first_seen, excluded.first_seen),
			last_seen  = MAX(nodes.last_seen, excluded.last_seen)`,
		uri, kind, blob, now, now)
	if err != nil {
		return fmt.Errorf("put node %s: %w", uri, err)
	}
	return nil
}

// recordURI says where a record belongs.
//
// It is the one place that maps a Go type to a node, and the records that are
// deliberately not here are worth as much as the ones that are. An Announcement
// and a FullText both name a paper: filing either under the paper's URI would
// put one view of a paper where another view lives, and the two would take
// turns overwriting each other depending on which read ran last. Their claims
// carry what they know.
func recordURI(record any) (string, string) {
	switch r := record.(type) {
	case Paper:
		return graph.Paper(r.ID), graph.KindPaper
	case *Paper:
		return graph.Paper(r.ID), graph.KindPaper
	case Category:
		return graph.Category(r.Code), graph.KindCategory
	case *Category:
		return graph.Category(r.Code), graph.KindCategory
	case Set:
		return graph.Set(r.SetSpec), graph.KindSet
	case *Set:
		return graph.Set(r.SetSpec), graph.KindSet
	case Person:
		return personURI(r)
	case *Person:
		return personURI(*r)
	default:
		return "", ""
	}
}

// personURI files a person under the identifier page and files a name search
// nowhere.
//
// A name search matched strings. Filing it as a person would mark a name read
// on the strength of a string match, which is the one thing doc 04 section 2
// spends a page saying not to do.
func personURI(p Person) (string, string) {
	if !p.Identified || p.ArxivID == "" {
		return "", ""
	}
	return graph.Author(p.ArxivID), graph.KindAuthor
}

// Node reads one node back, record and all. A node that is not there is not an
// error: the answer to "have you heard of this" is no.
func (s *Store) Node(uri string) (*StoredNode, error) {
	row := s.db.QueryRow(`SELECT uri, kind, record, first_seen, last_seen FROM nodes WHERE uri = ?`, uri)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

// readableKinds are the node kinds this tool can go and fetch.
//
// A DOI, a licence, a journal reference and a hashed URL are nodes it names and
// does not read, so they are never on a frontier and a store full of them does
// not look like a crawl with work left to do.
var readableKinds = []string{graph.KindPaper, graph.KindCategory, graph.KindAuthor, graph.KindName}

// Frontier is what a crawl reads next: nodes of a kind nothing has fetched,
// oldest sighting first so a walk does not keep rediscovering the same corner.
//
// An empty kind means every kind this tool can read.
func (s *Store) Frontier(kind string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT uri FROM nodes WHERE record IS NULL`
	var args []any
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	} else {
		q += ` AND kind IN ('` + strings.Join(readableKinds, "','") + `')`
	}
	q += ` ORDER BY first_seen, uri LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			return out, err
		}
		out = append(out, uri)
	}
	return out, rows.Err()
}

// Nodes lists nodes of a kind, read or not, for anything that wants to page
// through the store without writing SQL.
func (s *Store) Nodes(kind string, limit int) ([]StoredNode, error) {
	q := `SELECT uri, kind, record, first_seen, last_seen FROM nodes`
	var args []any
	if kind != "" {
		q += ` WHERE kind = ?`
		args = append(args, kind)
	}
	q += ` ORDER BY uri`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []StoredNode
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return out, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// scanner is what Node and Nodes have in common: sql.Row and sql.Rows both
// scan.
type scanner interface{ Scan(dest ...any) error }

func scanNode(sc scanner) (*StoredNode, error) {
	var (
		uri, kind   string
		record      []byte
		first, last int64
	)
	if err := sc.Scan(&uri, &kind, &record, &first, &last); err != nil {
		return nil, err
	}
	return &StoredNode{
		URI:       uri,
		Kind:      kind,
		Record:    record,
		FirstSeen: time.Unix(first, 0).UTC(),
		LastSeen:  time.Unix(last, 0).UTC(),
	}, nil
}

// Papers, Categories, People and Sets read records back out of the store as
// what they were, which is what an export walks.
//
// They come back typed rather than as raw JSON so an export writes a paper's
// own columns. A CSV of a node row would carry a uri, a kind and a blob, which
// is a file nothing can read.
func (s *Store) Papers(limit int) ([]Paper, error) {
	return recordsOfKind[Paper](s, graph.KindPaper, limit)
}

func (s *Store) Categories(limit int) ([]Category, error) {
	return recordsOfKind[Category](s, graph.KindCategory, limit)
}

func (s *Store) People(limit int) ([]Person, error) {
	return recordsOfKind[Person](s, graph.KindAuthor, limit)
}

func (s *Store) Sets(limit int) ([]Set, error) {
	return recordsOfKind[Set](s, graph.KindSet, limit)
}

func recordsOfKind[T any](s *Store, kind string, limit int) ([]T, error) {
	q := `SELECT record FROM nodes WHERE kind = ? AND record IS NOT NULL ORDER BY uri`
	args := []any{kind}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return out, err
		}
		var rec T
		if err := json.Unmarshal(blob, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ─── claims ──────────────────────────────────────────────────────────────────

// PutClaims writes a set of claims and every node they named, in one
// transaction, and returns how many rows the claims table gained.
//
// The nodes come along because that is the whole point of the plane: a claim
// naming a paper is a paper the store now knows exists. Writing the claim
// without the node would leave the frontier empty on a store full of claims.
//
// The count is a row count taken either side rather than the rows the inserts
// touched, because an upsert reports a row touched whether it inserted one or
// updated one, and a second read of the same paper is not thirty one new
// claims.
func (s *Store) PutClaims(edges []graph.Edge) (int, error) {
	if len(edges) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var before int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM claims`).Scan(&before); err != nil {
		return 0, err
	}

	claim, err := tx.Prepare(`
		INSERT INTO claims (from_uri, predicate, to_uri, source, surface, note, position, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(from_uri, predicate, to_uri, source) DO UPDATE SET
			surface  = excluded.surface,
			note     = CASE WHEN excluded.note <> '' THEN excluded.note ELSE claims.note END,
			position = CASE WHEN excluded.position <> 0 THEN excluded.position ELSE claims.position END,
			seen_at  = excluded.seen_at`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = claim.Close() }()

	node, err := tx.Prepare(`
		INSERT INTO nodes (uri, kind, record, first_seen, last_seen)
		VALUES (?, ?, NULL, ?, ?)
		ON CONFLICT(uri) DO UPDATE SET last_seen = MAX(nodes.last_seen, excluded.last_seen)`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = node.Close() }()

	now := s.stamp()
	seen := map[string]bool{}
	for _, e := range edges {
		if err := e.Validate(); err != nil {
			// The table is checked on the way in as well as on the way out. A
			// store is where a bad claim would live longest.
			return 0, fmt.Errorf("refused: %w", err)
		}
		for _, end := range []string{e.From, e.To} {
			if seen[end] {
				continue
			}
			seen[end] = true
			kind, ok := graph.KindOf(end)
			if !ok || graph.IsVersion(end) {
				continue
			}
			if _, err := node.Exec(end, kind, now, now); err != nil {
				return 0, fmt.Errorf("put node %s: %w", end, err)
			}
		}
		if _, err := claim.Exec(e.From, e.Predicate, e.To, e.Source, e.Surface, e.Note, e.Position, now); err != nil {
			return 0, fmt.Errorf("put claim %s %s %s: %w", e.From, e.Predicate, e.To, err)
		}
	}

	var after int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM claims`).Scan(&after); err != nil {
		return 0, err
	}
	return after - before, tx.Commit()
}

// Claims reads claims back, filtered by whichever ends the caller gave.
func (s *Store) Claims(from, predicate, to string, limit int) ([]graph.Edge, error) {
	q := `SELECT from_uri, predicate, to_uri, source, surface, note, position FROM claims WHERE 1=1`
	var args []any
	if from != "" {
		q += ` AND from_uri = ?`
		args = append(args, from)
	}
	if predicate != "" {
		q += ` AND predicate = ?`
		args = append(args, predicate)
	}
	if to != "" {
		q += ` AND to_uri = ?`
		args = append(args, to)
	}
	q += ` ORDER BY from_uri, predicate, position, to_uri, source`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []graph.Edge
	for rows.Next() {
		var e graph.Edge
		if err := rows.Scan(&e.From, &e.Predicate, &e.To, &e.Source, &e.Surface, &e.Note, &e.Position); err != nil {
			return out, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Label is the readable name some claim gave a node, empty when none did.
//
// It exists for one thing and it is worth spelling out: a name node's URI is a
// slug, and searching arXiv for ashish-vaswani finds nothing. The spelling
// arXiv printed is in the note on the claims that named it, so that is where a
// crawl looks before it goes and searches for a person.
func (s *Store) Label(uri string) (string, error) {
	var note string
	err := s.db.QueryRow(`
		SELECT note FROM claims
		WHERE note <> '' AND (from_uri = ? OR to_uri = ?)
		ORDER BY seen_at LIMIT 1`, uri, uri).Scan(&note)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return note, err
}

// ─── reads ───────────────────────────────────────────────────────────────────

// Read is one request, logged.
//
// This is what makes a record's sources checkable afterwards. A claim says a URL
// asserted it; this table says that URL was fetched, when, on which plane, and
// what came back, which is the difference between a provenance field and a
// provenance field somebody can verify.
type Read struct {
	URL     string    `json:"url" kit:"id" table:"url,truncate"`
	Surface string    `json:"surface" table:"surface"`
	Plane   string    `json:"plane" table:"plane"`
	Status  int       `json:"status" table:"status"`
	Bytes   int64     `json:"bytes" table:"bytes"`
	At      time.Time `json:"at" table:"at"`
	Error   string    `json:"error,omitempty" table:"error,truncate"`
}

// NewRead describes one request from its URL, filling in the surface and the
// plane rather than making every caller work them out.
func NewRead(rawURL string, status int, bytes int64, at time.Time, err error) Read {
	r := Read{URL: rawURL, Surface: surfaceOfURL(rawURL), Status: status, Bytes: bytes, At: at}
	if u, perr := url.Parse(rawURL); perr == nil && u.Host != "" {
		if plane, ok := PlaneFor(u.Host); ok {
			r.Plane = plane.Name
		}
	}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// PutRead appends one request to the audit log.
func (s *Store) PutRead(r Read) error {
	at := r.At
	if at.IsZero() {
		at = time.Unix(s.stamp(), 0)
	}
	_, err := s.db.Exec(
		`INSERT INTO reads (url, surface, plane, status, bytes, at, error) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.URL, r.Surface, r.Plane, r.Status, r.Bytes, at.Unix(), r.Error)
	return err
}

// Reads is the log back out, newest first, which is what somebody debugging a
// crawl wants to see.
func (s *Store) Reads(limit int) ([]Read, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT url, surface, plane, status, bytes, at, error FROM reads ORDER BY at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Read
	for rows.Next() {
		var r Read
		var at int64
		if err := rows.Scan(&r.URL, &r.Surface, &r.Plane, &r.Status, &r.Bytes, &at, &r.Error); err != nil {
			return out, err
		}
		r.At = time.Unix(at, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// ─── stats and query ─────────────────────────────────────────────────────────

// StatRow is one line of `arxiv db stats`: which table, which bucket, how many.
type StatRow struct {
	Table string `json:"table" table:"table"`
	Key   string `json:"key" kit:"id" table:"key"`
	Rows  int64  `json:"rows" table:"rows"`
	// Bytes is filled on the reads rows only, because how much a crawl
	// downloaded is the number that says where the time went.
	Bytes int64 `json:"bytes,omitempty" table:"bytes"`
}

// Stats is nodes by kind, claims by predicate, and reads by plane and status,
// which is the fastest way to see that a crawl spent its afternoon on
// arxiv.org and got 429s for it.
func (s *Store) Stats() ([]StatRow, error) {
	var out []StatRow

	rows, err := s.db.Query(`
		SELECT kind, COUNT(*), SUM(CASE WHEN record IS NULL THEN 1 ELSE 0 END)
		FROM nodes GROUP BY kind ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var total, unread int64
		if err := rows.Scan(&kind, &total, &unread); err != nil {
			_ = rows.Close()
			return out, err
		}
		out = append(out, StatRow{Table: "nodes", Key: kind, Rows: total})
		if unread > 0 {
			// The unread count is its own row rather than a column, because it is
			// the frontier and it is the number a crawl is deciding on.
			out = append(out, StatRow{Table: "nodes", Key: kind + " (not read)", Rows: unread})
		}
	}
	if err := closeRows(rows); err != nil {
		return out, err
	}

	rows, err = s.db.Query(`SELECT predicate, COUNT(*) FROM claims GROUP BY predicate ORDER BY COUNT(*) DESC, predicate`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p string
		var n int64
		if err := rows.Scan(&p, &n); err != nil {
			_ = rows.Close()
			return out, err
		}
		out = append(out, StatRow{Table: "claims", Key: p, Rows: n})
	}
	if err := closeRows(rows); err != nil {
		return out, err
	}

	rows, err = s.db.Query(`
		SELECT plane, surface, status, COUNT(*), SUM(bytes)
		FROM reads GROUP BY plane, surface, status ORDER BY COUNT(*) DESC, plane, surface, status`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var plane, surface string
		var status, n, bytes int64
		if err := rows.Scan(&plane, &surface, &status, &n, &bytes); err != nil {
			_ = rows.Close()
			return out, err
		}
		key := strings.TrimSpace(plane + " " + surface)
		out = append(out, StatRow{Table: "reads", Key: fmt.Sprintf("%s %d", key, status), Rows: n, Bytes: bytes})
	}
	return out, closeRows(rows)
}

func closeRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}

// Query runs the caller's SQL and returns the columns and the rows.
//
// There is no query builder and no wrapper on purpose. The schema is three
// tables a person can hold in their head, and the questions worth asking are
// ones nobody would have thought to add a flag for.
func (s *Store) Query(text string) ([]string, [][]any, error) {
	rows, err := s.db.Query(text)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}
