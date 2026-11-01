// Package version names the build every szept binary runs, and cleans the
// version strings others report about themselves.
package version

import "runtime/debug"

// V: the release tag, stamped by make (-X szept/internal/version.V=v1.2.3).
// Only stamped clients update themselves (internal/update).
var V string

// String: the release tag, else the commit, else "dev".
func String() string {
	if V != "" {
		return V
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return bi.Main.Version
		}
	}
	return "dev"
}

// Clean: a version a peer or client sent, cut to 32 characters of
// [0-9A-Za-z.+-]; anything else ends it. It's shown in UIs and logs, so it
// must not carry markup or control characters.
func Clean(s string) string {
	for i, r := range s {
		ok := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '+' || r == '-'
		if !ok || i == 32 {
			return s[:i]
		}
	}
	return s
}
