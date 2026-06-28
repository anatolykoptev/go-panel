package shell

// SidebarCookie is the cookie persisting the operator's collapse choice.
// Server-readable (vs localStorage) so Layout renders the collapsed class at
// SSR time — no flash-of-expanded-sidebar on first load.
// Mirrors shadcn/ui's server-read sidebar_state cookie pattern.
//
// The type carrying this state is ChromeState (shell/chrome_state.go);
// parsing lives in the resource layer (resource.chromeStateFrom).
const SidebarCookie = "sb-c"

// GroupsCookie is the cookie persisting which nav groups are collapsed.
// Value is URL-encoded (encodeURIComponent in JS), comma-separated group names.
// Only collapsed group names are stored (non-default); absent = no groups collapsed.
// Server-readable so Layout emits data-collapsed=true at SSR time — no flash.
const GroupsCookie = "sb-g"
