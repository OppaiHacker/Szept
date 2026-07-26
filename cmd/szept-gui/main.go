// szept-gui: klient Fyne na Linux/Windows/Android.
//
// Android jest tu platformą pierwszą: dolny pasek zakładek, pełnoekranowe
// strony zamiast dialogów, dotykowe wiersze list (patrz nav.go, chat.go,
// profile.go). Pulpit dostaje ten sam kod z gałęziami !u.mobile.
package main

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	mobiledrv "fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/identity"
)

//go:embed Icon.png
var iconPNG []byte

// msec: skrót na czas animacji (canvas.New*Animation bierze time.Duration).
const msec = time.Millisecond

// ---- paleta ----

var (
	cBg       = color.NRGBA{0x10, 0x12, 0x18, 0xff} // tło
	cSurface  = color.NRGBA{0x1a, 0x1d, 0x26, 0xff} // panele, inputy
	cSurface2 = color.NRGBA{0x23, 0x27, 0x36, 0xff} // przyciski, dymki innych
	cBubbleMe = color.NRGBA{0x45, 0x3c, 0x78, 0xff} // dymki własne
	cAccent   = color.NRGBA{0x8b, 0x7c, 0xf6, 0xff} // fiolet szeptu
	cFg       = color.NRGBA{0xe9, 0xeb, 0xf2, 0xff}
	cMuted    = color.NRGBA{0x8a, 0x91, 0xa3, 0xff}
	cSep      = color.NRGBA{0x26, 0x2b, 0x38, 0xff}
	cGreen    = color.NRGBA{0x4a, 0xde, 0x80, 0xff}
	cOrange   = color.NRGBA{0xfb, 0xbf, 0x24, 0xff}
	cRed      = color.NRGBA{0xf8, 0x71, 0x71, 0xff}
)

// kolory autorów w grupach (deterministyczne po pubkey)
var authorPalette = []color.NRGBA{
	{0x7d, 0xd3, 0xfc, 0xff}, {0xf9, 0xa8, 0xd4, 0xff}, {0x86, 0xef, 0xac, 0xff},
	{0xfd, 0xe0, 0x47, 0xff}, {0xfd, 0xba, 0x74, 0xff}, {0xc4, 0xb5, 0xfd, 0xff},
}

func authorColor(pub []byte) color.NRGBA {
	if len(pub) == 0 {
		return cMuted
	}
	return authorPalette[int(pub[0])%len(authorPalette)]
}

// mobileUI: ustawiane raz w main(); tekst i dotykowe rozmiary skalują się w górę.
var mobileUI bool

// fs: rozmiar tekstu — na telefonie +2, żeby drobne podpisy były czytelne
func fs(s float32) float32 {
	if mobileUI {
		return s + 2
	}
	return s
}

// ---- theme ----

type szTheme struct{ base fyne.Theme }

func (t szTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return cBg
	case theme.ColorNameForeground:
		return cFg
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return cAccent
	case theme.ColorNameButton:
		return cSurface2
	case theme.ColorNameInputBackground:
		return cSurface
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return cSep
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground, theme.ColorNameHeaderBackground:
		return cSurface
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return cMuted
	case theme.ColorNameHover:
		return color.NRGBA{0xff, 0xff, 0xff, 0x0c}
	case theme.ColorNamePressed:
		return color.NRGBA{0x00, 0x00, 0x00, 0x30}
	case theme.ColorNameSelection:
		return color.NRGBA{0x8b, 0x7c, 0xf6, 0x50}
	case theme.ColorNameSuccess:
		return cGreen
	case theme.ColorNameWarning:
		return cOrange
	case theme.ColorNameError:
		return cRed
	case theme.ColorNameScrollBar:
		return color.NRGBA{0xff, 0xff, 0xff, 0x28}
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{0xff, 0xff, 0xff, 0xff}
	}
	return t.base.Color(n, theme.VariantDark)
}

func (t szTheme) Font(s fyne.TextStyle) fyne.Resource { return t.base.Font(s) }
func (t szTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(n)
}

func (t szTheme) Size(n fyne.ThemeSizeName) float32 {
	// mobile: większy tekst i padding — cele dotykowe ~44dp, czytelność na małym ekranie
	m := func(desktop, mobile float32) float32 {
		if mobileUI {
			return mobile
		}
		return desktop
	}
	switch n {
	case theme.SizeNamePadding:
		return m(5, 6)
	case theme.SizeNameInnerPadding:
		return m(10, 13)
	case theme.SizeNameText:
		return m(14, 16)
	case theme.SizeNameCaptionText:
		return m(12, 13)
	case theme.SizeNameHeadingText:
		return m(22, 24)
	case theme.SizeNameSubHeadingText:
		return m(16, 18)
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return 10
	case theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameCardRadius, theme.SizeNameDialogRadius:
		return 14
	}
	return t.base.Size(n)
}

// ---- ui ----

type chatRow struct {
	info   core.ChatInfo
	unread int
}

// chatPrev: podgląd ostatniej wiadomości na liście czatów. Trzymamy go w cache
// pod liczbą zdarzeń — inaczej każde odświeżenie odszyfrowywałoby całą historię
// każdego czatu, a to na telefonie widać.
type chatPrev struct {
	n    int
	text string
	ts   time.Time
}

type ui struct {
	a       fyne.App
	w       fyne.Window
	c       *core.Core
	cfg     *core.Config
	cfgFile string
	mobile  bool

	// mobilna powłoka: root to Stack, gdzie [0] jest ekranem głównym,
	// a kolejne warstwy to pełnoekranowe strony (nav.go)
	root     *fyne.Container
	tabBody  *fyne.Container
	tabViews []fyne.CanvasObject
	tabs     []*navTab
	tab      int
	convPage fyne.CanvasObject // strona rozmowy (mobile)

	rows      []chatRow
	sel       int
	chatBox   *fyne.Container    // mobile: dotykowe wiersze czatów
	chatLst   *widget.List       // desktop: lista czatów
	friendBox *fyne.Container    // lista znajomych
	meBox     *fyne.Container    // zakładka „Ty” (profil + ustawienia)
	nav       *container.AppTabs // desktop: Czaty | Znajomi
	msgBox    *fyne.Container
	msgScr    *container.Scroll
	center    *fyne.Container   // desktop: przełącza welcome <-> rozmowa
	convRoot  fyne.CanvasObject // widok rozmowy

	title      *canvas.Text
	subtitle   *canvas.Text
	headAvatar *fyne.Container
	callBtn    *widget.Button
	micBtn     *widget.Button
	camBtn     *widget.Button
	scrBtn     *widget.Button

	banner     *fyne.Container
	bannerLbl  *canvas.Text
	bannerDot  *canvas.Circle
	bannerAnim *fyne.Animation
	callBar    *fyne.Container // mic/kamera/ekran/rozłącz — tylko w trakcie rozmowy

	entry   *cmdEntry       // pole wiadomości; "/" odpala komendy (commands.go)
	hintBox *fyne.Container // podpowiedzi komend nad polem
	hints   []cmdDef        // aktualnie dopasowane
	hintSel int             // podświetlona podpowiedź
	sys     map[string][]sysMsg

	clipBar  *fyne.Container // "wykryto kod w schowku"
	clipLbl  *canvas.Text
	clipCode string
	clipSeen string // kod już odrzucony/użyty — nie pokazuj drugi raz

	statusDot *canvas.Circle
	statusLbl *canvas.Text
	statusBar *fyne.Container

	seen   map[string]int      // chatID -> liczba eventów przy ostatnim obejrzeniu
	unread map[string]int      // chatID -> badge z poprzedniego odświeżenia
	prev   map[string]chatPrev // chatID -> podgląd ostatniej wiadomości

	shown     int    // ile wiadomości rysujemy w otwartym czacie
	grew      bool   // właśnie doładowaliśmy starsze — przewijanie ma zostać na miejscu
	msgSig    string // odcisk narysowanej rozmowy — bez zmian nie ruszamy przewijania
	lastCount int    // wiadomości w wybranym czacie (animacja nowej)
	lastChat  string // do wykrycia zmiany czatu (bez fałszywego flasha)
	booted    bool

	vidWin  fyne.Window
	vidDlg  dialog.Dialog // mobile: dialog zamiast drugiego okna
	vidGrid *fyne.Container
	vidImgs map[string]*canvas.Image // peer+kind -> image

	anims    []*anim        // GIF-y awatarów — lista ginie z każdym odświeżeniem list
	msgAnims []*anim        // GIF-y w dymkach — żyją, dopóki rozmowa nie jest rysowana od nowa
	decoded  map[string]any // cache rozpakowanych obrazków, klucz = skrót bloba
}

func main() {
	dataFlag := flag.String("data", "", "katalog danych")
	connFlag := flag.String("connector", "", "adres connectora")
	flag.Parse()

	// stałe ID = katalog Preferences i tożsamość aplikacji. To samo ID siedzi
	// w FyneApp.toml, więc na Androidzie nic się nie przenosi.
	a := app.NewWithID("chat.szept")
	// ikona okna: X11 i Windows biorą ją stąd. Wayland nie ma takiego protokołu —
	// tam ikona idzie z app_id + pliku .desktop, a app_id fyne nie ustawia.
	a.SetIcon(fyne.NewStaticResource("icon.png", iconPNG))
	mobileUI = fyne.CurrentDevice().IsMobile()
	a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})
	w := a.NewWindow("szept")
	if !mobileUI {
		w.Resize(fyne.NewSize(1060, 680))
	}

	cfgFile, dataDir := boot.Dirs()
	if mobileUI {
		// scoped storage: tylko katalog aplikacji jest zapisywalny
		root := a.Storage().RootURI().Path()
		cfgFile = filepath.Join(root, "config.json")
		dataDir = filepath.Join(root, "data")
	}
	if *dataFlag != "" {
		dataDir = *dataFlag
	}
	cfg, _ := boot.LoadConfig(cfgFile)
	if cfg == nil {
		cfg = &core.Config{}
	}
	if *connFlag != "" {
		cfg.Connector = *connFlag
	}
	cfg.DataDir = dataDir

	u := &ui{
		a: a, w: w, cfg: cfg, cfgFile: cfgFile,
		mobile:  mobileUI,
		vidImgs: map[string]*canvas.Image{},
		decoded: map[string]any{},
		seen:    map[string]int{}, unread: map[string]int{},
		prev: map[string]chatPrev{},
		sys:  map[string][]sysMsg{},
	}

	start := func(id *identity.Identity) {
		c, err := core.Open(*cfg, id)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		u.c = c
		c.Start()
		u.buildMain()
		go u.watch()
		go u.statusTicker()
		c.OnVideoFrame = u.onFrame
	}

	if !boot.HasIdentity(dataDir) {
		u.setupScreen(cfgFile, start)
	} else {
		if id, err := boot.LoadIdentity(dataDir, nil); err == nil {
			if cfg.Connector == "" {
				u.askConnector(cfgFile, func() { start(id) })
			} else {
				start(id)
			}
		} else {
			u.passScreen(dataDir, start)
		}
	}
	w.ShowAndRun()
	if u.c != nil {
		u.c.Close()
	}
}

// ---- małe klocki ----

func brand(size float32) *canvas.Text {
	t := canvas.NewText("szept", cAccent)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

func mutedText(s string, size float32) *canvas.Text {
	t := canvas.NewText(s, cMuted)
	t.TextSize = fs(size)
	return t
}

// mutedWrap: szary opis, który zawija się na wąskim ekranie (canvas.Text nie umie)
func mutedWrap(s string) *widget.RichText {
	t := widget.NewRichText(&widget.TextSegment{
		Text: s,
		Style: widget.RichTextStyle{
			ColorName: theme.ColorNamePlaceHolder,
			SizeName:  theme.SizeNameCaptionText,
		},
	})
	t.Wrapping = fyne.TextWrapWord
	return t
}

// startScaffold: desktop centruje kartę; telefon dostaje pełną szerokość
// i przewijanie (klawiatura), bo Center pozwala treści wystawać poza ekran
func (u *ui) startScaffold(content fyne.CanvasObject) fyne.CanvasObject {
	if u.mobile {
		return container.NewVScroll(container.NewPadded(content))
	}
	return container.NewCenter(content)
}

func hspace(w float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, 1))
	return r
}

func vspace(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(1, h))
	return r
}

// card: zaokrąglony panel na ciemniejszym tle
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = 14
	return container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
}

// ---- ekrany startowe ----

func (u *ui) setupScreen(cfgFile string, done func(*identity.Identity)) {
	name := widget.NewEntry()
	name.PlaceHolder = "np. ala"
	conn := widget.NewEntry()
	conn.PlaceHolder = "ws://1.2.3.4:7777"
	conn.SetText(u.cfg.Connector)
	pass := widget.NewPasswordEntry()
	pass.PlaceHolder = "(opcjonalne)"
	form := widget.NewForm(
		widget.NewFormItem("Nazwa", name),
		widget.NewFormItem("Connector", conn),
		widget.NewFormItem("Hasło klucza", pass),
	)
	form.SubmitText = "Zaczynamy"
	form.OnSubmit = func() {
		if name.Text == "" || conn.Text == "" {
			dialog.ShowInformation("szept", "Podaj nazwę i adres connectora.", u.w)
			return
		}
		u.cfg.Connector = core.NormalizeWS(conn.Text)
		id, err := boot.CreateIdentity(u.cfg.DataDir, name.Text, []byte(pass.Text))
		if err != nil {
			dialog.ShowError(err, u.w)
			return
		}
		boot.SaveConfig(cfgFile, u.cfg)
		done(id)
	}
	u.w.SetContent(u.startScaffold(container.NewVBox(
		container.NewCenter(brand(34)),
		mutedWrap("szyfrowany komunikator P2P — bez serwera, bez śladów"),
		vspace(14),
		card(container.NewVBox(
			widget.NewLabelWithStyle("Pierwsze uruchomienie", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			form,
			mutedWrap("Connector to lekki serwer spotkań — np. ws://twoj-vps:7777"),
		)),
	)))
}

func (u *ui) passScreen(dataDir string, done func(*identity.Identity)) {
	pass := widget.NewPasswordEntry()
	pass.PlaceHolder = "hasło klucza"
	msg := canvas.NewText("", cRed)
	msg.TextSize = fs(12)
	try := func() {
		id, err := boot.LoadIdentity(dataDir, []byte(pass.Text))
		if err != nil {
			msg.Text = "Złe hasło — spróbuj jeszcze raz."
			msg.Refresh()
			return
		}
		done(id)
	}
	pass.OnSubmitted = func(string) { try() }
	unlock := bigButton("Odblokuj", theme.LoginIcon(), try)
	u.w.SetContent(u.startScaffold(container.NewVBox(
		container.NewCenter(brand(34)),
		vspace(14),
		card(container.NewVBox(
			widget.NewLabelWithStyle("Klucz tożsamości jest zaszyfrowany", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			pass, unlock, msg,
		)),
	)))
}

func (u *ui) askConnector(cfgFile string, done func()) {
	conn := widget.NewEntry()
	conn.PlaceHolder = "ws://1.2.3.4:7777"
	save := bigButton("Zapisz i połącz", theme.ConfirmIcon(), func() {
		if conn.Text == "" {
			return
		}
		u.cfg.Connector = core.NormalizeWS(conn.Text)
		boot.SaveConfig(cfgFile, u.cfg)
		done()
	})
	conn.OnSubmitted = func(string) { save.OnTapped() }
	u.w.SetContent(u.startScaffold(container.NewVBox(
		container.NewCenter(brand(34)),
		vspace(14),
		card(container.NewVBox(
			widget.NewLabelWithStyle("Podaj adres connectora", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			conn, save,
			mutedWrap("Znajdziesz go u osoby, która hostuje wasz serwer spotkań."),
		)),
	)))
}

// ---- główny układ ----

func (u *ui) buildMain() {
	u.buildConv()   // wspólny widok rozmowy (chat.go)
	u.startAnims()  // jeden zegar na wszystkie GIF-y (media.go)
	u.buildStatus() // pasek połączenia
	u.buildClipBar()

	// zakładka Znajomi
	u.friendBox = container.NewVBox()
	addFriend := bigButton("Dodaj znajomego", theme.ContentAddIcon(), u.codePage)
	friendsTab := container.NewBorder(nil, container.NewPadded(addFriend), nil, nil,
		container.NewVScroll(u.friendBox))

	if u.mobile {
		u.buildMobileShell(friendsTab)
	} else {
		u.buildDesktopShell(friendsTab)
	}
	u.refresh()
	u.updateStatus()
	u.checkClipboard()
	u.a.Lifecycle().SetOnEnteredForeground(func() { fyne.Do(u.checkClipboard) })
}

// buildMobileShell: dolne zakładki Czaty | Znajomi | Ty i stos stron nad nimi.
func (u *ui) buildMobileShell(friendsTab fyne.CanvasObject) {
	u.chatBox = container.NewVBox()
	chatsTab := container.NewBorder(
		container.NewPadded(container.NewGridWithColumns(2,
			widget.NewButtonWithIcon("Nowa grupa", theme.ContentAddIcon(), u.newGroupPage),
			widget.NewButtonWithIcon("Mam kod", theme.LoginIcon(), u.codePage))),
		nil, nil, nil, container.NewVScroll(u.chatBox))

	u.tabViews = []fyne.CanvasObject{chatsTab, friendsTab, u.mePage()}
	u.tabBody = container.NewStack(u.tabViews[0])
	nav := u.buildBottomNav(
		[]string{"Czaty", "Znajomi", "Ty"},
		[]fyne.Resource{theme.MailComposeIcon(), theme.AccountIcon(), theme.SettingsIcon()})

	head := container.NewHBox(hspace(4), brand(22), layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(9, 9), u.statusDot)),
		hspace(4), container.NewCenter(u.statusLbl), hspace(6))
	top := container.NewVBox(container.NewPadded(head), u.clipBar)

	home := container.NewBorder(top, nav, nil, nil, u.tabBody)
	u.root = container.NewStack(home)
	u.w.SetContent(u.root)
	u.selectTab(0)
	u.w.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if e.Name == mobiledrv.KeyBack {
			u.mobileBack()
		}
	})
}

// buildDesktopShell: lewy panel z zakładkami + rozmowa w splicie (jak dotąd).
func (u *ui) buildDesktopShell(friendsTab fyne.CanvasObject) {
	u.chatLst = widget.NewList(u.chatCount, u.chatTemplate, u.chatUpdate)
	u.chatLst.HideSeparators = true
	u.chatLst.OnSelected = func(id widget.ListItemID) { u.sel = id; u.shown = 0; u.refresh() }
	chatsTab := container.NewBorder(nil,
		container.NewPadded(container.NewGridWithColumns(2,
			widget.NewButtonWithIcon("Nowa grupa", theme.ContentAddIcon(), u.newGroupPage),
			widget.NewButtonWithIcon("Mam kod", theme.LoginIcon(), u.codePage))),
		nil, nil, u.chatLst)

	u.center = container.NewStack(u.convRoot)
	u.nav = container.NewAppTabs(
		container.NewTabItemWithIcon("Czaty", theme.ListIcon(), chatsTab),
		container.NewTabItemWithIcon("Znajomi", theme.AccountIcon(), friendsTab),
		container.NewTabItemWithIcon("Ty", theme.SettingsIcon(), u.mePage()),
	)
	u.nav.OnSelected = func(*container.TabItem) { u.checkClipboard() }
	head := container.NewHBox(brand(24), layout.NewSpacer())
	left := container.NewBorder(container.NewPadded(head), nil, nil, nil, u.nav)
	split := container.NewHSplit(left, u.center)
	split.SetOffset(0.26)
	u.w.SetContent(container.NewBorder(u.clipBar, u.statusBar, nil, nil, split))
}

// buildStatus: kropka i etykieta połączenia. Na telefonie siedzą w nagłówku,
// na pulpicie w pasku na dole — ten sam obiekt nie może wisieć w dwóch
// kontenerach, więc pasek powstaje tylko tam, gdzie jest używany.
func (u *ui) buildStatus() {
	u.statusDot = canvas.NewCircle(cOrange)
	u.statusLbl = mutedText("łączenie…", 12)
	if u.mobile {
		return
	}
	me := mutedText(u.c.ID.Name+" · "+identity.Fingerprint(u.c.ID.Pub), 11)
	u.statusBar = container.NewHBox(hspace(4),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(9, 9), u.statusDot)),
		hspace(4), container.NewCenter(u.statusLbl), layout.NewSpacer(),
		container.NewCenter(me), hspace(4))
}

// mobileBack: Wstecz zdejmuje po kolei stronę, potem wraca na zakładkę Czaty.
// Domyślnie fyne od razu ubija aktywność — dialogu nie dało się cofnąć.
func (u *ui) mobileBack() {
	ov := u.w.Canvas().Overlays()
	if top := ov.Top(); top != nil {
		ov.Remove(top)
		return
	}
	if u.popPage() {
		return
	}
	if u.tab != 0 {
		u.selectTab(0)
		return
	}
	if d, ok := u.a.Driver().(mobiledrv.Driver); ok {
		d.GoBack()
	}
}

// ---- odświeżanie ----

func (u *ui) cur() *core.ChatInfo {
	if u.sel >= 0 && u.sel < len(u.rows) {
		return &u.rows[u.sel].info
	}
	return nil
}

func (u *ui) watch() {
	ch := u.c.Subscribe()
	for range ch {
		fyne.Do(u.refresh)
	}
}

func (u *ui) statusTicker() {
	for {
		time.Sleep(3 * time.Second)
		fyne.Do(u.updateStatus)
	}
}

func (u *ui) updateStatus() {
	if u.c == nil {
		return
	}
	if u.c.ConnUp() {
		u.statusDot.FillColor = cGreen
		u.statusLbl.Text = "połączony · " + u.c.Cfg.Connector
		if u.mobile {
			u.statusLbl.Text = "połączony"
		}
	} else {
		u.statusDot.FillColor = cOrange
		u.statusLbl.Text = "łączenie z connectorem… (" + u.c.Cfg.Connector + ")"
		if e := u.c.ConnErr(); e != "" {
			u.statusDot.FillColor = cRed
			u.statusLbl.Text = "błąd: " + trunc(e, 52)
			if u.mobile {
				u.statusLbl.Text = "błąd połączenia"
			}
		} else if u.mobile {
			u.statusLbl.Text = "łączenie…"
		}
	}
	u.statusDot.Refresh()
	u.statusLbl.Refresh()
}

func (u *ui) refresh() {
	// awatary rysujemy od nowa w każdym odświeżeniu, więc ich animacje też;
	// GIF-y w dymkach mają własną listę, bo rozmowa często NIE jest przerysowywana
	u.anims = u.anims[:0]
	chats := u.c.Chats()
	u.rows = u.rows[:0]
	for _, ci := range chats {
		key := string(ci.ID)
		cnt := u.c.St.EventCount(ci.ID)
		if !u.booted {
			u.seen[key] = cnt // start bez lawiny badge'ów za historię
		}
		row := chatRow{info: ci}
		if cnt > u.seen[key] {
			row.unread = cnt - u.seen[key]
		}
		u.rows = append(u.rows, row)
	}
	u.booted = true

	if u.sel >= len(u.rows) {
		u.sel = 0
	}
	// otwarty czat = obejrzany; o pozostałych powiadamiamy systemowo
	open := u.chatOpen()
	total := 0
	for i := range u.rows {
		key := string(u.rows[i].info.ID)
		if i == u.sel && open {
			u.seen[key] = u.c.St.EventCount(u.rows[i].info.ID)
			u.rows[i].unread = 0
		} else if u.rows[i].unread > u.unread[key] {
			u.a.SendNotification(fyne.NewNotification(
				"szept — "+u.rows[i].info.Name, "nowa wiadomość"))
		}
		u.unread[key] = u.rows[i].unread
		total += u.rows[i].unread
	}
	u.rebuildChats()
	u.rebuildFriends()
	u.rebuildMe()
	u.setTabBadge(0, total)

	ch := u.cur()
	if ch == nil {
		u.showWelcome()
		return
	}
	if !u.mobile {
		u.hideWelcome()
	}
	u.updateConvHeader(ch)
	if open {
		u.renderMessages(ch)
	}
	u.renderCallState(ch)
}

// chatOpen: czy rozmowa jest teraz na ekranie. Na telefonie to osobna strona,
// więc gdy jej nie ma, nie ma sensu rysować dymków ani gasić odznaki.
func (u *ui) chatOpen() bool {
	if !u.mobile {
		return u.cur() != nil
	}
	return u.convPage != nil && u.onStack(u.convPage)
}

func (u *ui) onStack(o fyne.CanvasObject) bool {
	if u.root == nil {
		return false
	}
	for _, x := range u.root.Objects {
		if x == o {
			return true
		}
	}
	return false
}

// ---- welcome ----

func (u *ui) showWelcome() {
	if u.mobile {
		return // na telefonie zakładka Czaty sama pokazuje, co robić dalej
	}
	step := func(icon fyne.Resource, title, desc string, act func()) fyne.CanvasObject {
		b := widget.NewButtonWithIcon(title, icon, act)
		b.Alignment = widget.ButtonAlignLeading
		return container.NewVBox(b, mutedWrap(desc))
	}
	welcome := u.startScaffold(container.NewVBox(
		container.NewCenter(brand(38)),
		mutedWrap("nikt nie podsłucha — wszystko szyfrowane między wami"),
		vspace(16),
		card(container.NewVBox(
			widget.NewLabelWithStyle("Zacznij tutaj", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			step(theme.AccountIcon(), "Dodaj znajomego",
				"Zakładka Znajomi → pokaż swój QR albo wklej cudzy kod.", u.codePage),
			step(theme.ContentAddIcon(), "Utwórz grupę",
				"Załóż pokój i zaproś ludzi jednorazowym kodem z PIN-em.", u.newGroupPage),
			step(theme.LoginIcon(), "Mam kod zaproszenia",
				"Wklej kod SZC- i podaj PIN od zapraszającego.", u.codePage),
		)),
	))
	u.center.Objects = []fyne.CanvasObject{welcome}
	u.center.Refresh()
}

func (u *ui) hideWelcome() {
	if len(u.center.Objects) != 1 || u.center.Objects[0] != u.convRoot {
		u.center.Objects = []fyne.CanvasObject{u.convRoot}
		u.center.Refresh()
	}
}

// ---- drobne narzędzia ----

func spaced(s string) string {
	return strings.Join(strings.Split(s, ""), " ")
}

// trunc przycina po runach, nie bajtach — „ż” zajmuje dwa bajty i cięcie
// w środku dawało krzaki.
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// twoLine: tytuł i podpis w jednej, ciasnej kolumnie. widget.Label ma na
// telefonie 13 px wewnętrznego oddechu z każdej strony, więc dwa labele robiły
// wiersz wysoki na pół ekranu — canvas.Text nie ma paddingu, tylko nie zawija,
// stąd przycinanie.
func twoLine(title, desc string, titleSize, descSize float32, cut int) fyne.CanvasObject {
	t := canvas.NewText(trunc(title, cut), cFg)
	t.TextSize = fs(titleSize)
	t.TextStyle = fyne.TextStyle{Bold: true}
	if desc == "" {
		return container.NewVBox(t)
	}
	d := canvas.NewText(trunc(desc, cut+16), cMuted)
	d.TextSize = fs(descSize)
	return container.NewVBox(t, d)
}

// appVersion: metadane wstrzykuje `fyne package` (FyneApp.toml); zwykłe
// `go build` ich nie ma — wtedy "dev". Format zgodny z versionName w APK.
func appVersion(a fyne.App) string {
	m := a.Metadata()
	if m.Version == "" {
		return "dev"
	}
	return fmt.Sprintf("%s.%d", m.Version, m.Build)
}

// dlgSize: dialogi (pulpit) nie mogą wystawać poza okno.
func (u *ui) dlgSize(w, h float32) fyne.Size {
	cs := u.w.Canvas().Size()
	if cs.Width > 60 {
		w = fyne.Min(w, cs.Width-50)
	}
	if cs.Height > 200 {
		h = fyne.Min(h, cs.Height-170)
	}
	return fyne.NewSize(w, h)
}

// toast: krótka informacja bez zabierania ekranu. Na telefonie dialog byłby
// tu przesadą, a użytkownik i tak chce tylko potwierdzenia.
func (u *ui) toast(text string) {
	if ch := u.cur(); ch != nil && u.chatOpen() {
		u.sysOK(text, "")
		return
	}
	dialog.ShowInformation("szept", text, u.w)
}

func (u *ui) fail(err error) {
	if err == nil {
		return
	}
	dialog.ShowError(err, u.w)
}

// ---- wideo ----

func (u *ui) onFrame(chat, from []byte, kind string, jpg []byte) {
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return
	}
	fyne.Do(func() { u.showFrame(chat, from, kind, img) })
}

func (u *ui) showFrame(chat, from []byte, kind string, img image.Image) {
	if u.mobile {
		// mobile: brak dekoracji okien — dialog z przyciskiem Zamknij
		if u.vidDlg == nil {
			u.vidGrid = container.NewGridWithColumns(1)
			scr := container.NewVScroll(u.vidGrid)
			scr.SetMinSize(u.dlgSize(320, 440))
			u.vidDlg = dialog.NewCustom("szept — wideo", "Zamknij", scr, u.w)
			u.vidDlg.SetOnClosed(func() {
				u.vidDlg = nil
				u.vidImgs = map[string]*canvas.Image{}
			})
			u.vidDlg.Show()
		}
	} else if u.vidWin == nil {
		u.vidGrid = container.NewGridWithColumns(2)
		u.vidWin = u.a.NewWindow("szept — wideo")
		u.vidWin.Resize(fyne.NewSize(880, 520))
		u.vidWin.SetContent(u.vidGrid)
		u.vidWin.SetOnClosed(func() {
			u.vidWin = nil
			u.vidImgs = map[string]*canvas.Image{}
		})
		u.vidWin.Show()
	}
	key := string(from) + kind
	ci, ok := u.vidImgs[key]
	if !ok {
		ci = canvas.NewImageFromImage(img)
		ci.FillMode = canvas.ImageFillContain
		if u.mobile {
			ci.SetMinSize(fyne.NewSize(300, 170))
		} else {
			ci.SetMinSize(fyne.NewSize(420, 240))
		}
		u.vidImgs[key] = ci

		who := "ja"
		if string(from) != string(u.c.ID.Pub) {
			if st, err := u.c.ChatState(chat); err == nil {
				if mem, ok := st.Members[string(from)]; ok {
					who = mem.Name
				}
			}
		}
		lbl := "kamera"
		if kind == "s" {
			lbl = "ekran"
		}
		cap := canvas.NewText(who+" · "+lbl, cMuted)
		cap.TextSize = fs(12)
		cap.Alignment = fyne.TextAlignCenter
		u.vidGrid.Add(container.NewBorder(nil, cap, nil, nil, ci))
	}
	ci.Image = img
	ci.Refresh()
}

// ---- schowek: zamiast skanera QR w aplikacji ----

func (u *ui) buildClipBar() {
	bg := canvas.NewRectangle(color.NRGBA{0x2c, 0x26, 0x4a, 0xff})
	bg.CornerRadius = 10
	u.clipLbl = canvas.NewText("", cFg)
	u.clipLbl.TextSize = fs(12)
	use := bigButton("Użyj", theme.ConfirmIcon(), func() {
		code := u.clipCode
		u.clipSeen = code
		u.clipBar.Hide()
		u.applyCode(code)
	})
	no := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		u.clipSeen = u.clipCode
		u.clipBar.Hide()
	})
	u.clipBar = container.NewStack(bg, container.NewPadded(container.NewBorder(nil, nil, nil,
		container.NewHBox(use, no), container.NewCenter(u.clipLbl))))
	u.clipBar.Hide()
}

// checkClipboard: aparat telefonu odczytuje QR i wrzuca tekst do schowka —
// po powrocie do aplikacji podchwytujemy go sami, bez skanera w środku.
func (u *ui) checkClipboard() {
	if u.clipBar == nil || u.c == nil {
		return
	}
	s := strings.TrimSpace(u.a.Clipboard().Content())
	own := s == u.c.FriendCode() // własny kod skopiowany ze strony — nie proponuj
	if own || s == u.clipSeen ||
		(!strings.HasPrefix(s, "SZF-") && !strings.HasPrefix(s, "SZC-")) {
		u.clipBar.Hide()
		return
	}
	u.clipCode = s
	u.clipLbl.Text = "Wykryto kod znajomego w schowku"
	if strings.HasPrefix(s, "SZC-") {
		u.clipLbl.Text = "Wykryto zaproszenie w schowku"
	}
	u.clipLbl.Refresh()
	u.clipBar.Show()
}

func (u *ui) applyCode(s string) {
	switch {
	case strings.HasPrefix(s, "SZF-"):
		f, err := u.c.AddFriend(s)
		if err != nil {
			u.fail(err)
			return
		}
		u.refresh()
		u.toast("Dodano znajomego: " + f.Name)
	case strings.HasPrefix(s, "SZC-"):
		pin := widget.NewEntry()
		pin.PlaceHolder = "6 cyfr od zapraszającego"
		join := func() {
			if err := u.c.Redeem(s, strings.TrimSpace(pin.Text)); err != nil {
				u.fail(err)
				return
			}
			u.popPage()
			u.toast("Działa! Łączę się z drugą stroną — musi być online.")
		}
		pin.OnSubmitted = func(string) { join() }
		u.page("Zaproszenie do grupy", container.NewVBox(
			mutedWrap("Kod jest jednorazowy. PIN dostajesz od zapraszającego innym kanałem."),
			pin,
			bigButton("Dołącz", theme.ConfirmIcon(), join),
		))
	default:
		u.fail(errors.New("kod musi zaczynać się od SZF- albo SZC-"))
	}
}
