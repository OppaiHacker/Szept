// Bytes that are too big for the JSON bridge: attachments, avatars, banners
// and live call frames all travel over the asset server instead.
//
// The webview talks to the asset server with plain <img src="/blob/…">, so the
// pictures never get base64-inflated through a method call, and the webview's
// own cache does the work an in-process image cache used to do in the Fyne UI.
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"szept/internal/core"
)

// routes intercepts our own URL space and lets everything else fall through to
// the embedded frontend.
func (a *App) routes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/blob/"):
			a.serveBlob(w, strings.TrimPrefix(p, "/blob/"))
		case strings.HasPrefix(p, "/avatar/"):
			a.serveCard(w, strings.TrimPrefix(p, "/avatar/"), false)
		case strings.HasPrefix(p, "/banner/"):
			a.serveCard(w, strings.TrimPrefix(p, "/banner/"), true)
		case strings.HasPrefix(p, "/frame/"):
			a.serveFrame(w, strings.TrimPrefix(p, "/frame/"))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// safeMimes: types we are willing to name on a response served from the app's
// own origin. The mime of an avatar or banner is copied straight from the peer's
// event, and attach.Verify only requires it to match the sniffed bytes — so a
// peer can hand over "text/html" plus HTML and pass both checks. Anything off
// this list is served as an opaque download instead.
var safeMimes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// writeMedia sets a content type the webview cannot be talked into executing.
func writeMedia(w http.ResponseWriter, mime string, data []byte) {
	if !safeMimes[mime] {
		mime = "application/octet-stream"
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Content-Type", mime)
	w.Write(data)
}

func (a *App) serveBlob(w http.ResponseWriter, name string) {
	c, err := a.live()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// "<hex>" or "<hex>/<filename>" — the name only exists so downloads keep it
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name = name[:i]
	}
	data, ok := c.BlobBytes(unhex(name))
	if !ok {
		http.NotFound(w, nil2req(w))
		return
	}
	// Content hashes are immutable, so this can be cached forever.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeMedia(w, http.DetectContentType(data), data)
}

func (a *App) serveCard(w http.ResponseWriter, pub string, banner bool) {
	c, err := a.live()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if i := strings.IndexByte(pub, '?'); i >= 0 {
		pub = pub[:i]
	}
	get := c.AvatarOf
	if banner {
		get = c.BannerOf
	}
	data, mime, _ := get(unhex(pub))
	if len(data) == 0 {
		http.NotFound(w, nil2req(w))
		return
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeMedia(w, mime, data)
}

// nil2req: http.NotFound needs a request it never reads. Keeps the callers tidy.
func nil2req(http.ResponseWriter) *http.Request {
	r, _ := http.NewRequest("GET", "/", nil)
	return r
}

// avatarURL / bannerURL carry the content hash as the cache buster, so a new
// picture is a new URL and an unchanged one is served from the webview cache.
func avatarURL(c *core.Core, pub []byte) string {
	card, ok := c.St.Profile(pub)
	if !ok || len(card.Blob) == 0 || !c.St.HasBlob(card.Blob) {
		return ""
	}
	return "/avatar/" + hexs(pub) + "?v=" + hexs(card.Blob)[:16]
}

func bannerURL(c *core.Core, pub []byte) string {
	card, ok := c.St.Profile(pub)
	if !ok || len(card.Banner) == 0 || !c.St.HasBlob(card.Banner) {
		return ""
	}
	return "/banner/" + hexs(pub) + "?v=" + hexs(card.Banner)[:16]
}

// blobURL: a content-addressed picture we already hold, "" otherwise.
func blobURL(c *core.Core, hash []byte) string {
	if len(hash) == 0 || !c.St.HasBlob(hash) {
		return ""
	}
	return "/blob/" + hexs(hash)
}

// ---- live call frames ----

type frame struct {
	mu   sync.Mutex
	jpeg []byte
	seq  uint64
	last time.Time
}

// onFrame is the core's video sink. It keeps only the newest picture per tile:
// the webview pulls it over HTTP, so a backlog would just be latency.
func (a *App) onFrame(chat, from []byte, kind string, jpg []byte) {
	key := hexs(chat) + "." + hexs(from) + "." + kind
	a.mu.Lock()
	f := a.frames[key]
	if f == nil {
		f = &frame{}
		a.frames[key] = f
	}
	a.frameNo++
	seq := a.frameNo
	a.mu.Unlock()

	f.mu.Lock()
	f.jpeg, f.seq, f.last = jpg, seq, time.Now()
	f.mu.Unlock()

	a.app.Event.Emit("frame", map[string]any{
		"key": key, "chat": hexs(chat), "from": hexs(from), "kind": kind, "seq": seq,
	})
}

func (a *App) dropFrames(chat string) {
	a.mu.Lock()
	for k := range a.frames {
		if strings.HasPrefix(k, chat+".") {
			delete(a.frames, k)
		}
	}
	a.mu.Unlock()
	a.app.Event.Emit("frames-cleared", chat)
}

func (a *App) serveFrame(w http.ResponseWriter, key string) {
	if i := strings.IndexByte(key, '?'); i >= 0 {
		key = key[:i]
	}
	a.mu.Lock()
	f := a.frames[key]
	a.mu.Unlock()
	if f == nil {
		http.NotFound(w, nil2req(w))
		return
	}
	f.mu.Lock()
	data := f.jpeg
	f.mu.Unlock()
	if len(data) == 0 {
		http.NotFound(w, nil2req(w))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(data)
}

// ---- data: URLs ----

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// decodeDataURL accepts what FileReader.readAsDataURL produces and refuses
// anything past the limit before allocating the decoded copy.
func decodeDataURL(s string, limit int) ([]byte, error) {
	i := strings.Index(s, ",")
	if !strings.HasPrefix(s, "data:") || i < 0 {
		return nil, errors.New("nieprawidłowe dane obrazu")
	}
	if !strings.Contains(s[:i], ";base64") {
		return nil, errors.New("obsługiwane są tylko dane base64")
	}
	payload := s[i+1:]
	// 4 base64 chars carry 3 bytes — reject oversize input without decoding it
	if len(payload)/4*3 > limit {
		return nil, fmt.Errorf("plik jest za duży (limit %s)", humanBytes(int64(limit)))
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, fmt.Errorf("plik jest za duży (limit %s)", humanBytes(int64(limit)))
	}
	return raw, nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatInt(n>>20, 10) + " MB"
	case n >= 1<<10:
		return strconv.FormatInt(n>>10, 10) + " kB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

func version() string {
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
