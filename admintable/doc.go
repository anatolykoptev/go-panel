// Package admintable provides SQL-injection-safe sort and filter primitives
// for server-rendered admin tables.
//
// Spec defines sortable columns and resolves safe ORDER BY state from URL params.
// FilterSpec defines filterable dimensions and composes safe WHERE fragments with
// positional bind args. Both ensure URL bytes never reach SQL — only
// author-declared compile-time SQLExpr values and literal operators do.
//
// go-panel/resource composes these to build the full list handler.
// The same primitives can be used independently in hand-written handlers.
package admintable
