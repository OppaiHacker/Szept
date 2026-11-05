package main

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Fling coasting must deliver content OPPOSITE to finger movement: finger down reveals
// earlier messages, i.e., reduces offset. Wrong sign gives a list
// that snaps back in the opposite direction on release.
func TestFlingDistance(t *testing.T) {
	if d := flingDistance(flingMinVel - 1); d != 0 {
		t.Errorf("wolne puszczenie ma nie rzucać, dostałem %v", d)
	}
	if d := flingDistance(-flingMinVel + 1); d != 0 {
		t.Errorf("wolne puszczenie w drugą stronę ma nie rzucać, dostałem %v", d)
	}
	if d := flingDistance(2000); d >= 0 {
		t.Errorf("palec w dół ma zmniejszać offset, dostałem %v", d)
	}
	if d := flingDistance(-2000); d <= 0 {
		t.Errorf("palec w górę ma zwiększać offset, dostałem %v", d)
	}
	// coasting time grows with distance, but neither drops below floor nor
	// exceeds ceiling — otherwise a light nudge would crawl as long as a hard flick
	if a, b := flingSpan(30), flingSpan(3000); a >= b || a < 200*time.Millisecond || b > flingTime {
		t.Errorf("czas wybiegu poza widełkami: krótki=%v długi=%v", a, b)
	}
}

// Fling scrolling must still scroll: Dragged should update offset,
// and fyne's clamp must not release it past content boundary.
func TestFlingScrollDragged(t *testing.T) {
	test.NewTempApp(t)
	tall := container.NewVBox()
	for i := 0; i < 60; i++ {
		tall.Add(widget.NewLabel("wiersz"))
	}
	s := vscroll(tall)
	s.Resize(fyne.NewSize(300, 200))

	up := &fyne.DragEvent{Dragged: fyne.NewDelta(0, -50)} // finger moving up
	s.Dragged(up)
	if s.Offset.Y <= 0 {
		t.Fatalf("przeciągnięcie w górę nie przewinęło: offset=%v", s.Offset.Y)
	}
	// fyne dampening loop runs while |e.Dragged| > 2 — zeroed
	// event is the only way for it to hand off coasting to our animation
	if up.Dragged != (fyne.Delta{}) {
		t.Errorf("zdarzenie nie zostało wyzerowane: %+v", up.Dragged)
	}
	s.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, 9999)}) // moving down, past start
	if s.Offset.Y != 0 {
		t.Fatalf("offset wyszedł poza początek treści: %v", s.Offset.Y)
	}
}

// fitText truncates to available width and appends ellipsis only when needed.
func TestFitText(t *testing.T) {
	test.NewTempApp(t)
	const size = 14
	var style fyne.TextStyle
	long := "ekipa-z-osiedla-i-jeszcze-kawałek-nazwy"
	full := fyne.MeasureText(long, size, style).Width

	if got := fitText(long, size, style, full+10); got != long {
		t.Errorf("tekst się mieści, a został skrócony do %q", got)
	}
	half := fitText(long, size, style, full/2)
	if !strings.HasSuffix(half, "…") {
		t.Errorf("skrócony tekst bez wielokropka: %q", half)
	}
	if w := fyne.MeasureText(half, size, style).Width; w > full/2 {
		t.Errorf("skrócony tekst nadal nie mieści się: %v > %v", w, full/2)
	}
	if got := fitText("", size, style, 100); got != "" {
		t.Errorf("pusty tekst zmienił się w %q", got)
	}
}
