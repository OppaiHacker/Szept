package main

import "testing"

func TestPingsMe(t *testing.T) {
	for text, want := range map[string]bool{
		"hej @Ola":         true,
		"(@ola) patrz":     true,
		"@everyone, uwaga": true,
		"mail ola@ola.pl":  false,
		"@olaf":            false,
		"bez pingu":        false,
	} {
		if got := pingsMe(text, "Ola"); got != want {
			t.Errorf("pingsMe(%q) = %v, want %v", text, got, want)
		}
	}
}
