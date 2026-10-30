// szept-gui: Fyne client for Linux/Windows/Android.
//
// Android is the primary platform here: bottom tab bar, full-screen
// pages instead of dialogs, touch list rows (see nav.go, chat.go,
// profile.go). Desktop gets the same code with !u.mobile branches.
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
	"os"
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

// msec: shortcut for animation duration (canvas.New*Animation takes time.Duration).
const msec = time.Millisecond

// ---- palette ----

// One palette with the wails client (cmd/szept-wails/frontend/app.css), so the
// phone and the desktop are visibly the same app. Surfaces get DARKER towards
// the edges of the screen (app bar, bottom navigation, composer) and lighter
// towards the content — that is the entire depth model, because fyne draws no
// shadow around a container and a phone has no window frame to lean on.
var (
	cDeep     = color.NRGBA{0x0b, 0x0c, 0x11, 0xff} // app bar, bottom nav, composer
	cBg       = color.NRGBA{0x10, 0x13, 0x19, 0xff} // content background
	cSurface  = color.NRGBA{0x16, 0x1a, 0x22, 0xff} // cards
	cSurface2 = color.NRGBA{0x1e, 0x23, 0x2d, 0xff} // inputs, buttons, peer bubbles
	cSurface3 = color.NRGBA{0x29, 0x30, 0x3c, 0xff} // chips, quiet badges
	cBubbleMe = color.NRGBA{0x6c, 0x60, 0xbf, 0xff} // own bubbles (setAccent recomputes)
	cAccent   = color.NRGBA{0x8b, 0x7c, 0xf6, 0xff} // szept purple accent
	// accent wash behind the active tab and other "this is live" surfaces;
	// alpha rather than a mixed constant, so it follows a custom accent too
	cAccentSoft = color.NRGBA{0x8b, 0x7c, 0xf6, 0x2e}
	cFgStrong   = color.NRGBA{0xf5, 0xf7, 0xfc, 0xff} // names, titles, unread rows
	cFg         = color.NRGBA{0xe4, 0xe8, 0xf1, 0xff}
	cFg2        = color.NRGBA{0xb4, 0xbc, 0xcc, 0xff} // previews of unread chats
	cMuted      = color.NRGBA{0x83, 0x8d, 0x9f, 0xff}
	cFaint      = color.NRGBA{0x6b, 0x74, 0x84, 0xff} // timestamps, placeholders
	cSep        = color.NRGBA{0xff, 0xff, 0xff, 0x14}
	cGreen      = color.NRGBA{0x3f, 0xbf, 0x7f, 0xff}
	cOrange     = color.NRGBA{0xee, 0xb1, 0x34, 0xff}
	cRed        = color.NRGBA{0xee, 0x4d, 0x55, 0xff}
)

// author colors in groups (deterministic by pubkey)
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

// mobileUI: set once in main(); text and touch targets scale up.
var mobileUI bool

// fs: text size — on mobile +2 so small captions stay legible
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
	// buttons and inputs share a tone: both are "things you can act on" resting
	// one step above whatever surface they sit on
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return cSurface2
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return cSep
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground, theme.ColorNameHeaderBackground:
		return cSurface
	case theme.ColorNamePlaceHolder:
		return cFaint
	case theme.ColorNameDisabled:
		return cMuted
	case theme.ColorNameHover:
		return color.NRGBA{0xff, 0xff, 0xff, 0x0c}
	case theme.ColorNamePressed:
		return color.NRGBA{0xff, 0xff, 0xff, 0x14}
	case theme.ColorNameSelection:
		return color.NRGBA{cAccent.R, cAccent.G, cAccent.B, 0x50}
	case theme.ColorNameSuccess:
		return cGreen
	case theme.ColorNameWarning:
		return cOrange
	case theme.ColorNameError:
		return cRed
	case theme.ColorNameScrollBar:
		return color.NRGBA{0xff, 0xff, 0xff, 0x28}
	// One colour drives two things: the scrim behind a dialog and the gradient
	// container.Scroll paints on every edge with hidden content behind it
	// (internal/widget/scroller.go). Transparent used to be the only way to keep
	// that gradient from smearing over the old light-grey content background;
	// against these near-black surfaces a black scrim disappears into the
	// layering, and a dialog finally has something to float above.
	case theme.ColorNameShadow:
		return color.NRGBA{0x00, 0x00, 0x00, 0x99}
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
	// mobile: larger text and padding — touch targets ~44dp, legibility on small screen
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
	// on mobile text field and icon buttons are ~42 px high,
	// so radius 20 makes them pill-shaped and circular
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return m(10, 20)
	case theme.SizeNameSelectionRadius:
		return 10
	// a phone card fills the width, so it needs a rounder corner than a small
	// desktop panel to read as one object instead of a cropped rectangle
	case theme.SizeNameCardRadius, theme.SizeNameDialogRadius:
		return m(14, 18)
	case theme.SizeNameScrollBarSmall:
		return m(3, 3)
	}
	return t.base.Size(n)
}

// ---- ui ----

type chatRow struct {
	info   core.ChatInfo
	unread int
}

// chatPrev: preview of last message on chat list. Cached under
// event count — otherwise every refresh would decrypt full history
// of every chat, which is noticeable on mobile.
type chatPrev struct {
	rev  uint64
	text string
	ts   time.Time
}

type ui struct {
	relayWarned map[string]bool // pubs already told about a relayed link
	a           fyne.App
	w           fyne.Window
	c           *core.Core
	cfg         *core.Config
	cfgFile     string
	mobile      bool

	// konta (profile): baseDir to katalog danych bez profilu (albo ten z
	// -data / piaskownicy Androida), enter — wejście do aplikacji po
	// zalogowaniu. Trzymane na ui, bo ekran logowania potrafi się przełączyć
	// na inne konto i przejść tę samą drogę jeszcze raz (login()).
	baseDir  string
	dataFlag string
	enter    func(*identity.Identity)

	// mobile shell: root is Stack, where [0] is main screen,
	// and stacked layers are full-screen pages (nav.go)
	root     *fyne.Container
	tabBody  *fyne.Container
	tabViews []fyne.CanvasObject
	tabs     []*navTab
	tab      int
	convPage *fyne.Container // conversation page (mobile)

	rows       []chatRow
	sel        int
	chatBox    *fyne.Container    // touch chat rows (both platforms)
	friendBox  *fyne.Container    // friend list
	meBox      *fyne.Container    // settings list (sidebar on desktop, tab on mobile)
	setPane    *fyne.Container    // desktop: section shown next to settings list
	mainRoot   fyne.CanvasObject  // desktop: main screen, restored when settings close
	nav        *container.AppTabs // desktop: Chats | Friends
	msgBox     *fyne.Container
	msgScr     *flingScroll
	center     *fyne.Container   // desktop: toggles welcome <-> conversation
	convRoot   fyne.CanvasObject // conversation view
	convBottom *fyne.Container   // conversation bottom bar: hints, call panel, composer
	memberBox  *fyne.Container   // desktop: member column next to the conversation
	memberHead *clipText

	title      *clipText
	subtitle   *clipText
	headAvatar *fyne.Container
	callBtn    *widget.Button
	micBtn     *widget.Button
	camBtn     *widget.Button
	scrBtn     *widget.Button

	banner     *fyne.Container
	bannerLbl  *clipText
	bannerSub  *clipText // druga linia: czat · uczestnicy
	bannerDot  *canvas.Circle
	bannerAnim *fyne.Animation
	callBar    *fyne.Container // media buttons — only during a call
	deafBtn    *widget.Button
	spatialBtn *widget.Button

	entry   *cmdEntry       // message field; "/" triggers commands (commands.go)
	hintBox *fyne.Container // command hints above field
	hints   []cmdDef        // currently matched
	hintSel int             // highlighted hint
	sys     map[string][]sysMsg

	statusDot *canvas.Circle
	statusLbl *canvas.Text
	statusBar *fyne.Container

	seen   map[string]int      // chatID -> event count at last view
	unread map[string]int      // chatID -> badge from previous refresh
	prev   map[string]chatPrev // chatID -> preview of last message

	shown     int    // how many messages rendered in open chat
	grew      bool   // just loaded older — scroll should stay in place
	msgSig    string // fingerprint of rendered conversation — skip scroll if unchanged
	memSig    string // same for the member column
	lastCount int    // messages in selected chat (new message animation)
	lastChat  string // detect chat change (prevent false flash)
	booted    bool

	vidWin  fyne.Window
	vidDlg  dialog.Dialog // mobile: dialog instead of second window
	vidGrid *fyne.Container
	vidImgs map[string]*canvas.Image // peer+kind -> image

	sp     *scene3d        // call's 3D scene (nil = closed)
	spPage *fyne.Container // mobile scene page (to detect Back)

	anims    []*anim        // avatar GIFs — list cleared on every list refresh
	msgAnims []*anim        // bubble GIFs — persist until conversation is redrawn
	decoded  map[string]any // cache of unpacked images, key = blob hash
}

func main() {
	dataFlag := flag.String("data", "", "data directory")
	connFlag := flag.String("connector", "", "connector address")
	flag.Parse()

	// fixed ID = Preferences directory and app identity. Same ID is in
	// FyneApp.toml, so on Android nothing moves.
	a := app.NewWithID("chat.szept")
	// window icon: X11 and Windows take it from here. Wayland lacks this protocol —
	// there icon comes from app_id + .desktop file, which fyne doesn't set.
	a.SetIcon(fyne.NewStaticResource("icon.png", iconPNG))
	// SZEPT_MOBILE=1 forces mobile layout on desktop. Without this, only way
	// to view mobile UI was building APK and deploying to phone, costing
	// a full build cycle for every tweak.
	phoneTest := os.Getenv("SZEPT_MOBILE") == "1"
	mobileUI = fyne.CurrentDevice().IsMobile() || phoneTest
	a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})
	w := a.NewWindow("szept")
	// Without this fyne pushes all content by theme.Padding() from every edge
	// (mobile: internal/driver/mobile/canvas.go sizeContent). Result: message input
	// bar and bottom navigation floated as detached rectangles.
	// We distribute margins ourselves where needed.
	w.SetPadded(false)
	switch {
	case phoneTest:
		w.Resize(fyne.NewSize(411, 820)) // typical Android in dp
	case !mobileUI:
		w.Resize(fyne.NewSize(1060, 680))
	}

	cfgFile, dataDir := boot.Dirs()
	if mobileUI {
		// scoped storage: only app directory is writable
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
	if col, ok := hexColor(cfg.Accent); ok {
		setAccent(col)
		a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})
	}

	u := &ui{
		a: a, w: w, cfg: cfg, cfgFile: cfgFile,
		baseDir: dataDir, dataFlag: *dataFlag,
		mobile:  mobileUI,
		vidImgs: map[string]*canvas.Image{},
		decoded: map[string]any{},
		seen:    map[string]int{}, unread: map[string]int{},
		relayWarned: map[string]bool{},
		prev:        map[string]chatPrev{},
		sys:         map[string][]sysMsg{},
	}

	u.enter = func(id *identity.Identity) {
		c, err := core.Open(*cfg, id)
		if err != nil {
			u.openFailScreen(id, err)
			return
		}
		u.c = c
		u.prev = map[string]chatPrev{} // keyed by store revision, which restarts per account
		c.Start()
		if cfg.WebhookOn && cfg.WebhookPort > 0 {
			if err := c.StartWebhook(cfg.WebhookPort, c.WebhookToken()); err != nil {
				u.fail(err)
			}
		}
		u.buildMain()
		go u.watch()
		go u.statusTicker()
		c.OnVideoFrame = u.onFrame
	}

	u.login()
	w.ShowAndRun()
	if u.c != nil {
		u.c.Close()
	}
}

// ---- small building blocks ----

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

// monoText: fixed-width caption for codes and fingerprints. Not clipText —
// a code is only useful whole, so it must set the width of what holds it.
func monoText(s string, size float32, col color.Color) *canvas.Text {
	t := canvas.NewText(s, col)
	t.TextSize = fs(size)
	t.TextStyle = fyne.TextStyle{Monospace: true}
	return t
}

// mutedWrap: gray caption wrapping on narrow screen (canvas.Text cannot)
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

// centerWrap: the same caption, centred — for empty states, where a left-hung
// paragraph under a centred icon looks like a layout accident.
func centerWrap(s string) *widget.RichText {
	t := mutedWrap(s)
	t.Segments[0].(*widget.TextSegment).Style.Alignment = fyne.TextAlignCenter
	return t
}

// startScaffold: desktop centers card; mobile gets full width
// and scrolling (keyboard), because Center lets content overflow screen
func (u *ui) startScaffold(content fyne.CanvasObject) fyne.CanvasObject {
	if u.mobile {
		return vscroll(container.NewPadded(content))
	}
	// Center ściska dziecko do MinSize — formularz logowania kurczył się do
	// szerokości najdłuższego słowa. hspace przypina wygodną szerokość karty
	// (ta sama sztuczka co w dlgBody).
	return container.NewCenter(container.NewPadded(
		container.NewVBox(hspace(460), content)))
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

// card: rounded panel on darker background
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = theme.Size(theme.SizeNameCardRadius)
	return container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
}

// pad: content with exact margins. container.NewPadded gives one theme padding
// on all four sides, and nearly every piece of phone chrome wants a different
// number vertically than horizontally.
func pad(v, h float32, content fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(v, v, h, h), content)
}

// tightVBox / tightHBox: boxes with an exact gap instead of the theme padding.
// A bar is a row plus the hairline under it, and the default 6 px between them
// is a hole in the bar rather than a design.
func tightVBox(gap float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedVBoxLayout(gap), objs...)
}

func tightHBox(gap float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedHBoxLayout(gap), objs...)
}

// pill: rounded plate under content — chips, badges, the composer field.
func pill(fill color.Color, radius float32, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius
	return container.NewStack(bg, content)
}

// dotChip: state as a chip — coloured dot plus a word on a quiet plate. Same
// shape as the header's connection chip, so "a dot in a pill" means the same
// thing everywhere in the app.
func dotChip(text string, tone color.Color) fyne.CanvasObject {
	dot := container.NewCenter(container.NewGridWrap(fyne.NewSize(7, 7), canvas.NewCircle(tone)))
	t := canvas.NewText(text, cFg2)
	t.TextSize = fs(11)
	return pill(cSurface2, 999, pad(4, 10, tightHBox(6, dot, container.NewCenter(t))))
}

// hairline: the 1 px rule that closes a bar off from the content beneath it.
func hairline() fyne.CanvasObject {
	r := canvas.NewRectangle(cSep)
	r.SetMinSize(fyne.NewSize(1, 1))
	return r
}

// ---- startup screens ----

// profileDir: katalog danych aktywnego konta. -data i Android (piaskownica)
// wygrywają z wyborem profilu.
func (u *ui) profileDir() string {
	if u.mobile || u.dataFlag != "" {
		return u.baseDir
	}
	return boot.ProfileDir(u.cfg.Profile)
}

// login: ekran startowy aktywnego konta — założenie tożsamości, hasło albo
// wejście od razu. Wołane też po przełączeniu konta na ekranie logowania,
// więc musi znosić wielokrotne wywołanie.
func (u *ui) login() {
	dir := u.profileDir()
	u.cfg.DataDir = dir
	if !boot.HasIdentity(dir) {
		u.setupScreen()
		return
	}
	id, err := boot.LoadIdentity(dir, nil)
	if err != nil {
		u.passScreen(dir)
		return
	}
	if u.cfg.Connector == "" {
		u.askConnector(func() { u.enter(id) })
		return
	}
	u.enter(id)
}

func (u *ui) setupScreen() {
	name := widget.NewEntry()
	name.PlaceHolder = "np. ala"
	name.SetText(u.cfg.Profile) // nowe konto: nazwa profilu jako punkt wyjścia
	conn := widget.NewEntry()
	conn.PlaceHolder = "ws://1.2.3.4:7777"
	conn.SetText(u.cfg.Connector)
	pass := widget.NewPasswordEntry()
	pass.PlaceHolder = "(zalecane)"
	form := widget.NewForm(
		widget.NewFormItem("Nazwa", name),
		widget.NewFormItem("Connector", conn),
		widget.NewFormItem("Hasło klucza", pass),
	)
	// the form's own submit is a small button pinned to the right edge — on a
	// phone the first action of the whole app deserves the full width
	start := func() {
		if name.Text == "" || conn.Text == "" {
			u.info("szept", "Podaj nazwę i adres connectora.")
			return
		}
		create := func() {
			u.cfg.Connector = core.NormalizeWS(conn.Text)
			id, err := boot.CreateIdentity(u.cfg.DataDir, name.Text, []byte(pass.Text))
			if err != nil {
				u.fail(err)
				return
			}
			u.saveCfg()
			u.enter(id)
		}
		if pass.Text != "" {
			create()
			return
		}
		// seizure of the device is in the threat model: no password has to be
		// a deliberate choice, never a skipped field
		d := dialog.NewConfirm("Bez hasła?",
			"Każdy, kto weźmie to urządzenie (policja na granicy, złodziej, ktoś z domowników),\n"+
				"przeczyta cały klucz i całą historię rozmów. Hasło szyfruje jedno i drugie.\n"+
				"Kontynuować bez hasła?",
			func(ok bool) {
				if ok {
					create()
				}
			}, u.w)
		d.SetConfirmText("Bez hasła")
		d.SetDismissText("Ustawię hasło")
		d.Show()
	}
	title := "Pierwsze uruchomienie"
	if u.cfg.Profile != "" {
		title = "Nowe konto „" + u.cfg.Profile + "”"
	}
	u.w.SetContent(u.startScaffold(container.NewVBox(
		container.NewCenter(brand(34)),
		mutedWrap("szyfrowany komunikator P2P — bez serwera, bez śladów"),
		vspace(14),
		card(container.NewVBox(
			widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			form,
			bigButton("Zaczynamy", theme.ConfirmIcon(), start),
			mutedWrap("Connector to lekki serwer spotkań — np. ws://twoj-vps:7777"),
		)),
		u.profileBar(),
	)))
}

func (u *ui) passScreen(dataDir string) {
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
		u.enter(id)
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
		u.profileBar(),
	)))
}

// openFailScreen: core.Open padło — najczęściej baza została w starszym formacie
// po aktualizacji aplikacji. Wcześniej leciał sam dialog z błędem i po „OK"
// zostawał pusty ekran bez żadnego wyjścia, a na telefonie pliku bazy nie da się
// skasować ręcznie. Klucz tożsamości zostaje nietknięty, więc po skasowaniu bazy
// wchodzi się na to samo konto — tracone są czaty, wiadomości i znajomi.
func (u *ui) openFailScreen(id *identity.Identity, err error) {
	dbPath := filepath.Join(u.cfg.DataDir, "szept.db")
	reset := bigButton("Zacznij od nowa", theme.MediaReplayIcon(), func() {
		u.confirm("Czaty, wiadomości i lista znajomych znikną z aplikacji. Klucz "+
			"tożsamości zostaje — wejdziesz na to samo konto, tylko z pustą "+
			"historią. Stary plik nie jest kasowany, ląduje obok jako szept.db.stara.", func() {
			// przeniesienie zamiast os.Remove: baza jest nieczytelna dla tej wersji,
			// ale nadal zaszyfrowana danymi użytkownika — kasowanie jej za niego
			// jednym tapnięciem to nie nasza decyzja
			if e := os.Rename(dbPath, dbPath+".stara"); e != nil && !os.IsNotExist(e) {
				u.fail(e)
				return
			}
			u.enter(id)
		})
	})
	u.w.SetContent(u.startScaffold(container.NewVBox(
		container.NewCenter(brand(34)),
		vspace(14),
		card(container.NewVBox(
			widget.NewLabelWithStyle("Nie udało się otworzyć bazy", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			mutedWrap(err.Error()),
			vspace(6),
			reset,
			mutedWrap("Plik: "+dbPath),
		)),
	)))
}

func (u *ui) askConnector(done func()) {
	conn := widget.NewEntry()
	conn.PlaceHolder = "ws://1.2.3.4:7777"
	save := bigButton("Zapisz i połącz", theme.ConfirmIcon(), func() {
		if conn.Text == "" {
			return
		}
		u.cfg.Connector = core.NormalizeWS(conn.Text)
		u.saveCfg()
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

// ---- main layout ----

func (u *ui) buildMain() {
	u.buildConv()   // shared conversation view (chat.go)
	u.startAnims()  // single ticker for all GIFs (media.go)
	u.buildStatus() // connection bar

	// Friends tab
	u.friendBox = container.NewVBox()
	addFriend := bigButton("Dodaj znajomego", theme.ContentAddIcon(), u.codePage)
	friendsTab := container.NewBorder(nil, pad(9, 12, addFriend), nil, nil,
		vscroll(u.friendBox))

	if u.mobile {
		u.buildMobileShell(friendsTab)
	} else {
		u.buildDesktopShell(friendsTab)
	}
	u.refresh()
	u.updateStatus()
}

// buildMobileShell: bottom tabs Chats | Friends | Settings and stacked pages above.
func (u *ui) buildMobileShell(friendsTab fyne.CanvasObject) {
	u.chatBox = container.NewVBox()
	chatsTab := container.NewBorder(
		pad(8, 10, container.NewGridWithColumns(2,
			widget.NewButtonWithIcon("Nowa grupa", theme.ContentAddIcon(), u.newGroupPage),
			widget.NewButtonWithIcon("Mam kod", theme.LoginIcon(), u.codePage))),
		nil, nil, nil, vscroll(u.chatBox))

	u.tabViews = []fyne.CanvasObject{chatsTab, friendsTab, u.mePage()}
	// tabBody has its own slideLayout so tab switches can slide
	// in from the side instead of swapping view in one frame
	u.tabBody = slidePage(u.tabViews[0])
	nav := u.buildBottomNav(
		[]string{"Czaty", "Znajomi", "Ustawienia"},
		[]fyne.Resource{theme.MailComposeIcon(), theme.AccountIcon(), theme.SettingsIcon()})

	head := container.NewBorder(nil, nil, vcenter(brand(21)), vcenter(u.statusChip()))
	top := container.NewVBox(
		container.NewStack(canvas.NewRectangle(cDeep),
			tightVBox(0, pad(11, 14, head), hairline())))

	home := container.NewBorder(top, nav, nil, nil,
		container.NewStack(canvas.NewRectangle(cBg), u.tabBody))
	u.root = container.NewStack(home)
	u.w.SetContent(u.root)
	u.selectTab(0)
	u.w.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if e.Name == mobiledrv.KeyBack {
			u.mobileBack()
		}
	})
}

// buildDesktopShell: left panel with tabs + conversation in split (as before).
func (u *ui) buildDesktopShell(friendsTab fyne.CanvasObject) {
	u.chatBox = container.NewVBox()
	chatsTab := container.NewBorder(nil,
		container.NewPadded(container.NewGridWithColumns(2,
			widget.NewButtonWithIcon("Nowa grupa", theme.ContentAddIcon(), u.newGroupPage),
			widget.NewButtonWithIcon("Mam kod", theme.LoginIcon(), u.codePage))),
		nil, nil, vscroll(u.chatBox))

	u.center = container.NewStack(u.convRoot)
	u.nav = container.NewAppTabs(
		container.NewTabItemWithIcon("Czaty", theme.ListIcon(), chatsTab),
		container.NewTabItemWithIcon("Znajomi", theme.AccountIcon(), friendsTab),
	)
	gear := widget.NewButtonWithIcon("", theme.SettingsIcon(), u.openSettings)
	gear.Importance = widget.LowImportance
	// przełącznik kont pod ręką — wcześniej siedział cztery kliknięcia głęboko
	// w ustawieniach i nikt go tam nie znajdował
	acct := widget.NewButtonWithIcon(profileLabel(u.cfg.Profile), theme.AccountIcon(), func() {
		u.openSettings()
		u.accountsPage()
	})
	acct.Importance = widget.LowImportance
	head := container.NewHBox(brand(24), layout.NewSpacer(), acct, gear)
	left := container.NewBorder(container.NewPadded(head), nil, nil, nil, u.nav)
	split := container.NewHSplit(left, u.center)
	split.SetOffset(0.26)
	u.mainRoot = container.NewBorder(nil, u.statusBar, nil, nil, split)
	u.w.SetContent(u.mainRoot)
}

// buildStatus: connection dot and label. On mobile they sit in header,
// on desktop in bottom bar — same object cannot sit in two
// containers, so bar is created only where used.
func (u *ui) buildStatus() {
	u.statusDot = canvas.NewCircle(cOrange)
	u.statusLbl = mutedText("łączenie…", 12)
	if u.mobile {
		u.statusLbl.Color = cFg2 // inside a chip, muted gray reads as disabled
		return
	}
	me := mutedText(u.c.ID.Name+" · "+identity.Fingerprint(u.c.ID.Pub), 11)
	u.statusBar = container.NewHBox(hspace(4),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(9, 9), u.statusDot)),
		hspace(4), container.NewCenter(u.statusLbl), layout.NewSpacer(),
		container.NewCenter(me), hspace(4))
}

// statusChip: connection state as one chip in the header instead of a loose dot
// with text beside it. Bounded by its own plate it reads as a control, and the
// header keeps a shape when the label swaps between "łączenie…" and an error.
func (u *ui) statusChip() fyne.CanvasObject {
	dot := container.NewCenter(container.NewGridWrap(fyne.NewSize(7, 7), u.statusDot))
	row := tightHBox(6, dot, container.NewCenter(u.statusLbl))
	return pill(cSurface2, 999, pad(5, 10, row))
}

// mobileBack: Back button pops pages one by one, then returns to Chats tab.
// By default fyne kills activity immediately — dialog couldn't be backed out.
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

// ---- refresh ----

func (u *ui) cur() *core.ChatInfo {
	if u.sel >= 0 && u.sel < len(u.rows) {
		return &u.rows[u.sel].info
	}
	return nil
}

// watch: single refresh per event burst. DAG sync fires hundreds per
// second, and refresh() rebuilds lists — without batching,
// mobile freezes on rebuilds instead of rendering scroll.
func (u *ui) watch() {
	ch := u.c.Subscribe()
	for range ch {
		time.Sleep(80 * time.Millisecond)
		for drained := false; !drained; {
			select {
			case <-ch:
			default:
				drained = true
			}
		}
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
	// avatars rendered fresh on every refresh, so their animations too;
	// bubble GIFs have separate list, as conversation is often NOT redrawn
	u.anims = u.anims[:0]
	chats := u.c.Chats()
	u.rows = u.rows[:0]
	for _, ci := range chats {
		key := string(ci.ID)
		cnt := u.c.St.EventCount(ci.ID)
		if !u.booted {
			u.seen[key] = cnt // start without avalanche of unread badges for history
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
	// open chat = viewed; remaining notified via system notifications
	open := u.chatOpen()
	total := 0
	for i := range u.rows {
		key := string(u.rows[i].info.ID)
		if i == u.sel && open {
			u.seen[key] = u.c.St.EventCount(u.rows[i].info.ID)
			u.rows[i].unread = 0
		} else if u.rows[i].unread > u.unread[key] && !u.cfg.NoNotify {
			u.a.SendNotification(fyne.NewNotification(
				"szept — "+u.rows[i].info.Name, "nowa wiadomość"))
		}
		u.unread[key] = u.rows[i].unread
		total += u.rows[i].unread
	}
	u.rebuildVisible()
	u.setTabBadge(0, total)
	u.warnRelayed()

	ch := u.cur()
	if ch == nil {
		u.showWelcome()
		return
	}
	if !u.mobile {
		u.hideWelcome()
	}
	// app bar and bubbles rendered only when conversation is on screen — on mobile
	// from chat list nobody sees them, and avatar in app bar causes image decode
	if open {
		u.updateConvHeader(ch)
		u.renderMessages(ch)
		u.renderMembers(ch)
	}
	u.renderCallState(ch)
	u.spatialSync()
}

// rebuildVisible: on mobile tabs sit stacked beneath each other and only one is
// visible. Rebuilding all three on every event decrypted previews,
// decoded avatars, and created hundreds of new widgets for nothing —
// most visible as stutter during scrolling.
func (u *ui) rebuildVisible() {
	if !u.mobile {
		u.rebuildChats()
		u.rebuildFriends()
		u.rebuildMe()
		return
	}
	switch u.tab {
	case 0:
		u.rebuildChats()
	case 1:
		u.rebuildFriends()
	case 2:
		u.rebuildMe()
	}
}

// chatOpen: whether conversation is currently on screen. On mobile it's a separate page,
// so when absent, rendering bubbles or clearing badges is useless.
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
		return // on mobile Chats tab shows what to do next
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

// ---- small utilities ----

func spaced(s string) string {
	return strings.Join(strings.Split(s, ""), " ")
}

// trunc cuts by runes, not bytes — Polish characters take 2 bytes and middle
// split resulted in corrupted text.
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// vcenter: vertical centering WITHOUT narrowing horizontally. container.NewCenter
// sets child size strictly to MinSize, and clipText has small min width
// intentionally so it compresses — in Center it ended up as just "…".
func vcenter(o fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(layout.NewSpacer(), o, layout.NewSpacer())
}

// twoLine: title and subtitle in single compact column — both lines clip
// to remaining row space (clipText, scroll.go).
func twoLine(title, desc string, titleSize, descSize float32) fyne.CanvasObject {
	t := newClipText(title, fs(titleSize), cFg).bold()
	if desc == "" {
		return container.NewVBox(t)
	}
	return container.NewVBox(t, newClipText(desc, fs(descSize), cMuted))
}

// appVersion: metadata injected by `fyne package` (FyneApp.toml); plain
// `go build` lacks them — fallback to "dev". Format matches versionName in APK.
func appVersion(a fyne.App) string {
	m := a.Metadata()
	if m.Version == "" {
		return "dev"
	}
	return fmt.Sprintf("%s.%d", m.Version, m.Build)
}

// dlgSize: dialogs (desktop) cannot exceed window size.
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

// toast: brief notice without taking over screen. On mobile a dialog would be
// overkill, and user just wants confirmation.
// relayFacts: what a relay operator can and cannot do.
const relayFacts = "• Operator connectora widzi, kto z kim rozmawia, kiedy, jak długo i ile danych przesyłacie. Może to zapisywać.\n" +
	"• Może zablokować albo zepsuć połączenie w dowolnej chwili.\n" +
	"• Connector może postawić każdy, także cenzor albo policja. Jeśli nie wiesz, kto prowadzi Twój connector, zakładaj, że te dane trafiają do wroga.\n" +
	"• Nie widzi treści: wiadomości, pliki i rozmowy głosowe są szyfrowane end-to-end między wami. Nagrany ruch to dla niego szum, a klucze nie przechodzą przez connector."

// warnRelayed tells once per person and session that a link goes through a
// relay — also when it was the other side who chose relay-only.
func (u *ui) warnRelayed() {
	for _, p := range u.c.P2P.Peers() {
		addr := p.Relay()
		if addr == "" || u.relayWarned[string(p.Pub)] {
			continue
		}
		u.relayWarned[string(p.Pub)] = true
		u.sysInfo("Połączenie przez przekaźnik",
			u.c.PeerName(p.Pub)+" — połączenie idzie przez przekaźnik connectora ("+addr+").\n\n"+relayFacts)
	}
}

func (u *ui) toast(text string) {
	if ch := u.cur(); ch != nil && u.chatOpen() {
		u.sysOK(text, "")
		return
	}
	u.info("szept", text)
}

// ---- video ----

func (u *ui) onFrame(chat, from []byte, kind string, jpg []byte) {
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return
	}
	fyne.Do(func() { u.showFrame(chat, from, kind, img) })
}

func (u *ui) showFrame(chat, from []byte, kind string, img image.Image) {
	if u.mobile {
		// mobile: no window decorations — dialog with Close button
		if u.vidDlg == nil {
			u.vidGrid = container.NewGridWithColumns(1)
			scr := vscroll(u.vidGrid)
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

func (u *ui) applyCode(s string) {
	switch {
	case strings.HasPrefix(s, "SZF-"):
		f, err := u.c.AddFriend(s)
		if err != nil {
			u.fail(err)
			return
		}
		u.refresh()
		u.toast("Dodano znajomego: " + f.Display())
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
