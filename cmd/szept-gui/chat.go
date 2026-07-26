package main

// Lista czatów i widok rozmowy. Układ wzorowany na mobilnym Discordzie:
// wiersz czatu = awatar + nazwa + zajawka ostatniej wiadomości + odznaka,
// rozmowa = osobna pełnoekranowa strona z paskiem, dymkami i polem tekstowym.

import (
	"bytes"
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

	"szept/internal/attach"
	"szept/internal/core"
	"szept/internal/identity"
)

// msgPage: ile dymków rysujemy od razu. Telefon nie ma po co budować pół
// tysiąca widgetów przy wejściu do czatu — starsze dociąga przycisk u góry.
const msgPage = 40

// grupowanie: kolejne wiadomości tej samej osoby w tym odstępie zlewają się
// w jeden blok (jeden podpis, jeden awatar) — tak robi Discord.
const groupWindow = 5 * time.Minute

// ---- lista czatów ----

// rebuildChats: mobile rysuje własne wiersze, pulpit odświeża widget.List.
func (u *ui) rebuildChats() {
	if !u.mobile {
		u.chatLst.Refresh()
		if len(u.rows) > 0 {
			typing := u.entry != nil && u.w.Canvas().Focused() == fyne.Focusable(u.entry)
			u.chatLst.Select(u.sel) // podświetlenie aktywnego czatu
			if typing {
				u.w.Canvas().Focus(u.entry) // Select kradnie fokus polu
			}
		}
		return
	}
	u.chatBox.RemoveAll()
	for i, r := range u.rows {
		i, r := i, r
		u.chatBox.Add(u.chatRow(r, func() {
			u.sel = i
			u.shown = 0
			u.openConv()
		}))
	}
	if len(u.rows) == 0 {
		u.chatBox.Add(container.NewPadded(card(container.NewVBox(
			widget.NewLabelWithStyle("Pusto tutaj", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			mutedWrap("Dodaj znajomego jego kodem albo załóż grupę — przyciski są nad listą."),
		))))
	}
	u.chatBox.Refresh()
}

// chatRow: awatar, nazwa, zajawka, po prawej godzina i odznaka.
func (u *ui) chatRow(r chatRow, tap func()) fyne.CanvasObject {
	prev := u.chatPreview(r.info)
	body := twoLine(r.info.Name, prev.text, 15, 12, 22)
	when := mutedText("", 10)
	if !prev.ts.IsZero() {
		when.Text = shortTime(prev.ts)
	}
	right := container.NewVBox(container.NewCenter(when))
	if r.unread > 0 {
		right.Add(container.NewCenter(badge(r.unread)))
	} else if len(r.info.InCall) > 0 {
		call := canvas.NewText("📞", cGreen)
		call.TextSize = fs(12)
		right.Add(container.NewCenter(call))
	} else if r.info.Online > 0 {
		on := canvas.NewText(fmt.Sprintf("● %d", r.info.Online), cGreen)
		on.TextSize = fs(10)
		right.Add(container.NewCenter(on))
	}

	// awatar grupy: przy dwóch osobach twarz rozmówcy, w grupie inicjał nazwy
	av := u.chatAvatar(r.info)
	row := container.NewBorder(nil, nil,
		container.NewHBox(hspace(2), container.NewCenter(av), hspace(8)),
		right, container.NewCenter(body))
	return tappable(container.NewPadded(row), tap, nil)
}

func (u *ui) chatAvatar(ci core.ChatInfo) fyne.CanvasObject {
	if len(ci.Members) == 2 {
		for _, m := range ci.Members {
			if !bytes.Equal(m.Pub, u.c.ID.Pub) {
				return u.avatarView(m.Pub, m.Name, avatarBig)
			}
		}
	}
	return u.avatarView(ci.ID, ci.Name, avatarBig)
}

func badge(n int) fyne.CanvasObject {
	bg := canvas.NewRectangle(cAccent)
	bg.CornerRadius = 9
	txt := canvas.NewText(fmt.Sprint(n), theme.Color(theme.ColorNameForegroundOnPrimary))
	if n > 99 {
		txt.Text = "99+"
	}
	txt.TextSize = fs(10)
	txt.TextStyle = fyne.TextStyle{Bold: true}
	w := float32(22)
	if len(txt.Text) > 2 {
		w = 30
	}
	return container.NewGridWrap(fyne.NewSize(w, 18),
		container.NewStack(bg, container.NewCenter(txt)))
}

// chatPreview: ostatnia wiadomość czatu, z cache pod liczbą zdarzeń —
// odszyfrowanie całej historii każdego czatu przy każdym odświeżeniu byłoby
// widoczne na telefonie.
func (u *ui) chatPreview(ci core.ChatInfo) chatPrev {
	key := string(ci.ID)
	n := u.c.St.EventCount(ci.ID)
	if p, ok := u.prev[key]; ok && p.n == n {
		return p
	}
	p := chatPrev{n: n, text: "brak wiadomości"}
	msgs := u.c.Messages(ci.ID)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Kind == "info" {
			continue
		}
		who := ""
		if bytes.Equal(m.Author, u.c.ID.Pub) {
			who = "Ty: "
		} else if len(ci.Members) > 2 {
			who = m.Name + ": "
		}
		body := m.Text
		if m.Kind == "file" {
			body = "📎 " + m.File
			if m.Text != "" {
				body += " · " + m.Text
			}
		}
		p.text = who + strings.ReplaceAll(body, "\n", " ")
		p.ts = m.TS
		break
	}
	u.prev[key] = p
	return p
}

// shortTime: godzina dla dzisiejszych, data dla starszych.
func shortTime(t time.Time) string {
	now := time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	if now.Sub(t) < 7*24*time.Hour {
		return []string{"nd", "pn", "wt", "śr", "cz", "pt", "sb"}[int(t.Weekday())]
	}
	return t.Format("02.01")
}

var plMonths = []string{"sty", "lut", "mar", "kwi", "maj", "cze",
	"lip", "sie", "wrz", "paź", "lis", "gru"}

// dayLabel: separator dnia w rozmowie.
func dayLabel(t time.Time) string {
	now := time.Now()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch today.Sub(d) {
	case 0:
		return "Dziś"
	case 24 * time.Hour:
		return "Wczoraj"
	}
	return fmt.Sprintf("%d %s %d", t.Day(), plMonths[int(t.Month())-1], t.Year())
}

// ---- lista czatów na pulpicie (widget.List) ----

func (u *ui) chatCount() int { return len(u.rows) }

func (u *ui) chatTemplate() fyne.CanvasObject {
	name := widget.NewLabel("czat")
	name.Truncation = fyne.TextTruncateEllipsis
	call := canvas.NewText("📞", cGreen)
	call.TextSize = fs(12)
	online := canvas.NewText("", cGreen)
	online.TextSize = fs(12)
	badgeBg := canvas.NewRectangle(cAccent)
	badgeBg.CornerRadius = 9
	badgeTxt := canvas.NewText("0", theme.Color(theme.ColorNameForegroundOnPrimary))
	badgeTxt.TextSize = fs(11)
	badgeTxt.TextStyle = fyne.TextStyle{Bold: true}
	b := container.NewGridWrap(fyne.NewSize(22, 18),
		container.NewStack(badgeBg, container.NewCenter(badgeTxt)))
	right := container.NewHBox(container.NewCenter(call), container.NewCenter(online),
		container.NewCenter(b), hspace(2))
	return container.NewBorder(nil, nil, nil, right, name)
}

func (u *ui) chatUpdate(i widget.ListItemID, o fyne.CanvasObject) {
	if i >= len(u.rows) {
		return
	}
	r := u.rows[i]
	border := o.(*fyne.Container)
	name := border.Objects[0].(*widget.Label)
	right := border.Objects[1].(*fyne.Container)
	call := right.Objects[0].(*fyne.Container).Objects[0].(*canvas.Text)
	online := right.Objects[1].(*fyne.Container).Objects[0].(*canvas.Text)
	b := right.Objects[2].(*fyne.Container).Objects[0].(*fyne.Container)
	badgeTxt := b.Objects[0].(*fyne.Container).Objects[1].(*fyne.Container).Objects[0].(*canvas.Text)

	name.TextStyle = fyne.TextStyle{Bold: r.unread > 0}
	name.SetText(r.info.Name)
	name.Refresh() // SetText nie odświeża przy samej zmianie TextStyle
	if len(r.info.InCall) > 0 {
		call.Show()
	} else {
		call.Hide()
	}
	if r.info.Online > 0 {
		online.Text = fmt.Sprintf("● %d", r.info.Online)
		online.Show()
	} else {
		online.Hide()
	}
	online.Refresh()
	if r.unread > 0 {
		badgeTxt.Text = fmt.Sprintf("%d", r.unread)
		b.Show()
	} else {
		b.Hide()
	}
	b.Refresh()
}

// ---- widok rozmowy ----

func (u *ui) buildConv() {
	u.title = canvas.NewText("", cFg)
	u.title.TextSize = fs(16)
	u.title.TextStyle = fyne.TextStyle{Bold: true}
	u.subtitle = mutedText("", 11)
	u.headAvatar = container.NewStack()
	u.msgBox = container.NewVBox()
	u.msgScr = container.NewVScroll(u.msgBox)

	composer := u.buildComposer() // pole wiadomości + komendy / (commands.go)
	u.buildCallControls()

	more := widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), nil)
	more.OnTapped = func() { u.chatMenu(more) }
	actions := container.NewHBox(u.callBtn, more)

	var back fyne.CanvasObject = hspace(4)
	if u.mobile {
		b := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { u.popPage() })
		b.Importance = widget.LowImportance
		back = b
	}
	head := container.NewBorder(nil, nil,
		container.NewHBox(back, container.NewCenter(u.headAvatar), hspace(6)),
		actions,
		container.NewVBox(layout.NewSpacer(), u.title, u.subtitle, layout.NewSpacer()))
	bar := container.NewStack(canvas.NewRectangle(cSurface),
		container.NewVBox(container.NewPadded(head), canvas.NewRectangle(cSep)))

	bottom := container.NewVBox(u.hintBox, u.banner, composer)
	u.convRoot = container.NewBorder(bar, bottom, nil, nil, u.msgScr)
}

// buildCallControls: przyciski mediów żyją w banerze rozmowy, więc poza
// rozmową pasek tytułu zostaje pusty.
func (u *ui) buildCallControls() {
	lbl := func(s string) string {
		if u.mobile {
			return ""
		}
		return s
	}
	u.callBtn = widget.NewButtonWithIcon(lbl("Rozmowa"), theme.MediaPlayIcon(), func() {
		ch := u.cur()
		if ch == nil {
			return
		}
		if call := u.c.CallState(ch.ID); call != nil && call.Joined {
			u.c.LeaveCall(ch.ID)
		} else if err := u.c.JoinCall(ch.ID); err != nil {
			u.fail(err)
		}
		u.refresh()
	})
	u.callBtn.Importance = widget.HighImportance
	u.micBtn = widget.NewButtonWithIcon(lbl("Mic"), theme.MediaRecordIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.c.ToggleMic(ch.ID)
			u.refresh()
		}
	})
	u.camBtn = widget.NewButtonWithIcon(lbl("Kamera"), theme.MediaVideoIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.fail(u.c.ToggleCam(ch.ID, ""))
		}
	})
	u.scrBtn = widget.NewButtonWithIcon(lbl("Ekran"), theme.ComputerIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.fail(u.c.ToggleScreen(ch.ID))
		}
	})
	hang := widget.NewButtonWithIcon(lbl("Rozłącz"), theme.MediaStopIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.c.LeaveCall(ch.ID)
			u.refresh()
		}
	})
	hang.Importance = widget.DangerImportance

	u.bannerDot = canvas.NewCircle(cRed)
	u.bannerLbl = canvas.NewText("", cFg)
	u.bannerLbl.TextSize = fs(13)
	u.bannerLbl.TextStyle = fyne.TextStyle{Bold: true}
	bg := canvas.NewRectangle(cBubbleMe)
	bg.CornerRadius = 10
	dot := container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), u.bannerDot))
	if u.mobile {
		// kamera i ekran nie mają na Androidzie źródła (brak V4L2/X11) — nie kuś
		u.callBar = container.NewHBox(u.micBtn, hang)
	} else {
		u.callBar = container.NewHBox(u.micBtn, u.camBtn, u.scrBtn, hang)
	}
	head := container.NewHBox(dot, hspace(6), container.NewCenter(u.bannerLbl), layout.NewSpacer())
	var body fyne.CanvasObject
	if u.mobile { // wąski ekran: przyciski pod tekstem
		body = container.NewVBox(head, u.callBar)
	} else {
		body = container.NewBorder(nil, nil, nil, u.callBar, head)
	}
	u.banner = container.NewStack(bg, container.NewPadded(body))
	u.banner.Hide()
}

func (u *ui) updateConvHeader(ch *core.ChatInfo) {
	name := ch.Name
	if u.mobile { // canvas.Text nie zawija — długa nazwa wypchnęłaby przyciski poza ekran
		name = trunc(name, 16)
	}
	u.title.Text = name
	u.title.Refresh()
	on := ""
	if ch.Online > 0 {
		on = fmt.Sprintf(" · %d online", ch.Online)
	}
	u.subtitle.Text = fmt.Sprintf("%d członków%s", len(ch.Members), on)
	if u.mobile {
		// canvas.Text nie zawija ani nie przycina: pełny podpis wchodził pod
		// przyciski po prawej. Na wąskim ekranie ważniejsze jest „kto jest”.
		u.subtitle.Text = fmt.Sprintf("%d osób", len(ch.Members))
		if ch.Online > 0 {
			u.subtitle.Text = fmt.Sprintf("%d z %d online", ch.Online+1, len(ch.Members))
		}
	}
	u.subtitle.Refresh()
	u.headAvatar.Objects = []fyne.CanvasObject{u.chatAvatar(*ch)}
	u.headAvatar.Refresh()
}

// openConv: wchodzi w rozmowę — na telefonie jako pełnoekranowa strona.
func (u *ui) openConv() {
	u.shown = 0
	u.msgSig = ""
	u.refresh()
	if !u.mobile {
		return
	}
	if u.convPage == nil {
		u.convPage = container.NewStack(backdrop(), u.convRoot)
	}
	if !u.onStack(u.convPage) {
		u.root.Add(u.convPage)
	}
	u.root.Refresh()
	u.refresh() // dopiero teraz chatOpen() widzi stronę i rysuje wiadomości
}

// openChat: przełącza na wskazany czat i (na telefonie) otwiera jego stronę.
func (u *ui) openChat(chat []byte) {
	u.refresh()
	for i, r := range u.rows {
		if bytes.Equal(r.info.ID, chat) {
			u.sel = i
			break
		}
	}
	if u.mobile {
		u.popAllPages()
	}
	u.openConv()
}

// ---- rysowanie wiadomości ----

func (u *ui) renderMessages(ch *core.ChatInfo) {
	msgs := u.c.Messages(ch.ID)
	if u.shown == 0 {
		u.shown = msgPage
	}
	older := 0
	if len(msgs) > u.shown {
		older = len(msgs) - u.shown
		msgs = msgs[older:]
	}
	sig := u.msgSignature(ch, msgs, older)
	if sig == u.msgSig {
		return // nic się nie zmieniło — nie ruszamy przewijania ani nie dekodujemy obrazków
	}
	sameChat := u.lastChat == string(ch.ID)
	stick := !sameChat || u.nearBottom()
	keep := u.msgScr.Offset.Y
	oldH := u.msgBox.MinSize().Height
	u.msgSig = sig
	u.msgAnims = u.msgAnims[:0] // dymki idą do kosza razem z ich animacjami

	u.msgBox.RemoveAll()
	// sloty na odtwarzanie GIF-ów dostają najnowsze wiadomości — stąd przegląd
	// listy od końca
	animate := map[int]bool{}
	for i, slots := len(msgs)-1, 0; i >= 0 && slots < maxAnims; i-- {
		if msgs[i].Kind == "file" && attach.Animated(msgs[i].Mime) {
			animate[i] = true
			slots++
		}
	}
	if older > 0 {
		u.msgBox.Add(container.NewPadded(widget.NewButton(
			fmt.Sprintf("Pokaż wcześniejsze (%d)", older), func() {
				cur := u.cur() // lista czatów mogła się w tym czasie przetasować
				if cur == nil {
					return
				}
				u.shown += msgPage
				u.msgSig = ""
				u.grew = true
				u.renderMessages(cur)
			})))
	}
	var lastBubble *canvas.Rectangle
	var lastOwn bool
	var prev *core.Msg
	for mi := range msgs {
		m := msgs[mi]
		if m.Kind == "info" {
			u.msgBox.Add(infoEmbed(m))
			prev = nil
			continue
		}
		if prev == nil || prev.TS.Day() != m.TS.Day() || prev.TS.Month() != m.TS.Month() {
			u.msgBox.Add(daySeparator(m.TS))
			prev = nil
		}
		grouped := prev != nil && bytes.Equal(prev.Author, m.Author) &&
			m.TS.Sub(prev.TS) < groupWindow && m.TS.After(prev.TS.Add(-groupWindow))
		u.msgBox.Add(u.bubbleRow(m, animate[mi], grouped, &lastBubble, &lastOwn))
		prev = &msgs[mi]
	}
	if len(msgs) == 0 {
		u.msgBox.Add(container.NewPadded(container.NewCenter(mutedWrap(
			"Nikt jeszcze nic nie napisał — przełam pierwsze lody, albo wpisz / po komendy."))))
	}
	// odpowiedzi komend: lokalne, nie lecą do sieci — zawsze na końcu rozmowy
	for i, s := range u.sys[string(ch.ID)] {
		u.msgBox.Add(u.sysEmbed(i, s))
	}
	u.msgBox.Refresh()

	// przewijanie: zostajemy tam, gdzie użytkownik czyta. Skok na dół tylko
	// wtedy, gdy i tak był na dole (albo właśnie wszedł do czatu) — inaczej
	// każda przychodząca wiadomość wyrywałaby go z historii.
	switch {
	case stick:
		u.scrollToEnd()
	case u.grew:
		// doładowanie starszych rośnie w GÓRĘ: przesuwamy o tyle, ile doszło,
		// żeby czytana wiadomość została pod tym samym palcem
		u.grew = false
		u.msgScr.ScrollToOffset(fyne.NewPos(0, keep+u.msgBox.MinSize().Height-oldH))
	default:
		u.msgScr.ScrollToOffset(fyne.NewPos(0, keep))
	}

	// mignięcie najnowszej cudzej wiadomości (nie przy zmianie czatu)
	if sameChat && len(msgs)+older > u.lastCount && u.lastCount > 0 && lastBubble != nil && !lastOwn {
		flashBubble(lastBubble)
	}
	u.lastChat = string(ch.ID)
	u.lastCount = len(msgs) + older
}

func flashBubble(bg *canvas.Rectangle) {
	a := canvas.NewColorRGBAAnimation(cAccent, cSurface2, 700*msec, func(c color.Color) {
		bg.FillColor = c
		bg.Refresh()
	})
	a.Start()
}

// msgSignature: odcisk tego, co widać. Bez zmian nie przebudowujemy listy —
// oszczędza dekodowanie obrazków i, co ważniejsze, nie gubi pozycji przewijania.
func (u *ui) msgSignature(ch *core.ChatInfo, msgs []core.Msg, older int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%x|%d|%d|%d|", ch.ID, len(msgs), older, len(u.sys[string(ch.ID)]))
	for _, m := range msgs {
		if m.Kind == "file" {
			fmt.Fprintf(&b, "%x%t%t%t;", m.ID[:4], m.Have, m.HavePrev, m.Getting)
		}
	}
	if n := len(msgs); n > 0 {
		fmt.Fprintf(&b, "%x", msgs[n-1].ID[:6])
	}
	return b.String()
}

// scrollToEnd: skok na koniec rozmowy. container.Scroll liczy maksymalne
// przesunięcie z ROZMIARU treści, a ten jest znany dopiero po ułożeniu widoku —
// przy pierwszym wejściu do czatu wywołanie w tym samym cyklu nic nie robiło.
// Stąd druga próba po chwili.
func (u *ui) scrollToEnd() {
	u.msgScr.ScrollToBottom()
	go func() {
		time.Sleep(60 * time.Millisecond)
		fyne.Do(func() { u.msgScr.ScrollToBottom() })
	}()
}

// nearBottom: czy użytkownik patrzy na koniec rozmowy.
func (u *ui) nearBottom() bool {
	max := u.msgBox.MinSize().Height - u.msgScr.Size().Height
	if max <= 0 {
		return true
	}
	return u.msgScr.Offset.Y >= max-60
}

func daySeparator(t time.Time) fyne.CanvasObject {
	line := canvas.NewRectangle(cSep)
	line.SetMinSize(fyne.NewSize(1, 1))
	lbl := mutedText(dayLabel(t), 10)
	return container.NewPadded(container.NewBorder(nil, nil, nil, nil,
		container.NewHBox(layout.NewSpacer(), container.NewCenter(lbl), layout.NewSpacer())))
}

// bubbleRow: jeden dymek z podpisem i awatarem (albo bez, gdy grupujemy).
func (u *ui) bubbleRow(m core.Msg, animate, grouped bool,
	lastBubble **canvas.Rectangle, lastOwn *bool) fyne.CanvasObject {
	own := bytes.Equal(m.Author, u.c.ID.Pub)
	bg := canvas.NewRectangle(cSurface2)
	if own {
		bg.FillColor = cBubbleMe
	}
	bg.CornerRadius = 14

	var content fyne.CanvasObject
	if m.Kind == "file" {
		body := container.NewVBox(u.attachView(m, animate))
		if m.Text != "" { // podpis pod załącznikiem
			cap := widget.NewLabel(m.Text)
			cap.Wrapping = fyne.TextWrapWord
			body.Add(cap)
		}
		content = container.NewPadded(body)
	} else {
		txt := widget.NewLabel(m.Text)
		txt.Wrapping = fyne.TextWrapWord
		content = txt
	}
	stamp := mutedText(m.TS.Format("15:04"), 9)
	inner := container.NewVBox(content,
		container.NewHBox(layout.NewSpacer(), stamp, hspace(4)))
	bubble := tappable(container.NewStack(bg, inner), nil, func() { u.msgActions(m) })
	*lastBubble, *lastOwn = bg, own

	margin := float32(60)
	if !u.mobile {
		margin = 200
	}
	var row fyne.CanvasObject
	if own {
		row = container.NewBorder(nil, nil, hspace(margin), hspace(2), bubble)
	} else {
		av := fyne.CanvasObject(hspace(avatarSmall))
		if !grouped {
			av = container.NewVBox(u.avatarView(m.Author, m.Name, avatarSmall), layout.NewSpacer())
		}
		left := container.NewHBox(hspace(2), av, hspace(4))
		body := bubble
		if !grouped {
			who := canvas.NewText(m.Name, authorColor(m.Author))
			who.TextSize = fs(11)
			who.TextStyle = fyne.TextStyle{Bold: true}
			return container.NewBorder(nil, nil, left, hspace(margin),
				container.NewVBox(who, bubble))
		}
		row = container.NewBorder(nil, nil, left, hspace(margin), body)
	}
	return row
}

// ---- akcje wiadomości (długie przytrzymanie) ----

func (u *ui) msgActions(m core.Msg) {
	ch := u.cur()
	if ch == nil {
		return
	}
	chat := bytes.Clone(ch.ID) // strona żyje dłużej niż wskaźnik na wiersz listy
	head := widget.NewLabel(trunc(m.Name+" · "+m.TS.Format("02.01 15:04"), 60))
	head.TextStyle = fyne.TextStyle{Bold: true}
	body := m.Text
	if m.Kind == "file" {
		body = m.File + " · " + attach.HumanSize(m.Size)
	}
	quote := widget.NewLabel(trunc(body, 240))
	quote.Wrapping = fyne.TextWrapWord

	rows := container.NewVBox(card(container.NewVBox(head, quote)), vspace(6))
	if m.Text != "" {
		rows.Add(navRow(theme.ContentCopyIcon(), "Kopiuj tekst", "", func() {
			u.w.Clipboard().SetContent(m.Text)
			u.popPage()
			u.toast("Skopiowane")
		}))
	}
	if m.Kind == "file" && m.Have {
		rows.Add(navRow(theme.DocumentSaveIcon(), "Zapisz plik", m.File, func() {
			u.popPage()
			u.saveAttachment(m)
		}))
	}
	if m.Kind == "file" && !m.Have {
		rows.Add(navRow(theme.DownloadIcon(), "Pobierz pełny plik",
			attach.HumanSize(m.Size)+" — nadawca musi być online", func() {
				u.popPage()
				u.fetchFull(m)
			}))
	}
	rows.Add(navRow(theme.VisibilityOffIcon(), "Ukryj u mnie",
		"Zniknie tylko na tym urządzeniu — u innych zostaje", func() {
			u.fail(u.c.HideMsg(chat, m.ID))
			u.msgSig = ""
			u.popPage()
			u.refresh()
		}))
	rows.Add(vspace(8))
	rows.Add(mutedWrap("Historia czatu jest podpisana i replikowana u wszystkich, " +
		"więc „usuń u wszystkich” nie istnieje. Ukrycie działa lokalnie."))
	u.page("Wiadomość", rows)
}

// ---- menu czatu ----

func (u *ui) chatMenu(anchor fyne.CanvasObject) {
	ch := u.cur()
	if ch == nil {
		return
	}
	owner := bytes.Equal(ch.Owner, u.c.ID.Pub)
	if !u.mobile {
		items := []*fyne.MenuItem{fyne.NewMenuItem("Członkowie", u.membersPage)}
		if owner {
			items = append([]*fyne.MenuItem{fyne.NewMenuItem("Zaproś do czatu", u.invitePage)}, items...)
		}
		items = append(items, fyne.NewMenuItem("Zmień nazwę", u.renamePage))
		if u.c.HiddenCount(ch.ID) > 0 {
			items = append(items, fyne.NewMenuItem("Przywróć ukryte", u.unhideAll))
		}
		m := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.w.Canvas())
		m.ShowAtRelativePosition(fyne.NewPos(0, anchor.Size().Height), anchor)
		return
	}
	rows := container.NewVBox()
	if owner {
		rows.Add(navRow(theme.ContentAddIcon(), "Zaproś do czatu",
			"Jednorazowy kod + PIN, ważny 10 minut", u.invitePage))
	} else {
		rows.Add(navRow(theme.InfoIcon(), "Zaproszenia",
			"Wystawia je tylko założyciel czatu", nil))
	}
	rows.Add(navRow(theme.AccountIcon(), "Członkowie",
		fmt.Sprintf("%d osób · %d online", len(ch.Members), ch.Online), u.membersPage))
	rows.Add(navRow(theme.DocumentCreateIcon(), "Zmień nazwę czatu", ch.Name, u.renamePage))
	if n := u.c.HiddenCount(ch.ID); n > 0 {
		rows.Add(navRow(theme.VisibilityIcon(), "Przywróć ukryte",
			fmt.Sprintf("%d schowanych wiadomości", n), u.unhideAll))
	}
	rows.Add(navRow(theme.MediaPlayIcon(), "Rozmowa głosowa", "Dołącz do rozmowy w tym czacie", func() {
		u.popPage()
		u.callBtn.OnTapped()
	}))
	u.page(ch.Name, rows)
}

func (u *ui) unhideAll() {
	if ch := u.cur(); ch != nil {
		u.fail(u.c.UnhideAll(ch.ID))
		u.msgSig = ""
		u.popPage()
		u.refresh()
	}
}

func (u *ui) renamePage() {
	ch := u.cur()
	if ch == nil {
		return
	}
	chat := bytes.Clone(ch.ID)
	name := widget.NewEntry()
	name.SetText(ch.Name)
	save := func() {
		if strings.TrimSpace(name.Text) == "" {
			return
		}
		u.fail(u.c.Rename(chat, strings.TrimSpace(name.Text)))
		u.popPage()
		u.refresh()
	}
	name.OnSubmitted = func(string) { save() }
	u.page("Nazwa czatu", container.NewVBox(name,
		bigButton("Zapisz", theme.ConfirmIcon(), save),
		mutedWrap("Nazwa jest częścią podpisanej historii — zobaczą ją wszyscy członkowie.")))
}

func (u *ui) newGroupPage() {
	name := widget.NewEntry()
	name.PlaceHolder = "np. ekipa-z-osiedla"
	create := func() {
		if strings.TrimSpace(name.Text) == "" {
			return
		}
		id, err := u.c.CreateChat(strings.TrimSpace(name.Text))
		if err != nil {
			u.fail(err)
			return
		}
		u.popPage()
		u.openChat(id)
	}
	name.OnSubmitted = func(string) { create() }
	u.page("Nowa grupa", container.NewVBox(name,
		bigButton("Utwórz", theme.ContentAddIcon(), create),
		mutedWrap("Zostajesz założycielem: tylko ty możesz zapraszać i usuwać ludzi.")))
}

func (u *ui) invitePage() {
	ch := u.cur()
	if ch == nil {
		return
	}
	code, pin, err := u.c.NewInvite(ch.ID)
	if err != nil {
		u.fail(err)
		return
	}
	pinTxt := canvas.NewText(spaced(pin), cAccent)
	pinTxt.TextSize = fs(26)
	pinTxt.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	u.page("Zaproszenie", container.NewVBox(
		u.codeBox(code),
		vspace(6),
		card(container.NewVBox(
			mutedText("PIN", 11),
			container.NewCenter(pinTxt),
			mutedWrap("Podaj PIN drugiej osobie innym kanałem (np. głosowo)."))),
		mutedWrap("Kod jednorazowy · wygasa po 10 minutach."),
	))
}

func (u *ui) membersPage() {
	ch := u.cur()
	if ch == nil {
		return
	}
	amOwner := bytes.Equal(ch.Owner, u.c.ID.Pub)
	box := container.NewVBox()
	for _, m := range ch.Members {
		m := m
		online := u.c.Online(m.Pub) || bytes.Equal(m.Pub, u.c.ID.Pub)
		tag := ""
		if bytes.Equal(m.Pub, ch.Owner) {
			tag = "założyciel"
		}
		box.Add(u.personRow(m.Pub, m.Name, tag, online, func() {
			u.profilePage(m.Pub, m.Name, amOwner && !bytes.Equal(m.Pub, u.c.ID.Pub), ch)
		}))
	}
	if amOwner {
		box.Add(vspace(8))
		box.Add(widget.NewButtonWithIcon("Rotuj klucz (nowa epoka)", theme.ViewRefreshIcon(), func() {
			u.fail(u.c.Rekey(ch.ID))
			u.toast("Nowa epoka klucza")
		}))
		box.Add(mutedWrap("Po rotacji nowe wiadomości szyfruje świeży klucz."))
	}
	u.page("Członkowie — "+ch.Name, box)
}

// personRow: wspólny wiersz osoby (członkowie, znajomi).
func (u *ui) personRow(pub []byte, name, tag string, online bool, tap func()) fyne.CanvasObject {
	dot := canvas.NewText("●", cMuted)
	dot.TextSize = fs(11)
	if online {
		dot.Color = cGreen
	}
	sub := identity.Fingerprint(pub)
	if tag != "" {
		sub = tag + " · " + sub
	}
	row := container.NewBorder(nil, nil,
		container.NewHBox(hspace(2), container.NewCenter(dot), hspace(4),
			container.NewCenter(u.avatarView(pub, name, avatarRow)), hspace(8)),
		container.NewCenter(widget.NewIcon(theme.NavigateNextIcon())),
		container.NewCenter(twoLine(name, sub, 14, 10, 22)))
	return tappable(container.NewPadded(row), tap, nil)
}

// ---- stan rozmowy ----

func (u *ui) renderCallState(ch *core.ChatInfo) {
	call := u.c.CallState(ch.ID)
	switch {
	case call != nil && call.Joined:
		mic := "mic wyłączony"
		icon := theme.MediaRecordIcon()
		if call.Mic {
			mic = "mic włączony"
			icon = theme.MediaPauseIcon()
		}
		u.micBtn.SetIcon(icon)
		u.bannerLbl.Text = fmt.Sprintf("W ROZMOWIE · %d uczest. · %s", len(call.Members)+1, mic)
		u.callBtn.Hide() // rozłączenie jest w banerze
		u.callBar.Show()
		u.showBanner()
	case call != nil && len(call.Members) > 0:
		u.bannerLbl.Text = fmt.Sprintf("Trwa rozmowa (%d) — dołącz", len(call.Members))
		u.callBtn.Show()
		u.callBar.Hide()
		u.showBanner()
	default:
		u.callBtn.Show()
		u.callBar.Hide()
		u.hideBanner()
	}
	u.bannerLbl.Refresh()
}

func (u *ui) showBanner() {
	if !u.banner.Visible() {
		u.banner.Show()
		u.bannerAnim = canvas.NewColorRGBAAnimation(cRed, color.NRGBA{0xf8, 0x71, 0x71, 0x30},
			900*msec, func(c color.Color) {
				u.bannerDot.FillColor = c
				u.bannerDot.Refresh()
			})
		u.bannerAnim.RepeatCount = fyne.AnimationRepeatForever
		u.bannerAnim.AutoReverse = true
		u.bannerAnim.Start()
	}
}

func (u *ui) hideBanner() {
	if u.banner.Visible() {
		u.banner.Hide()
		if u.bannerAnim != nil {
			u.bannerAnim.Stop()
			u.bannerAnim = nil
		}
	}
}
