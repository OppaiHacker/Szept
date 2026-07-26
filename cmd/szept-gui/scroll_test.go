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

// Rzut ma dowieźć treść PRZECIWNIE do ruchu palca: palec w dół odsłania
// wcześniejsze wiadomości, czyli zmniejsza offset. Pomyłka w znaku daje listę,
// która po puszczeniu odskakuje w drugą stronę.
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
	// czas wybiegu rośnie z drogą, ale nie schodzi poniżej podłogi ani nie
	// przekracza sufitu — inaczej lekkie pchnięcie pełzło tyle co mocny rzut
	if a, b := flingSpan(30), flingSpan(3000); a >= b || a < 200*time.Millisecond || b > flingTime {
		t.Errorf("czas wybiegu poza widełkami: krótki=%v długi=%v", a, b)
	}
}

// Przewijanie z wybiegiem musi nadal przewijać: Dragged ma ruszyć offset,
// a clamp fyne nie może go puścić poza treść.
func TestFlingScrollDragged(t *testing.T) {
	test.NewTempApp(t)
	tall := container.NewVBox()
	for i := 0; i < 60; i++ {
		tall.Add(widget.NewLabel("wiersz"))
	}
	s := vscroll(tall)
	s.Resize(fyne.NewSize(300, 200))

	up := &fyne.DragEvent{Dragged: fyne.NewDelta(0, -50)} // palec w górę
	s.Dragged(up)
	if s.Offset.Y <= 0 {
		t.Fatalf("przeciągnięcie w górę nie przewinęło: offset=%v", s.Offset.Y)
	}
	// wygaszająca pętla fyne kręci się dopóki |e.Dragged| > 2 — wyzerowane
	// zdarzenie jest jedynym sposobem, żeby oddała wybieg naszej animacji
	if up.Dragged != (fyne.Delta{}) {
		t.Errorf("zdarzenie nie zostało wyzerowane: %+v", up.Dragged)
	}
	s.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, 9999)}) // w dół, poza początek
	if s.Offset.Y != 0 {
		t.Fatalf("offset wyszedł poza początek treści: %v", s.Offset.Y)
	}
}

// fitText skraca do dostępnej szerokości i tylko wtedy dokłada wielokropek.
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
