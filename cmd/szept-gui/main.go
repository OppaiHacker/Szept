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

// Palette like in mobile Discord (same neutral grays), only accent
// stays szept purple. Three background levels matter here: app bars and navigation are
// DARKER than content — giving a layered feel without shadows, which fyne
// doesn't draw around containers anyway.
var (
	cDeep     = color.NRGBA{0x1e, 0x1f, 0x22, 0xff} // top bar, bottom nav
	cBg       = color.NRGBA{0x31, 0x33, 0x38, 0xff} // content background
	cSurface  = color.NRGBA{0x2b, 0x2d, 0x31, 0xff} // cards, input bar
	cSurface2 = color.NRGBA{0x38, 0x3a, 0x40, 0xff} // buttons, inputs, peer bubbles
	cBubbleMe = color.NRGBA{0x5b, 0x4f, 0xd6, 0xff} // own bubbles
	cAccent   = color.NRGBA{0x8b, 0x7c, 0xf6, 0xff} // szept purple accent
	cFg       = color.NRGBA{0xdb, 0xde, 0xe1, 0xff}
	cMuted    = color.NRGBA{0x94, 0x9b, 0xa4, 0xff}
	cSep      = color.NRGBA{0x3f, 0x41, 0x47, 0xff}
	cGreen    = color.NRGBA{0x23, 0xa5, 0x5a, 0xff}
	cOrange   = color.NRGBA{0xf0, 0xb2, 0x32, 0xff}
	cRed      = color.NRGBA{0xed, 0x42, 0x45, 0xff}
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
	// container.Scroll draws gradient shadow on every edge with hidden content
	// (internal/widget/scroller.go) — in chat it looked like a dirty smudge
	// above text field and below title bar. Shadow cannot be disabled
	// other than by color, and bars provide layering via their background anyway.
	case theme.ColorNameShadow:
		return color.Transparent
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
	// so radius 20 makes them pill-shaped and circular — like Discord composer
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return m(10, 20)
	case theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameCardRadius, theme.SizeNameDialogRadius:
		return 14
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
	meBox      *fyne.Container    // "You" tab (profile + settings)
	nav        *container.AppTabs // desktop: Chats | Friends
	msgBox     *fyne.Container
	msgScr     *flingScroll
	center     *fyne.Container   // desktop: toggles welcome <-> conversation
	convRoot   fyne.CanvasObject // conversation view
	convBottom *fyne.Container   // conversation bottom bar: hints, call panel, composer

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

	clipBar  *fyne.Container // "detected code in clipboard"
	clipLbl  *canvas.Text
	clipCode string
	clipSeen string // code already dismissed/used — don't show twice

	statusDot *canvas.Circle
	statusLbl *canvas.Text
	statusBar *fyne.Container

	seen   map[string]int      // chatID -> event count at last view
	unread map[string]int      // chatID -> badge from previous refresh
	prev   map[string]chatPrev // chatID -> preview of last message

	shown     int    // how many messages rendered in open chat
	grew      bool   // just loaded older — scroll should stay in place
	msgSig    string // fingerprint of rendered conversation — skip scroll if unchanged
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
	// konta (profile): wybrane konto podmienia katalog danych; -data wygrywa
	if cfg.Profile != "" && !mobileUI && *dataFlag == "" {
		dataDir = boot.ProfileDir(cfg.Profile)
	}
	cfg.DataDir = dataDir
	if col, ok := hexColor(cfg.Accent); ok {
		setAccent(col)
		a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})
	}

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
			u.fail(err)
			return
		}
		u.c = c
		c.Start()
		if cfg.WebhookOn && cfg.WebhookPort > 0 && cfg.WebhookToken != "" {
			if err := c.StartWebhook(cfg.WebhookPort, cfg.WebhookToken); err != nil {
				u.fail(err)
			}
		}
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

// startScaffold: desktop centers card; mobile gets full width
// and scrolling (keyboard), because Center lets content overflow screen
func (u *ui) startScaffold(content fyne.CanvasObject) fyne.CanvasObject {
	if u.mobile {
		return vscroll(container.NewPadded(content))
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

// card: rounded panel on darker background
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = 14
	return container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
}

// ---- startup screens ----

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
			u.info("szept", "Podaj nazwę i adres connectora.")
			return
		}
		u.cfg.Connector = core.NormalizeWS(conn.Text)
		id, err := boot.CreateIdentity(u.cfg.DataDir, name.Text, []byte(pass.Text))
		if err != nil {
			u.fail(err)
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

// ---- main layout ----

func (u *ui) buildMain() {
	u.buildConv()   // shared conversation view (chat.go)
	u.startAnims()  // single ticker for all GIFs (media.go)
	u.buildStatus() // connection bar
	u.buildClipBar()

	// Friends tab
	u.friendBox = container.NewVBox()
	addFriend := bigButton("Dodaj znajomego", theme.ContentAddIcon(), u.codePage)
	friendsTab := container.NewBorder(nil, container.NewPadded(addFriend), nil, nil,
		vscroll(u.friendBox))

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

// buildMobileShell: bottom tabs Chats | Friends | You and stacked pages above.
func (u *ui) buildMobileShell(friendsTab fyne.CanvasObject) {
	u.chatBox = container.NewVBox()
	chatsTab := container.NewBorder(
		container.NewPadded(container.NewGridWithColumns(2,
			widget.NewButtonWithIcon("Nowa grupa", theme.ContentAddIcon(), u.newGroupPage),
			widget.NewButtonWithIcon("Mam kod", theme.LoginIcon(), u.codePage))),
		nil, nil, nil, vscroll(u.chatBox))

	u.tabViews = []fyne.CanvasObject{chatsTab, friendsTab, u.mePage()}
	// tabBody has its own slideLayout so tab switches can slide
	// in from the side instead of swapping view in one frame
	u.tabBody = slidePage(u.tabViews[0])
	nav := u.buildBottomNav(
		[]string{"Czaty", "Znajomi", "Ty"},
		[]fyne.Resource{theme.MailComposeIcon(), theme.AccountIcon(), theme.SettingsIcon()})

	head := container.NewHBox(hspace(4), brand(22), layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(9, 9), u.statusDot)),
		hspace(4), container.NewCenter(u.statusLbl), hspace(6))
	top := container.NewVBox(
		container.NewStack(canvas.NewRectangle(cDeep), container.NewPadded(head)),
		u.clipBar)

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
		container.NewTabItemWithIcon("Ty", theme.SettingsIcon(), u.mePage()),
	)
	u.nav.OnSelected = func(*container.TabItem) { u.checkClipboard() }
	head := container.NewHBox(brand(24), layout.NewSpacer())
	left := container.NewBorder(container.NewPadded(head), nil, nil, nil, u.nav)
	split := container.NewHSplit(left, u.center)
	split.SetOffset(0.26)
	u.w.SetContent(container.NewBorder(u.clipBar, u.statusBar, nil, nil, split))
}

// buildStatus: connection dot and label. On mobile they sit in header,
// on desktop in bottom bar — same object cannot sit in two
// containers, so bar is created only where used.
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

// ---- clipboard: replacing in-app QR scanner ----

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

// checkClipboard: phone camera reads QR and places text in clipboard —
// upon returning to app we pick it up automatically, without in-app scanner.
func (u *ui) checkClipboard() {
	if u.clipBar == nil || u.c == nil {
		return
	}
	s := strings.TrimSpace(u.a.Clipboard().Content())
	own := s == u.c.FriendCode() // own code copied from page — don't suggest
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
