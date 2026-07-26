package main

// Klocki, których fyne nie ma, a bez których dotyk wygląda na zepsuty:
// przewijanie z wybiegiem, jednolinijkowy tekst przycinany do dostępnej
// szerokości i przesuwana warstwa do wjeżdżania stron z boku.

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ---- przewijanie z wybiegiem ----

// Dlaczego to w ogóle istnieje — fyne 2.8 ma po puszczeniu palca własny wybieg,
// ale zrobiony tak (internal/driver/mobile/driver.go, tapUpCanvas):
//
//	go func() {
//	    for |delta| > 2 { delta *= 0.92; DoFromGoroutine(Dragged, false); sleep(16ms) }
//	    DoFromGoroutine(DragEnd, false)
//	}()
//
// Osobna gorutyna dorzuca pełne odświeżenie przewijania co 16 ms NIE PATRZĄC,
// czy poprzednie się skończyło. Gdy klatka trwa dłużej (a na telefonie przy
// kilkuset widgetach trwa), kolejka rośnie i wybieg zamienia się w szarpanie —
// dokładnie to, co widać jako „nie scrolluje, tylko się zacina”. Do tego droga
// wybiegu bierze się z JEDNEJ ostatniej różnicy pozycji, nie z prędkości, więc
// nie ma związku z tym, jak mocno rzuciło się listą.
//
// flingScroll przejmuje sprawę: mierzy prawdziwą prędkość, zeruje zdarzenie (to
// gasi pętlę fyne, patrz Dragged) i dowozi treść jedną animacją, którą runner
// fyne tyka raz na NARYSOWANĄ klatkę — nigdy szybciej, niż GPU nadąża.
const (
	flingMinVel = float32(180)  // px/s — niżej to nie rzut, tylko puszczenie
	flingCoast  = float32(0.35) // sekundy wybiegu przy zmierzonej prędkości
	flingTime   = 700 * time.Millisecond
	// velKeep: wygładzanie prędkości. Jedno drgnięcie palca przy puszczaniu nie
	// może zdecydować o całym rzucie.
	velKeep = float32(0.6)
)

// activeFling: w danej chwili leci najwyżej jeden rzut, więc dotknięcie
// gdziekolwiek (tapBox.Tapped) umie go zatrzymać bez znajomości drzewa.
// Musi wracać do nil TAKŻE po samoczynnym końcu animacji — inaczej pierwsze
// dotknięcie po każdym rzucie byłoby brane za „zatrzymaj” i przepadało.
var activeFling *flingScroll

func stopFling() {
	if activeFling != nil {
		activeFling.halt()
	}
}

type flingScroll struct {
	container.Scroll
	vel   float32 // px/s; dodatnie = palec w dół
	lastT time.Time
	anim  *fyne.Animation
}

// vscroll: pionowe przewijanie z wybiegiem — zamiennik container.NewVScroll.
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
	// dt > 120 ms to nie ten sam ruch — zaczynamy liczyć prędkość od zera
	if dt := now.Sub(s.lastT); dt > 0 && dt < 120*time.Millisecond {
		s.vel = s.vel*velKeep + e.Dragged.DY/float32(dt.Seconds())*(1-velKeep)
	} else {
		s.vel = 0
	}
	s.lastT = now
	s.scrollBy(-e.Dragged.DY)

	// Zerowanie zdarzenia gasi wygaszającą pętlę fyne: warunek tej pętli to
	// |e.Dragged| > 2, a mnożenie zera nic nie da, więc po JEDNYM obrocie
	// wywoła DragEnd i odda wybieg naszej animacji. Zdarzenia z tapMove są
	// tworzone od nowa przy każdym ruchu palca, więc nic tu nie tracimy.
	e.Dragged = fyne.Delta{}
}

// flingDistance: ile treści dowieźć po puszczeniu palca. Znak jak w scrollBy —
// palec w dół (DY > 0) cofa rozmowę, czyli zmniejsza offset.
func flingDistance(vel float32) float32 {
	if vel > -flingMinVel && vel < flingMinVel {
		return 0 // puszczenie, nie rzut
	}
	return -vel * flingCoast
}

// flingSpan: czas wybiegu rośnie z drogą, ale ma sufit. Stały czas znaczyłby, że
// leciutkie pchnięcie listy pełznie te same 700 ms co porządny rzut.
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
			// runner zawsze domyka tickiem 1.0 — tu, a nie przez Stop(), żeby nie
			// grzebać w liście animacji z jej własnego wywołania
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

// ---- tekst przycinany do szerokości ----

// clipText: jedna linia, która sama dokłada wielokropek, gdy się nie mieści.
// canvas.Text nie umie ani zawijać, ani przycinać — stąd wcześniejsze cięcie na
// sztywną liczbę znaków, które na szerokim ekranie marnowało miejsce, a na
// wąskim i tak wypychało treść za krawędź. widget.Label umie Truncation, ale
// dokłada theme.InnerPadding() (na telefonie 13 px) z każdej strony i wiersz
// listy robił się dwa razy wyższy.
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

// MinSize: sama wysokość linii. Szerokość zostaje mała, żeby layout mógł ścisnąć
// wiersz — o przycięcie treści dba Layout.
func (r *clipRenderer) MinSize() fyne.Size {
	h := fyne.MeasureText("Ag", r.t.txt.TextSize, r.t.txt.TextStyle).Height
	return fyne.NewSize(16, h)
}

func (r *clipRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.t.txt} }
func (r *clipRenderer) Refresh()                     { r.Layout(r.t.Size()) }
func (r *clipRenderer) Destroy()                     {}

// fitText: najdłuższy prefiks, który mieści się w w pikselach. Zgadujemy długość
// z proporcji i dopiero potem poprawiamy — inaczej długa nazwa to kilkadziesiąt
// pomiarów tekstu na każdy wiersz listy.
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

// ---- przesuwana warstwa (wjeżdżanie stron) ----

// slideLayout: warstwa na pełny ekran, którą można odsunąć w poziomie.
// container.Stack sadza dzieci na (0,0) przy każdym układaniu, więc samo
// animowanie Move() nic nie da — pierwszy layout wraca wszystko na miejsce.
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

// slideDur: krótko — na telefonie każda animacja dłuższa niż ~200 ms zaczyna być
// odczuwana jako opieszałość interfejsu, nie jako płynność.
const slideDur = 190 * time.Millisecond

// slideIn wjeżdża warstwą z podanego przesunięcia do zera. W ticku ruszamy tylko
// Move (Resize zostawia layout kontenera), więc klatka to jedno przesunięcie.
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

// slidePage: strona gotowa do wjechania z boku.
func slidePage(content fyne.CanvasObject) *fyne.Container {
	return container.New(&slideLayout{}, content)
}
