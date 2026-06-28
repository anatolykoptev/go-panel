package shell

import "context"

// ChromeState carries per-request shell (chrome) configuration threaded into
// Layout via context.  It is the single context seam for all per-request
// state the layout renders, replacing the narrower SidebarState.
//
// CollapsedGroups and Profile are zero-valued in Phase 2 and will be
// populated by Phases 4 and 7 respectively.
//
// shell imports neither net/http nor auth; cookie parsing lives in the
// resource layer (resource.chromeStateFrom).
type ChromeState struct {
	Collapsed       bool
	CollapsedGroups map[string]bool
	Profile         ProfileConfig
}

// ProfileConfig is a placeholder for Phase 7 profile state.
// Zero value is safe.
type ProfileConfig struct{}

type chromeCtxKey struct{}

// ContextWithChrome stashes ChromeState in ctx for Layout to read.
// Chain after other context decorators (locale.WithLocale, etc.).
func ContextWithChrome(ctx context.Context, s ChromeState) context.Context {
	return context.WithValue(ctx, chromeCtxKey{}, s)
}

// ChromeFromContext returns the threaded ChromeState.
// Returns a zero value (Collapsed=false) when no state was stashed — safe default.
func ChromeFromContext(ctx context.Context) ChromeState {
	if s, ok := ctx.Value(chromeCtxKey{}).(ChromeState); ok {
		return s
	}
	return ChromeState{}
}
