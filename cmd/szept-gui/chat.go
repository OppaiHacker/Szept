package main

// Chat list and conversation view. Usual mobile messenger layout:
// chat row = avatar + name + preview of last message + badge,
// conversation = separate full-screen page with app bar, bubbles, and text input.

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
	"szept/internal/event"
	"szept/internal/identity"
)

// msgPage: number of bubbles rendered immediately. On mobile, there's no reason
// to build 500 widgets upon entering a chat — older ones are pulled by top button.
// Fixed ceiling, not real virtualization. widget.List can
// recycle rows, but requires a known height for each row, which a bubble
// with wrapped text and thumbnail does not have.
const msgPage = 30

// grouping: consecutive messages from the same person within this window merge
// into one block (single header, single avatar).
const groupWindow = 5 * time.Minute

// ---- chat list ----

// rebuildChats: identical touch rows on both platforms. Desktop previously had
// widget.List with plain chat names — without avatar, preview, and timestamp it looked
// like a different app than mobile, and selection drew a faint strip across the full
// width. There are tens of chats, so row recycling provided no benefit.
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
		u.chatBox.Add(pad(16, 12, card(emptyState(theme.MailComposeIcon(), "Pusto tutaj",
			"Dodaj znajomego jego kodem albo załóż grupę — przyciski są nad listą."))))
	}
	u.chatBox.Refresh()
}

// chatRow: avatar with a presence dot, name, preview, and on the right the time
// with a badge under it.
//
// Unread rows lift their own text a step (white name, brighter preview) rather
// than painting the whole row — the badge already says "new", the row only has
// to be findable while scrolling past it. Presence moved onto the avatar: the
// old "● 2" in the corner competed with the badge for the same spot and lost.
func (u *ui) chatRow(r chatRow, sel bool, tap func()) fyne.CanvasObject {
	prev := u.chatPreview(r.info)
	unread := r.unread > 0

	name := newClipText(r.info.Name, fs(15), cFg).bold()
	sub := newClipText(prev.text, fs(12), cMuted)
	if unread {
		name.setColor(cFgStrong)
		sub.setColor(cFg2)
	}

	when := canvas.NewText("", cFaint)
	when.TextSize = fs(10)
	if !prev.ts.IsZero() {
		when.Text = shortTime(prev.ts)
	}
	// time on the name's line, badge on the preview's — a tail column centred
	// against two lines of text lines up with neither of them
	var tail fyne.CanvasObject
	switch {
	case unread:
		when.Color = cAccent
		tail = badge(r.unread)
	case len(r.info.InCall) > 0:
		tail = livePill("rozmowa")
	}
	head := container.NewBorder(nil, nil, nil,
		tightHBox(0, hspace(8), container.NewCenter(when)), vcenter(name))
	body := tightVBox(3, head, container.NewBorder(nil, nil, nil, tail, vcenter(sub)))

	// group avatar: for two members, partner's face; in group, initial of group name
	av := presence(u.chatAvatar(r.info, avatarBig), avatarBig, r.info.Online > 0, cBg)
	row := container.NewBorder(nil, nil,
		tightHBox(0, container.NewCenter(av), hspace(6)), nil, vcenter(body))
	inner := fyne.CanvasObject(pad(8, 12, row))
	if sel {
		bg := canvas.NewRectangle(cSurface2)
		bg.CornerRadius = 12
		// accent edge on the selected row — the same marker the wails client
		// uses, and the only one that survives a busy avatar next to it
		edge := canvas.NewRectangle(cAccent)
		edge.CornerRadius = 2
		edge.SetMinSize(fyne.NewSize(3, 22))
		mark := container.NewVBox(layout.NewSpacer(), edge, layout.NewSpacer())
		inner = pad(2, 8, container.NewStack(bg,
			container.NewBorder(nil, nil, mark, nil, pad(8, 8, row))))
	}
	return tappable(inner, tap, nil)
}

// presence: green dot cut into the bottom-right corner of an avatar. Its ring
// is painted in the colour of whatever the avatar sits on, so the dot reads as
// a hole punched in the picture rather than a sticker dropped on top.
func presence(av fyne.CanvasObject, size float32, online bool, ring color.Color) fyne.CanvasObject {
	if !online {
		return av
	}
	d := fyne.Max(11, size*0.28)
	dot := container.NewGridWrap(fyne.NewSize(d, d), container.NewStack(
		canvas.NewCircle(ring), pad(2.5, 2.5, canvas.NewCircle(cGreen))))
	return container.NewGridWrap(fyne.NewSize(size, size), container.NewStack(av,
		container.NewVBox(layout.NewSpacer(),
			container.NewHBox(layout.NewSpacer(), dot))))
}

// livePill: green chip for "something is happening in this chat right now".
func livePill(text string) fyne.CanvasObject {
	t := canvas.NewText(text, cGreen)
	t.TextSize = fs(10)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return pill(color.NRGBA{cGreen.R, cGreen.G, cGreen.B, 0x2b}, 999, pad(2, 8, t))
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

// badge: red pill with unread count — same color as badge
// on bottom navigation, so it means the same thing in both places.
func badge(n int) fyne.CanvasObject {
	bg := canvas.NewRectangle(cRed)
	bg.CornerRadius = 999
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

// chatPreview: last chat message, cached under the chat's store revision —
// decrypting full history of every chat on every refresh would be
// visible on mobile.
func (u *ui) chatPreview(ci core.ChatInfo) chatPrev {
	key := string(ci.ID)
	rev := u.c.St.Rev(ci.ID)
	if p, ok := u.prev[key]; ok && p.rev == rev {
		return p
	}
	p := chatPrev{rev: rev, text: "brak wiadomości"}
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

// shortTime: time for today's messages, date for older ones.
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

// dayLabel: day separator in conversation.
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

// ---- conversation view ----

func (u *ui) buildConv() {
	u.title = newClipText("", fs(16), cFg).bold()
	u.subtitle = newClipText("", fs(11), cMuted)
	u.headAvatar = container.NewStack()
	u.msgBox = container.NewVBox()
	u.msgScr = vscroll(u.msgBox)

	composer := u.buildComposer() // text field + / commands (commands.go)
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
	// avatar gets a fixed frame avatarRow×avatarRow. Without it, empty Stack has
	// zero MinSize and Border calculates the left column BEFORE the first
	// updateConvHeader — avatar would render over title and subtitle later.
	headAv := container.NewGridWrap(fyne.NewSize(avatarRow, avatarRow), u.headAvatar)
	head := container.NewBorder(nil, nil,
		tightHBox(0, back, container.NewCenter(headAv), hspace(8)),
		actions,
		vcenter(tightVBox(1, u.title, u.subtitle)))
	bar := container.NewStack(canvas.NewRectangle(cDeep),
		tightVBox(0, pad(6, 6, head), hairline()))

	var members fyne.CanvasObject
	if !u.mobile {
		members = u.buildMemberPane()
	}
	u.convBottom = container.NewVBox(u.hintBox, u.banner, composer)
	u.convRoot = container.NewBorder(bar, u.convBottom, nil, members,
		container.NewStack(canvas.NewRectangle(cBg), u.msgScr))
}

// ---- member column ----

// buildMemberPane: fixed-width column on the right of the conversation —
// who belongs to the chat and who is around right now. Mobile keeps
// membersPage instead: a phone screen has no width to give away.
func (u *ui) buildMemberPane() fyne.CanvasObject {
	u.memberBox = container.NewVBox()
	u.memberHead = newClipText("", fs(11), cMuted).bold()
	line := canvas.NewRectangle(cSep)
	line.SetMinSize(fyne.NewSize(1, 1))
	body := container.NewBorder(container.NewPadded(u.memberHead), nil, line, nil,
		vscroll(u.memberBox))
	// hspace pins the column width: Stack takes the widest child's MinSize,
	// and clipText shrinks to a single letter on its own (scroll.go).
	return container.NewStack(canvas.NewRectangle(cDeep), hspace(196), body)
}

// renderMembers: online first, offline below. Signature guard, because refresh()
// runs on every event burst and each row decodes an avatar.
func (u *ui) renderMembers(ch *core.ChatInfo) {
	if u.memberBox == nil {
		return
	}
	call := u.c.CallState(ch.ID)
	type row struct {
		m    event.Member
		call bool
	}
	var on, off []row
	var sig strings.Builder
	fmt.Fprintf(&sig, "%x;", ch.ID) // two chats can hold the same pair of people
	for _, m := range ch.Members {
		me := bytes.Equal(m.Pub, u.c.ID.Pub)
		r := row{m: m}
		if call != nil {
			r.call = call.Members[string(m.Pub)] || (me && call.Joined)
		}
		live := me || u.c.Online(m.Pub)
		if live {
			on = append(on, r)
		} else {
			off = append(off, r)
		}
		status, _ := u.c.StatusOf(m.Pub)
		fmt.Fprintf(&sig, "%x/%s/%s/%t%t;", m.Pub, m.Name, status, live, r.call)
	}
	if sig.String() == u.memSig {
		return
	}
	u.memSig = sig.String()

	u.memberHead.setText(fmt.Sprintf("CZŁONKOWIE — %d", len(ch.Members)))
	u.memberBox.RemoveAll()
	for _, grp := range []struct {
		title string
		rows  []row
	}{{"ONLINE", on}, {"OFFLINE", off}} {
		if len(grp.rows) == 0 {
			continue
		}
		u.memberBox.Add(section(fmt.Sprintf("%s — %d", grp.title, len(grp.rows))))
		for _, r := range grp.rows {
			u.memberBox.Add(u.memberRow(r.m, ch, grp.title == "ONLINE", r.call))
		}
	}
	u.memberBox.Refresh()
}

// memberRow: compact entry in the column — dot, avatar, name and, underneath,
// founder tag or call status. Tap opens the same profile as the members page.
func (u *ui) memberRow(m event.Member, ch *core.ChatInfo, live, inCall bool) fyne.CanvasObject {
	dot := canvas.NewText("●", cMuted)
	dot.TextSize = fs(9)
	if live {
		dot.Color = cGreen
	}
	sub, _ := u.c.StatusOf(m.Pub)
	if sub == "" && bytes.Equal(m.Pub, ch.Owner) {
		sub = "założyciel"
	}
	if inCall {
		sub = "🎧 w rozmowie"
	}
	row := container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(dot), hspace(4),
			container.NewCenter(u.avatarView(m.Pub, m.Name, avatarSmall)), hspace(6)),
		nil, vcenter(twoLine(m.Name, sub, 13, 10)))
	kick := bytes.Equal(ch.Owner, u.c.ID.Pub) && !bytes.Equal(m.Pub, u.c.ID.Pub)
	pub, name := bytes.Clone(m.Pub), m.Name
	return tappable(container.NewPadded(row), func() {
		u.profilePage(pub, name, kick, ch)
	}, nil)
}

// buildCallControls: the call panel — green connection status with chat name
// and hang-up on top, a row of tiles below: camera, screen, 3D scene,
// microphone, deafen (muted output) and volume.
func (u *ui) buildCallControls() {
	u.callBtn = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
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
	if !u.mobile {
		u.callBtn.SetText("Rozmowa")
	}
	u.callBtn.Importance = widget.HighImportance
	u.micBtn = widget.NewButtonWithIcon("", theme.MediaRecordIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.c.ToggleMic(ch.ID)
			u.refresh()
		}
	})
	u.deafBtn = widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.c.ToggleDeafen(ch.ID)
			u.refresh()
		}
	})
	u.camBtn = widget.NewButtonWithIcon("", theme.MediaVideoIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.fail(u.c.ToggleCam(ch.ID, u.cfg.CamDevice))
		}
	})
	u.scrBtn = widget.NewButtonWithIcon("", theme.ComputerIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.fail(u.c.ToggleScreen(ch.ID))
		}
	})
	u.spatialBtn = widget.NewButtonWithIcon("", theme.ViewFullScreenIcon(), u.spatialPage)
	gear := widget.NewButtonWithIcon("", theme.SettingsIcon(), u.voicePage)
	hang := widget.NewButtonWithIcon("", theme.MediaStopIcon(), func() {
		if ch := u.cur(); ch != nil {
			u.c.LeaveCall(ch.ID)
			u.refresh()
		}
	})
	hang.Importance = widget.DangerImportance

	// głośność pod ręką — to samo pokrętło co w ustawieniach Głos i wideo
	vol := widget.NewSlider(0, 200)
	vol.Step = 5
	if v := u.cfg.OutputVol; v > 0 {
		vol.Value = v * 100
	} else {
		vol.Value = 100
	}
	vol.OnChanged = func(v float64) { u.c.SetOutputVolume(v / 100) }
	vol.OnChangeEnded = func(v float64) {
		u.cfg.OutputVol = v / 100
		u.saveCfg()
	}

	// clipText, nie canvas.Text: podpis „czat · N uczest. · mic” bywa dłuższy
	// niż pasek, a canvas.Text podaje MinSize całego napisu — Border wypychał
	// przyciski poza krawędź i dawał im UJEMNĄ szerokość (panika w painterze).
	u.bannerDot = canvas.NewCircle(cGreen)
	u.bannerLbl = newClipText("", fs(13), cGreen).bold()
	u.bannerSub = newClipText("", fs(11), cMuted)

	// the panel is a card floating over the thread, so it gets its own surface
	// one step above the composer under it, not the chrome colour
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = 16
	dot := container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), u.bannerDot))
	head := container.NewBorder(nil, nil,
		tightHBox(0, dot, hspace(8)), hang,
		vcenter(tightVBox(1, u.bannerLbl, u.bannerSub)))

	if u.mobile {
		// camera and screen have no source on Android (lacking V4L2/X11) — don't offer them
		u.callBar = container.NewVBox(
			container.NewGridWithColumns(4, u.micBtn, u.deafBtn, u.spatialBtn, gear),
			container.NewBorder(nil, nil, widget.NewIcon(theme.VolumeUpIcon()), nil, vol))
	} else {
		u.callBar = container.NewVBox(
			container.NewGridWithColumns(6, u.camBtn, u.scrBtn, u.spatialBtn, u.micBtn, u.deafBtn, gear),
			container.NewBorder(nil, nil, widget.NewIcon(theme.VolumeUpIcon()), nil, vol))
	}
	// inset from the screen edges: a rounded card whose corners touch the frame
	// looks like a failed full-width bar
	u.banner = pad(0, 8, container.NewStack(bg, pad(10, 12, container.NewVBox(head, u.callBar))))
	u.banner.Hide()
}

func (u *ui) updateConvHeader(ch *core.ChatInfo) {
	u.title.setText(ch.Name)
	sub, tone := fmt.Sprintf("%d członków", len(ch.Members)), cMuted
	if ch.Online > 0 {
		sub = fmt.Sprintf("%d z %d online", ch.Online+1, len(ch.Members))
	}
	// typing is the one subtitle that changes on its own — accent tells the eye
	// it is live and not just another count that happens to have moved
	var relayed []string
	for _, m := range ch.Members {
		if !bytes.Equal(m.Pub, u.c.ID.Pub) && u.c.Relay(m.Pub) != "" {
			relayed = append(relayed, m.Name)
		}
	}
	if len(relayed) > 0 {
		sub, tone = sub+" · przez przekaźnik", cOrange
	}
	if typing := u.c.TypingPeers(ch.ID); len(typing) > 0 {
		sub, tone = strings.Join(typing, ", ")+" pisze…", cAccent
	}
	u.subtitle.setText(sub)
	u.subtitle.setColor(tone)
	u.headAvatar.Objects = []fyne.CanvasObject{
		presence(u.chatAvatar(*ch, avatarRow), avatarRow, ch.Online > 0, cDeep)}
	u.headAvatar.Refresh()
}

// openConv: opens conversation — on mobile as a full-screen page.
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
	u.refresh() // only now chatOpen() sees the page and renders messages
	if fresh {
		// transition only after rendering bubbles — animating an empty page
		// that fills halfway through looks worse than no animation
		slideIn(u.convPage, u.w.Canvas().Size().Width)
	}
}

// openChat: switches to specified chat and (on mobile) opens its page.
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

// ---- rendering messages ----

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
		return // nothing changed — skip scroll adjustments and image decoding
	}
	sameChat := u.lastChat == string(ch.ID)
	stick := !sameChat || u.nearBottom()
	keep := u.msgScr.Offset.Y
	oldH := u.msgBox.MinSize().Height
	u.msgSig = sig
	u.msgAnims = u.msgAnims[:0] // bubbles are discarded along with their animations

	u.msgBox.RemoveAll()
	// slots for playing GIFs are assigned to latest messages — hence iterating
	// list backwards
	animate := map[int]bool{}
	for i, slots := len(msgs)-1, 0; i >= 0 && slots < maxAnims; i-- {
		if msgs[i].Kind == "file" && attach.Animated(msgs[i].Mime) {
			animate[i] = true
			slots++
		}
	}
	if older > 0 {
		// centred pill, not a full-width bar: it is a way back into history, not
		// a message, and at full width it reads as the first thing in the thread
		u.msgBox.Add(pad(6, 0, container.NewCenter(widget.NewButton(
			fmt.Sprintf("Pokaż wcześniejsze (%d)", older), func() {
				cur := u.cur() // chat list could have been reshuffled during this time
				if cur == nil {
					return
				}
				u.shown += msgPage
				u.msgSig = ""
				u.grew = true
				u.renderMessages(cur)
			}))))
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
	// command responses: local, not sent over network — always at conversation end
	for i, s := range u.sys[string(ch.ID)] {
		u.msgBox.Add(u.sysEmbed(i, s))
	}
	u.msgBox.Refresh()

	// scrolling: stay where user is reading. Jump to bottom only
	// when already at bottom (or just entered chat) — otherwise
	// incoming messages would yank user out of history.
	switch {
	case stick:
		u.scrollToEnd()
	case u.grew:
		// loading older messages grows UPWARDS: scroll by newly added height
		// so currently read message stays under the same finger
		u.grew = false
		u.msgScr.ScrollToOffset(fyne.NewPos(0, keep+u.msgBox.MinSize().Height-oldH))
	default:
		u.msgScr.ScrollToOffset(fyne.NewPos(0, keep))
	}

	// flash latest incoming message (not on chat switch)
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

// msgSignature: fingerprint of visible content. Skip rebuild if unchanged —
// saves image decoding and, more importantly, preserves scroll position.
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

// scrollToEnd: jump to end of conversation. container.Scroll calculates max
// offset from content SIZE, which is only known after layout pass —
// calling in the same cycle on first chat entry did nothing.
// Hence second attempt after brief delay.
func (u *ui) scrollToEnd() {
	u.msgScr.ScrollToBottom()
	go func() {
		time.Sleep(60 * time.Millisecond)
		fyne.Do(func() { u.msgScr.ScrollToBottom() })
	}()
}

// nearBottom: whether user is looking near the end of conversation.
func (u *ui) nearBottom() bool {
	max := u.msgBox.MinSize().Height - u.msgScr.Size().Height
	if max <= 0 {
		return true
	}
	return u.msgScr.Offset.Y >= max-60
}

// daySeparator: date as a floating chip in the middle of the thread.
// It used to be a rule with the date covering its middle, which needed the
// label's background to match the thread's exactly — one palette change and a
// grey box appeared around every date. A chip owns its background instead.
func daySeparator(t time.Time) fyne.CanvasObject {
	lbl := canvas.NewText(strings.ToUpper(dayLabel(t)), cMuted)
	lbl.TextSize = fs(10)
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	return pad(8, 0, container.NewCenter(pill(cSurface2, 999, pad(4, 11, lbl))))
}

// bubbleMax: remaining pixels for text inside bubble.
// Calculated from real screen width, not character count — phone widths vary.
func (u *ui) bubbleMax() float32 {
	w := u.w.Canvas().Size().Width
	if w <= 0 {
		return 0
	}
	return w - bubbleMargin(u.mobile) - avatarSmall - 8*theme.Padding()
}

// bubbleFits: whether text fits in bubble without wrapping. Important because
// wrapping widget.Label reports min width from longest word —
// as long as wrapping is enabled, bubble cannot hug content and expands
// across full screen width like a header.
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

// wrapFor: content-hugging bubble must have wrapping OFF — see bubbleFits.
func wrapFor(hug bool) fyne.TextWrap {
	if hug {
		return fyne.TextWrapOff
	}
	return fyne.TextWrapWord
}

// bubbleMargin: empty margin on opposite side of bubble so message never reaches
// both edges — otherwise author is unclear.
func bubbleMargin(mobile bool) float32 {
	if mobile {
		return 60
	}
	return 200
}

// bubbleRow: single bubble with header and avatar (or without when grouped).
// Timestamp appears only on first message of series — subsequent bubbles
// from same author are bare. Not just visual: bubble without
// footer saves 5 widgets, and we scroll hundreds here.
func (u *ui) bubbleRow(m core.Msg, animate, grouped bool,
	lastBubble **canvas.Rectangle, lastOwn *bool) fyne.CanvasObject {
	own := bytes.Equal(m.Author, u.c.ID.Pub)
	bg := canvas.NewRectangle(cSurface2)
	if own {
		bg.FillColor = cBubbleMe
	}
	bg.CornerRadius = 18

	var content fyne.CanvasObject
	hug := false // content-width bubble instead of full row width
	// thumbnail and file tile have fixed size, so bubble can wrap them —
	// width is then determined solely by caption
	if m.Kind == "file" {
		body := container.NewVBox(u.attachView(m, animate))
		hug = true
		if m.Text != "" { // attachment caption
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

	// reactions as their own chip under the text: loose emoji inside the bubble
	// read as part of the message, which is exactly what a reaction is not
	if len(m.Reactions) > 0 {
		rTxt := canvas.NewText(formatReactionsGUI(m.Reactions), cFg)
		rTxt.TextSize = fs(11)
		content = container.NewVBox(content,
			container.NewHBox(pill(cSurface3, 999, pad(2, 8, rTxt))))
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

// ---- message actions (long press) ----

func (u *ui) msgActions(m core.Msg) {
	ch := u.cur()
	if ch == nil {
		return
	}
	chat := bytes.Clone(ch.ID) // page outlives pointer to list row
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

// ---- chat menu ----

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
	history := widget.NewCheck("Nowi członkowie widzą wcześniejszą historię", nil)
	create := func() {
		if strings.TrimSpace(name.Text) == "" {
			return
		}
		id, err := u.c.CreateChat(strings.TrimSpace(name.Text), !history.Checked)
		if err != nil {
			u.fail(err)
			return
		}
		u.popPage()
		u.openChat(id)
	}
	name.OnSubmitted = func(string) { create() }
	u.page("Nowa grupa", container.NewVBox(name,
		history, mutedWrap("Bez tego nowa osoba widzi tylko to, co napisano po jej dołączeniu: dostaje klucze od momentu wejścia, a Ty po każdym dodaniu kogoś zmieniasz klucz automatycznie."),
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

// personRow: shared person row (members, friends). Presence lives on the avatar
// here too — the grey bullet that used to lead the row was a third column of
// meaning before the eye had even reached the name.
func (u *ui) personRow(pub []byte, name, tag string, online bool, tap func()) fyne.CanvasObject {
	title := newClipText(name, fs(14), cFg).bold()
	code := newClipText(identity.Fingerprint(pub), fs(10), cFaint).mono()
	body := fyne.CanvasObject(tightVBox(3, title, code))
	if tag != "" {
		// Border, not HBox: clipText keeps a tiny MinSize on purpose so rows can
		// squeeze, and in a box layout that is all the width it would ever get
		body = tightVBox(3, title, container.NewBorder(nil, nil,
			tightHBox(0, tagChip(tag), hspace(2)), nil, vcenter(code)))
	}
	av := presence(u.avatarView(pub, name, avatarRow), avatarRow, online, cBg)
	chevron := widget.NewIcon(theme.NewColoredResource(theme.NavigateNextIcon(),
		theme.ColorNamePlaceHolder))
	row := container.NewBorder(nil, nil,
		tightHBox(0, container.NewCenter(av), hspace(6)),
		container.NewCenter(chevron), vcenter(body))
	return tappable(pad(7, 12, row), tap, nil)
}

// tagChip: quiet label for a role ("założyciel") next to a name.
func tagChip(text string) fyne.CanvasObject {
	t := canvas.NewText(text, cFg2)
	t.TextSize = fs(9)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewCenter(pill(cSurface3, 999, pad(2, 7, t)))
}

// ---- call state ----

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
		if call.Deaf {
			u.deafBtn.SetIcon(theme.VolumeMuteIcon())
			mic += " · głuchy"
		} else {
			u.deafBtn.SetIcon(theme.VolumeUpIcon())
		}
		u.bannerLbl.setText("Nawiązano połączenie")
		u.bannerLbl.setColor(cGreen)
		u.bannerDot.FillColor = cGreen
		u.bannerSub.setText(fmt.Sprintf("%s · %d uczest. · %s", ch.Name, len(call.Members)+1, mic))
		u.callBtn.Hide() // hang-up lives in the panel
		u.callBar.Show()
		u.showBanner()
	case call != nil && len(call.Members) > 0:
		u.bannerLbl.setText(fmt.Sprintf("Trwa rozmowa (%d) — dołącz", len(call.Members)))
		u.bannerLbl.setColor(cFg)
		u.bannerDot.FillColor = cOrange
		u.bannerSub.setText(ch.Name)
		u.callBtn.Show()
		u.callBar.Hide()
		u.showBanner()
	default:
		u.callBtn.Show()
		u.callBar.Hide()
		u.hideBanner()
	}
	u.bannerDot.Refresh()
}

// relayoutBottom: pokazanie/ukrycie panelu zmienia WYSOKOŚĆ dolnego paska, ale
// Show() na dziecku nie każe rodzicowi się przeliczyć. Bez tego świeżo odsłonięty
// panel zostaje z rozmiarem zerowym: container.NewPadded robi z niego ujemny,
// a przycisk z taką ramką wywraca painter (slice bounds out of range).
func (u *ui) relayoutBottom() {
	if u.convBottom != nil {
		u.convBottom.Refresh()
	}
	if u.convRoot != nil {
		u.convRoot.Refresh()
	}
}

func (u *ui) showBanner() {
	if !u.banner.Visible() {
		u.banner.Show()
		u.relayoutBottom()
		u.bannerAnim = canvas.NewColorRGBAAnimation(cGreen, color.NRGBA{0x23, 0xa5, 0x5a, 0x30},
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
		u.relayoutBottom()
		if u.bannerAnim != nil {
			u.bannerAnim.Stop()
			u.bannerAnim = nil
		}
	}
}
