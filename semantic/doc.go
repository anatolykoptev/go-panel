// Package semantic provides a multi-source pgvector semantic-search layer.
//
// It fans out ANN queries over multiple declared Sources (e.g. content_vectors
// and feed_place_vectors), merges the results by cosine-similarity score, and
// returns a global top-k hit list. The package never crashes on partial Source
// failure — degraded Sources are logged and skipped.
//
// Typical usage:
//
//	store := semantic.New(pool, embedder, []semantic.Source{contentSource, placeSource})
//	hits, err := store.SemanticSearch(ctx, "рестораны", 20, semantic.Filters{})
package semantic
