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
	// Supports declares which optional filter columns this Source has.
	Supports SourceCaps
	// ExpectModel is the model name expected on rows (informational only).
	ExpectModel string
}

// Filters narrows the ANN result set.
type Filters struct {
	// Kinds restricts to Sources whose effective kind matches. Empty = all Sources.
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
	// Model is the embedding model name.
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
type PgxPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
}

// Option configures a Store.
type Option func(*Store)

// Store is the multi-source semantic-search query layer.
type Store struct {
	pool     PgxPool
	embedder Embedder
	sources  []Source
}

// New creates a Store. pool and embedder must be non-nil.
func New(pool PgxPool, embedder Embedder, sources []Source, opts ...Option) *Store {
	s := &Store{
		pool:     pool,
		embedder: embedder,
		sources:  sources,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// sourceEffectiveKind returns the kind string used for filtering decisions.
// For Sources with KindColumn (dynamic kind), we use the Source.Name as the
// routing key; for Sources with KindConst, we use KindConst.
func sourceEffectiveKind(src Source) string {
	if src.KindConst != "" {
		return src.KindConst
	}
	// Dynamic kind column: the Source name is used as the routing key.
	return src.Name
}

// sourceIncluded returns true if this Source should be included given the Kinds filter.
func sourceIncluded(src Source, f Filters) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	effective := sourceEffectiveKind(src)
	for _, k := range f.Kinds {
		if k == effective {
			return true
		}
	}
	return false
}

// sourceHits queries a single Source and returns its hits.
// On any error, it logs and returns nil (degrade-never-crash).
func (s *Store) sourceHits(ctx context.Context, src Source, vec []float32, k int, f Filters) []Hit {
	hits, err := s.querySource(ctx, src, vec, k, f)
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

	// Pin HNSW GUC parameters for recall quality.
	if _, err := tx.Exec(ctx, `SET LOCAL hnsw.ef_search = 40`); err != nil {
		return nil, fmt.Errorf("SET LOCAL hnsw.ef_search: %w", err)
	}
	// iterative_scan requires pgvector >= 0.8; swallow "unrecognized parameter" errors.
	if _, err := tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan = 'strict_order'`); err != nil {
		log.Printf("semantic: SET LOCAL hnsw.iterative_scan not supported (pgvector <0.8): %v", err)
	}

	sql, args := buildSQL(src, vec, k, f)
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	model := src.ExpectModel
	if model == "" {
		model = "multilingual-e5-large"
	}

	var hits []Hit
	for rows.Next() {
		h := Hit{Source: src.Name, Model: model}

		// Build scan targets based on what columns we selected.
		// Order: id, [kind,] [lang,] score
		var scanArgs []interface{}
		scanArgs = append(scanArgs, &h.ID)
		if src.KindColumn != "" {
			scanArgs = append(scanArgs, &h.Kind)
		} else {
			// Kind from constant — set after scan.
		}
		if src.LangColumn != "" {
			scanArgs = append(scanArgs, &h.Lang)
		}
		scanArgs = append(scanArgs, &h.Score)

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		// Fill in constant kind if no kind column.
		if src.KindColumn == "" {
			h.Kind = src.KindConst
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

	// SELECT clause: id, [kind,] [lang,] score
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
	sb.WriteString(", 1 - (")
	sb.WriteString(src.VecColumn)
	sb.WriteString(" <=> ")
	sb.WriteString(vecParam)
	sb.WriteString(") AS score")

	sb.WriteString(" FROM ")
	sb.WriteString(src.Table)

	sb.WriteString(" WHERE 1=1")

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
// Sources are queried concurrently; partial failures are logged and skipped
// (degrade-never-crash). Results are merged by Score descending and the global
// top-k are returned.
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

	// Sort by Score descending.
	sort.Slice(all, func(i, j int) bool {
		return all[i].Score > all[j].Score
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
// Search with ExcludeID=id. Returns nil, nil if the id is not found (degrade-never-crash).
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
		return nil, nil
	}

	// Fetch the stored vector for this id.
	vec, err := s.fetchVector(ctx, src, id)
	if err != nil {
		log.Printf("semantic: Related: fetch vector for id=%d source=%q: %v", id, sourceName, err)
		return nil, nil
	}
	if vec == nil {
		return nil, nil
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
