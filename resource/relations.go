// Package resource — relations.go implements declarative BelongsTo
// cross-resource linking (Phase 2 P1 core, issue #101).
//
// A Relation declares that one resource's cell holds a foreign key into
// another resource and asks go-panel to replace that raw FK with an
// XSS-safe CrossLinkCell anchor pointing at the target resource's detail
// route. Resolution is batch-first: a ResolveLabels closure (the PRIMARY
// recommended path) resolves all FK ids in one call, avoiding an N+1 DoS
// (ADR-2). A FetchRow fallback exists for resources that cannot supply a
// batch closure, but it is gated: only when the target Resource declares a
// non-empty Scope (tenant enforcement proof) AND total fallback calls stay
// at or below FALLBACK_CAP (ADR-3). An unscoped target with nil
// ResolveLabels yields slog.Warn plus the raw FK left in place.
//
// Register-time validation is SELF-CONTAINED: ForeignKey must match an
// existing Sort.Columns[].Key on the SAME resource (ADR-6). Target-resource
// existence is deferred to runtime (resolveRelations) to avoid
// order-dependency and impossible circular registrations; circular
// Relations are documented unsupported. Empty or zero FK values are skipped
// (ADR-10). Errors are logged via slog.Warn with NO FK values (ADR-9) and
// the raw FK is left in place (graceful degradation).
//
// resolveRelations is NOT wired into makeListHandler yet (Phase 3).
package resource

import (
	"context"
	"fmt"
	"log/slog"
)

// FALLBACK_CAP is the maximum total FetchRow fallback calls permitted across
// all relations of a single resolveRelations invocation. When the cumulative
// count would exceed this cap, the remaining fallback lookups are skipped
// (slog.Warn + raw FK preserved) to avoid an N+1 DoS (ADR-3).
const FALLBACK_CAP = 50

// Relation declares a BelongsTo cross-resource link from one resource's
// foreign-key cell to another resource's display value (P1 — no Type enum,
// BelongsTo only, ADR-1).
//
// ForeignKey must name an existing Sort.Columns[].Key on the SAME resource
// (validated at Register time by validateRelationsConfig — ADR-6
// self-contained validation). Target-resource existence is deferred to
// runtime (resolveRelations) to avoid order-dependency and impossible
// circular registrations.
//
// ResolveLabels is the PRIMARY recommended resolution path: a batch closure
// that resolves all FK ids in one call, avoiding an N+1 FetchRow DoS
// (ADR-2). When nil, resolveRelations falls back to target.FetchRow per id,
// gated by the target Resource declaring a non-empty Scope (tenant
// enforcement proof, ADR-3) AND total fallback calls staying at or below
// FALLBACK_CAP. An unscoped target with nil ResolveLabels yields slog.Warn
// plus the raw FK left in place (ADR-3 tenant gate).
//
// Tenant context flows via ctx (tenant.From) for both ResolveLabels and the
// FetchRow fallback (ADR-7).
type Relation struct {
	// Resource is the target resource Name (e.g. "users"). Existence is
	// checked at runtime, not Register time (ADR-6).
	Resource string
	// ForeignKey is the Sort.Columns[].Key on THIS resource whose cell
	// value holds the target row id. Must match a column key
	// (Register-time validation, ADR-6).
	ForeignKey string
	// DisplayKey is the key in the target row map (FetchRow) or the
	// ResolveLabels result map whose value is rendered as the anchor text.
	DisplayKey string
	// ResolveLabels, when non-nil, is the PRIMARY batch resolution path.
	// It receives the de-duplicated non-empty FK ids and must return a map
	// of id -> display value. Errors are logged (no FK values) and leave
	// the raw FK in place (ADR-9).
	ResolveLabels func(ctx context.Context, ids []string) (map[string]string, error)
}

// resolveRelations resolves every BelongsTo Relation declared on r against
// rows, replacing each foreign-key cell with an XSS-safe CrossLinkCell
// anchor pointing at the target resource's detail route. It is a no-op when
// r declares no Relations (backward compatible). NOT wired into
// makeListHandler yet (Phase 3).
//
// rows is mutated in place: matching FK cells are replaced with HTML anchor
// cells (Cell.HTML=true). Non-matching rows/cells are left untouched.
func resolveRelations(ctx context.Context, p *Panel, r *Resource, rows []Row) error {
	if len(r.Relations) == 0 {
		return nil
	}
	// fallbackCalls counts FetchRow invocations across ALL relations of this
	// call, enforcing the global FALLBACK_CAP (ADR-3 DoS cap).
	fallbackCalls := 0
	for _, rel := range r.Relations {
		// Find the cell index for this relation's FK column on r.
		fkIdx := -1
		for i, col := range r.Sort.Columns {
			if col.Key == rel.ForeignKey {
				fkIdx = i
				break
			}
		}
		if fkIdx < 0 {
			// Impossible after validateRelationsConfig at Register time;
			// fail safe and skip rather than panic at runtime.
			continue
		}

		// Collect non-empty, non-"0" FK ids and the rows that carry them
		// (ADR-10 empty/zero skip). Track per-row so we can replace cells
		// after resolution.
		type rowRef struct {
			rowIdx int
			fkID   string
		}
		var refs []rowRef
		seen := make(map[string]bool)
		for i := range rows {
			if fkIdx >= len(rows[i].Cells) {
				continue
			}
			fk := rows[i].Cells[fkIdx].Value
			if fk == "" || fk == "0" {
				continue
			}
			refs = append(refs, rowRef{i, fk})
			seen[fk] = true
		}
		if len(refs) == 0 {
			continue
		}

		// De-duplicated id list for batch lookup.
		ids := make([]string, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}

		labels := make(map[string]string)
		if rel.ResolveLabels != nil {
			// PRIMARY path: one batch call (ADR-2).
			got, err := rel.ResolveLabels(ctx, ids)
			if err != nil {
				slog.Warn("resource: resolve relation failed", "resource", r.Name, "relation", rel.Resource, "err", err)
				continue // leave raw FK (ADR-9)
			}
			labels = got
		} else {
			// FetchRow fallback (ADR-3): permitted only when the target
			// Resource declares a non-empty Scope (tenant enforcement
			// proof) AND total fallback calls stay at or below
			// FALLBACK_CAP.
			target := findResource(p, rel.Resource)
			if target == nil {
				slog.Warn("resource: resolve relation failed", "resource", r.Name, "relation", rel.Resource, "err", fmt.Errorf("target resource %q not registered", rel.Resource))
				continue
			}
			if target.FetchRow == nil {
				slog.Warn("resource: resolve relation failed", "resource", r.Name, "relation", rel.Resource, "err", fmt.Errorf("target resource %q has no FetchRow", rel.Resource))
				continue
			}
			if target.Scope.Column == "" {
				slog.Warn("resource: resolve relation failed", "resource", r.Name, "relation", rel.Resource, "err", fmt.Errorf("target resource %q has empty Scope (ADR-3 tenant gate)", rel.Resource))
				continue
			}
			if fallbackCalls+len(ids) > FALLBACK_CAP {
				slog.Warn("resource: resolve relation failed", "resource", r.Name, "relation", rel.Resource, "err", fmt.Errorf("fallback cap %d exceeded (ADR-3 DoS cap)", FALLBACK_CAP))
				continue
			}
			// Tenant context flows via ctx (ADR-7) — FetchRow closures
			// are expected to read tenant.From(ctx) themselves, same as
			// Lister.
			for _, id := range ids {
				row, err := target.FetchRow(ctx, id)
				if err != nil {
					slog.Warn("resource: resolve relation failed", "resource", r.Name, "relation", rel.Resource, "err", err)
					continue // leave this id's raw FK (ADR-9)
				}
				fallbackCalls++
				if v, ok := row[rel.DisplayKey]; ok && v != "" {
					labels[id] = v
				}
			}
		}

		// Replace FK cells with CrossLinkCell anchors. Missing display
		// values leave the raw FK in place (ADR-9 graceful degradation).
		for _, ref := range refs {
			disp, ok := labels[ref.fkID]
			if !ok || disp == "" {
				continue
			}
			rows[ref.rowIdx].Cells[fkIdx] = Cell{
				Value: CrossLinkCell(p.basePath, rel.Resource, ref.fkID, disp),
				HTML:  true,
			}
		}
	}
	return nil
}

// findResource returns a pointer to the registered Resource named name, or
// nil when no such resource is registered. Used by resolveRelations for the
// FetchRow fallback path; target existence is a runtime check (ADR-6).
func findResource(p *Panel, name string) *Resource {
	for i := range p.resources {
		if p.resources[i].Name == name {
			return &p.resources[i]
		}
	}
	return nil
}

// validateRelationsConfig panics at Register time if any Relation's
// ForeignKey does not match a Sort.Columns[].Key on the SAME resource
// (ADR-6 self-contained validation). Target-resource existence is NOT
// checked here — it is deferred to runtime (resolveRelations) to avoid
// order-dependency and impossible circular registrations.
func validateRelationsConfig(r *Resource) {
	if len(r.Relations) == 0 {
		return
	}
	keys := make(map[string]bool, len(r.Sort.Columns))
	for _, col := range r.Sort.Columns {
		keys[col.Key] = true
	}
	for _, rel := range r.Relations {
		if !keys[rel.ForeignKey] {
			panic(fmt.Sprintf("resource.Register %q: Relation.ForeignKey %q does not match any Sort.Columns[].Key (ADR-6)", r.Name, rel.ForeignKey))
		}
	}
}
