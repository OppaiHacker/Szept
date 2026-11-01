package version

import "testing"

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"v1.2.3":         "v1.2.3",
		"v1.2.3-rc1+abc": "v1.2.3-rc1+abc",
		"v1<script>":     "v1",
		"v1\n\x1b[31m":   "v1",
		"":               "",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q", in, got)
		}
	}
}
