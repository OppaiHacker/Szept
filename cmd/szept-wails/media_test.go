package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeDataURL(t *testing.T) {
	const limit = 9 // "obrazek" -> 7 B passes, longer input does not

	got, err := decodeDataURL("data:image/png;base64,b2JyYXplaw==", limit)
	if err != nil || string(got) != "obrazek" {
		t.Fatalf("poprawny data: URL = %q, %v", got, err)
	}

	bad := []struct {
		name string
		in   string
	}{
		{"bez prefiksu", "image/png;base64,b2JyYXplaw=="},
		{"bez przecinka", "data:image/png;base64"},
		{"nie base64", "data:image/png,obrazek"},
		{"za duży", "data:image/png;base64," + strings.Repeat("A", 64)},
		{"zepsuty base64", "data:image/png;base64,!!!!"},
	}
	for _, c := range bad {
		if _, err := decodeDataURL(c.in, limit); err == nil {
			t.Errorf("%s: oczekiwano błędu", c.name)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 512: "512 B", 2048: "2 kB", 5 << 20: "5 MB"} {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, chciano %q", in, got, want)
		}
	}
}

// SZ-13: the mime of an avatar or banner comes from the peer's event, and the
// route sits in the app's own origin — so anything but a plain image must be
// served as an opaque download, never as a type the webview would render.
func TestCardMimeWhitelist(t *testing.T) {
	for mime, want := range map[string]string{
		"image/png":                "image/png",
		"image/jpeg":               "image/jpeg",
		"image/gif":                "image/gif",
		"image/webp":               "image/webp",
		"text/html":                "application/octet-stream",
		"image/svg+xml":            "application/octet-stream",
		"application/javascript":   "application/octet-stream",
		"text/html; charset=utf-8": "application/octet-stream",
		"":                         "application/octet-stream",
	} {
		w := httptest.NewRecorder()
		writeMedia(w, mime, []byte("bajty"))
		if got := w.Header().Get("Content-Type"); got != want {
			t.Errorf("mime %q podany jako %q, chciano %q", mime, got, want)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("mime %q: brak nosniff (%q)", mime, got)
		}
	}
}

// Media routes must answer on their own paths and leave everything else to the
// embedded frontend — a fallthrough bug would serve index.html as a picture.
func TestRoutes(t *testing.T) {
	a := &App{} // no core: every media route is expected to refuse, not to panic
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("frontend"))
	})
	h := a.routes(next)

	for _, path := range []string{"/blob/aabb", "/avatar/aabb", "/banner/aabb", "/frame/x.y.cam"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == http.StatusOK {
			t.Errorf("%s: 200 bez zalogowanego rdzenia", path)
		}
		if w.Body.String() == "frontend" {
			t.Errorf("%s: trafiło do frontendu zamiast do obsługi mediów", path)
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/app.js", nil))
	if w.Body.String() != "frontend" {
		t.Errorf("/app.js nie trafiło do frontendu: %q", w.Body.String())
	}
}
