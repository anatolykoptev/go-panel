package semantic_test

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/go-panel/semantic"
)

// testDSN returns the test DSN or empty string.
func testDSN() string {
	return os.Getenv("SEMANTIC_TEST_DSN")
}

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
		pool.Exec(context.Background(), `
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
			Name:       "content",
			Table:      "sem_test_content_vectors",
			IDColumn:   "id",
			VecColumn:  "vec",
			KindColumn: "kind",
			LangColumn: "lang",
			Supports: semantic.SourceCaps{
				Kind:     true,
				Lang:     true,
				Podborka: true,
				Segment:  true,
			},
			ExpectModel: "multilingual-e5-large",
		},
		{
			Name:        "place",
			Table:       "sem_test_place_vectors",
			IDColumn:    "id",
			VecColumn:   "vec",
			KindConst:   "place",
			Supports:    semantic.SourceCaps{},
			ExpectModel: "multilingual-e5-large",
		},
	}
}

// TestSearch_MultiSource_FanOut: no kind filter → hits from BOTH tables, merged by score.
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

// TestSearch_KindRouting_ContentOnly: Filters.Kinds=["content"] returns only content hits.
func TestSearch_KindRouting_ContentOnly(t *testing.T) {
	pool := openPool(t)
	setupTables(t, pool)

	insertContent(t, pool, 1, "collection", "ru", false, "")
	insertPlace(t, pool, 101)

	emb := newFakeEmbedder(1024)
	store := semantic.New(pool, emb, defaultSources())

	ctx := context.Background()
	hits, err := store.Search(ctx, unitVec(1024), 10, semantic.Filters{Kinds: []string{"content"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.Source != "content" {
			t.Errorf("expected only content hits, got source=%q", h.Source)
		}
	}
	if len(hits) == 0 {
		t.Error("expected content hits, got none")
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
