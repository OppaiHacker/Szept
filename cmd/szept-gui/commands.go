// Komendy / (jak na Discordzie): pole wiadomości rozpoznaje wpis od "/",
// podpowiada dopasowane komendy nad polem, a odpowiedzi wracają jako lokalne
// „embedy” systemowe w rozmowie — widoczne tylko u nas, nie lecą do sieci.
package main

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/event"
	"szept/internal/identity"
)

// maxSys: starsze komunikaty spadają z góry — rozmowa nie ma być logiem
const maxSys = 8

// ---- rejestr komend ----

type cmdDef struct {
	name     string
	args     string
	desc     string
	noMobile bool // kamera/ekran nie mają źródła na Androidzie
	run      func(*ui, string)
}

// wypełniane w init(), nie w inicjalizatorze: /pomoc czyta ten sam rejestr,
// a Go nie przepuszcza cyklu zmienna→funkcja→zmienna
var commands []cmdDef

func init() {
	commands = []cmdDef{
		{name: "/pomoc", desc: "spis komend", run: (*ui).cmdHelp},
		{name: "/nowy", args: "<nazwa>", desc: "nowa grupa", run: (*ui).cmdNew},
		{name: "/dm", args: "<znajomy>", desc: "czat na dwoje ze znajomym", run: (*ui).cmdDM},
		{name: "/kod", desc: "mój kod QR i pole na cudzy", run: func(u *ui, _ string) { u.codePage() }},
		{name: "/znajomy", args: "<kod SZF->", desc: "dodaj znajomego z kodu", run: (*ui).cmdApply},
		{name: "/dolacz", args: "<kod SZC->", desc: "wejdź do grupy z zaproszenia", run: (*ui).cmdApply},
		{name: "/zapros", desc: "kod zaproszenia + PIN do tego czatu", run: needChat((*ui).cmdInvite)},
		{name: "/kto", desc: "członkowie czatu", run: needChat((*ui).cmdWho)},
		{name: "/kick", args: "<nazwa>", desc: "usuń członka (rotuje klucz)", run: needChat((*ui).cmdKick)},
		{name: "/rekey", desc: "rotacja klucza — nowa epoka", run: needChat((*ui).cmdRekey)},
		{name: "/zmien", args: "<nazwa>", desc: "zmień nazwę czatu", run: needChat((*ui).cmdRename)},
		{name: "/call", desc: "dołącz do rozmowy", run: needChat((*ui).cmdCall)},
		{name: "/rozlacz", desc: "wyjdź z rozmowy", run: needChat((*ui).cmdHang)},
		{name: "/mic", desc: "mikrofon włącz/wyłącz", run: needChat((*ui).cmdMic)},
		{name: "/cam", args: "[urządzenie]", desc: "kamera włącz/wyłącz", noMobile: true, run: needChat((*ui).cmdCam)},
		{name: "/ekran", desc: "udostępnianie ekranu", noMobile: true, run: needChat((*ui).cmdScreen)},
		{name: "/wyslij", desc: "zdjęcie, GIF albo film z dysku", run: needChat((*ui).cmdSendFile)},
		{name: "/awatar", desc: "zmień swój awatar", run: func(u *ui, _ string) { u.avatarPage() }},
		{name: "/connector", args: "[adres]", desc: "pokaż albo zmień connector", run: (*ui).cmdConnector},
		{name: "/ja", desc: "twój profil i ustawienia", run: (*ui).cmdMe},
		{name: "/wyczysc", desc: "schowaj komunikaty systemowe", run: (*ui).cmdClear},
	}
}

// needChat: komendy działające na rozmowie mają jeden wspólny warunek wstępny
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

// ---- implementacje ----

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
	id, err := u.c.CreateChat(arg)
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
		if strings.EqualFold(f.Name, arg) ||
			strings.HasPrefix(strings.ToLower(f.Name), strings.ToLower(arg)) {
			id, err := u.c.CreateDirect(f)
			if err != nil {
				u.sysErr(err.Error())
				return
			}
			u.openChat(id)
			u.sysOK("Czat z "+f.Name, "Piszcie — wiadomości szyfrowane od końca do końca.")
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
	for _, m := range ch.Members {
		if strings.EqualFold(m.Name, arg) {
			if err := u.c.Kick(ch.ID, m.Pub); err != nil {
				u.sysErr(err.Error())
				return
			}
			u.sysOK("Usunięto "+m.Name, "Klucz zrotowany — dalszych wiadomości już nie odczyta.")
			return
		}
	}
	u.sysErr("Nie ma tu nikogo o nazwie „" + arg + "”. Lista: /kto")
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
	if err := u.c.ToggleScreen(ch.ID); err != nil {
		u.sysErr(err.Error())
	}
}

func (u *ui) cmdSendFile(*core.ChatInfo, string) { u.attachPage() }

// cmdMe: profil i ustawienia — na telefonie to zakładka, na pulpicie strona.
func (u *ui) cmdMe(string) {
	if u.mobile {
		u.selectTab(2)
		return
	}
	u.nav.SelectIndex(2)
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

// ---- lokalne komunikaty systemowe (embedy) ----

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

// pushSys: bez wybranego czatu nie ma gdzie wstawić embedu — wtedy dialog
func (u *ui) pushSys(m sysMsg) {
	ch := u.cur()
	if ch == nil {
		dialog.ShowInformation("szept — "+m.title, m.body, u.w)
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

// embedCard: panel z kolorowym paskiem po lewej — jak embed na Discordzie.
// Border daje paskowi jego szerokość minimalną i pełną wysokość karty.
func embedCard(tone color.NRGBA, content, right fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = 8
	bar := canvas.NewRectangle(tone)
	bar.CornerRadius = 2
	bar.SetMinSize(fyne.NewSize(4, 4))
	return container.NewStack(bg,
		container.NewBorder(nil, nil, bar, right, container.NewPadded(content)))
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

// infoEmbed: zdarzenia członkostwa i rotacji klucza — jedna linia w embedzie
func infoEmbed(m core.Msg) fyne.CanvasObject {
	tone, icon := cMuted, theme.InfoIcon()
	switch m.Info {
	case event.KindCreate:
		tone, icon = cAccent, theme.NewPrimaryThemedResource(theme.HomeIcon())
	case event.KindAdd:
		tone, icon = cGreen, theme.NewSuccessThemedResource(theme.ContentAddIcon())
	case event.KindRemove:
		tone, icon = cRed, theme.NewErrorThemedResource(theme.CancelIcon())
	case event.KindRekey:
		tone, icon = cOrange, theme.NewWarningThemedResource(theme.ViewRefreshIcon())
	}
	txt := canvas.NewText(m.Text, cFg)
	txt.TextSize = fs(12)
	return embedCard(tone, container.NewHBox(
		widget.NewIcon(icon), hspace(2), container.NewCenter(txt), layout.NewSpacer(),
		container.NewCenter(mutedText(m.TS.Format("15:04"), 10))), nil)
}

// ---- pole wiadomości z podpowiedziami ----

// cmdEntry: Entry, które oddaje strzałki/Tab/Enter listy podpowiedzi, zamiast
// przesuwać kursor i wysyłać. AcceptsTab decyduje, czy Tab dojdzie do TypedKey
// (inaczej driver przenosi fokus dalej).
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
	if u.mobile { // niżej i tak stoi klawiatura
		return 4
	}
	return 6
}

func (u *ui) buildComposer() fyne.CanvasObject {
	e := newCmdEntry()
	e.PlaceHolder = "napisz wiadomość albo / po komendy…"
	if u.mobile { // na telefonie długi placeholder nie mieści się w polu
		e.PlaceHolder = "wiadomość albo /"
	}
	u.entry = e
	u.hintBox = container.NewVBox()
	u.hintBox.Hide()

	e.OnChanged = func(string) { u.updateHints() }
	e.OnSubmitted = func(s string) { u.submit(s) }
	e.keys = u.hintKey
	e.wantTab = func() bool { return u.hintBox.Visible() }

	send := widget.NewButtonWithIcon("", theme.MailSendIcon(), func() { u.submit(e.Text) })
	send.Importance = widget.HighImportance
	// spinacz: pełna strona z galerią i systemowym wyborem (media.go);
	// tekst z pola leci jako podpis pod plikiem
	add := widget.NewButtonWithIcon("", theme.ContentAddIcon(), u.attachPage)
	return container.NewBorder(nil, nil, add, send, e)
}

func (u *ui) submit(s string) {
	s = strings.TrimSpace(s)
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
		dialog.ShowError(err, u.w)
	}
}

// matchCommands: dopasowanie po fragmencie nazwy, dopóki nie ma argumentów
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
	u.hintBox.RemoveAll()
	u.hintBox.Add(mutedText("KOMENDY", 10))
	for i := 0; i < n; i++ {
		i, c := i, u.hints[i]
		row := widget.NewButton(strings.TrimSpace(c.name+" "+c.args)+"  —  "+c.desc,
			func() { u.pickHint(i) })
		row.Alignment = widget.ButtonAlignLeading
		row.Importance = widget.LowImportance
		if i == u.hintSel {
			row.Importance = widget.HighImportance
		}
		u.hintBox.Add(row)
	}
	if len(u.hints) > n {
		u.hintBox.Add(mutedText("…i jeszcze kilka — dopisz literę", 10))
	}
	u.hintBox.Show()
	u.hintBox.Refresh()
}

// pickHint: wstawia nazwę i spację — dalej użytkownik dopisuje argument
func (u *ui) pickHint(i int) {
	if i < 0 || i >= len(u.hints) {
		return
	}
	u.entry.SetText(u.hints[i].name + " ")
	u.entry.CursorColumn = len(u.entry.Text)
	u.entry.Refresh()
	u.hideHints()
	u.w.Canvas().Focus(u.entry) // klik w podpowiedź myszką zabrał fokus przyciskowi
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
