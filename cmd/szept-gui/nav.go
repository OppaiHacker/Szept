package main

// Nawigacja mobilna: stos pełnoekranowych stron zamiast dialogów.
//
// Na telefonie dialog fyne to kwadrat na środku ekranu — mało miejsca, klawiatura
// go zasłania, a Wstecz go nie zdejmuje. Dlatego wszystko, co na pulpicie jest
// okienkiem, na Androidzie jest osobną stroną z paskiem tytułu i strzałką wstecz.
// u.page() rozgałęzia: mobile → strona, desktop → zwykły dialog.

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// appBar: górny pasek strony — strzałka wstecz, tytuł i opcjonalne akcje.
func (u *ui) appBar(title string, back func(), actions ...fyne.CanvasObject) fyne.CanvasObject {
	t := canvas.NewText(title, cFg)
	t.TextSize = fs(17)
	t.TextStyle = fyne.TextStyle{Bold: true}
	var left fyne.CanvasObject
	if back != nil {
		b := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), back)
		b.Importance = widget.LowImportance
		left = b
	} else {
		left = hspace(theme.Padding())
	}
	right := container.NewHBox(actions...)
	bg := canvas.NewRectangle(cSurface)
	row := container.NewBorder(nil, nil, left, right, container.NewCenter(t))
	return container.NewStack(bg, container.NewVBox(row, canvas.NewRectangle(cSep)))
}

// backdrop: nieprzezroczyste tło strony, które ZJADA dotknięcia. Samo
// prostokątne tło ich nie łapie — fyne szuka pierwszego obiektu z interfejsem
// Tappable idąc od wierzchu, więc kliknięcie w puste miejsce strony trafiałoby
// w dolny pasek zakładek leżący pod nią.
func backdrop() fyne.CanvasObject {
	return tappable(canvas.NewRectangle(cBg), func() {}, nil)
}

// pushPage kładzie stronę na wierzchu. Stack rysuje warstwy jedna na drugiej,
// więc bez własnego tła prześwitywałby ekran pod spodem.
func (u *ui) pushPage(title string, content fyne.CanvasObject, actions ...fyne.CanvasObject) {
	page := container.NewStack(backdrop(), container.NewBorder(
		u.appBar(title, func() { u.popPage() }, actions...), nil, nil, nil, content))
	u.root.Add(page)
	u.root.Refresh()
}

func (u *ui) popPage() bool {
	if u.root == nil || len(u.root.Objects) <= 1 {
		return false
	}
	u.root.Remove(u.root.Objects[len(u.root.Objects)-1])
	u.root.Refresh()
	return true
}

// popAllPages: po wejściu do innego czatu nie ma sensu wracać do stron
// poprzedniego (np. jego członków).
func (u *ui) popAllPages() {
	for u.popPage() {
	}
}

// page: to samo wywołanie działa na obu platformach — telefon dostaje stronę,
// pulpit dialog przycięty do okna.
func (u *ui) page(title string, content fyne.CanvasObject, actions ...fyne.CanvasObject) {
	if u.mobile {
		u.pushPage(title, container.NewVScroll(container.NewPadded(content)), actions...)
		return
	}
	scr := container.NewVScroll(content)
	scr.SetMinSize(u.dlgSize(440, 620))
	dialog.ShowCustom(title, "Zamknij", scr, u.w)
}

// ---- klocki stron ----

// navRow: wiersz listy ustawień — ikona, tytuł, opis, szewron.
func navRow(icon fyne.Resource, title, desc string, tap func()) fyne.CanvasObject {
	row := container.NewBorder(nil, nil,
		container.NewHBox(hspace(4), container.NewCenter(widget.NewIcon(icon)), hspace(10)),
		container.NewCenter(widget.NewIcon(theme.NavigateNextIcon())),
		container.NewCenter(twoLine(title, desc, 14, 11, 26)))
	return tappable(container.NewPadded(row), tap, nil)
}

// section: nagłówek grupy wierszy (jak w ustawieniach Discorda).
func section(title string) fyne.CanvasObject {
	t := canvas.NewText(title, cMuted)
	t.TextSize = fs(11)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewPadded(t)
}

// bigButton: główna akcja strony.
func bigButton(label string, icon fyne.Resource, tap func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tap)
	b.Importance = widget.HighImportance
	return b
}

// ---- tapowalne kontenery ----

// tapBox: dowolna zawartość, która reaguje na dotknięcie i długie przytrzymanie.
// Fyne nie ma takiego klocka, a bez niego wiersz listy musiałby być przyciskiem
// (jeden label, brak awatara). Przeciągnięcie palcem po ekranie nie odpala
// tap — driver mobilny gubi zdarzenie, gdy zaczęło się przewijanie.
type tapBox struct {
	widget.BaseWidget
	content fyne.CanvasObject
	tap     func()
	long    func()
	hover   *canvas.Rectangle
}

func tappable(content fyne.CanvasObject, tap, long func()) *tapBox {
	t := &tapBox{content: content, tap: tap, long: long}
	t.hover = canvas.NewRectangle(color.Transparent)
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(t.hover, t.content))
}

func (t *tapBox) Tapped(*fyne.PointEvent) {
	if t.tap != nil {
		t.flash()
		t.tap()
	}
}

func (t *tapBox) TappedSecondary(*fyne.PointEvent) {
	if t.long != nil {
		t.flash()
		t.long()
	}
}

// flash: krótkie rozjaśnienie zamiast stanu wciśnięcia — na dotyku i tak nie ma
// kursora, a potwierdzenie dotknięcia jest potrzebne.
func (t *tapBox) flash() {
	t.hover.FillColor = color.NRGBA{0xff, 0xff, 0xff, 0x1a}
	t.hover.Refresh()
	a := canvas.NewColorRGBAAnimation(color.NRGBA{0xff, 0xff, 0xff, 0x1a},
		color.NRGBA{0xff, 0xff, 0xff, 0x00}, 250*msec, func(c color.Color) {
			t.hover.FillColor = c
			t.hover.Refresh()
		})
	a.Start()
}

// ---- dolna nawigacja ----

// navTab: jedna zakładka dolnego paska. Własna, bo container.AppTabs na dole
// daje wąskie, słabo widoczne cele dotykowe i nie umie odznaki.
type navTab struct {
	icon  fyne.Resource
	label string
	badge int
	img   *canvas.Image
	txt   *canvas.Text
	dot   *fyne.Container
	dotN  *canvas.Text
}

func (u *ui) buildBottomNav(labels []string, icons []fyne.Resource) fyne.CanvasObject {
	bg := canvas.NewRectangle(cSurface)
	row := container.NewGridWithColumns(len(labels))
	u.tabs = nil
	for i := range labels {
		i := i
		t := &navTab{icon: icons[i], label: labels[i]}
		t.img = canvas.NewImageFromResource(theme.NewThemedResource(icons[i]))
		t.img.FillMode = canvas.ImageFillContain
		t.img.SetMinSize(fyne.NewSize(22, 22))
		t.txt = canvas.NewText(labels[i], cMuted)
		t.txt.TextSize = fs(10)
		t.txt.Alignment = fyne.TextAlignCenter
		t.dotN = canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xff})
		t.dotN.TextSize = fs(10)
		t.dotN.TextStyle = fyne.TextStyle{Bold: true}
		badgeBg := canvas.NewRectangle(cRed)
		badgeBg.CornerRadius = 8
		t.dot = container.NewGridWrap(fyne.NewSize(18, 16),
			container.NewStack(badgeBg, container.NewCenter(t.dotN)))
		t.dot.Hide()
		icon := container.NewStack(container.NewCenter(container.NewGridWrap(
			fyne.NewSize(22, 22), t.img)),
			container.NewHBox(layout.NewSpacer(), container.NewVBox(t.dot, layout.NewSpacer())))
		cell := container.NewPadded(container.NewVBox(icon, t.txt))
		row.Add(tappable(cell, func() { u.selectTab(i) }, nil))
		u.tabs = append(u.tabs, t)
	}
	return container.NewStack(bg, container.NewVBox(canvas.NewRectangle(cSep), row))
}

// selectTab przełącza zakładkę główną (dolny pasek) i zdejmuje strony ze stosu.
func (u *ui) selectTab(i int) {
	if i < 0 || i >= len(u.tabViews) {
		return
	}
	u.popAllPages()
	u.tab = i
	u.tabBody.Objects = []fyne.CanvasObject{u.tabViews[i]}
	u.tabBody.Refresh()
	for j, t := range u.tabs {
		if j == i {
			t.txt.Color = cAccent
			t.img.Resource = theme.NewPrimaryThemedResource(t.icon)
		} else {
			t.txt.Color = cMuted
			t.img.Resource = theme.NewThemedResource(t.icon)
		}
		t.txt.Refresh()
		t.img.Refresh()
	}
	u.checkClipboard()
	u.refresh()
}

// setTabBadge: liczba nieprzeczytanych na ikonie zakładki.
func (u *ui) setTabBadge(i, n int) {
	if i < 0 || i >= len(u.tabs) {
		return
	}
	t := u.tabs[i]
	if n <= 0 {
		t.dot.Hide()
		return
	}
	s := "9+"
	if n < 10 {
		s = string(rune('0' + n))
	}
	t.dotN.Text = s
	t.dotN.Refresh()
	t.dot.Show()
}
