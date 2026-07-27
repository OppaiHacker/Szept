package i18n

import "testing"

func TestI18n(t *testing.T) {
	SetLang("en")
	if Lang() != "en" {
		t.Fatalf("expected en, got %s", Lang())
	}
	if got := T("group_name_required"); got != "Provide group name" {
		t.Fatalf("expected 'Provide group name', got %q", got)
	}

	SetLang("pl")
	if Lang() != "pl" {
		t.Fatalf("expected pl, got %s", Lang())
	}
	if got := T("group_name_required"); got != "Podaj nazwę grupy" {
		t.Fatalf("expected 'Podaj nazwę grupy', got %q", got)
	}
}
