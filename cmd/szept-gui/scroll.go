package main

// Building blocks that fyne lacks, but without which touch feels broken:
// scrolling with fling coasting, single-line text truncated to available
// width, and slidable layer for sliding in pages from the side.

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ---- scrolling with fling coasting ----

// Why this exists — fyne 2.8 has native fling after finger release,
// but implemented as (internal/driver/mobile/driver.go, tapUpCanvas):
//
//	go func() {
//	    for |delta| > 2 { delta *= 0.92; DoFromGoroutine(Dragged, false); sleep(16ms) }
//	    DoFromGoroutine(DragEnd, false)
//	}()
//
// A separate goroutine pushes a full scroll refresh every 16 ms WITHOUT CHECKING
// whether the previous one finished. When a frame takes longer (which happens on phone
// with hundreds of widgets), queue builds up and fling turns into stuttering —
// exactly what appears as "stuck instead of scrolling". Furthermore fling distance
// comes from a SINGLE last position diff, not velocity, so it has no relation to how hard
// the list was flicked.
//
// flingScroll takes over: measures true velocity, zeroes event (which
// kills fyne loop, see Dragged) and delivers content via a single animation, which fyne
// runner ticks once per RENDERED frame — never faster than GPU handles.
const (
	flingMinVel = float32(180)  // px/s — below this is release, not flick
	flingCoast  = float32(0.35) // seconds of coasting at measured speed
	flingTime   = 700 * time.Millisecond
	// velKeep: velocity smoothing. Single finger jitter upon release must not
	// dictate entire fling.
	velKeep = float32(0.6)
)

// activeFling: at most one fling runs at a time, so tapping anywhere
// (tapBox.Tapped) can halt it without walking the component tree.
// Must reset to nil ALSO after animation finishes naturally — otherwise
// first tap after any fling would be interpreted as "stop" and lost.
var activeFling *flingScroll

func stopFling() {
	if activeFling != nil {
		activeFling.halt()
	}
}

type flingScroll struct {
	container.Scroll
	vel   float32 // px/s; positive = finger moving down
	lastT time.Time
	anim  *fyne.Animation
}

// vscroll: vertical scroll with fling coasting — drop-in replacement for container.NewVScroll.
func vscroll(content fyne.CanvasObject) *flingScroll {
	s := &flingScroll{}
	s.Direction = container.ScrollVerticalOnly
	s.Content = content
	s.ExtendBaseWidget(s)
	return s
}

func (s *flingScroll) scrollBy(dy float32) {
	s.ScrollToOffset(fyne.NewPos(s.Offset.X, s.Offset.Y+dy))
}

func (s *flingScroll) Dragged(e *fyne.DragEvent) {
	s.halt()
	now := time.Now()
	// dt > 120 ms is not the same gesture — start velocity calculation from zero
	if dt := now.Sub(s.lastT); dt > 0 && dt < 120*time.Millisecond {
		s.vel = s.vel*velKeep + e.Dragged.DY/float32(dt.Seconds())*(1-velKeep)
	} else {
		s.vel = 0
	}
	s.lastT = now
	s.scrollBy(-e.Dragged.DY)

	// Zeroing event kills fyne dampening loop: loop condition is
	// |e.Dragged| > 2, and multiplying zero changes nothing, so after ONE iteration
	// it invokes DragEnd and hands off fling to our animation. Events from tapMove are
	// created afresh on each finger move, so nothing is lost here.
	e.Dragged = fyne.Delta{}
}

// flingDistance: how much content to deliver after release. Sign matches scrollBy —
// finger down (DY > 0) scrolls up chat history, reducing offset.
func flingDistance(vel float32) float32 {
	if vel > -flingMinVel && vel < flingMinVel {
		return 0 // release, not flick
	}
	return -vel * flingCoast
}

// flingSpan: coasting duration scales with distance, but has a ceiling. Constant time
// would mean a gentle nudge crawls for the same 700 ms as a hard flick.
func flingSpan(dist float32) time.Duration {
	if dist < 0 {
		dist = -dist
	}
	d := time.Duration(float32(flingTime) * fyne.Min(1, dist/400))
	return max(d, 220*time.Millisecond)
}

func (s *flingScroll) DragEnd() {
	dist := flingDistance(s.vel)
	s.vel = 0
	if dist == 0 {
		return
	}
	from := s.Offset.Y
	s.anim = fyne.NewAnimation(flingSpan(dist), func(p float32) {
		s.ScrollToOffset(fyne.NewPos(s.Offset.X, from+dist*p))
		if p == 1 {
			// runner always completes with tick 1.0 — handled here rather than Stop()
			// to avoid mutating animation list from inside its callback
			s.anim = nil
			if activeFling == s {
				activeFling = nil
			}
		}
	})
	s.anim.Curve = fyne.AnimationEaseOut
	activeFling = s
	s.anim.Start()
}

func (s *flingScroll) halt() {
	if s.anim != nil {
		s.anim.Stop()
		s.anim = nil
	}
	if activeFling == s {
		activeFling = nil
	}
}

// ---- text truncated to width ----

// clipText: single line that appends ellipsis automatically when overflowing.
// canvas.Text can neither wrap nor clip — hence previous hardcoded character limit
// cut-off, which wasted space on wide screens and still pushed text off-edge on narrow screens.
// widget.Label supports Truncation, but adds theme.InnerPadding() (13 px on mobile)
// on both sides, doubling list row height.
type clipText struct {
	widget.BaseWidget
	full string
	txt  *canvas.Text
}

func newClipText(s string, size float32, col color.Color) *clipText {
	t := &clipText{full: s}
	t.txt = canvas.NewText(s, col)
	t.txt.TextSize = size
	t.ExtendBaseWidget(t)
	return t
}

func (t *clipText) bold() *clipText {
	t.txt.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

func (t *clipText) setText(s string) {
	if t.full == s {
		return
	}
	t.full = s
	t.Refresh()
}

func (t *clipText) CreateRenderer() fyne.WidgetRenderer { return &clipRenderer{t: t} }

type clipRenderer struct{ t *clipText }

func (r *clipRenderer) Layout(size fyne.Size) {
	r.t.txt.Text = fitText(r.t.full, r.t.txt.TextSize, r.t.txt.TextStyle, size.Width)
	r.t.txt.Resize(size)
	r.t.txt.Refresh()
}

// MinSize: line height only. Width stays small so layout can compress
// row — content clipping handled by Layout.
func (r *clipRenderer) MinSize() fyne.Size {
	h := fyne.MeasureText("Ag", r.t.txt.TextSize, r.t.txt.TextStyle).Height
	return fyne.NewSize(16, h)
}

func (r *clipRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.t.txt} }
func (r *clipRenderer) Refresh()                     { r.Layout(r.t.Size()) }
func (r *clipRenderer) Destroy()                     {}

// fitText: longest prefix fitting within w pixels. Guesses initial length
// from ratio before refining — otherwise long strings would trigger dozens
// of text measurements per list row.
func fitText(s string, size float32, style fyne.TextStyle, w float32) string {
	if s == "" || w <= 0 {
		return s
	}
	full := fyne.MeasureText(s, size, style).Width
	if full <= w {
		return s
	}
	r := []rune(s)
	n := int(float32(len(r)) * w / full)
	if n >= len(r) {
		n = len(r) - 1
	}
	for n > 0 && fyne.MeasureText(string(r[:n])+"…", size, style).Width > w {
		n--
	}
	for n+1 < len(r) && fyne.MeasureText(string(r[:n+1])+"…", size, style).Width <= w {
		n++
	}
	if n <= 0 {
		return "…"
	}
	return string(r[:n]) + "…"
}

// ---- slidable layer (page slide-in) ----

// slideLayout: full-screen layer that can be offset horizontally.
// container.Stack resets children to (0,0) on every layout pass, so animating
// Move() alone is undone by layout.
type slideLayout struct{ off float32 }

func (s *slideLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var m fyne.Size
	for _, o := range objs {
		m = m.Max(o.MinSize())
	}
	return m
}

func (s *slideLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(s.off, 0))
	}
}

// slideDur: short — on phone any animation longer than ~200 ms starts feeling
// sluggish rather than smooth.
const slideDur = 190 * time.Millisecond

// slideIn slides layer from specified offset to zero. On tick we only call
// Move (Resize preserves container layout), making each frame a single offset update.
func slideIn(page *fyne.Container, from float32) {
	lay, ok := page.Layout.(*slideLayout)
	if !ok || from == 0 {
		return
	}
	move := func(x float32) {
		lay.off = x
		for _, o := range page.Objects {
			o.Move(fyne.NewPos(x, 0))
		}
	}
	move(from)
	a := fyne.NewAnimation(slideDur, func(p float32) { move(from * (1 - p)) })
	a.Curve = fyne.AnimationEaseOut
	a.Start()
}

// slidePage: page container prepared for sliding in from side.
func slidePage(content fyne.CanvasObject) *fyne.Container {
	return container.New(&slideLayout{}, content)
}
