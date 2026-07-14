package identity

import "testing"

func TestSafeReturnURL(t *testing.T) {
	host := "panel.example.com"
	tests := []struct {
		name   string
		raw    string
		host   string
		want   string
	}{
		// Baseline: empty, root, simple relative paths.
		{"empty", "", host, rootPath},
		{"root", rootPath, host, rootPath},
		{"relative_dashboard", "/dashboard", host, "/dashboard"},
		{"relative_with_query", "/dashboard?tab=users", host, "/dashboard?tab=users"},

		// Cross-origin absolute URLs are rejected.
		{"abs_other_host", "https://evil.com/phish", host, rootPath},
		{"abs_http_other_host", "http://evil.com", host, rootPath},

		// Protocol-relative targets are rejected.
		{"protocol_relative", "//evil.com", host, rootPath},
		{"protocol_relative_encoded", "/%2F%2Fevil", host, rootPath},

		// Backslash tricks (raw and percent-encoded) are rejected.
		{"backslash_raw", "/\\evil.com", host, rootPath},
		{"backslash_encoded", "/%5Cevil", host, rootPath},

		// Same-host scheme-bearing paths are rejected.
		{"same_host_double_slash", "https://panel.example.com//evil", host, rootPath},
		{"same_host_scheme_bearing", "https://panel.example.com/http://evil", host, rootPath},

		// Same-host backslash paths (raw and encoded) are rejected.
		{"same_host_backslash_raw", "https://panel.example.com/\\evil", host, rootPath},
		{"same_host_backslash_encoded", "https://panel.example.com/%5Cevil", host, rootPath},

		// Same-host double-slash encoded is rejected.
		{"same_host_double_slash_encoded", "https://panel.example.com/%2F%2Fevil", host, rootPath},

		// Valid same-host absolute URL is rewritten to its RequestURI.
		{"same_host_ok", "https://panel.example.com/dashboard?tab=users", host, "/dashboard?tab=users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeReturnURL(tt.raw, tt.host); got != tt.want {
				t.Fatalf("safeReturnURL(%q, %q) = %q, want %q", tt.raw, tt.host, got, tt.want)
			}
		})
	}
}
