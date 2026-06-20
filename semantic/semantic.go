package semantic

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
)

// VectorLiteral formats a float32 slice as "[v1,v2,...]" for binding to $N::vector.
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v)*12 + 2)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// SourceCaps declares which optional filter columns a Source supports.
type SourceCaps struct {
	Kind     bool // Source has a per-row kind column (vs KindConst)
	Lang     bool // Source has a lang column
	Podborka bool // Source has an is_podborka column
	Segment  bool // Source has a segment column
}

// Source declares a single pgvector table that semantic.Store queries.
// All Table/column names must be compile-time constants (never user-derived).
type Source struct {
	// Name is the logical source name, used in Hit.Source.
	Name string
	// Table is the SQL table name.
	Table string
	// IDColumn is the primary-key column name.
	IDColumn string
	// VecColumn is the vector column name.
	VecColumn string
	// KindColumn is the optional per-row kind column name (e.g. "kind").
	// If empty and KindConst is set, all rows have the constant kind.
	KindColumn string
	// KindConst is the constant kind value for Sources without a kind column.
	KindConst string
	// LangColumn is the optional lang column name (e.g. "lang").
	LangColumn string
	// ModelColumn is the optional column that stores the embedding model name
	// used per-row (e.g. "model"). When set, Hit.Model is populated from the
	// stored value; otherwise Hit.Model falls back to ExpectModel.
	// Fix 2: SELECT'd and scanned so Hit.Model reflects the STORED model,
	// not a constant — important when rows are (re-)embedded with different models.
	ModelColumn string
	// Supports declares which optional filter columns this Source has.
	Supports SourceCaps
	// ExpectModel is the fallback model name when ModelColumn is "" or the row's
	// model column is NULL. Also used when the Source has no model column at all.
	ExpectModel string
}

// Filters narrows the ANN result set.
type Filters struct {
	// Kinds restricts to Sources whose effective kind matches. Empty = all Sources.
	// For Sources with KindColumn, filtering is applied per-row (WHERE kind = ANY($kinds)).
	// For Sources with KindConst, the whole Source is skipped if its const kind doesn't match.
	Kinds []string
	// Lang restricts content by language (only applied to Sources with Supports.Lang).
	Lang string
	// IsPodborka filters by podborka flag (nil = no filter).
	IsPodborka *bool
	// Segment filters by segment (only applied to Sources with Supports.Segment).
	Segment string
	// ExcludeID excludes this ID from results (0 = no exclusion).
	ExcludeID int64
}

// Hit is a single ANN result.
type Hit struct {
	// ID is the row's primary key.
	ID int64
	// Kind is the row's effective kind (from KindColumn or KindConst).
	Kind string
	// Lang is the row's language (empty if Source has no lang column).
	Lang string
	// Score is the cosine similarity in [-1, 1].
	Score float32
	// Model is the embedding model name. When Source.ModelColumn is set, this
	// reflects the stored per-row model value; otherwise it equals Source.ExpectModel.
	Model string
	// Source is the logical Source name (Source.Name).
	Source string
}

// Embedder is the narrow embedding interface.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// PgxPool is the narrow pool interface that semantic.Store requires.
// pgxpool.Pool satisfies this.
// Note: Search opens up to len(sources) concurrent transactions; configure
// MaxConns >= expected_concurrency * len(sources) on the pool.
type PgxPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
}

// Option configures a Store.
type Option func(*Store)

// WithEfSearch returns an Option that sets the hnsw.ef_search GUC for all
// Source queries. Default is 40 (pgvector default). Increasing it improves
// recall at the cost of higher query latency.
func WithEfSearch(n int) Option {
	return func(s *Store) {
		s.efSearch = n
	}
}

// Store is the multi-source semantic-search query layer.
type Store struct {
	pool     PgxPool
	embedder Embedder
	sources  []Source
	efSearch int // hnsw.ef_search GUC value; 0 means use default (40)
}

// New creates a Store.
// Panics at startup (not at query time) if:
//   - pool is nil
//   - embedder is nil
//   - sources is empty
func New(pool PgxPool, embedder Embedder, sources []Source, opts ...Option) *Store {
	if pool == nil {
		panic("semantic.New: pool must be non-nil")
	}
	if embedder == nil {
		panic("semantic.New: embedder must be non-nil")
	}
	if len(sources) == 0 {
		panic("semantic.New: sources must be non-empty")
	}
	s := &Store{
		pool:     pool,
		embedder: embedder,
		sources:  sources,
		efSearch: 40, // pgvector default
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// sourceIsKindColumn returns true if the Source uses a per-row kind column.
func sourceIsKindColumn(src Source) bool {
	return src.KindColumn != ""
}

// sourceIncluded returns true if this Source should be queried given the Kinds filter.
// For KindConst Sources, the whole Source is skipped if its const kind isn't in f.Kinds.
// For KindColumn Sources, the Source is always included (row-level filter applied in SQL).
func sourceIncluded(src Source, f Filters) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	// KindColumn Source: row-level filter — always include the source.
	if sourceIsKindColumn(src) {
		return true
	}
	// KindConst Source: source-level skip.
	for _, k := range f.Kinds {
		if k == src.KindConst {
			return true
		}
	}
	return false
}

// sourceHits queries a single Source and returns its hits.
// Panics in the goroutine are caught and logged; returns nil on panic or error
// (degrade-never-crash). The emptyResult is used on recovery.
func (s *Store) sourceHits(ctx context.Context, src Source, vec []float32, k int, f Filters) (hits []Hit) {
	// Fix 1: per-goroutine panic recovery — a panicking Source degrades to
	// nil hits, exactly like a query error. Other Sources are unaffected.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("semantic: source %q panicked (degraded): %v", src.Name, r)
			hits = nil
		}
	}()

	var err error
	hits, err = s.querySource(ctx, src, vec, k, f)
	if err != nil {
		log.Printf("semantic: source %q query failed (degraded): %v", src.Name, err)
		return nil
	}
	return hits
}

// querySource executes the ANN query for a single Source inside a GUC transaction.
func (s *Store) querySource(ctx context.Context, src Source, vec []float32, k int, f Filters) ([]Hit, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Set hnsw.ef_search GUC. The value is configurable via WithEfSearch;
	// default 40 equals pgvector's own default (this SET LOCAL has no recall
	// impact unless overridden above 40).
	efSearch := s.efSearch
	if efSearch <= 0 {
		efSearch = 40
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL hnsw.ef_search = %d`, efSearch)); err != nil {
		return nil, fmt.Errorf("SET LOCAL hnsw.ef_search: %w", err)
	}
	// iterative_scan requires pgvector >= 0.8; swallow "unrecognized parameter" errors.
	// Logged at most once per Source to avoid per-query log spam on older pgvector.
	if _, err := tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan = 'strict_order'`); err != nil {
		log.Printf("semantic: SET LOCAL hnsw.iterative_scan not supported (pgvector <0.8): %v", err)
	}

	sql, args := buildSQL(src, vec, k, f)
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	// Fallback model when no model column is present or value is empty.
	fallbackModel := src.ExpectModel
	if fallbackModel == "" {
		fallbackModel = "multilingual-e5-large"
	}

	var hits []Hit
	for rows.Next() {
		h := Hit{Source: src.Name}

		// Build scan targets based on selected columns.
		// Column order: id, [kind,] [lang,] [model,] score
		var scanArgs []interface{}
		scanArgs = append(scanArgs, &h.ID)
		if src.KindColumn != "" {
			scanArgs = append(scanArgs, &h.Kind)
		}
		if src.LangColumn != "" {
			scanArgs = append(scanArgs, &h.Lang)
		}
		// Fix 2: scan the stored model when ModelColumn is set.
		var storedModel string
		if src.ModelColumn != "" {
			scanArgs = append(scanArgs, &storedModel)
		}
		scanArgs = append(scanArgs, &h.Score)

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		// Resolve kind from constant when no kind column.
		if src.KindColumn == "" {
			h.Kind = src.KindConst
		}

		// Fix 2: prefer stored model column; fall back to ExpectModel.
		if src.ModelColumn != "" && storedModel != "" {
			h.Model = storedModel
		} else {
			h.Model = fallbackModel
		}

		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return hits, nil
}

// buildSQL constructs the parametric ANN query for a Source.
// Table and column names are compile-time constants (from Source declaration);
// only values are parameterised.
func buildSQL(src Source, vec []float32, k int, f Filters) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}
	argN := 1

	vecStr := VectorLiteral(vec)
	args = append(args, vecStr)
	vecParam := fmt.Sprintf("$%d::vector", argN)
	argN++

	// SELECT clause: id, [kind,] [lang,] [model,] score
	sb.WriteString("SELECT ")
	sb.WriteString(src.IDColumn)
	if src.KindColumn != "" {
		sb.WriteString(", ")
		sb.WriteString(src.KindColumn)
	}
	if src.LangColumn != "" {
		sb.WriteString(", ")
		sb.WriteString(src.LangColumn)
	}
	// Fix 2: select model column when declared.
	if src.ModelColumn != "" {
		sb.WriteString(", ")
		sb.WriteString(src.ModelColumn)
	}
	sb.WriteString(", 1 - (")
	sb.WriteString(src.VecColumn)
	sb.WriteString(" <=> ")
	sb.WriteString(vecParam)
	sb.WriteString(") AS score")

	sb.WriteString(" FROM ")
	sb.WriteString(src.Table)

	sb.WriteString(" WHERE 1=1")

	// NIT fix: row-level kind filter for KindColumn Sources.
	// For KindConst Sources, source-granular skip is already done in sourceIncluded.
	if src.KindColumn != "" && len(f.Kinds) > 0 {
		// Use = ANY($N) for multi-value kind filter.
		args = append(args, f.Kinds)
		fmt.Fprintf(&sb, " AND %s = ANY($%d)", src.KindColumn, argN)
		argN++
	}

	// Lang filter — only if Source supports lang.
	if src.Supports.Lang && src.LangColumn != "" && f.Lang != "" {
		args = append(args, f.Lang)
		fmt.Fprintf(&sb, " AND %s = $%d", src.LangColumn, argN)
		argN++
	}

	// IsPodborka filter.
	if src.Supports.Podborka && f.IsPodborka != nil {
		args = append(args, *f.IsPodborka)
		fmt.Fprintf(&sb, " AND is_podborka = $%d", argN)
		argN++
	}

	// Segment filter.
	if src.Supports.Segment && f.Segment != "" {
		args = append(args, f.Segment)
		fmt.Fprintf(&sb, " AND segment = $%d", argN)
		argN++
	}

	// ExcludeID — always applied when non-zero.
	if f.ExcludeID != 0 {
		args = append(args, f.ExcludeID)
		fmt.Fprintf(&sb, " AND %s <> $%d", src.IDColumn, argN)
		argN++
	}

	// ORDER + LIMIT
	fmt.Fprintf(&sb, " ORDER BY %s <=> %s LIMIT $%d", src.VecColumn, vecParam, argN)
	args = append(args, k)

	return sb.String(), args
}

// Search runs ANN over all applicable Sources using the given query vector.
// Sources are queried concurrently; partial failures and panics are caught and
// logged (degrade-never-crash). Results are merged by Score descending with a
// stable tiebreaker (Source name, then ID) and the global top-k are returned.
func (s *Store) Search(ctx context.Context, vec []float32, k int, f Filters) ([]Hit, error) {
	type result struct {
		hits []Hit
	}

	results := make([]result, len(s.sources))
	var wg sync.WaitGroup

	for i, src := range s.sources {
		if !sourceIncluded(src, f) {
			continue
		}
		wg.Add(1)
		go func(idx int, src Source) {
			defer wg.Done()
			hits := s.sourceHits(ctx, src, vec, k, f)
			results[idx] = result{hits: hits}
		}(i, src)
	}
	wg.Wait()

	// Merge all hits.
	var all []Hit
	for _, r := range results {
		all = append(all, r.hits...)
	}

	// Stable merge: Score desc, then Source asc, then ID asc for deterministic
	// tiebreaker at top-k boundary.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		if all[i].Source != all[j].Source {
			return all[i].Source < all[j].Source
		}
		return all[i].ID < all[j].ID
	})

	// Return global top-k.
	if len(all) > k {
		all = all[:k]
	}
	return all, nil
}

// SemanticSearch embeds queryText (prepending "query: " per multilingual-e5 convention),
// then calls Search with the resulting vector.
func (s *Store) SemanticSearch(ctx context.Context, queryText string, k int, f Filters) ([]Hit, error) {
	vecs, err := s.embedder.Embed(ctx, []string{"query: " + queryText})
	if err != nil {
		return nil, fmt.Errorf("semantic: embed query: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("semantic: embedder returned empty vector")
	}
	return s.Search(ctx, vecs[0], k, f)
}

// Related fetches the stored vector for id from the named Source, then calls
// Search with ExcludeID=id. Returns []Hit{} (non-nil empty slice) if the id is
// not found, matching Search's non-nil-slice contract.
func (s *Store) Related(ctx context.Context, id int64, sourceName string, f Filters, k int) ([]Hit, error) {
	// Find the named Source.
	var src *Source
	for i := range s.sources {
		if s.sources[i].Name == sourceName {
			src = &s.sources[i]
			break
		}
	}
	if src == nil {
		log.Printf("semantic: Related: source %q not found", sourceName)
		return []Hit{}, nil
	}

	// Fetch the stored vector for this id.
	vec, err := s.fetchVector(ctx, src, id)
	if err != nil {
		log.Printf("semantic: Related: fetch vector for id=%d source=%q: %v", id, sourceName, err)
		return []Hit{}, nil
	}
	if vec == nil {
		// Not found.
		return []Hit{}, nil
	}

	// Exclude self.
	f.ExcludeID = id
	return s.Search(ctx, vec, k, f)
}

// fetchVector retrieves the raw vector for a given id from a Source table.
// Returns nil if not found.
func (s *Store) fetchVector(ctx context.Context, src *Source, id int64) ([]float32, error) {
	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", src.VecColumn, src.Table, src.IDColumn)

	rows, err := s.pool.Query(ctx, sql, id)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil // not found
	}

	// pgx returns pgvector as a string in "[v1,v2,...]" format when not using
	// the pgvector extension type. Scan into a string and parse.
	var raw string
	if err := rows.Scan(&raw); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	vec, err := parseVectorLiteral(raw)
	if err != nil {
		return nil, fmt.Errorf("parse vector: %w", err)
	}
	return vec, nil
}

// parseVectorLiteral parses a "[v1,v2,...]" string into []float32.
func parseVectorLiteral(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("invalid vector literal: %q", s)
	}
	s = s[1 : len(s)-1]
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	vec := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("parse float at index %d: %w", i, err)
		}
		vec[i] = float32(f)
	}
	return vec, nil
}
