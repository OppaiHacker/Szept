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
// ponytail: stały sufit, nie prawdziwa wirtualizacja. widget.List umie
// recyklingować wiersze, ale wymaga znanej wysokości każdego z nich, a dymek
// z zawijanym tekstem i miniaturą jej nie ma.
const msgPage = 30

// grupowanie: kolejne wiadomości tej samej osoby w tym odstępie zlewają się
// w jeden blok (jeden podpis, jeden awatar) — tak robi Discord.
const groupWindow = 5 * time.Minute

// ---- lista czatów ----

// rebuildChats: te same dotykowe wiersze na obu platformach. Pulpit miał tu
// widget.List z samą nazwą czatu — bez awatara, zajawki i godziny wyglądał jak
// inna aplikacja niż telefon, a zaznaczenie rysowało bladą płachtę na całą
// szerokość. Czatów są dziesiątki, więc recykling wierszy nic tu nie dawał.
func (u *ui) rebuildChats() {
	u.chatBox.RemoveAll()
	for i, r := range u.rows {
		i, r := i, r
		u.chatBox.Add(u.chatRow(r, !u.mobile && i == u.sel, func() {
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
func (u *ui) chatRow(r chatRow, sel bool, tap func()) fyne.CanvasObject {
	prev := u.chatPreview(r.info)
	body := twoLine(r.info.Name, prev.text, 15, 12)
	when := mutedText("", 10)
	if !prev.ts.IsZero() {
		when.Text = shortTime(prev.ts)
	}
	right := container.NewVBox(container.NewCenter(when))
	switch {
	case r.unread > 0:
		right.Add(container.NewCenter(badge(r.unread)))
	case len(r.info.InCall) > 0:
		call := canvas.NewText("📞", cGreen)
		call.TextSize = fs(12)
		right.Add(container.NewCenter(call))
	case r.info.Online > 0:
		on := canvas.NewText(fmt.Sprintf("● %d", r.info.Online), cGreen)
		on.TextSize = fs(10)
		right.Add(container.NewCenter(on))
	}

	// awatar grupy: przy dwóch osobach twarz rozmówcy, w grupie inicjał nazwy
	av := u.chatAvatar(r.info, avatarBig)
	row := container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(av), hspace(6)),
		right, vcenter(body))
	inner := fyne.CanvasObject(container.NewPadded(row))
	if sel {
		bg := canvas.NewRectangle(cSurface2)
		bg.CornerRadius = 10
		inner = container.NewPadded(container.NewStack(bg, container.NewPadded(row)))
	}
	return tappable(inner, tap, nil)
}

func (u *ui) chatAvatar(ci core.ChatInfo, size float32) fyne.CanvasObject {
	if len(ci.Members) == 2 {
		for _, m := range ci.Members {
			if !bytes.Equal(m.Pub, u.c.ID.Pub) {
				return u.avatarView(m.Pub, m.Name, size)
			}
		}
	}
	return u.avatarView(ci.ID, ci.Name, size)
}

// badge: czerwona pigułka z liczbą nieprzeczytanych — ten sam kolor co odznaka
// na dolnej nawigacji, żeby jedno oznaczało to samo w dwóch miejscach.
func badge(n int) fyne.CanvasObject {
	bg := canvas.NewRectangle(cRed)
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

// ---- widok rozmowy ----

func (u *ui) buildConv() {
	u.title = newClipText("", fs(16), cFg).bold()
	u.subtitle = newClipText("", fs(11), cMuted)
	u.headAvatar = container.NewStack()
	u.msgBox = container.NewVBox()
	u.msgScr = vscroll(u.msgBox)

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
	// awatar dostaje stałą klatkę avatarRow×avatarRow. Bez niej pusty Stack ma
	// MinSize zero i Border wylicza lewą kolumnę PRZED pierwszym
	// updateConvHeader — awatar dorysowywał się potem na tytule i podpisie.
	headAv := container.NewGridWrap(fyne.NewSize(avatarRow, avatarRow), u.headAvatar)
	head := container.NewBorder(nil, nil,
		container.NewHBox(back, container.NewCenter(headAv), hspace(6)),
		actions,
		container.NewVBox(layout.NewSpacer(), u.title, u.subtitle, layout.NewSpacer()))
	line := canvas.NewRectangle(cSep)
	line.SetMinSize(fyne.NewSize(1, 1))
	bar := container.NewStack(canvas.NewRectangle(cDeep),
		container.NewVBox(container.NewPadded(head), line))

	bottom := container.NewVBox(u.hintBox, u.banner, composer)
	u.convRoot = container.NewBorder(bar, bottom, nil, nil,
		container.NewStack(canvas.NewRectangle(cBg), u.msgScr))
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
	u.title.setText(ch.Name)
	sub := fmt.Sprintf("%d członków", len(ch.Members))
	if ch.Online > 0 {
		sub = fmt.Sprintf("%d z %d online", ch.Online+1, len(ch.Members))
	}
	if typing := u.c.TypingPeers(ch.ID); len(typing) > 0 {
		sub = strings.Join(typing, ", ") + " pisze..."
	}
	u.subtitle.setText(sub)
	u.headAvatar.Objects = []fyne.CanvasObject{u.chatAvatar(*ch, avatarRow)}
	u.headAvatar.Refresh()
}

// openConv: wchodzi w rozmowę — na telefonie jako pełnoekranowa strona.
func (u *ui) openConv() {
	if ch := u.cur(); ch != nil {
		u.c.MarkChatRead(ch.ID)
	}
	u.shown = 0
	u.msgSig = ""
	u.refresh()
	if !u.mobile {
		return
	}
	if u.convPage == nil {
		u.convPage = slidePage(container.NewStack(backdrop(), u.convRoot))
	}
	fresh := !u.onStack(u.convPage)
	if fresh {
		u.root.Add(u.convPage)
	}
	u.root.Refresh()
	u.refresh() // dopiero teraz chatOpen() widzi stronę i rysuje wiadomości
	if fresh {
		// wjazd dopiero po narysowaniu dymków — animowanie pustej strony, która
		// w połowie drogi dopiero się wypełnia, wygląda gorzej niż brak animacji
		slideIn(u.convPage, u.w.Canvas().Size().Width)
	}
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

// daySeparator: kreska przez całą szerokość z datą w środku. Kreska była tu
// tworzona i nigdy nie dodana do widoku — została sama, wisząca w powietrzu
// etykieta. Etykieta zasłania kreskę własnym tłem, bo fyne nie umie przerwać
// prostokąta w środku.
func daySeparator(t time.Time) fyne.CanvasObject {
	line := canvas.NewRectangle(cSep)
	line.SetMinSize(fyne.NewSize(1, 1))
	lbl := mutedText(strings.ToUpper(dayLabel(t)), 10)
	cover := canvas.NewRectangle(cBg)
	cover.CornerRadius = 8
	return container.NewPadded(container.NewStack(
		container.NewVBox(layout.NewSpacer(), line, layout.NewSpacer()),
		container.NewCenter(container.NewStack(cover, container.NewPadded(lbl)))))
}

// bubbleMax: ile pikseli zostaje na sam tekst w dymku. Liczone z realnej
// szerokości ekranu, nie z liczby znaków — telefony bywają różnie szerokie.
func (u *ui) bubbleMax() float32 {
	w := u.w.Canvas().Size().Width
	if w <= 0 {
		return 0
	}
	return w - bubbleMargin(u.mobile) - avatarSmall - 8*theme.Padding()
}

// bubbleFits: czy tekst zmieści się w dymku bez zawijania. Ma to znaczenie, bo
// zawijający widget.Label podaje minimalną szerokość z najdłuższego słowa —
// dopóki zawijanie jest włączone, dymek nie potrafi przytulić treści i „ok”
// rozciąga się na całą szerokość ekranu jak nagłówek.
func (u *ui) bubbleFits(s string) bool {
	max := u.bubbleMax()
	if max <= 0 || strings.Count(s, "\n") > 3 {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if fyne.MeasureText(line, theme.TextSize(), fyne.TextStyle{}).Width > max {
			return false
		}
	}
	return true
}

// wrapFor: dymek przytulający treść musi mieć zawijanie WYŁĄCZONE — patrz bubbleFits.
func wrapFor(hug bool) fyne.TextWrap {
	if hug {
		return fyne.TextWrapOff
	}
	return fyne.TextWrapWord
}

// bubbleMargin: pusty pas z drugiej strony dymka, żeby wiadomość nigdy nie sięgała
// obu krawędzi — inaczej nie widać, kto mówi.
func bubbleMargin(mobile bool) float32 {
	if mobile {
		return 60
	}
	return 200
}

// bubbleRow: jeden dymek z podpisem i awatarem (albo bez, gdy grupujemy).
// Godzina pojawia się tylko przy pierwszej wiadomości z serii — kolejne dymki
// tej samej osoby są nagie, tak jak w Discordzie. To nie tylko wygląd: dymek bez
// stopki to pięć widgetów mniej, a przewijamy ich tu kilkaset.
func (u *ui) bubbleRow(m core.Msg, animate, grouped bool,
	lastBubble **canvas.Rectangle, lastOwn *bool) fyne.CanvasObject {
	own := bytes.Equal(m.Author, u.c.ID.Pub)
	bg := canvas.NewRectangle(cSurface2)
	if own {
		bg.FillColor = cBubbleMe
	}
	bg.CornerRadius = 16

	var content fyne.CanvasObject
	hug := false // dymek szerokości treści zamiast na całą szerokość wiersza
	// miniatura i kafelek pliku mają znany rozmiar, więc dymek może je obleźć —
	// o szerokości decyduje wtedy tylko podpis
	if m.Kind == "file" {
		body := container.NewVBox(u.attachView(m, animate))
		hug = true
		if m.Text != "" { // podpis pod załącznikiem
			cap := widget.NewLabel(m.Text)
			hug = u.bubbleFits(m.Text)
			cap.Wrapping = wrapFor(hug)
			body.Add(cap)
		}
		content = container.NewPadded(body)
	} else {
		txtVal := m.Text
		if m.Deleted {
			txtVal = "[Wiadomość usunięta]"
		}
		txt := widget.NewLabel(txtVal)
		hug = u.bubbleFits(txtVal)
		txt.Wrapping = wrapFor(hug)
		content = txt
	}

	if len(m.Reactions) > 0 {
		rTxt := mutedText(formatReactionsGUI(m.Reactions), 10)
		content = container.NewVBox(content, rTxt)
	}

	bubble := tappable(container.NewStack(bg, content), nil, func() { u.msgActions(m) })
	*lastBubble, *lastOwn = bg, own

	margin := bubbleMargin(u.mobile)
	if own {
		body := fyne.CanvasObject(bubble)
		if !grouped {
			tsStr := m.TS.Format("15:04")
			if m.ACK == "read" {
				tsStr += " ✓✓"
			} else if m.ACK == "delivered" {
				tsStr += " ✓"
			}
			body = container.NewVBox(container.NewHBox(layout.NewSpacer(),
				mutedText(tsStr, 9), hspace(4)), bubble)
		}
		if hug {
			return container.NewHBox(layout.NewSpacer(), body, hspace(2))
		}
		return container.NewBorder(nil, nil, hspace(margin), hspace(2), body)
	}

	av := fyne.CanvasObject(hspace(avatarSmall))
	if !grouped {
		av = container.NewVBox(u.avatarView(m.Author, m.Name, avatarSmall), layout.NewSpacer())
	}
	left := container.NewHBox(hspace(2), av)
	body := fyne.CanvasObject(bubble)
	if !grouped {
		who := canvas.NewText(m.Name, authorColor(m.Author))
		who.TextSize = fs(11)
		who.TextStyle = fyne.TextStyle{Bold: true}
		body = container.NewVBox(
			container.NewHBox(who, mutedText(m.TS.Format("15:04"), 9)), bubble)
	}
	if hug {
		return container.NewHBox(left, body, layout.NewSpacer())
	}
	return container.NewBorder(nil, nil, left, hspace(margin), body)
}

func formatReactionsGUI(rm map[string]string) string {
	counts := map[string]int{}
	for _, emoji := range rm {
		counts[emoji]++
	}
	var parts []string
	for emoji, c := range counts {
		if c > 1 {
			parts = append(parts, fmt.Sprintf("%s %d", emoji, c))
		} else {
			parts = append(parts, emoji)
		}
	}
	return strings.Join(parts, " ")
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

	emojiRow := container.NewHBox(layout.NewSpacer())
	for _, em := range []string{"👍", "❤️", "😂", "😮", "😢", "🔥"} {
		em := em
		btn := widget.NewButton(em, func() {
			u.fail(u.c.ReactMsg(chat, m.ID, em))
			u.msgSig = ""
			u.popPage()
			u.refresh()
		})
		emojiRow.Add(btn)
	}
	emojiRow.Add(layout.NewSpacer())
	rows.Add(emojiRow)
	rows.Add(vspace(6))

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
	rows.Add(navRow(theme.DeleteIcon(), "Usuń u wszystkich",
		"Wysyła zdarzenie usunięcia wiadomości do DAG w czacie", func() {
			u.fail(u.c.DeleteMsg(chat, m.ID))
			u.msgSig = ""
			u.popPage()
			u.refresh()
		}))
	rows.Add(navRow(theme.VisibilityOffIcon(), "Ukryj u mnie",
		"Zniknie tylko na tym urządzeniu — u innych zostaje", func() {
			u.fail(u.c.HideMsg(chat, m.ID))
			u.msgSig = ""
			u.popPage()
			u.refresh()
		}))
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
		vcenter(twoLine(name, sub, 14, 10)))
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
