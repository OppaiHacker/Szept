// / commands: text input recognizes entries starting with "/",
// suggests matching commands above input, and responses return as local
// system "embeds" in conversation — visible only to us, never sent over network.
package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/event"
	"szept/internal/identity"
)

// maxSys: older messages drop off top — chat shouldn't be a log
const maxSys = 8

// ---- command registry ----

type cmdDef struct {
	name     string
	args     string
	desc     string
	noMobile bool // camera/screen have no source on Android
	run      func(*ui, string)
}

// populated in init(), not in initializer: /help reads the same registry,
// and Go doesn't allow cycle variable->function->variable
var commands []cmdDef

func init() {
	commands = []cmdDef{
		{name: "/pomoc", desc: "spis komend", run: (*ui).cmdHelp},
		{name: "/help", desc: "command list", run: (*ui).cmdHelp},
		{name: "/nowy", args: "<nazwa>", desc: "nowa grupa; nowi członkowie nie widzą wcześniejszej historii", run: (*ui).cmdNew},
		{name: "/new", args: "<name>", desc: "create group", run: (*ui).cmdNew},
		{name: "/dm", args: "<znajomy>", desc: "czat na dwoje ze znajomym", run: (*ui).cmdDM},
		{name: "/kod", desc: "mój kod QR i pole na cudzy", run: func(u *ui, _ string) { u.codePage() }},
		{name: "/code", desc: "my QR code and friend input", run: func(u *ui, _ string) { u.codePage() }},
		{name: "/znajomy", args: "<kod SZF->", desc: "dodaj znajomego z kodu", run: (*ui).cmdApply},
		{name: "/friend", args: "<code SZF->", desc: "add friend from code", run: (*ui).cmdApply},
		{name: "/dolacz", args: "<kod SZC->", desc: "wejdź do grupy z zaproszenia", run: (*ui).cmdApply},
		{name: "/join", args: "<code SZC->", desc: "join group from invite", run: (*ui).cmdApply},
		{name: "/zapros", desc: "kod zaproszenia + PIN do tego czatu", run: needChat((*ui).cmdInvite)},
		{name: "/invite", desc: "generate invite code + PIN", run: needChat((*ui).cmdInvite)},
		{name: "/szukaj", args: "<fraza>", desc: "szukaj w historii czatu", run: needChat((*ui).cmdSearch)},
		{name: "/search", args: "<query>", desc: "search chat history", run: needChat((*ui).cmdSearch)},
		{name: "/kto", desc: "członkowie czatu", run: needChat((*ui).cmdWho)},
		{name: "/who", desc: "list chat members", run: needChat((*ui).cmdWho)},
		{name: "/kick", args: "<nazwa|odcisk>", desc: "usuń członka (rotuje klucz)", run: needChat((*ui).cmdKick)},
		{name: "/rekey", desc: "rotacja klucza — nowa epoka", run: needChat((*ui).cmdRekey)},
		{name: "/zmien", args: "<nazwa>", desc: "zmień nazwę czatu", run: needChat((*ui).cmdRename)},
		{name: "/rename", args: "<name>", desc: "rename chat", run: needChat((*ui).cmdRename)},
		{name: "/call", desc: "dołącz do rozmowy", run: needChat((*ui).cmdCall)},
		{name: "/rozlacz", desc: "wyjdź z rozmowy", run: needChat((*ui).cmdHang)},
		{name: "/leave", desc: "leave call", run: needChat((*ui).cmdHang)},
		{name: "/hangup", desc: "leave call", run: needChat((*ui).cmdHang)},
		{name: "/mic", desc: "mikrofon włącz/wyłącz", run: needChat((*ui).cmdMic)},
		{name: "/cam", args: "[urządzenie]", desc: "kamera włącz/wyłącz", noMobile: true, run: needChat((*ui).cmdCam)},
		{name: "/ekran", desc: "udostępnianie ekranu", noMobile: true, run: needChat((*ui).cmdScreen)},
		{name: "/screen", desc: "share screen", noMobile: true, run: needChat((*ui).cmdScreen)},
		{name: "/wyslij", desc: "zdjęcie, GIF albo film z dysku", run: needChat((*ui).cmdSendFile)},
		{name: "/send", desc: "send file attachment", run: needChat((*ui).cmdSendFile)},
		{name: "/awatar", desc: "zmień swój awatar", run: func(u *ui, _ string) { u.avatarPage() }},
		{name: "/avatar", desc: "change avatar", run: func(u *ui, _ string) { u.avatarPage() }},
		{name: "/connector", args: "[adres]", desc: "pokaż albo zmień connector", run: (*ui).cmdConnector},
		{name: "/ja", desc: "twój profil i ustawienia", run: (*ui).cmdMe},
		{name: "/me", desc: "my profile and settings", run: (*ui).cmdMe},
		{name: "/wyczysc", desc: "schowaj komunikaty systemowe", run: (*ui).cmdClear},
		{name: "/clear", desc: "clear system logs", run: (*ui).cmdClear},
		{name: "/lang", args: "<pl|en>", desc: "change language / zmień język", run: (*ui).cmdLang},
		{name: "/jezyk", args: "<pl|en>", desc: "zmień język / change language", run: (*ui).cmdLang},
	}
}

// needChat: commands operating on conversation share a common prerequisite
func needChat(f func(*ui, *core.ChatInfo, string)) func(*ui, string) {
	return func(u *ui, arg string) {
		ch := u.cur()
		if ch == nil {
			u.sysErr("Najpierw wybierz czat z listy.")
			return
		}
		f(u, ch, arg)
	}
}

func (u *ui) runCommand(line string) {
	name, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	for _, c := range commands {
		if strings.EqualFold(c.name, name) {
			c.run(u, arg)
			return
		}
	}
	u.sysErr("Nie znam komendy " + name + " — wpisz /pomoc.")
}

// ---- implementations ----

func (u *ui) cmdHelp(string) {
	var b strings.Builder
	for _, c := range commands {
		if u.mobile && c.noMobile {
			continue
		}
		b.WriteString(strings.TrimSpace(c.name+" "+c.args) + " — " + c.desc + "\n")
	}
	u.sysInfo("Komendy", strings.TrimRight(b.String(), "\n"))
}

func (u *ui) cmdNew(arg string) {
	if arg == "" {
		u.sysErr("Podaj nazwę: /nowy <nazwa>")
		return
	}
	id, err := u.c.CreateChat(arg, true)
	if err != nil {
		u.sysErr(err.Error())
		return
	}
	u.openChat(id)
	u.sysOK("Grupa gotowa", "Utworzono „"+arg+"”. Zaproś kogoś: /zapros")
}

func (u *ui) cmdDM(arg string) {
	if arg == "" {
		u.sysErr("Podaj znajomego: /dm <nazwa>")
		return
	}
	friends, _ := u.c.St.Friends()
	for _, f := range friends {
		if strings.EqualFold(f.Display(), arg) ||
			strings.HasPrefix(strings.ToLower(f.Display()), strings.ToLower(arg)) {
			id, err := u.c.CreateDirect(f)
			if err != nil {
				u.sysErr(err.Error())
				return
			}
			u.openChat(id)
			u.sysOK("Czat z "+f.Display(), "Piszcie — wiadomości szyfrowane od końca do końca.")
			return
		}
	}
	u.sysErr("Nie znam znajomego „" + arg + "”. Dodaj kodem: /znajomy <kod>")
}

func (u *ui) cmdApply(arg string) {
	if arg == "" {
		u.sysErr("Wklej kod: /znajomy SZF-… albo /dolacz SZC-…")
		return
	}
	u.applyCode(strings.Fields(arg)[0])
}

func (u *ui) cmdInvite(*core.ChatInfo, string) { u.invitePage() }

func (u *ui) cmdSearch(ch *core.ChatInfo, arg string) {
	if arg == "" {
		u.sysErr("Podaj frazę: /szukaj <fraza>")
		return
	}
	res, err := u.c.SearchChat(ch.ID, arg)
	if err != nil {
		u.sysErr(err.Error())
		return
	}
	var b strings.Builder
	for _, m := range res {
		b.WriteString("[" + m.TS.Format("15:04") + "] " + m.Name + ": " + m.Text + "\n")
	}
	u.sysInfo(fmt.Sprintf("Wyniki dla „%s” (%d)", arg, len(res)), strings.TrimRight(b.String(), "\n"))
}

func (u *ui) cmdWho(ch *core.ChatInfo, _ string) {
	var b strings.Builder
	for _, m := range ch.Members {
		dot := "○ "
		if u.c.Online(m.Pub) || string(m.Pub) == string(u.c.ID.Pub) {
			dot = "● "
		}
		b.WriteString(dot + m.Name + "  " + identity.Fingerprint(m.Pub) + "\n")
	}
	u.sysInfo("Członkowie — "+ch.Name, strings.TrimRight(b.String(), "\n"))
}

func (u *ui) cmdKick(ch *core.ChatInfo, arg string) {
	if arg == "" {
		u.sysErr("Podaj nazwę: /kick <nazwa>")
		return
	}
	found := core.FindMembers(ch.Members, arg)
	switch len(found) {
	case 0:
		u.sysErr("Nie ma tu nikogo o nazwie „" + arg + "”. Lista: /kto")
		return
	case 1:
	default:
		var b strings.Builder
		for _, m := range found {
			b.WriteString(m.Name + "  " + identity.Fingerprint(m.Pub) + "\n")
		}
		u.sysErr("Kilka osób nazywa się „" + arg + "”. Nikogo nie usunięto. Podaj odcisk: /kick <odcisk>\n" + strings.TrimRight(b.String(), "\n"))
		return
	}
	m := found[0]
	if err := u.c.Kick(ch.ID, m.Pub); err != nil {
		u.sysErr(err.Error())
		return
	}
	u.sysOK("Usunięto "+m.Name, "Klucz zrotowany — dalszych wiadomości już nie odczyta.")
}

func (u *ui) cmdRekey(ch *core.ChatInfo, _ string) {
	if err := u.c.Rekey(ch.ID); err != nil {
		u.sysErr(err.Error())
		return
	}
	u.sysOK("Nowa epoka klucza", "Nowe wiadomości szyfruje świeży klucz.")
}

func (u *ui) cmdRename(ch *core.ChatInfo, arg string) {
	if arg == "" {
		u.sysErr("Podaj nazwę: /zmien <nazwa>")
		return
	}
	if err := u.c.Rename(ch.ID, arg); err != nil {
		u.sysErr(err.Error())
	}
}

func (u *ui) cmdCall(ch *core.ChatInfo, _ string) {
	if err := u.c.JoinCall(ch.ID); err != nil {
		u.sysErr(err.Error())
		return
	}
	u.refresh()
}

func (u *ui) cmdHang(ch *core.ChatInfo, _ string) {
	u.c.LeaveCall(ch.ID)
	u.refresh()
}

func (u *ui) cmdMic(ch *core.ChatInfo, _ string) {
	u.c.ToggleMic(ch.ID)
	u.refresh()
}

func (u *ui) cmdCam(ch *core.ChatInfo, arg string) {
	if err := u.c.ToggleCam(ch.ID, arg); err != nil {
		u.sysErr(err.Error())
	}
}

func (u *ui) cmdScreen(ch *core.ChatInfo, _ string) {
	if err := u.c.ToggleScreen(ch.ID, 0); err != nil {
		u.sysErr(err.Error())
	}
}

func (u *ui) cmdLang(arg string) {
	if arg == "" {
		u.sysInfo("Język / Language", "Aktualny / Current: "+u.c.Lang()+"\nZmiana / Change: /lang pl lub /lang en")
		return
	}
	if err := u.c.SetLang(arg); err != nil {
		u.sysErr(err.Error())
	} else {
		u.sysOK("Język / Language", "Zmieniono / Language set: "+u.c.Lang())
	}
}

func (u *ui) cmdSendFile(*core.ChatInfo, string) { u.attachPage() }

// cmdMe: profile and settings — on mobile a tab, on desktop the settings view.
func (u *ui) cmdMe(string) {
	if u.mobile {
		u.selectTab(2)
		return
	}
	u.openSettings()
}

func (u *ui) cmdConnector(arg string) {
	if arg == "" {
		state := "łączę…"
		if u.c.ConnUp() {
			state = "połączony"
		} else if e := u.c.ConnErr(); e != "" {
			state = "błąd: " + e
		}
		u.sysInfo("Connector", u.c.Cfg.Connector+"\n"+state+"\nZmiana: /connector <adres>")
		return
	}
	url := core.NormalizeWS(arg)
	u.cfg.Connector = url
	if err := boot.SaveConfig(u.cfgFile, u.cfg); err != nil {
		u.sysErr("zapis konfiguracji: " + err.Error())
		return
	}
	u.c.SetConnector(url)
	u.updateStatus()
	u.sysOK("Connector zmieniony", url+" — łączę…")
}

func (u *ui) cmdClear(string) {
	if ch := u.cur(); ch != nil {
		delete(u.sys, string(ch.ID))
		u.refresh()
	}
}

// ---- local system messages (embeds) ----

type sysMsg struct {
	tone  color.NRGBA
	icon  fyne.Resource
	title string
	body  string
	ts    time.Time
}

func (u *ui) sysInfo(title, body string) {
	u.pushSys(sysMsg{cAccent, theme.NewPrimaryThemedResource(theme.InfoIcon()), title, body, time.Now()})
}

func (u *ui) sysOK(title, body string) {
	u.pushSys(sysMsg{cGreen, theme.NewSuccessThemedResource(theme.ConfirmIcon()), title, body, time.Now()})
}

func (u *ui) sysErr(body string) {
	u.pushSys(sysMsg{cRed, theme.NewErrorThemedResource(theme.ErrorIcon()), "Nie wyszło", body, time.Now()})
}

// pushSys: without a selected chat there's nowhere to place embed — fallback to dialog
func (u *ui) pushSys(m sysMsg) {
	ch := u.cur()
	if ch == nil {
		u.info("szept — "+m.title, m.body)
		return
	}
	k := string(ch.ID)
	u.sys[k] = append(u.sys[k], m)
	if n := len(u.sys[k]); n > maxSys {
		u.sys[k] = u.sys[k][n-maxSys:]
	}
	u.refresh()
}

func (u *ui) dropSys(i int) {
	ch := u.cur()
	if ch == nil {
		return
	}
	k := string(ch.ID)
	if i < 0 || i >= len(u.sys[k]) {
		return
	}
	u.sys[k] = append(u.sys[k][:i], u.sys[k][i+1:]...)
	u.refresh()
}

// embedCard: panel with colored strip on the left.
// Border gives strip its minimum width and full card height.
func embedCard(tone color.NRGBA, content, right fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = 8
	bar := canvas.NewRectangle(tone)
	bar.CornerRadius = 2
	bar.SetMinSize(fyne.NewSize(4, 4))
	// Padded on outside: embed lies in the same column as bubbles, which never
	// touch screen edge — without this, colored strip went all the way to the edge.
	return container.NewPadded(container.NewStack(bg,
		container.NewBorder(nil, nil, bar, right, container.NewPadded(content))))
}

func (u *ui) sysEmbed(i int, s sysMsg) fyne.CanvasObject {
	title := canvas.NewText(s.title, s.tone)
	title.TextSize = fs(12)
	title.TextStyle = fyne.TextStyle{Bold: true}
	body := widget.NewLabel(s.body)
	body.Wrapping = fyne.TextWrapWord
	drop := widget.NewButtonWithIcon("", theme.CancelIcon(), func() { u.dropSys(i) })
	drop.Importance = widget.LowImportance
	head := container.NewHBox(widget.NewIcon(s.icon), container.NewCenter(title), layout.NewSpacer())
	content := container.NewVBox(head, body,
		mutedText(s.ts.Format("15:04")+" · widzisz to tylko ty", 10))
	return embedCard(s.tone, content, container.NewVBox(drop, layout.NewSpacer()))
}

// infoEmbed: membership and rekey events as a centred chip, the same shape as
// the day separator.
//
// These used to be full-width cards with a coloured strip, which on a phone
// turned "ala added bob" into a block as loud as a message — and a chat opens
// on exactly those events, so the first screen of every new conversation was a
// wall of them. The kind is now a coloured dot; the text carries the rest.
func infoEmbed(m core.Msg) fyne.CanvasObject {
	tone := cMuted
	switch m.Info {
	case event.KindCreate:
		tone = cAccent
	case event.KindAdd:
		tone = cGreen
	case event.KindRemove:
		tone = cRed
	case event.KindRekey:
		tone = cOrange
	}
	dot := container.NewCenter(container.NewGridWrap(fyne.NewSize(6, 6), canvas.NewCircle(tone)))
	txt := canvas.NewText(m.Text+" · "+m.TS.Format("15:04"), cMuted)
	txt.TextSize = fs(11)
	return pad(3, 0, container.NewCenter(
		pill(cSurface, 999, pad(4, 11, tightHBox(6, dot, container.NewCenter(txt))))))
}

// ---- message entry with autocomplete ----

// cmdEntry: Entry that yields arrow keys/Tab/Enter to hint list instead of
// moving cursor and submitting. AcceptsTab determines whether Tab reaches TypedKey
// (otherwise driver moves focus away).
type cmdEntry struct {
	widget.Entry
	keys    func(*fyne.KeyEvent) bool
	wantTab func() bool
}

func newCmdEntry() *cmdEntry {
	e := &cmdEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *cmdEntry) AcceptsTab() bool { return e.wantTab != nil && e.wantTab() }

func (e *cmdEntry) TypedKey(k *fyne.KeyEvent) {
	if e.keys != nil && e.keys(k) {
		return
	}
	e.Entry.TypedKey(k)
}

func (u *ui) maxHints() int {
	if u.mobile { // keyboard sits below on mobile
		return 4
	}
	return 6
}

func (u *ui) buildComposer() fyne.CanvasObject {
	e := newCmdEntry()
	e.PlaceHolder = "napisz wiadomość albo / po komendy…"
	if u.mobile { // on phone long placeholder doesn't fit in input
		e.PlaceHolder = "wiadomość albo /"
	}
	u.entry = e
	u.hintBox = container.NewVBox()
	u.hintBox.Hide()

	e.OnChanged = func(s string) {
		if ch := u.cur(); ch != nil {
			u.c.SendTyping(ch.ID, len(s) > 0 && !strings.HasPrefix(s, "/"))
		}
		u.updateHints()
	}
	e.OnSubmitted = func(s string) { u.submit(s) }
	e.keys = u.hintKey
	e.wantTab = func() bool { return u.hintBox.Visible() }

	send := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() { u.submit(e.Text) })
	send.Importance = widget.HighImportance
	// paperclip: full page with gallery and system picker (media.go);
	// text from input field goes as attachment caption
	add := widget.NewButtonWithIcon("", theme.ContentAddIcon(), u.attachPage)
	add.Importance = widget.LowImportance
	row := container.NewBorder(nil, nil, add, send, e)
	// the composer is chrome, so it takes the darkest surface — the field then
	// sits a step ABOVE its bar instead of melting into it, and the accented
	// send button is the brightest thing on the screen's bottom edge
	return container.NewStack(canvas.NewRectangle(cDeep), pad(7, 8, row))
}

func (u *ui) submit(s string) {
	s = strings.TrimSpace(s)
	if ch := u.cur(); ch != nil {
		u.c.SendTyping(ch.ID, false)
	}
	if s == "" {
		return
	}
	u.hideHints()
	u.entry.SetText("")
	if strings.HasPrefix(s, "/") {
		u.runCommand(s)
		return
	}
	ch := u.cur()
	if ch == nil {
		u.sysErr("Najpierw wybierz czat z listy.")
		return
	}
	if err := u.c.Send(ch.ID, s); err != nil {
		u.fail(err)
	}
}

// matchCommands: match by substring until arguments are typed
func (u *ui) matchCommands(text string) []cmdDef {
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \n") {
		return nil
	}
	q := strings.ToLower(strings.TrimPrefix(text, "/"))
	var out []cmdDef
	for _, c := range commands {
		if u.mobile && c.noMobile {
			continue
		}
		if strings.Contains(c.name[1:], q) {
			out = append(out, c)
		}
	}
	return out
}

func (u *ui) hideHints() {
	if u.hintBox != nil && u.hintBox.Visible() {
		u.hintBox.Hide()
	}
	u.hintSel = 0
}

func (u *ui) updateHints() {
	u.hints = u.matchCommands(u.entry.Text)
	n := len(u.hints)
	if n > u.maxHints() {
		n = u.maxHints()
	}
	if n == 0 {
		u.hideHints()
		return
	}
	if u.hintSel >= n {
		u.hintSel = 0
	}
	// the list is a card floating above the composer: built into a child box, so
	// the plate is not wiped by the RemoveAll on the next keystroke
	list := tightVBox(3, mutedText("KOMENDY", 10))
	for i := 0; i < n; i++ {
		i, c := i, u.hints[i]
		row := widget.NewButton(strings.TrimSpace(c.name+" "+c.args)+"  —  "+c.desc,
			func() { u.pickHint(i) })
		row.Alignment = widget.ButtonAlignLeading
		row.Importance = widget.LowImportance
		if i == u.hintSel {
			row.Importance = widget.HighImportance
		}
		list.Add(row)
	}
	if len(u.hints) > n {
		list.Add(mutedText("…i jeszcze kilka — dopisz literę", 10))
	}
	u.hintBox.RemoveAll()
	u.hintBox.Add(pad(0, 8, pill(cSurface, 16, pad(9, 10, list))))
	u.hintBox.Show()
	u.hintBox.Refresh()
}

// pickHint: inserts command name and space — user then types arguments
func (u *ui) pickHint(i int) {
	if i < 0 || i >= len(u.hints) {
		return
	}
	u.entry.SetText(u.hints[i].name + " ")
	u.entry.CursorColumn = len(u.entry.Text)
	u.entry.Refresh()
	u.hideHints()
	u.w.Canvas().Focus(u.entry) // mouse click on hint stole focus for the button
}

func (u *ui) hintKey(k *fyne.KeyEvent) bool {
	if u.hintBox == nil || !u.hintBox.Visible() {
		return false
	}
	n := len(u.hints)
	if n > u.maxHints() {
		n = u.maxHints()
	}
	switch k.Name {
	case fyne.KeyUp:
		u.hintSel = (u.hintSel + n - 1) % n
		u.updateHints()
	case fyne.KeyDown:
		u.hintSel = (u.hintSel + 1) % n
		u.updateHints()
	case fyne.KeyTab, fyne.KeyReturn, fyne.KeyEnter:
		u.pickHint(u.hintSel)
	case fyne.KeyEscape:
		u.hideHints()
	default:
		return false
	}
	return true
}
