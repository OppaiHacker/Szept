package attach

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 3), uint8(y * 3), 0x80, 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testGIF(t *testing.T, frames int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < frames; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 32, 32), color.Palette{color.Black, color.White})
		p.SetColorIndex(i%32, i%32, 1)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 10)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Zwykłe pliki wolno wysyłać, ale nie wolno im udawać mediów: typ bierze się
// z magicznych bajtów, nie z nazwy, a awatarem nie zostaną nigdy.
func TestNonMedia(t *testing.T) {
	notMedia := map[string][]byte{
		"html":   []byte("<html><script>alert(1)</script></html>"),
		"skrypt": []byte("#!/bin/sh\nrm -rf /\n"),
		"elf":    {0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0},
		"svg":    []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>x()</script></svg>`),
	}
	for name, data := range notMedia {
		m, err := Check(data, "foto.png")
		if err != nil {
			t.Errorf("%s: zwykły plik odrzucony: %v", name, err)
			continue
		}
		if IsMedia(m.Mime) {
			t.Errorf("%s: wykryty jako medium (%q)", name, m.Mime)
		}
		if strings.HasSuffix(m.Name, ".png") {
			t.Errorf("%s: nazwa %q udaje obraz", name, m.Name)
		}
		if _, _, err := Avatar(data); err == nil {
			t.Errorf("%s: przeszedł jako awatar", name)
		}
	}
	if _, err := Check(nil, "puste"); err == nil {
		t.Error("pusty plik przeszedł")
	}
	// PNG z podmienionym nagłówkiem: magic mówi PNG, treść to śmieci
	fake := append([]byte("\x89PNG\r\n\x1a\n"), []byte("<script>alert(1)</script>")...)
	if _, err := Check(fake, "x.png"); err == nil {
		t.Error("podrobiony PNG przeszedł walidację")
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":       "passwd.png",
		`C:\Windows\system32\x`:  "x.png",
		"zdjęcie.png":            "zdjęcie.png",
		"payload.png.sh":         "payload_png.png", // media: rozszerzenie z typu
		".bashrc":                "bashrc.png",
		"":                       "zalacznik.png",
		"a\x00b\nc":              "abc.png",
		strings.Repeat("x", 200): strings.Repeat("x", 60) + ".png",
	}
	for in, want := range cases {
		if got := SafeName(in, "image/png"); got != want {
			t.Errorf("SafeName(%q, png) = %q, chciałem %q", in, got, want)
		}
	}
	// zwykły plik: rozszerzenie zostaje, ale tylko jedno i tylko alfanumeryczne
	generic := map[string]string{
		"raport.pdf":       "raport.pdf",
		"raport.pdf.exe":   "raport_pdf.exe",
		"../../evil/x.zip": "x.zip",
		"dane":             "dane.bin",
		"a.t?r":            "a.tr",
	}
	for in, want := range generic {
		if got := SafeName(in, "application/octet-stream"); got != want {
			t.Errorf("SafeName(%q, bin) = %q, chciałem %q", in, got, want)
		}
	}
}

// Podgląd dużego obrazka: zmniejszony JPEG, który przechodzi Verify u odbiorcy.
func TestPreview(t *testing.T) {
	out, w, h, err := Preview(testPNG(t, 1600, 800), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if w != PreviewSide || h != PreviewSide/2 {
		t.Fatalf("podgląd %dx%d", w, h)
	}
	if err := Verify(out, "image/jpeg"); err != nil {
		t.Fatalf("podgląd nie przechodzi Verify: %v", err)
	}
	if _, _, _, err := Preview([]byte("#!/bin/sh\n"), "text/plain"); err == nil {
		t.Error("podgląd nie-obrazu przeszedł")
	}
}

func TestCheckImage(t *testing.T) {
	m, err := Check(testPNG(t, 40, 20), "../../evil/foto.PNG")
	if err != nil {
		t.Fatal(err)
	}
	if m.Mime != "image/png" || m.W != 40 || m.H != 20 || m.Name != "foto.png" {
		t.Fatalf("zła meta: %+v", m)
	}
	if _, err := Check(make([]byte, MaxFileBytes+1), "wielki.png"); err == nil {
		t.Error("plik ponad limit przeszedł")
	}
}

// Awatar musi być przekodowany: doklejony ładunek nie może przeżyć.
func TestAvatarStripsPayload(t *testing.T) {
	payload := []byte("<script>alert('xss')</script>PK\x03\x04ukryte-archiwum")
	poly := append(testPNG(t, 300, 200), payload...)
	if _, _, err := image.DecodeConfig(bytes.NewReader(poly)); err != nil {
		t.Fatal("test bez sensu: polyglot nie dekoduje się jako obraz")
	}
	out, mime, err := Avatar(poly)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q", mime)
	}
	if bytes.Contains(out, payload) || bytes.Contains(out, []byte("script")) {
		t.Fatal("ładunek przeżył normalizację awatara")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != AvatarSide || cfg.Height != AvatarSide {
		t.Fatalf("awatar %dx%d, chciałem %d²", cfg.Width, cfg.Height, AvatarSide)
	}
	if len(out) > MaxAvatarOut {
		t.Fatalf("awatar %d B ponad limit", len(out))
	}
}

// GIF-a da się ustawić jako awatar i zostaje animowanym GIF-em.
func TestAvatarGIF(t *testing.T) {
	out, mime, err := Avatar(testGIF(t, 8))
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/gif" {
		t.Fatalf("mime = %q, chciałem image/gif", mime)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 8 {
		t.Fatalf("%d klatek po normalizacji, chciałem 8", len(g.Image))
	}
	if b := g.Image[0].Bounds(); b.Dx() != AvatarSide || b.Dy() != AvatarSide {
		t.Fatalf("klatka %v", b)
	}
	frames, delays, err := GifFrames(out)
	if err != nil || len(frames) != 8 || len(delays) != 8 {
		t.Fatalf("GifFrames: %v %d", err, len(frames))
	}
}

// Blob z sieci: typ musi zgadzać się z tym, co zapowiedział podpisany event.
func TestVerify(t *testing.T) {
	data := testPNG(t, 8, 8)
	if err := Verify(data, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := Verify(data, "video/mp4"); err == nil {
		t.Error("przyjąłem PNG podszywający się pod wideo")
	}
	if err := Verify([]byte("<html>"), "image/png"); err == nil {
		t.Error("przyjąłem HTML jako obraz")
	}
}

// bombJPEG: poprawny nagłówek JPEG (SOI + SOF0) deklarujący podane wymiary —
// tak wygląda bomba dekompresyjna: 3 MB pliku, gigabajty po zdekodowaniu.
func bombJPEG(w, h int) []byte {
	return []byte{
		0xff, 0xd8, // SOI
		0xff, 0xc0, 0x00, 0x11, 0x08, // SOF0, len 17, precyzja 8
		byte(h >> 8), byte(h), byte(w >> 8), byte(w),
		0x03, 0x01, 0x22, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01, // 3 komponenty
	}
}

func TestCheckDims(t *testing.T) {
	if err := CheckDims(testPNG(t, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := CheckDims(bombJPEG(65535, 65535)); err == nil {
		t.Error("przyjąłem JPEG deklarujący 65535x65535 px")
	}
	if err := CheckDims(bombJPEG(9000, 100)); err == nil {
		t.Error("przyjąłem JPEG szerszy niż limit boku")
	}
}
