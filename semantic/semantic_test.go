package semantic_test

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/go-panel/semantic"
)

// testDSN returns the test DSN or empty string.
func testDSN() string {
	return os.Getenv("SEMANTIC_TEST_DSN")
}

// ptrBool returns a pointer to a bool literal; used in filter tests.
func ptrBool(b bool) *bool { return &b }

// fakeEmbedder captures Embed calls for inspection.
type fakeEmbedder struct {
	mu     sync.Mutex
	inputs []string
	vec    []float32 // fixed vec to return
}

func newFakeEmbedder(dim int) *fakeEmbedder {
	v := make([]float32, dim)
	// unit-normalise: all 1/sqrt(dim)
	val := float32(1.0 / math.Sqrt(float64(dim)))
	for i := range v {
		v[i] = val
	}
	return &fakeEmbedder{vec: v}
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, texts...)
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = f.vec
	}
	return out, nil
}

func (f *fakeEmbedder) LastInputs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.inputs...)
}

// openPool opens a pgxpool; skips the test if DSN is not set.
func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("SEMANTIC_TEST_DSN not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// setupTables creates the test tables, dropping them first.
func setupTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS sem_test_content_vectors;
		CREATE TABLE sem_test_content_vectors (
			id        BIGINT PRIMARY KEY,
			kind      TEXT    NOT NULL,
			lang      TEXT    NOT NULL,
			is_podborka BOOL  NOT NULL DEFAULT false,
			segment   TEXT    NOT NULL DEFAULT '',
			vec       vector(1024) NOT NULL
		);
		DROP TABLE IF EXISTS sem_test_place_vectors;
		CREATE TABLE sem_test_place_vectors (
			id  BIGINT PRIMARY KEY,
			vec vector(1024) NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DROP TABLE IF EXISTS sem_test_content_vectors;
			DROP TABLE IF EXISTS sem_test_place_vectors;
		`)
	})
}

// unitVec builds a unit vector of the given dimension.
func unitVec(dim int) []float32 {
	v := make([]float32, dim)
	val := float32(1.0 / math.Sqrt(float64(dim)))
	for i := range v {
		v[i] = val
	}
	return v
}

// insertContent inserts a row into sem_test_content_vectors.
func insertContent(t *testing.T, pool *pgxpool.Pool, id int64, kind, lang string, isPodborka bool, segment string) {
	t.Helper()
	ctx := context.Background()
	vec := unitVec(1024)
	_, err := pool.Exec(ctx,
		`INSERT INTO sem_test_content_vectors(id, kind, lang, is_podborka, segment, vec)
		 VALUES ($1, $2, $3, $4, $5, $6::vector)`,
		id, kind, lang, isPodborka, segment, semantic.VectorLiteral(vec))
	if err != nil {
		t.Fatalf("insertContent(%d): %v", id, err)
	}
}

// insertPlace inserts a row into sem_test_place_vectors.
func insertPlace(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()
	ctx := context.Background()
	vec := unitVec(1024)
	_, err := pool.Exec(ctx,
		`INSERT INTO sem_test_place_vectors(id, vec) VALUES ($1, $2::vector)`,
		id, semantic.VectorLiteral(vec))
	if err != nil {
		t.Fatalf("insertPlace(%d): %v", id, err)
	}
}

// defaultSources returns the two test sources.
func defaultSources() []semantic.Source {
	return []semantic.Source{
		{
			Name:           "content",
			Table:          "sem_test_content_vectors",
			IDColumn:       "id",
			VecColumn:      "vec",
			KindColumn:     "kind",
			LangColumn:     "lang",
			PodborkaColumn: "is_podborka",
			SegmentColumn:  "segment",
			ExpectModel:    "multilingual-e5-large",
		},
		{
			Name:        "place",
			Table:       "sem_test_place_vectors",
			IDColumn:    "id",
			VecColumn:   "vec",
			KindConst:   "place",
			ExpectModel: "multilingual-e5-large",
		},
	}
}

// TestSearch_MultiSource_FanOut: no kind filter -> hits from BOTH tables, merged by score.
func TestSearch_MultiSource_FanOut(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertContent(t, pool, 2, "collection", "ru", false, "")
	insertPlace(t, pool, 101)
	insertPlace(t, pool, 102)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits, got none")
	}

	hasContent, hasPlace := false, false
	for _, h := range hits {
		if h.Source == "content" {
			hasContent = true
		}
		if h.Source == "place" {
			hasPlace = true
		}
	}
	if !hasContent {
		t.Error("expected at least one content hit")
	}
	if !hasPlace {
		t.Error("expected at least one place hit")
	}
}

// TestSearch_KindRouting_PlaceOnly: Filters.Kinds=["place"] returns only place hits.
func TestSearch_KindRouting_PlaceOnly(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertPlace(t, pool, 101)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{Kinds: []string{"place"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.Source != "place" {
			t.Errorf("expected only place hits, got source=%q", h.Source)
		}
	}
	if len(hits) == 0 {
		t.Error("expected place hits, got none")
	}
}

// TestSearch_KindRouting_ContentOnly: Filters.Kinds=["collection"] returns only rows whose
// stored kind = "collection" (row-level filter for KindColumn Sources). The place Source
// (KindConst="place") is skipped at source level since "place" not in ["collection"].
func TestSearch_KindRouting_ContentOnly(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertPlace(t, pool, 101)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	// Filter by row kind "collection": content source applies row-level WHERE,
	// place source is excluded by KindConst source-level check.
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{Kinds: []string{"collection"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.Source != "content" {
			t.Errorf("expected only content hits, got source=%q", h.Source)
		}
	}
	if len(hits) == 0 {
		t.Error("expected content hits (kind=collection), got none")
	}
}

// TestSearch_LangFilter_NoExcludePlaceSource: lang filter narrows content but does NOT
// exclude the place Source (which has no lang column).
func TestSearch_LangFilter_NoExcludePlaceSource(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertContent(t, pool, 2, "collection", "en", false, "")
	insertPlace(t, pool, 101)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{Lang: "ru"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	hasPlace := false
	hasEnContent := false
	for _, h := range hits {
		if h.Source == "place" {
			hasPlace = true
		}
		if h.Source == "content" && h.Lang == "en" {
			hasEnContent = true
		}
	}
	if !hasPlace {
		t.Error("lang filter should NOT exclude place source (no lang column)")
	}
	if hasEnContent {
		t.Error("lang=ru filter should exclude lang=en content rows")
	}
}

// TestSearch_PartialDegrade: if content table is dropped, Search returns place hits + no crash.
func TestSearch_PartialDegrade(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertPlace(t, pool, 101)

	// Drop content table to trigger Source failure.
	_, err := pool.Exec(context.Background(), `DROP TABLE sem_test_content_vectors`)
	if err != nil {
		t.Fatalf("drop table: %v", err)
	}

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{})
	// Must NOT error (degrade-never-crash).
	if err != nil {
		t.Fatalf("Search must not error on partial failure: %v", err)
	}

	hasPlace := false
	for _, h := range hits {
		if h.Source == "place" {
			hasPlace = true
		}
	}
	if !hasPlace {
		t.Error("expected place hits to survive despite content source failure")
	}
}

// TestRelated_ExcludesSelf: Related excludes the queried id from results.
func TestRelated_ExcludesSelf(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertContent(t, pool, 2, "collection", "ru", false, "")

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Related(ctx, 1, "content", semantic.Filters{}, 10)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	for _, h := range hits {
		if h.ID == 1 {
			t.Error("Related should exclude self (id=1)")
		}
	}
}

// TestSemanticSearch_PrefixQuery: SemanticSearch prepends "query: " to the input text.
func TestSemanticSearch_PrefixQuery(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	_, _ = store.SemanticSearch(ctx, "рестораны", 5, semantic.Filters{})

	inputs := emb.LastInputs()
	if len(inputs) == 0 {
		t.Fatal("embedder was not called")
	}
	if !strings.HasPrefix(inputs[0], "query: ") {
		t.Errorf("expected 'query: ' prefix, got %q", inputs[0])
	}
	if inputs[0] != "query: рестораны" {
		t.Errorf("expected 'query: рестораны', got %q", inputs[0])
	}
}

// TestScore_Range: scores are in [-1, 1].
func TestScore_Range(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertContent(t, pool, 2, "collection", "en", false, "")
	insertPlace(t, pool, 101)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.Score < -1.0 || h.Score > 1.0 {
			t.Errorf("score %f out of range [-1, 1] for hit id=%d", h.Score, h.ID)
		}
	}
}

// TestVectorLiteral: unit test for VectorLiteral formatting.
func TestVectorLiteral(t *testing.T) {
	v := []float32{1.5, -2.25, 0.0}
	s := semantic.VectorLiteral(v)
	if s != "[1.5,-2.25,0]" {
		t.Errorf("VectorLiteral(%v) = %q, want [1.5,-2.25,0]", v, s)
	}
	// Empty slice.
	s2 := semantic.VectorLiteral(nil)
	if s2 != "[]" {
		t.Errorf("VectorLiteral(nil) = %q, want []", s2)
	}
}

// =============================================================================
// NEW TESTS -- written FIRST (RED phase), implementation follows
// =============================================================================

// panicQueryTx wraps a real pgx.Tx but panics when Query is called for a
// specific table. This simulates an unrecovered panic in a Source goroutine.
type panicQueryTx struct {
	pgx.Tx
	panicTable string
}

func (t *panicQueryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, t.panicTable) {
		panic("simulated panic querying " + t.panicTable)
	}
	return t.Tx.Query(ctx, sql, args...)
}

// panicBeginPool wraps a pgxpool.Pool; Begin returns a panicQueryTx for
// transactions that will query panicTable, and a normal tx for others.
// Since we cannot tell at Begin time which table a tx will query, we always
// wrap -- the tx itself decides whether to panic based on the SQL it sees.
type panicBeginPool struct {
	real       *pgxpool.Pool
	panicTable string
}

func (p *panicBeginPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.real.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &panicQueryTx{Tx: tx, panicTable: p.panicTable}, nil
}

func (p *panicBeginPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.real.Query(ctx, sql, args...)
}

// TestSearch_PanicRecovery verifies that a panicking Source goroutine is caught
// (degrade-never-crash), and the healthy Source's hits survive.
//
// RED evidence: without per-goroutine recover(), the panic propagates through
// the goroutine scheduler and crashes the test process.
// GREEN: with defer/recover in each source goroutine, the panic is caught and
// logged; the place source returns its hits normally.
func TestSearch_PanicRecovery(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)
	insertPlace(t, pool, 101)
	// No content rows -- but content table exists, panic fires on SELECT.

	emb := newFakeEmbedder(1024)

	// panicBeginPool panics when content table is queried.
	pp := &panicBeginPool{real: pool, panicTable: "sem_test_content_vectors"}
	store := semantic.New(pp, emb, defaultSources())

	ctx := context.Background()
	// MUST NOT panic the test process.
	// MUST return place hits.
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{})
	if err != nil {
		t.Fatalf("Search must not error after source panic (degrade-never-crash): %v", err)
	}

	hasPlace := false
	for _, h := range hits {
		if h.Source == "place" {
			hasPlace = true
		}
	}
	if !hasPlace {
		t.Error("place hits must survive despite content source panic")
	}
}

// TestHitModel_FromRow verifies that when Source.ModelColumn is set, the
// returned Hit.Model equals the STORED row model, not src.ExpectModel.
//
// RED evidence: ModelColumn field does not exist on Source -- compile error.
// GREEN: ModelColumn is added, SELECT'd, and scanned into Hit.Model.
func TestHitModel_FromRow(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS sem_test_model_vectors;
		CREATE TABLE sem_test_model_vectors (
			id    BIGINT PRIMARY KEY,
			model TEXT NOT NULL,
			vec   vector(1024) NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS sem_test_model_vectors`)
	})

	const storedModel = "bge-m3"
	const expectModel = "multilingual-e5-large"

	_, err = pool.Exec(ctx,
		`INSERT INTO sem_test_model_vectors(id, model, vec) VALUES ($1, $2, $3::vector)`,
		999, storedModel, semantic.VectorLiteral(unitVec(1024)))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	src := semantic.Source{
		Name:        "model_test",
		Table:       "sem_test_model_vectors",
		IDColumn:    "id",
		VecColumn:   "vec",
		KindConst:   "article",
		ExpectModel: expectModel,
		ModelColumn: "model", // NEW FIELD -- causes compile error until implemented
	}

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, []semantic.Source{src})

	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	for _, h := range hits {
		if h.Model != storedModel {
			t.Errorf("Hit.Model = %q, want stored model %q (ExpectModel=%q was wrong value)",
				h.Model, storedModel, expectModel)
		}
	}
}

// TestSearch_KindColumn_RowFilter verifies that for a Source WITH KindColumn,
// Filters.Kinds narrows at row level (WHERE kind = ANY($kinds)), not source level.
//
// RED evidence: current code uses sourceEffectiveKind returning src.Name="content"
// for KindColumn sources, which never matches "article" -- the source is skipped
// entirely; no row-level filter is applied.
// GREEN: buildSQL emits row-level WHERE clause for KindColumn sources.
func TestSearch_KindColumn_RowFilter(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	// Insert one article and one video in the same KindColumn source.
	insertContent(t, pool, 10, "article", "ru", false, "")
	insertContent(t, pool, 11, "video", "ru", false, "")

	emb := newFakeEmbedder(1024)
	// Single source with KindColumn="kind" covering both article and video.
	sources := []semantic.Source{
		{
			Name:        "content",
			Table:       "sem_test_content_vectors",
			IDColumn:    "id",
			VecColumn:   "vec",
			KindColumn:  "kind",
			LangColumn:  "lang",
			ExpectModel: "multilingual-e5-large",
		},
	}
	store := semantic.New(pool, emb, sources)

	ctx := context.Background()
	// Filter by kind=article -> only the article row (id=10) should appear.
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{Kinds: []string{"article"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected article hits, got none")
	}
	for _, h := range hits {
		if h.Kind != "article" {
			t.Errorf("expected only kind=article, got kind=%q (id=%d)", h.Kind, h.ID)
		}
	}
}

// TestRelated_NotFound_NonNilSlice verifies that Related returns []Hit{} (not nil)
// when the id is absent from the Source -- matching Search's non-nil-slice contract.
//
// RED evidence: Related returns (nil, nil) on not-found.
// GREEN: Related returns ([]Hit{}, nil) on not-found.
func TestRelated_NotFound_NonNilSlice(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)
	// No rows inserted.

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Related(ctx, 9999, "content", semantic.Filters{}, 10)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if hits == nil {
		t.Error("Related returned nil slice on not-found, want []Hit{} (non-nil empty slice)")
	}
}

// TestSearch_FilterPodborka verifies that Filters.IsPodborka drives live row
// filtering through the real Store.Search code path.
//
// This is an end-to-end integration test: it exercises the SHIPPED Store.Search
// function (not a hand-copied query string), confirms that the PodborkaColumn
// configured in the Source actually filters rows in the real pgvector DB.
//
// RED sanity (falsification): point the content Source's PodborkaColumn at a
// non-existent column ("_bad_col") -> Store.Search degrades (SQL error) and
// returns no hits that match the filter, so the final assertion fails.
// GREEN: restore PodborkaColumn="is_podborka" -> correct rows returned.
func TestSearch_FilterPodborka(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	// Insert 3 podborka rows and 2 non-podborka rows.
	insertContent(t, pool, 201, "collection", "ru", true, "")
	insertContent(t, pool, 202, "collection", "ru", true, "")
	insertContent(t, pool, 203, "collection", "ru", true, "")
	insertContent(t, pool, 204, "collection", "ru", false, "")
	insertContent(t, pool, 205, "collection", "ru", false, "")

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())
	ctx := context.Background()

	// Filter: IsPodborka=true -> expect only rows 201, 202, 203.
	trueHits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{
		IsPodborka: ptrBool(true),
		Kinds:      []string{"collection"},
	})
	if err != nil {
		t.Fatalf("Search(IsPodborka=true): %v", err)
	}
	if len(trueHits) == 0 {
		t.Fatal("Search(IsPodborka=true): expected hits, got none")
	}
	for _, h := range trueHits {
		if h.Source != "content" {
			continue // place source has no is_podborka column; skip those hits
		}
		if h.ID != 201 && h.ID != 202 && h.ID != 203 {
			t.Errorf("Search(IsPodborka=true): unexpected hit id=%d (expected only 201/202/203)", h.ID)
		}
	}

	// Filter: IsPodborka=false -> expect only rows 204, 205.
	falseHits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{
		IsPodborka: ptrBool(false),
		Kinds:      []string{"collection"},
	})
	if err != nil {
		t.Fatalf("Search(IsPodborka=false): %v", err)
	}
	if len(falseHits) == 0 {
		t.Fatal("Search(IsPodborka=false): expected hits, got none")
	}
	for _, h := range falseHits {
		if h.Source != "content" {
			continue
		}
		if h.ID != 204 && h.ID != 205 {
			t.Errorf("Search(IsPodborka=false): unexpected hit id=%d (expected only 204/205)", h.ID)
		}
	}
}

// TestSearch_FilterSegment verifies that Filters.Segment drives live row
// filtering through the real Store.Search code path.
//
// This is an end-to-end integration test: it exercises the SHIPPED Store.Search
// function (not a hand-copied query string), confirms that the SegmentColumn
// configured in the Source actually filters rows in the real pgvector DB.
//
// RED sanity (falsification): point the content Source's SegmentColumn at a
// non-existent column ("_bad_col") -> Store.Search degrades (SQL error) and
// returns no hits, so the assertion on len(hits)==0 fires.
// GREEN: restore SegmentColumn="segment" -> only segment-"a" rows returned.
func TestSearch_FilterSegment(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	// Insert 3 rows with segment "a" and 2 with segment "b".
	insertContent(t, pool, 301, "collection", "ru", false, "a")
	insertContent(t, pool, 302, "collection", "ru", false, "a")
	insertContent(t, pool, 303, "collection", "ru", false, "a")
	insertContent(t, pool, 304, "collection", "ru", false, "b")
	insertContent(t, pool, 305, "collection", "ru", false, "b")

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())
	ctx := context.Background()

	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{
		Segment: "a",
		Kinds:   []string{"collection"},
	})
	if err != nil {
		t.Fatalf("Search(Segment=a): %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search(Segment=a): expected hits, got none")
	}
	for _, h := range hits {
		if h.Source != "content" {
			continue // place source has no segment column
		}
		if h.ID != 301 && h.ID != 302 && h.ID != 303 {
			t.Errorf("Search(Segment=a): unexpected hit id=%d (expected only 301/302/303)", h.ID)
		}
	}
}
