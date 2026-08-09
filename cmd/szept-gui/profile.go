package main

// Profiles and settings: settings list (own profile + settings), profile
// of friend/chat member, friend list, QR codes. On mobile all these are
// full-screen pages (nav.go), on desktop the same content in dialogs.

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	qrcode "github.com/skip2/go-qrcode"

	"szept/internal/core"
	"szept/internal/identity"
	"szept/internal/store"
)

// ---- settings list ----

// mePage: profile header + settings sections. Mobile: "Ustawienia" tab.
// Desktop: sidebar of the settings view (openSettings in settings.go).
func (u *ui) mePage() fyne.CanvasObject {
	u.meBox = container.NewVBox()
	return vscroll(u.meBox)
}

// rebuildMe: redraws the list on name, avatar, or connector change.
func (u *ui) rebuildMe() {
	if u.meBox == nil {
		return
	}
	name := newClipText(u.c.ID.Name, fs(20), cFgStrong).bold()
	fp := monoText(identity.Fingerprint(u.c.ID.Pub), 11, cFg2)
	state, tone := "łączenie z connectorem…", cOrange
	if u.c.ConnUp() {
		state, tone = "połączony", cGreen
	} else if e := u.c.ConnErr(); e != "" {
		state, tone = "błąd połączenia", cRed
	}
	status, bio := u.c.StatusOf(u.c.ID.Pub)

	// code and connection as chips: two facts of different kinds, one about you
	// and one about the network, that used to run together as three grey lines
	col := container.NewVBox(name,
		container.NewHBox(pill(cSurface2, 999, pad(3, 9, fp))),
		container.NewHBox(dotChip(state, tone)))
	if status != "" {
		col.Add(mutedText(status, 12))
	}
	head := fyne.CanvasObject(container.NewBorder(nil, nil,
		tightHBox(0, container.NewCenter(
			presence(u.avatarView(u.c.ID.Pub, u.c.ID.Name, avatarHuge), avatarHuge,
				u.c.ConnUp(), cSurface)), hspace(6)),
		nil, vcenter(col)))
	if b := u.bannerView(u.c.ID.Pub, u.bannerW()); b != nil {
		head = container.NewVBox(b, head)
	}

	u.meBox.RemoveAll()
	u.meBox.Add(container.NewPadded(card(head)))
	u.meBox.Add(section("KONTO"))
	u.meBox.Add(navRow(theme.DocumentCreateIcon(), "Nazwa", u.c.ID.Name, u.nickPage))
	u.meBox.Add(navRow(theme.AccountIcon(), "Awatar", "Zdjęcie widoczne przy twoich wiadomościach", u.avatarPage))
	u.meBox.Add(navRow(theme.InfoIcon(), "Status i opis", statusDesc(status, bio), u.statusPage))
	u.meBox.Add(navRow(theme.ColorPaletteIcon(), "Baner", "Pasek na górze twojego profilu", u.bannerPage))
	u.meBox.Add(navRow(theme.VisibilityIcon(), "Mój kod / QR", "Pokaż go, żeby ktoś dodał cię do znajomych", u.codePage))
	u.meBox.Add(section("USTAWIENIA"))
	u.meBox.Add(navRow(theme.MenuExpandIcon(), "Pluginy", "wkrótce",
		u.comingSoonPage("Pluginy", "Rozszerzenia pisane przez społeczność.")))
	u.meBox.Add(navRow(theme.DocumentSaveIcon(), "Backup & restore",
		"Kopia tożsamości i bazy w jednym pliku", u.backupPage))
	u.meBox.Add(navRow(theme.ComputerIcon(), "Connectory", u.c.Cfg.Connector, u.connectorsPage))
	u.meBox.Add(navRow(theme.DownloadIcon(), "Updater", "wkrótce",
		u.comingSoonPage("Updater", "Aktualizacje aplikacji prosto z niej samej.")))
	u.meBox.Add(navRow(theme.MediaPlayIcon(), "Głos i wideo",
		"Głośność, kamera, scena 3D", u.voicePage))
	u.meBox.Add(navRow(theme.WarningIcon(), "Hasła i szyfrowanie",
		"Zmiana hasła szyfrującego bazę", u.passPage))
	u.meBox.Add(navRow(theme.LoginIcon(), "Konta",
		"Wiele profili (baz) na jednym urządzeniu", u.accountsPage))
	u.meBox.Add(navRow(theme.SettingsIcon(), "System",
		"Powiadomienia, multimedia, ścieżki", u.systemPage))
	u.meBox.Add(navRow(theme.ColorPaletteIcon(), "Wygląd", "Kolor akcentu", u.appearancePage))
	u.meBox.Add(navRow(theme.MailSendIcon(), "Boty / webhooki",
		"Lokalny webhook do wysyłania wiadomości", u.botsPage))
	u.meBox.Add(section("O APLIKACJI"))
	u.meBox.Add(navRow(theme.InfoIcon(), "szept "+appVersion(u.a),
		"P2P, end-to-end, bez serwera treści", u.aboutPage))
	u.meBox.Add(vspace(10))
	u.meBox.Refresh()
}

func (u *ui) nickPage() {
	e := widget.NewEntry()
	e.SetText(u.c.ID.Name)
	save := func() {
		if err := u.c.SetNick(e.Text); err != nil {
			u.fail(err)
			return
		}
		u.popPage()
		u.refresh()
		u.toast("Nazwa zmieniona")
	}
	e.OnSubmitted = func(string) { save() }
	body := container.NewVBox(e,
		bigButton("Zapisz", theme.ConfirmIcon(), save),
		mutedWrap("Nowa nazwa rozejdzie się do wszystkich twoich czatów jako podpisane "+
			"zdarzenie — zobaczą ją też ci, którzy są teraz offline. Poprzednie nazwy "+
			"zostają w historii czatu i widzi je każdy jego członek."))
	if box := u.nickHistoryBox(u.c.ID.Pub); box != nil {
		body.Add(box)
	}
	u.page("Nazwa", body)
}

// statusDesc: one-line summary of the card for the settings row.
func statusDesc(status, bio string) string {
	switch {
	case status != "":
		return status
	case bio != "":
		return trunc(strings.ReplaceAll(bio, "\n", " "), 48)
	}
	return "Krótki status i opis widoczne w twoim profilu"
}

func (u *ui) statusPage() {
	status, bio := u.c.StatusOf(u.c.ID.Pub)
	st := widget.NewEntry()
	st.PlaceHolder = "np. gram, wrócę wieczorem"
	st.SetText(status)
	desc := widget.NewMultiLineEntry()
	desc.PlaceHolder = "kilka zdań o sobie"
	desc.Wrapping = fyne.TextWrapWord
	desc.SetText(bio)
	desc.SetMinRowsVisible(4)
	save := func() {
		if err := u.c.SetStatus(st.Text); err != nil {
			u.fail(err)
			return
		}
		if err := u.c.SetBio(desc.Text); err != nil {
			u.fail(err)
			return
		}
		u.popPage()
		u.refresh()
		u.toast("Profil zapisany")
	}
	st.OnSubmitted = func(string) { save() }
	u.page("Status i opis", container.NewVBox(
		section("STATUS"), st,
		mutedWrap(fmt.Sprintf("Jedna linia pod nazwą, najwyżej %d znaków.", core.MaxStatus)),
		vspace(6), section("O MNIE"), desc,
		mutedWrap(fmt.Sprintf("Najwyżej %d znaków.", core.MaxBio)),
		vspace(6), bigButton("Zapisz", theme.ConfirmIcon(), save),
		mutedWrap("Status, opis i baner rozchodzą się do wszystkich twoich czatów jako "+
			"podpisane zdarzenie — widzą je członkowie tych czatów, nikt więcej."),
	))
}

// nickHistoryBox: names a person announced, newest first. Returns nil when
// there is nothing to show (never renamed, or no shared history yet).
func (u *ui) nickHistoryBox(pub []byte) fyne.CanvasObject {
	hist := u.c.NickHistory(pub)
	if len(hist) < 2 {
		return nil // only the current name — no story to tell
	}
	rows := container.NewVBox()
	for i := len(hist) - 1; i >= 0; i-- {
		h := hist[i]
		tag := "wcześniej"
		if i == len(hist)-1 {
			tag = "teraz"
		}
		rows.Add(container.NewBorder(nil, nil, nil,
			container.NewCenter(mutedText(h.TS.Format("02.01.2006"), 11)),
			widget.NewLabel(h.Name+" · "+tag)))
	}
	return container.NewVBox(section("HISTORIA NAZW"),
		container.NewPadded(card(rows)),
		mutedWrap("Nazwy są częścią podpisanej historii czatu — tej listy nie da się "+
			"po cichu zmienić i widzi ją każdy członek."))
}

func (u *ui) connState() string {
	if u.c.ConnUp() {
		return "połączony"
	}
	if e := u.c.ConnErr(); e != "" {
		return "błąd: " + e
	}
	return "łączenie…"
}

func (u *ui) aboutPage() {
	fp := widget.NewLabel(identity.Fingerprint(u.c.ID.Pub))
	fp.TextStyle = fyne.TextStyle{Monospace: true}
	u.page("O aplikacji", container.NewVBox(
		container.NewCenter(brand(30)),
		container.NewCenter(mutedText("wersja "+appVersion(u.a), 12)),
		vspace(8),
		card(container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("Tożsamość", widget.NewLabel(u.c.ID.Name)),
				widget.NewFormItem("Odcisk", fp),
				widget.NewFormItem("Connector", widget.NewLabel(u.c.Cfg.Connector)),
				widget.NewFormItem("Stan", widget.NewLabel(u.connState())),
			))),
		vspace(6),
		mutedWrap("Wiadomości szyfruje XChaCha20-Poly1305 kluczami epok czatu, "+
			"zdarzenia podpisuje Ed25519, transport to WebRTC DTLS z podpisanym "+
			"sygnalingiem. Każde urządzenie trzyma pełną kopię historii."),
	))
}

// ---- other person's profile ----

// profilePage: friend or chat member. Chat owner gets kick button here
// (rekeys encryption).
func (u *ui) profilePage(pub []byte, name string, canKick bool, ch *core.ChatInfo) {
	me := bytes.Equal(pub, u.c.ID.Pub)
	online := u.c.Online(pub) || me
	state, tone := "offline", cMuted
	if online {
		state, tone = "online", cGreen
	}
	dot := canvas.NewText("●", tone)
	dot.TextSize = fs(11)
	title := canvas.NewText(name, cFg)
	title.TextSize = fs(20)
	title.TextStyle = fyne.TextStyle{Bold: true}
	fp := widget.NewLabel(identity.Fingerprint(pub))
	fp.TextStyle = fyne.TextStyle{Monospace: true}

	status, bio := u.c.StatusOf(pub)
	head := container.NewVBox()
	if b := u.bannerView(pub, u.bannerW()); b != nil {
		head.Add(b)
	}
	head.Add(container.NewCenter(u.avatarView(pub, name, avatarHuge)))
	head.Add(container.NewCenter(title))
	head.Add(container.NewCenter(container.NewHBox(container.NewCenter(dot), hspace(4),
		container.NewCenter(mutedText(state, 11)))))
	if status != "" {
		head.Add(container.NewCenter(mutedText(status, 12)))
	}
	rows := container.NewVBox(container.NewPadded(card(head)))
	if bio != "" {
		rows.Add(section("O MNIE"))
		rows.Add(container.NewPadded(card(mutedWrap(bio))))
	}
	rows.Add(section("ODCISK KLUCZA"))
	rows.Add(container.NewPadded(container.NewBorder(nil, nil, nil,
		widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			u.w.Clipboard().SetContent(identity.Fingerprint(pub))
			u.toast("Odcisk skopiowany")
		}), fp)))
	rows.Add(mutedWrap("Porównajcie odcisk innym kanałem — jeśli się zgadza, " +
		"rozmawiacie z właściwą osobą."))
	if box := u.nickHistoryBox(pub); box != nil {
		rows.Add(box)
	}

	if !me {
		rows.Add(section("AKCJE"))
		friend := u.isFriend(pub)
		rows.Add(navRow(theme.MailComposeIcon(), "Napisz", "Czat na dwoje", func() {
			f := store2friend(pub, name)
			chat, err := u.c.CreateDirect(f)
			if err != nil {
				u.fail(err)
				return
			}
			u.openChat(chat)
		}))
		if !friend {
			rows.Add(navRow(theme.ContentAddIcon(), "Dodaj do znajomych",
				"Zapisze klucz publiczny na stałe", func() {
					u.fail(u.c.St.PutFriend(store2friend(pub, name)))
					u.popPage()
					u.refresh()
					u.toast("Dodano do znajomych")
				}))
		} else {
			rows.Add(navRow(theme.DeleteIcon(), "Usuń ze znajomych", "", func() {
				u.confirm("Usunąć "+name+" z listy znajomych?", func() {
					u.fail(u.c.St.DelFriend(pub))
					u.popPage()
					u.refresh()
				})
			}))
		}
		if canKick && ch != nil {
			chat := ch.ID
			rows.Add(navRow(theme.CancelIcon(), "Usuń z czatu",
				"Rotuje klucz — dalszych wiadomości nie odczyta", func() {
					u.confirm("Usunąć "+name+" z czatu i zrotować klucz?", func() {
						u.fail(u.c.Kick(chat, pub))
						u.popPage()
						u.refresh()
					})
				}))
		}
	}
	u.page(name, rows)
}

func (u *ui) isFriend(pub []byte) bool {
	fs, _ := u.c.St.Friends()
	for _, f := range fs {
		if bytes.Equal(f.Pub, pub) {
			return true
		}
	}
	return false
}

// store2friend: chat member has exactly what's needed for friend entry.
func store2friend(pub []byte, name string) store.Friend {
	return store.Friend{Pub: bytes.Clone(pub), Name: name}
}

// ---- friend list ----

// rebuildFriends redraws entire list on every refresh.
// ponytail: tens of friends, not thousands — add recycled widget.List
// when list starts to lag noticeably.
func (u *ui) rebuildFriends() {
	friends, _ := u.c.St.Friends()
	sort.Slice(friends, func(i, j int) bool { return friends[i].Display() < friends[j].Display() })
	u.friendBox.RemoveAll()
	for _, f := range friends {
		f := f
		status, _ := u.c.StatusOf(f.Pub)
		u.friendBox.Add(u.personRow(f.Pub, f.Display(), status, u.c.Online(f.Pub), func() {
			u.profilePage(f.Pub, f.Display(), false, nil)
		}))
	}
	if len(friends) == 0 {
		u.friendBox.Add(container.NewPadded(card(container.NewVBox(
			widget.NewLabelWithStyle("Nikogo tu jeszcze nie ma", fyne.TextAlignLeading,
				fyne.TextStyle{Bold: true}),
			mutedWrap("Wymieńcie się kodami — przycisk niżej pokaże twój QR. "+
				"Kod zawiera klucz publiczny, więc nikt się nie podszyje.")))))
	}
	u.friendBox.Refresh()
}

// ---- codes and QR ----

func (u *ui) qrImage(code string) fyne.CanvasObject {
	// -9 = exact 9px per module: nearest-neighbor won't blur or merge lines
	png, err := qrcode.Encode(code, qrcode.Medium, -9)
	if err != nil {
		return widget.NewLabel("(QR błąd)")
	}
	img := canvas.NewImageFromResource(fyne.NewStaticResource("qr.png", png))
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScalePixels
	side := float32(240)
	if u.mobile { // on mobile QR fills width — hard to scan small ones
		if cw := u.w.Canvas().Size().Width; cw > 160 {
			side = fyne.Min(cw-60, 360)
		}
	}
	img.SetMinSize(fyne.NewSize(side, side))
	return img
}

func (u *ui) codeBox(code string) fyne.CanvasObject {
	e := widget.NewMultiLineEntry()
	e.SetText(code)
	e.Wrapping = fyne.TextWrapBreak
	copyBtn := bigButton("Kopiuj kod", theme.ContentCopyIcon(), func() {
		u.w.Clipboard().SetContent(code)
		u.toast("Kod w schowku")
	})
	return container.NewVBox(container.NewCenter(u.qrImage(code)), e, copyBtn)
}

func (u *ui) codePage() {
	in := widget.NewMultiLineEntry()
	in.PlaceHolder = "wklej kod SZF-… albo SZC-…"
	in.Wrapping = fyne.TextWrapBreak
	paste := widget.NewButtonWithIcon("Wklej ze schowka", theme.ContentPasteIcon(), func() {
		in.SetText(strings.TrimSpace(u.a.Clipboard().Content()))
	})
	add := bigButton("Dodaj", theme.ConfirmIcon(), func() {
		u.applyCode(strings.TrimSpace(in.Text))
	})
	u.page("Kody znajomych", container.NewVBox(
		widget.NewLabelWithStyle("Twój kod", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		mutedWrap("Bezterminowy. Druga osoba może zeskanować QR aparatem telefonu, "+
			"skopiować kod i wrócić do szeptu — powitamy ją paskiem „wykryto kod”. "+
			"W kodzie nie ma twojej nazwy — ta dojdzie dopiero po połączeniu."),
		u.codeBox(u.c.FriendCode()),
		vspace(10),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Masz cudzy kod?", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		in, paste, add,
		mutedWrap("SZF- dodaje znajomego, SZC- to jednorazowe zaproszenie do grupy (poprosi o PIN)."),
	))
}
