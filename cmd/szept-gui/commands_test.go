package main

import "testing"

func TestRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		if c.name == "" || c.name[0] != '/' || c.desc == "" || c.run == nil {
			t.Fatalf("zła definicja komendy: %+v", c.name)
		}
		if seen[c.name] {
			t.Fatalf("duplikat komendy %s", c.name)
		}
		seen[c.name] = true
	}
}

func TestMatchCommands(t *testing.T) {
	desktop, phone := &ui{}, &ui{mobile: true}
	cases := []struct {
		in   string
		u    *ui
		want int
	}{
		{"", desktop, 0},
		{"hej", desktop, 0},
		{"/nowy grupa", desktop, 0}, // po spacji podpowiedzi znikają
		{"/", desktop, len(commands)},
		{"/", phone, len(commands) - 2}, // /cam i /ekran nie mają źródła na Androidzie
		{"/wyczysc", desktop, 1},
		{"/WYCZ", desktop, 1}, // wielkość liter bez znaczenia
		{"/zzz", desktop, 0},
	}
	for _, c := range cases {
		if got := len(c.u.matchCommands(c.in)); got != c.want {
			t.Errorf("matchCommands(%q, mobile=%v) = %d, chciałem %d", c.in, c.u.mobile, got, c.want)
		}
	}
}
