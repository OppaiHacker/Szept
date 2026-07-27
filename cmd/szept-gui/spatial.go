package main

// Scena 3D rozmowy: pokój w rzucie perspektywicznym, po którym przeciąga się
// uczestników. Pozycja steruje miksem na żywo: dalej = ciszej, w lewo/prawo =
// panorama stereo. Dotknięcie osoby otwiera jej suwak głośności i wyciszenie.
// Słuchacz („Ty”) stoi na dole sceny i się nie rusza — świat kręci się wokół.

import (
	"fmt"
	"math"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// spatialAudio: pozycja → miks. dx ∈ [-1,1] (lewo/prawo od słuchacza),
// depth ∈ [0,1] (0 = przy uchu, 1 = pod horyzontem). Czysta funkcja — test niżej.
func spatialAudio(dx, depth float32) (gain, pan float32) {
	// boki liczą się słabiej niż głębia — przesunięcie w lewo ma panoramować,
	// a nie od razu wyciszać
	dist := float32(math.Sqrt(float64(dx*dx*0.35 + depth*depth)))
	if dist > 1 {
		dist = 1
	}
	gain = 1 - 0.85*dist
	pan = dx * 0.8
	if pan > 1 {
		pan = 1
	} else if pan < -1 {
		pan = -1
	}
	return gain, pan
}

// ---- pionek uczestnika ----

type spToken struct {
	widget.BaseWidget
	sc     *scene3d
	pub    string
	name   string
	nx, ny float32 // pozycja na scenie, 0..1 (ny: 0 = horyzont, 1 = przy słuchaczu)
	fixed  bool    // słuchacz — nie do ruszenia

	circle  *canvas.Circle
	initial *canvas.Text
	label   *canvas.Text
	stat    *canvas.Text
}

func newToken(sc *scene3d, pub []byte, name string, fixed bool) *spToken {
	t := &spToken{sc: sc, pub: string(pub), name: name, fixed: fixed}
	t.circle = canvas.NewCircle(authorColor(pub))
	if fixed {
		t.circle.FillColor = cGreen
	}
	letter := "?"
	for _, r := range name {
		letter = string(r)
		break
	}
	t.initial = canvas.NewText(letter, cBg)
	t.initial.TextStyle = fyne.TextStyle{Bold: true}
	t.initial.Alignment = fyne.TextAlignCenter
	t.label = canvas.NewText(name, cFg)
	t.label.Alignment = fyne.TextAlignCenter
	t.stat = canvas.NewText("", cMuted)
	t.stat.Alignment = fyne.TextAlignCenter
	t.ExtendBaseWidget(t)
	return t
}

func (t *spToken) CreateRenderer() fyne.WidgetRenderer { return &tokenRenderer{t: t} }

func (t *spToken) Tapped(*fyne.PointEvent) {
	if !t.fixed {
		t.sc.selectTok(t)
	}
}

func (t *spToken) Dragged(e *fyne.DragEvent) {
	if t.fixed || t.sc.size.Width == 0 {
		return
	}
	t.nx = clampf(t.nx+e.Dragged.DX/t.sc.size.Width, 0, 1)
	t.ny = clampf(t.ny+e.Dragged.DY/t.sc.stageSpan(), 0, 1)
	t.sc.place(t)
	t.sc.applyAudio(t)
}

func (t *spToken) DragEnd() { t.sc.restack() }

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

type tokenRenderer struct{ t *spToken }

// Layout: kółko u góry, pod nim nazwa i stan. Średnica = szerokość pionka —
// scena skaluje pionek z głębią, więc wszystko liczy się od bieżącego rozmiaru.
func (r *tokenRenderer) Layout(s fyne.Size) {
	d := s.Width
	r.t.circle.Resize(fyne.NewSize(d, d))
	r.t.circle.Move(fyne.NewPos(0, 0))
	r.t.initial.TextSize = d * 0.42
	r.t.initial.Resize(fyne.NewSize(d, d))
	r.t.initial.Move(fyne.NewPos(0, d/2-r.t.initial.MinSize().Height/2))
	// podpis bywa szerszy od kółka — przycinamy go do 2× średnicy, inaczej
	// sąsiednie pionki nachodzą na siebie napisami
	r.t.label.TextSize = fs(10)
	r.t.label.Text = fitText(r.t.name, r.t.label.TextSize, r.t.label.TextStyle, d*2)
	r.t.label.Resize(fyne.NewSize(d, 13))
	r.t.label.Move(fyne.NewPos(0, d+1))
	r.t.stat.TextSize = fs(9)
	r.t.stat.Resize(fyne.NewSize(d, 12))
	r.t.stat.Move(fyne.NewPos(0, d+14))
}

func (r *tokenRenderer) MinSize() fyne.Size { return fyne.NewSize(24, 40) }
func (r *tokenRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.t.circle, r.t.initial, r.t.label, r.t.stat}
}
func (r *tokenRenderer) Refresh() { r.Layout(r.t.Size()) }
func (r *tokenRenderer) Destroy() {}

// ---- scena ----

const (
	spRadial = 9 // linie zbiegające do punktu zbiegu
	spDepth  = 6 // poziome linie głębi
)

type scene3d struct {
	widget.BaseWidget
	u    *ui
	chat []byte
	size fyne.Size

	bg    *canvas.Rectangle
	sky   *canvas.Rectangle
	lines []*canvas.Line
	me    *spToken
	toks  map[string]*spToken
	sel   *spToken

	// per-osoba dodatkowy mnożnik głośności i wyciszenie (sesyjne)
	gains map[string]float64
	mutes map[string]bool

	panelName *widget.Label
	panelVol  *widget.Slider
	panelMute *widget.Button
	panelHint fyne.CanvasObject
	panelBody *fyne.Container
}

func newScene3d(u *ui, chat []byte) *scene3d {
	sc := &scene3d{
		u: u, chat: chat,
		toks:  map[string]*spToken{},
		gains: map[string]float64{},
		mutes: map[string]bool{},
	}
	sc.bg = canvas.NewRectangle(cDeep)
	sc.sky = canvas.NewRectangle(cSurface)
	for i := 0; i < spRadial+spDepth+1; i++ {
		l := canvas.NewLine(cSep)
		l.StrokeWidth = 1
		sc.lines = append(sc.lines, l)
	}
	sc.me = newToken(sc, u.c.ID.Pub, "Ty", true)
	sc.me.nx, sc.me.ny = 0.5, 1
	sc.ExtendBaseWidget(sc)
	sc.sync()
	return sc
}

func (sc *scene3d) CreateRenderer() fyne.WidgetRenderer { return &sceneRenderer{sc: sc} }

type sceneRenderer struct{ sc *scene3d }

func (r *sceneRenderer) MinSize() fyne.Size { return fyne.NewSize(300, 280) }
func (r *sceneRenderer) Layout(s fyne.Size) { r.sc.layout(s) }
func (r *sceneRenderer) Refresh()           { r.sc.layout(r.sc.Size()) }
func (r *sceneRenderer) Destroy()           {}

func (r *sceneRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.sc.bg, r.sc.sky}
	for _, l := range r.sc.lines {
		objs = append(objs, l)
	}
	// dalsi rysują się pierwsi — bliżsi przykrywają ich jak w prawdziwym pokoju
	var ts []*spToken
	for _, t := range r.sc.toks {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].ny < ts[j].ny })
	for _, t := range ts {
		objs = append(objs, t)
	}
	return append(objs, r.sc.me)
}

// horizon: linia, nad którą jest „ściana” pokoju.
func (sc *scene3d) horizon() float32 { return sc.size.Height * 0.16 }

// stageSpan: piksele głębi między horyzontem a słuchaczem.
func (sc *scene3d) stageSpan() float32 { return sc.size.Height - sc.horizon() - 76 }

func (sc *scene3d) layout(s fyne.Size) {
	if s.Width <= 0 {
		return
	}
	sc.size = s
	hz := sc.horizon()
	sc.bg.Resize(s)
	sc.bg.Move(fyne.NewPos(0, 0))
	sc.sky.Resize(fyne.NewSize(s.Width, hz))
	sc.sky.Move(fyne.NewPos(0, 0))

	// punkt zbiegu na środku horyzontu; podłoga to wachlarz linii + poziome
	// linie głębi zagęszczające się przy horyzoncie (kwadratowa perspektywa).
	// Wachlarz sięga daleko poza ekran, żeby skrajne linie wychodziły bokiem,
	// a nie zbiegały się w dolnych rogach.
	vp := fyne.NewPos(s.Width/2, hz)
	li := 0
	for i := 0; i < spRadial; i++ {
		f := float32(i)/float32(spRadial-1)*2 - 0.5 // -0.5 .. 1.5
		l := sc.lines[li]
		li++
		l.Position1 = vp
		l.Position2 = fyne.NewPos(f*s.Width, s.Height)
	}
	for i := 0; i < spDepth; i++ {
		f := float32(i+1) / float32(spDepth)
		y := hz + f*f*(s.Height-hz)
		l := sc.lines[li]
		li++
		l.Position1 = fyne.NewPos(0, y)
		l.Position2 = fyne.NewPos(s.Width, y)
	}
	hl := sc.lines[li]
	hl.Position1 = fyne.NewPos(0, hz)
	hl.Position2 = fyne.NewPos(s.Width, hz)
	hl.StrokeColor = cAccent
	hl.StrokeWidth = 2

	for _, t := range sc.toks {
		sc.place(t)
	}
	sc.place(sc.me)
}

// place: pozycja znormalizowana → piksele; skala rośnie z bliskością (głębia).
func (sc *scene3d) place(t *spToken) {
	if sc.size.Width <= 0 {
		return
	}
	scale := 0.45 + 0.55*t.ny
	d := 46 * scale
	if t.fixed {
		d = 50
	}
	t.Resize(fyne.NewSize(d, d+28))
	t.Move(fyne.NewPos(t.nx*sc.size.Width-d/2, sc.horizon()+t.ny*sc.stageSpan()-d/2))
}

// restack: po przeciągnięciu poprawia kolejność rysowania (bliżsi na wierzchu).
func (sc *scene3d) restack() { sc.Refresh() }

// sync: dosypuje pionki nowych uczestników, zdejmuje tych, co wyszli.
func (sc *scene3d) sync() {
	call := sc.u.c.CallState(sc.chat)
	want := map[string]string{} // pub -> name
	if call != nil {
		st, err := sc.u.c.ChatState(sc.chat)
		for pub := range call.Members {
			name := "?"
			if err == nil {
				if m, ok := st.Members[pub]; ok {
					name = m.Name
				}
			}
			want[pub] = name
		}
	}
	changed := false
	for pub := range sc.toks {
		if _, ok := want[pub]; !ok {
			if sc.sel == sc.toks[pub] {
				sc.selectTok(nil)
			}
			delete(sc.toks, pub)
			changed = true
		}
	}
	i := len(sc.toks)
	for pub, name := range want {
		if _, ok := sc.toks[pub]; ok {
			continue
		}
		t := newToken(sc, []byte(pub), name, false)
		sc.arrangeOne(t, i, len(want))
		sc.toks[pub] = t
		sc.gains[pub] = 1
		sc.applyAudio(t)
		i++
		changed = true
	}
	if changed {
		if sc.panelBody != nil && sc.sel == nil {
			sc.panelHint = mutedWrap(sc.hintText())
			sc.selectTok(nil)
		}
		sc.Refresh()
	}
}

// arrangeOne: półokrąg przed słuchaczem — jak „All Around Me”, tylko od razu.
func (sc *scene3d) arrangeOne(t *spToken, i, n int) {
	if n < 1 {
		n = 1
	}
	th := (float64(i)/float64(n) - 0.5) * math.Pi * 0.7 // wachlarz ±63°
	t.nx = float32(0.5 + 0.42*math.Sin(th))
	t.ny = float32(1 - 0.55*math.Cos(th))
}

func (sc *scene3d) arrangeAll() {
	i, n := 0, len(sc.toks)
	for _, t := range sc.toks {
		sc.arrangeOne(t, i, n)
		sc.applyAudio(t)
		i++
	}
	sc.Refresh()
}

// applyAudio: pozycja + suwak + mute → mikser. Etykieta pokazuje efekt.
func (sc *scene3d) applyAudio(t *spToken) {
	dx := (t.nx - 0.5) * 2
	depth := 1 - t.ny
	gain, pan := spatialAudio(dx, depth)
	eff := float64(gain) * sc.gains[t.pub]
	sc.u.c.SetVoiceLevel([]byte(t.pub), float32(eff), pan)
	sc.u.c.MuteVoicePeer([]byte(t.pub), sc.mutes[t.pub])
	if sc.mutes[t.pub] {
		t.stat.Text = "wyciszony"
		t.stat.Color = cRed
	} else {
		t.stat.Text = fmt.Sprintf("%d%%", int(eff*100))
		t.stat.Color = cMuted
	}
	t.stat.Refresh()
}

// selectTok: podświetlenie + panel z suwakiem dla wybranej osoby.
func (sc *scene3d) selectTok(t *spToken) {
	if sc.sel != nil {
		sc.sel.circle.StrokeWidth = 0
		sc.sel.circle.Refresh()
	}
	sc.sel = t
	if t == nil {
		sc.panelBody.Objects = []fyne.CanvasObject{sc.panelHint}
		sc.panelBody.Refresh()
		return
	}
	t.circle.StrokeColor = cAccent
	t.circle.StrokeWidth = 3
	t.circle.Refresh()
	sc.panelName.SetText(t.label.Text)
	sc.panelVol.Value = sc.gains[t.pub] * 100
	sc.panelVol.Refresh()
	sc.updateMuteBtn()
	sc.panelBody.Objects = []fyne.CanvasObject{container.NewVBox(
		container.NewBorder(nil, nil, sc.panelName, sc.panelMute, sc.panelVol))}
	sc.panelBody.Refresh()
}

func (sc *scene3d) hintText() string {
	if len(sc.toks) == 0 {
		return "Nikt jeszcze nie dołączył do rozmowy — pionki pojawią się tu same, " +
			"gdy ktoś wejdzie."
	}
	return "Przeciągaj osoby po pokoju: dalej = ciszej, w bok = stereo. " +
		"Dotknij osoby, żeby dostać jej suwak i wyciszenie."
}

func (sc *scene3d) updateMuteBtn() {
	if sc.sel == nil {
		return
	}
	if sc.mutes[sc.sel.pub] {
		sc.panelMute.SetIcon(theme.VolumeMuteIcon())
	} else {
		sc.panelMute.SetIcon(theme.VolumeUpIcon())
	}
}

// panel: sterowanie wybraną osobą + akcje sceny.
func (sc *scene3d) panel() fyne.CanvasObject {
	sc.panelName = widget.NewLabel("")
	sc.panelName.TextStyle = fyne.TextStyle{Bold: true}
	sc.panelVol = widget.NewSlider(0, 200)
	sc.panelVol.Step = 5
	sc.panelVol.OnChanged = func(v float64) {
		if sc.sel != nil {
			sc.gains[sc.sel.pub] = v / 100
			sc.applyAudio(sc.sel)
		}
	}
	sc.panelMute = widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		if sc.sel == nil {
			return
		}
		sc.mutes[sc.sel.pub] = !sc.mutes[sc.sel.pub]
		sc.updateMuteBtn()
		sc.applyAudio(sc.sel)
	})
	sc.panelHint = mutedWrap(sc.hintText())
	sc.panelBody = container.NewStack(sc.panelHint)
	arrange := widget.NewButtonWithIcon("Wokół mnie", theme.ViewRefreshIcon(), sc.arrangeAll)
	return container.NewVBox(
		container.NewBorder(nil, nil, nil, arrange, sc.panelBody))
}

// ---- wejście z panelu rozmowy ----

func (u *ui) spatialPage() {
	ch := u.cur()
	if ch == nil {
		return
	}
	call := u.c.CallState(ch.ID)
	if call == nil || !call.Joined {
		u.toast("Najpierw dołącz do rozmowy")
		return
	}
	sc := newScene3d(u, ch.ID)
	u.sp = sc
	body := container.NewBorder(nil, container.NewPadded(sc.panel()), nil, nil, sc)
	if u.mobile {
		u.spPage = slidePage(container.NewStack(backdrop(), container.NewBorder(
			u.appBar("Scena 3D", func() { u.popPage(); u.sp = nil }), nil, nil, nil, body)))
		u.root.Add(u.spPage)
		u.root.Refresh()
		slideIn(u.spPage, u.w.Canvas().Size().Width)
		return
	}
	w := u.a.NewWindow("szept — scena 3D")
	w.Resize(fyne.NewSize(780, 560))
	w.SetContent(body)
	w.SetOnClosed(func() { u.sp = nil })
	w.Show()
}

// spatialSync: refresh() woła to przy każdej zmianie stanu — scena dosypuje
// i zdejmuje pionki, gdy ktoś dołącza/wychodzi. Nil-safe.
func (u *ui) spatialSync() {
	if u.sp == nil {
		return
	}
	if u.mobile && u.spPage != nil && !u.onStack(u.spPage) {
		u.sp, u.spPage = nil, nil // strona zdjęta Wstecz — scena umarła z nią
		return
	}
	u.sp.sync()
}
