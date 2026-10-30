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

// Generic files can be sent, but cannot impersonate media: type is determined
// from magic bytes, not filename, and they can never become avatars.
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
		if _, _, err := Avatar(data, image.Rectangle{}); err == nil {
			t.Errorf("%s: przeszedł jako awatar", name)
		}
	}
	if _, err := Check(nil, "puste"); err == nil {
		t.Error("pusty plik przeszedł")
	}
	// PNG with fake header: magic says PNG, content is trash
	fake := append([]byte("\x89PNG\r\n\x1a\n"), []byte("<script>alert(1)</script>")...)
	if _, err := Check(fake, "x.png"); err == nil {
		t.Error("fake PNG passed validation")
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":       "passwd.png",
		`C:\Windows\system32\x`:  "x.png",
		"zdjęcie.png":            "zdjęcie.png",
		"payload.png.sh":         "payload_png.png", // media: extension from type
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
	// generic file: only an allowlisted extension survives, the rest is .bin
	generic := map[string]string{
		"raport.pdf":       "raport.pdf",
		"raport.pdf.exe":   "raport_pdf.bin",
		"../../evil/x.zip": "x.zip",
		"dane":             "dane.bin",
		"a.t?xt":           "a.txt",
		"run.bat":          "run.bin",
		"x.PS1":            "x.bin",
		"app.hta":          "app.bin",
		"skrót.lnk":        "skrót.bin",
		"fake.png":         "fake.bin",
	}
	for in, want := range generic {
		if got := SafeName(in, "application/octet-stream"); got != want {
			t.Errorf("SafeName(%q, bin) = %q, chciałem %q", in, got, want)
		}
	}
}

// Thumbnail of large image: scaled-down JPEG that passes Verify at receiver.
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
		t.Error("non-image preview passed")
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
		t.Error("file over limit passed")
	}
}

// Avatar must be re-encoded: appended payload must not survive.
func TestAvatarStripsPayload(t *testing.T) {
	payload := []byte("<script>alert('xss')</script>PK\x03\x04ukryte-archiwum")
	poly := append(testPNG(t, 300, 200), payload...)
	if _, _, err := image.DecodeConfig(bytes.NewReader(poly)); err != nil {
		t.Fatal("pointless test: polyglot does not decode as image")
	}
	out, mime, err := Avatar(poly, image.Rectangle{})
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q", mime)
	}
	if bytes.Contains(out, payload) || bytes.Contains(out, []byte("script")) {
		t.Fatal("payload survived avatar normalization")
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

// GIF can be set as avatar and remains animated GIF.
func TestAvatarGIF(t *testing.T) {
	out, mime, err := Avatar(testGIF(t, 8), image.Rectangle{})
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

// Banner keeps a GIF animated (so an animated banner still moves) and never
// exceeds the frame cap — every frame is held unpacked in RAM while it plays.
func TestBannerGIF(t *testing.T) {
	out, mime, err := Banner(testGIF(t, 60), image.Rectangle{})
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
	if len(g.Image) < 2 || len(g.Image) > BannerFrames {
		t.Fatalf("%d klatek, chciałem 2..%d", len(g.Image), BannerFrames)
	}
	if b := g.Image[0].Bounds(); b.Dx() != BannerW || b.Dy() != BannerH {
		t.Fatalf("klatka %v", b)
	}
	if len(out) > MaxBannerOut {
		t.Fatalf("baner %d B ponad limit", len(out))
	}
	// a still image still comes out as PNG
	if _, mime, err := Banner(testPNG(t, 300, 300), image.Rectangle{}); err != nil ||
		mime != "image/png" {
		t.Fatalf("PNG baner: %v %q", err, mime)
	}
}

// halfPNG: left half red, right half blue — tells apart which region got cut.
func halfPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{0xff, 0, 0, 0xff}
			if x >= w/2 {
				c = color.RGBA{0, 0, 0xff, 0xff}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The framed rectangle decides what lands in the avatar. A rectangle from
// outside the image is not an error — it falls back to the center, because
// it arrives from the UI and must never be trusted blindly.
func TestAvatarCrop(t *testing.T) {
	data := halfPNG(t, 200, 100)
	redAt := func(out []byte, x, y int) bool {
		img, _, err := image.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		r, _, b, _ := img.At(x, y).RGBA()
		return r > 0xc000 && b < 0x4000
	}
	left, _, err := Avatar(data, image.Rect(0, 0, 100, 100))
	if err != nil {
		t.Fatal(err)
	}
	if !redAt(left, AvatarSide/2, AvatarSide/2) {
		t.Fatal("kadr z lewej połowy nie jest czerwony")
	}
	right, _, err := Avatar(data, image.Rect(100, 0, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	if redAt(right, AvatarSide/2, AvatarSide/2) {
		t.Fatal("kadr z prawej połowy wyszedł czerwony")
	}
	// nonsense rectangle: center crop, so both halves show up
	mid, _, err := Avatar(data, image.Rect(-900, -900, -800, -800))
	if err != nil {
		t.Fatal(err)
	}
	if !redAt(mid, 10, AvatarSide/2) || redAt(mid, AvatarSide-10, AvatarSide/2) {
		t.Fatal("kadr spoza obrazka nie wrócił do środka")
	}
}

// Network blob: type must match claim from signed event.
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

// bombJPEG: valid JPEG header (SOI + SOF0) claiming specified dimensions —
// classic decompression bomb: 3 MB file, gigabytes unpacked.
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
