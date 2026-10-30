package main

// Ustawienia: strony sekcji z listy ustawień (rebuildMe w profile.go).
// Każda sekcja idzie przez u.page(): mobile — osobna strona, desktop —
// prawa część widoku ustawień (openSettings), a poza nim dialog.

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"szept/internal/boot"
	"szept/internal/core"
)

// ---- widok ustawień (desktop) ----

// openSettings: ustawienia jako osobny widok na całe okno — lista sekcji
// z boku, treść sekcji obok. Sekcje w dialogach dusiły się w małym
// okienku, a formularze (connectory, boty) nie mieściły się na ekranie.
func (u *ui) openSettings() {
	u.setPane = container.NewStack()
	list := u.mePage() // tworzy u.meBox
	u.rebuildMe()
	u.settingsHome()
	split := container.NewHSplit(list,
		container.NewStack(canvas.NewRectangle(cBg), u.setPane))
	split.SetOffset(0.28)
	u.w.SetContent(container.NewBorder(
		u.appBar("Ustawienia", u.closeSettings), nil, nil, nil, split))
}

func (u *ui) closeSettings() {
	u.setPane = nil
	u.meBox = nil // lista żyje tylko w tym widoku
	u.w.SetContent(u.mainRoot)
	u.refresh()
}

// settingsHome: pusta prawa strona, zanim wybierzesz sekcję.
func (u *ui) settingsHome() {
	u.setPane.Objects = []fyne.CanvasObject{container.NewCenter(container.NewVBox(
		container.NewCenter(widget.NewIcon(theme.SettingsIcon())),
		container.NewCenter(mutedText("Wybierz sekcję z lewej", 13)),
	))}
	u.setPane.Refresh()
}

// saveCfg: jeden punkt zapisu config.json z komunikatem błędu.
func (u *ui) saveCfg() {
	if err := boot.SaveConfig(u.cfgFile, u.cfg); err != nil {
		u.fail(err)
	}
}

// ---- wkrótce ----

func (u *ui) comingSoonPage(title, desc string) func() {
	return func() {
		t := canvas.NewText("Wkrótce", cAccent)
		t.TextSize = fs(24)
		t.TextStyle = fyne.TextStyle{Bold: true}
		u.page(title, container.NewVBox(
			vspace(20),
			container.NewCenter(widget.NewIcon(theme.SettingsIcon())),
			container.NewCenter(t),
			container.NewCenter(mutedWrap(desc)),
		))
	}
}

// ---- backup & restore ----

func (u *ui) backupPage() {
	saveBtn := bigButton("Zapisz kopię (.zip)", theme.DocumentSaveIcon(), func() {
		fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil || w == nil {
				return
			}
			defer w.Close()
			if err := boot.Backup(u.cfg.DataDir, u.c.St.Snapshot, w); err != nil {
				u.fail(err)
				return
			}
			u.toast("Kopia zapisana")
		}, u.w)
		fd.SetFileName("szept-" + time.Now().Format("2006-01-02") + ".zip")
		u.showFileDlg(fd)
	})
	rows := container.NewVBox(
		card(container.NewVBox(
			widget.NewLabelWithStyle("Kopia zapasowa", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			mutedWrap("Zip z kluczem tożsamości i migawką bazy (robioną na żywo, spójną). "+
				"Baza jest zaszyfrowana kluczem z seeda, klucz chroni hasło tożsamości — "+
				"bez hasła kopia jest bezużyteczna dla znalazcy."),
			saveBtn,
		)),
		vspace(8),
	)
	if u.mobile {
		rows.Add(mutedWrap("Przywracanie kopii jest dostępne na pulpicie — " +
			"tam kopia wgrywa się jako nowe konto (profil)."))
	} else {
		restore := bigButton("Przywróć z kopii…", theme.UploadIcon(), func() {
			fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
				if err != nil || r == nil {
					return
				}
				path := r.URI().Path()
				r.Close()
				name := "kopia-" + time.Now().Format("0102-1504")
				if err := boot.Restore(path, boot.ProfileDir(name)); err != nil {
					u.fail(err)
					return
				}
				u.confirm("Kopia wgrana jako konto „"+name+"”. Przełączyć się "+
					"na nie teraz? Aplikacja uruchomi się od nowa.", func() {
					u.switchProfile(name)
				})
			}, u.w)
			u.showFileDlg(fd)
		})
		rows.Add(card(container.NewVBox(
			widget.NewLabelWithStyle("Przywracanie", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			mutedWrap("Kopia NIGDY nie nadpisuje obecnych danych — wgrywa się jako nowe "+
				"konto (profil), na które przełączasz się po restarcie."),
			restore,
		)))
	}
	u.page("Backup & restore", rows)
}

// ---- connectory ----

func (u *ui) connectorsPage() {
	active := widget.NewEntry()
	active.SetText(u.c.Cfg.Connector)
	setActive := func(url string) {
		url = core.NormalizeWS(strings.TrimSpace(url))
		if url == "" {
			return
		}
		u.cfg.Connector = url
		u.saveCfg()
		u.c.SetConnector(url)
		u.updateStatus()
		u.toast("Connector: " + url)
	}
	saveActive := bigButton("Zapisz i połącz", theme.ConfirmIcon(), func() { setActive(active.Text) })
	active.OnSubmitted = func(string) { saveActive.OnTapped() }

	reopen := func() { u.popPage(); u.connectorsPage() }

	var relay *widget.Check
	setRelay := func(on bool) {
		u.cfg.RelayOnly = on
		u.saveCfg()
		u.c.SetRelayOnly(on)
	}
	relay = widget.NewCheck("Ukryj mój adres IP", func(on bool) {
		if !on || u.cfg.RelayOnly {
			setRelay(on)
			return
		}
		relay.SetChecked(false) // stays off unless the warning is accepted
		dialog.NewCustomConfirm("Ukryć Twój adres IP?", "Rozumiem, włącz", "Anuluj",
			u.dlgBody("Rozmówcy zobaczą adres connectora zamiast Twojego. W zamian wszystko, co robisz, przechodzi przez connector.\n\n"+
				relayFacts+"\n\nKażdy Twój rozmówca dostanie informację, że łączycie się przez przekaźnik. "+
				"Rozmowy będą wolniejsze, a gdy connector nie ma przekaźnika, nie połączysz się z nikim."),
			func(ok bool) {
				if ok {
					setRelay(true)
					relay.SetChecked(true)
				}
			}, u.w).Show()
	})
	relay.Checked = u.cfg.RelayOnly
	relayHint := "Połączenia idą tylko przez przekaźnik connectora, rozmówcy widzą jego adres zamiast twojego. Wolniej. "
	if u.c.ConnRelay() {
		relayHint += "Ten connector ma przekaźnik."
	} else {
		relayHint += "Ten connector nie ma przekaźnika — z włączoną opcją nie połączysz się z nikim."
	}

	quiet := widget.NewCheck("Ukryj listę kontaktów przed connectorem", func(on bool) {
		u.cfg.QuietLookup = on
		u.saveCfg()
		u.c.SetQuietLookup(on)
	})
	quiet.Checked = u.cfg.QuietLookup

	rows := container.NewVBox(
		widget.NewLabelWithStyle("Aktywny connector", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		active, saveActive,
		mutedWrap("Stan: "+u.connState()),
		relay, mutedWrap(relayHint),
		quiet, mutedWrap("Normalnie aplikacja co 30 s pyta connector o każdy kontakt, więc connector zna całą Twoją listę kontaktów. Z tą opcją pyta tylko o osoby z czatów otwartych albo używanych w ostatnich 15 minutach. Kropki obecności pozostałych kontaktów nie będą aktualne."),
		vspace(6), section("ZAPISANE"),
	)
	for _, cn := range u.cfg.Connectors {
		cn := cn
		desc := ""
		if cn == u.c.Cfg.Connector {
			desc = "aktywny"
		}
		del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			u.cfg.Connectors = without(u.cfg.Connectors, cn)
			u.saveCfg()
			reopen()
		})
		del.Importance = widget.LowImportance
		rows.Add(container.NewBorder(nil, nil, nil, del,
			navRow(theme.ComputerIcon(), cn, desc, func() { setActive(cn) })))
	}
	if len(u.cfg.Connectors) == 0 {
		rows.Add(mutedWrap("Lista pusta — dodaj adresy, między którymi chcesz się przełączać."))
	}
	newC := widget.NewEntry()
	newC.PlaceHolder = "ws://inny-vps:7777"
	rows.Add(container.NewBorder(nil, nil, nil,
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
			url := core.NormalizeWS(strings.TrimSpace(newC.Text))
			if url == "" {
				return
			}
			u.cfg.Connectors = boot.AppendUnique(u.cfg.Connectors, url)
			u.saveCfg()
			reopen()
		}), newC))

	rows.Add(vspace(6))
	rows.Add(section("DYNAMICZNE LISTY"))
	rows.Add(mutedWrap("Adres HTTP zwracający tekst — jeden connector (ws://…) na linię. " +
		"„Odśwież” dopisuje nowe wpisy do zapisanych."))
	for _, l := range u.cfg.ConnectorLists {
		l := l
		del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			u.cfg.ConnectorLists = without(u.cfg.ConnectorLists, l)
			u.saveCfg()
			reopen()
		})
		del.Importance = widget.LowImportance
		rows.Add(container.NewBorder(nil, nil, nil, del,
			navRow(theme.SearchIcon(), l, "", nil)))
	}
	newL := widget.NewEntry()
	newL.PlaceHolder = "https://example.com/connectors.txt"
	rows.Add(container.NewBorder(nil, nil, nil,
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
			url := strings.TrimSpace(newL.Text)
			if !strings.HasPrefix(url, "https://") {
				u.fail(fmt.Errorf("adres listy musi zaczynać się od https://"))
				return
			}
			u.cfg.ConnectorLists = boot.AppendUnique(u.cfg.ConnectorLists, url)
			u.saveCfg()
			reopen()
		}), newL))
	if len(u.cfg.ConnectorLists) > 0 {
		rows.Add(bigButton("Odśwież listy teraz", theme.ViewRefreshIcon(), func() {
			lists := append([]string(nil), u.cfg.ConnectorLists...)
			go func() {
				found, errs := boot.FetchConnectorLists(lists)
				fyne.Do(func() {
					added := 0
					for _, cn := range found {
						before := len(u.cfg.Connectors)
						u.cfg.Connectors = boot.AppendUnique(u.cfg.Connectors, cn)
						added += len(u.cfg.Connectors) - before
					}
					u.saveCfg()
					msg := fmt.Sprintf("Dodano %d nowych connectorów", added)
					if errs > 0 {
						msg += fmt.Sprintf(" (%d list nie odpowiedziało)", errs)
					}
					u.toast(msg)
					reopen()
				})
			}()
		}))
	}
	u.page("Connectory", rows)
}

func without(s []string, v string) []string {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// ---- głos i wideo ----

func (u *ui) voicePage() {
	vol := u.cfg.OutputVol
	if vol <= 0 {
		vol = 1
	}
	volLbl := widget.NewLabel(fmt.Sprintf("%d%%", int(vol*100)))
	slider := widget.NewSlider(0, 200)
	slider.Step = 5
	slider.Value = vol * 100
	slider.OnChanged = func(v float64) {
		volLbl.SetText(fmt.Sprintf("%d%%", int(v)))
		u.c.SetOutputVolume(v / 100) // na żywo, słychać w trakcie rozmowy
	}
	slider.OnChangeEnded = func(v float64) {
		u.cfg.OutputVol = v / 100
		u.saveCfg()
	}

	cam := widget.NewEntry()
	cam.SetText(u.cfg.CamDevice)
	cam.PlaceHolder = "/dev/video0 (puste = domyślna)"
	saveCam := func() {
		u.cfg.CamDevice = strings.TrimSpace(cam.Text)
		u.saveCfg()
		u.toast("Zapisane")
	}
	cam.OnSubmitted = func(string) { saveCam() }

	u.page("Głos i wideo", container.NewVBox(
		section("GŁOŚNOŚĆ ROZMÓW"),
		container.NewBorder(nil, nil, nil, volLbl, slider),
		mutedWrap("Działa od razu, także w trakcie rozmowy. Głośność i wyciszenie "+
			"pojedynczych osób ustawisz w panelu rozmowy i na scenie 3D."),
		vspace(6), section("KAMERA"),
		cam,
		bigButton("Zapisz", theme.ConfirmIcon(), saveCam),
		mutedWrap("Ścieżka V4L2 na Linuksie. Na Androidzie kamera w rozmowach "+
			"nie jest jeszcze dostępna."),
		vspace(6), section("JAK TO CHODZI"),
		mutedWrap("Głos: surowe PCM 16 kHz mono, 20 ms ramki, pełna siatka P2P. "+
			"Wideo: klatki JPEG po niezawodnych kanałach danych. "+
			"Wszystko w DTLS między uczestnikami."),
	))
}

// ---- hasła i szyfrowanie ----

func (u *ui) passPage() {
	old := widget.NewPasswordEntry()
	old.PlaceHolder = "obecne hasło (puste, jeśli brak)"
	new1 := widget.NewPasswordEntry()
	new1.PlaceHolder = "nowe hasło"
	new2 := widget.NewPasswordEntry()
	new2.PlaceHolder = "nowe hasło jeszcze raz"
	change := bigButton("Zmień hasło", theme.ConfirmIcon(), func() {
		if new1.Text != new2.Text {
			u.fail(fmt.Errorf("nowe hasła się różnią"))
			return
		}
		if err := boot.ChangePass(u.cfg.DataDir, []byte(old.Text), []byte(new1.Text), u.c.St.Rekey); err != nil {
			u.fail(err)
			return
		}
		u.popPage()
		u.toast("Hasło zmienione")
	})
	u.page("Hasła i szyfrowanie", container.NewVBox(
		section("HASŁO SZYFROWANIA"),
		old, new1, new2, change,
		mutedWrap("Hasło chroni klucz tożsamości (argon2id → XChaCha20-Poly1305). "+
			"Baza danych jest szyfrowana osobnym losowym kluczem, trzymanym w tym samym pliku. "+
			"Zmiana hasła losuje nowy klucz i przepisuje bazę, więc stara kopia pliku ze starym hasłem jej nie otworzy. "+
			"Puste nowe hasło = brak hasła przy starcie."),
		vspace(8), section("CO JEST CZYM"),
		mutedWrap("Wiadomości: XChaCha20-Poly1305 kluczami epok czatu. "+
			"Podpisy zdarzeń: Ed25519. Transport: WebRTC DTLS. "+
			"Utrata hasła = utrata konta — nie ma „przypomnij hasło”."),
	))
}

// ---- konta (profile) ----

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// profileLabel: pusta nazwa profilu to katalog domyślny.
func profileLabel(name string) string {
	if name == "" {
		return "domyślne"
	}
	return name
}

// switchProfile: zapisuje wybór konta i wchodzi w nie. Na ekranie logowania
// wystarczy przejść ścieżkę login() jeszcze raz, w działającej aplikacji
// startujemy proces od nowa.
//
// Restart zamiast wymiany żywego core — zamknięcie bazy, connectora
// i rozmów w locie zostawia goroutine z Start() dobijające się do zamkniętej
// bazy. Gdyby przełączanie miało być bezszwowe, trzeba najpierw dać Core
// prawdziwe zamknięcie (kontekst + WaitGroup w Start).
func (u *ui) switchProfile(name string) {
	u.cfg.Profile = name
	u.saveCfg()
	if u.c == nil { // ekran logowania — nic jeszcze nie otwarte
		u.login()
		return
	}
	exe, err := os.Executable()
	if err == nil {
		u.c.StopWebhook() // port wolny, zanim nowy proces spróbuje go zająć
		err = exec.Command(exe, os.Args[1:]...).Start()
	}
	if err != nil {
		u.info("Konto", "Zapisane. Uruchom aplikację ponownie.")
		return
	}
	u.a.Quit()
}

// newProfile: nazwa nowego konta. Katalog powstaje dopiero przy zakładaniu
// tożsamości (boot.CreateIdentity), więc pusty profil nigdzie nie zostaje.
func (u *ui) newProfile(after func(name string)) {
	e := widget.NewEntry()
	e.PlaceHolder = "np. praca"
	dialog.NewForm("Nowe konto", "Utwórz i przełącz", "Anuluj",
		[]*widget.FormItem{widget.NewFormItem("Nazwa", e)},
		func(ok bool) {
			if !ok {
				return
			}
			name := strings.TrimSpace(e.Text)
			if !profileName.MatchString(name) {
				u.fail(fmt.Errorf("nazwa: małe litery, cyfry i myślniki (max 32)"))
				return
			}
			after(name)
		}, u.w).Show()
}

// profileBar: wybór konta na ekranie logowania. Bez niego jedyne wejście
// w inne konto prowadziło przez ustawienia — czyli przez zalogowanie się
// do tego, którego akurat nie chcesz.
func (u *ui) profileBar() fyne.CanvasObject {
	if u.mobile || u.dataFlag != "" {
		return vspace(0)
	}
	names := map[string]string{} // etykieta -> nazwa profilu
	var opts []string
	for _, p := range boot.Profiles() {
		names[profileLabel(p)] = p
		opts = append(opts, profileLabel(p))
	}
	sel := widget.NewSelect(opts, func(l string) {
		if names[l] != u.cfg.Profile {
			u.switchProfile(names[l])
		}
	})
	sel.Selected = profileLabel(u.cfg.Profile)
	add := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		u.newProfile(u.switchProfile)
	})
	return container.NewVBox(vspace(8), container.NewBorder(nil, nil,
		vcenter(mutedText("Konto", 12)), add, sel))
}

func (u *ui) accountsPage() {
	if u.mobile {
		u.page("Konta", container.NewVBox(card(mutedWrap(
			"Wiele kont na jednym urządzeniu jest na razie dostępne na pulpicie — "+
				"Android trzyma dane w piaskownicy aplikacji."))))
		return
	}
	if u.dataFlag != "" {
		// z -data wybór konta byłby po cichu ignorowany przy starcie
		u.page("Konta", container.NewVBox(card(mutedWrap(
			"Aplikacja działa na katalogu z -data "+u.dataFlag+" — konta z tego "+
				"ekranu obowiązują dopiero po starcie bez tego przełącznika."))))
		return
	}
	reopen := func() { u.popPage(); u.accountsPage() }
	rows := container.NewVBox(
		mutedWrap("Każde konto to osobna baza danych i tożsamość. "+
			"Przełączenie uruchamia aplikację od nowa na wybranym koncie."),
		section("KONTA NA TYM URZĄDZENIU"),
	)
	for _, p := range boot.Profiles() {
		p := p
		name := profileLabel(p)
		desc := ""
		if p == u.cfg.Profile {
			desc = "aktywne"
		}
		row := navRow(theme.AccountIcon(), name, desc, func() {
			if p == u.cfg.Profile {
				return
			}
			u.confirm("Przełączyć na konto „"+name+"”? Aplikacja uruchomi się od nowa.", func() {
				u.switchProfile(p)
			})
		})
		if p != "" && p != u.cfg.Profile {
			del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				u.confirm("Usunąć konto „"+name+"” WRAZ z jego bazą i tożsamością? "+
					"Tego nie da się cofnąć.", func() {
					if err := os.RemoveAll(boot.ProfileDir(p)); err != nil {
						u.fail(err)
						return
					}
					reopen()
				})
			})
			del.Importance = widget.LowImportance
			row = container.NewBorder(nil, nil, nil, del, row)
		}
		rows.Add(row)
	}
	rows.Add(section("NOWE KONTO"))
	rows.Add(bigButton("Nowe konto…", theme.ContentAddIcon(), func() {
		u.newProfile(u.switchProfile)
	}))
	rows.Add(mutedWrap("Aplikacja wystartuje od nowa i poprowadzi cię przez założenie " +
		"tożsamości dla nowego konta — connector podpowie się z obecnego."))
	u.page("Konta", rows)
}

// ---- system ----

func (u *ui) systemPage() {
	notif := widget.NewCheck("Powiadomienia o nowych wiadomościach", func(on bool) {
		u.cfg.NoNotify = !on
		u.saveCfg()
	})
	notif.Checked = !u.cfg.NoNotify

	u.page("System", container.NewVBox(
		section("POWIADOMIENIA"),
		notif,
		vspace(6), section("MULTIMEDIA"),
		navRow(theme.FolderOpenIcon(), "Folder z multimediami", u.mediaDir(), u.mediaDirPage),
		bigButton("Wyczyść pamięć podręczną obrazków", theme.DeleteIcon(), func() {
			u.decoded = map[string]any{}
			u.toast("Cache wyczyszczony")
		}),
		vspace(6), section("ŚCIEŻKI"),
		card(container.NewVBox(
			mutedWrap("Konfiguracja: "+u.cfgFile),
			mutedWrap("Dane: "+u.cfg.DataDir),
		)),
	))
}

// ---- wygląd ----

var accentPresets = []struct {
	name string
	hex  string
}{
	{"fiolet szeptu", "8b7cf6"},
	{"błękit", "5865f2"},
	{"zieleń", "23a55a"},
	{"róż", "f472b6"},
	{"bursztyn", "f0b232"},
}

func (u *ui) appearancePage() {
	rows := container.NewVBox(section("KOLOR AKCENTU"))
	sw := container.NewHBox()
	for _, p := range accentPresets {
		p := p
		col, _ := hexColor(p.hex)
		c := canvas.NewCircle(col)
		dot := container.NewGridWrap(fyne.NewSize(34, 34), c)
		sw.Add(tappable(container.NewPadded(dot), func() {
			u.cfg.Accent = p.hex
			u.saveCfg()
			u.applyAccent(p.hex)
			u.toast("Akcent: " + p.name)
		}, nil))
	}
	rows.Add(sw)
	rows.Add(mutedWrap("Zmiana działa od razu — akcent barwi przyciski, zaznaczenia i twoje dymki."))
	rows.Add(vspace(6))
	rows.Add(section("MOTYW"))
	rows.Add(mutedWrap("Ciemny — jedyny słuszny. Jasny motyw: wkrótce."))
	u.page("Wygląd", rows)
}

// hexColor: "RRGGBB" → kolor.
func hexColor(s string) (color.NRGBA, bool) {
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(s, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{r, g, b, 0xff}, true
}

// setAccent przebarwia akcent i własne dymki. Dymek to przyciemniony akcent —
// jak w oryginalnej parze kolorów.
func setAccent(col color.NRGBA) {
	cAccent = col
	cAccentSoft = color.NRGBA{R: col.R, G: col.G, B: col.B, A: 0x2e}
	cBubbleMe = color.NRGBA{
		R: uint8(int(col.R) * 78 / 100), G: uint8(int(col.G) * 78 / 100),
		B: uint8(int(col.B) * 78 / 100), A: 0xff}
}

// applyAccent: zmiana na żywo + wymuszenie przerysowania motywu.
func (u *ui) applyAccent(hex string) {
	col, ok := hexColor(hex)
	if !ok {
		return
	}
	setAccent(col)
	u.a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})
	u.msgSig = ""
	u.refresh()
}

// ---- boty / webhooki ----

func (u *ui) botsPage() {
	if u.cfg.WebhookPort == 0 {
		u.cfg.WebhookPort = 7780
	}
	port := widget.NewEntry()
	port.SetText(fmt.Sprint(u.cfg.WebhookPort))

	apply := func(on bool) {
		u.cfg.WebhookOn = on
		fmt.Sscan(port.Text, &u.cfg.WebhookPort)
		u.saveCfg()
		if on {
			if err := u.c.StartWebhook(u.cfg.WebhookPort, u.c.WebhookToken()); err != nil {
				u.fail(err)
				return
			}
			u.toast("Webhook nasłuchuje na 127.0.0.1")
		} else {
			u.c.StopWebhook()
		}
	}
	enable := widget.NewCheck("Webhook lokalny włączony", apply)
	enable.Checked = u.c.WebhookOn()

	tok := widget.NewEntry()
	tok.SetText(u.c.WebhookToken())
	tok.Disable()
	copyTok := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		u.w.Clipboard().SetContent(u.c.WebhookToken())
		u.toast("Token w schowku")
	})
	regen := widget.NewButtonWithIcon("Nowy token", theme.ViewRefreshIcon(), func() {
		if err := u.c.SetWebhookToken(core.NewWebhookToken()); err != nil {
			u.fail(err)
			return
		}
		tok.SetText(u.c.WebhookToken())
		if u.c.WebhookOn() {
			apply(true) // przeładuj nasłuch ze świeżym tokenem
		}
	})

	example := widget.NewLabel("curl -X POST http://127.0.0.1:" + port.Text + "/send \\\n" +
		"  -H \"Authorization: Bearer $TOKEN\" \\\n" +
		"  -d '{\"chat\":\"nazwa czatu\",\"text\":\"cześć z bota\"}'")
	example.TextStyle = fyne.TextStyle{Monospace: true}
	example.Wrapping = fyne.TextWrapBreak

	u.page("Boty / webhooki", container.NewVBox(
		section("WEBHOOK PRZYCHODZĄCY (LOKALNY)"),
		enable,
		widget.NewForm(widget.NewFormItem("Port", port)),
		container.NewBorder(nil, nil, nil, copyTok, tok),
		regen,
		mutedWrap("Nasłuch tylko na 127.0.0.1 — wiadomość wyśle każdy skrypt na tym "+
			"komputerze, który zna token. Tak robisz bota: skrypt woła webhook, "+
			"szept podpisuje i szyfruje jak zwykłą wiadomość."),
		card(example),
		vspace(6), section("BOTY REAGUJĄCE"),
		mutedWrap("Wychodzące webhooki (bot odpowiada na wiadomości): wkrótce."),
	))
}
