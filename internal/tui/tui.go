// Package tui: terminal client (bubbletea). Sidebar with chats & friends,
// message pane, slash commands, QR codes as unicode halfblocks, live video
// preview rendered as ANSI halfblock pixels.
package tui

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	qrcode "github.com/skip2/go-qrcode"
	xdraw "golang.org/x/image/draw"

	"szept/internal/attach"
	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/identity"
)

var (
	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stSel    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("13"))
	stOnline = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	stInfo   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	stName   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	stBorder = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8"))
)

type refreshMsg struct{}
type frameMsg struct {
	chat []byte
	from []byte
	kind string
	img  image.Image
}
type tickMsg time.Time

type videoView struct {
	img  image.Image
	from []byte
	kind string
	when time.Time
}

type model struct {
	c      *core.Core
	upd    chan struct{}
	w, h   int
	vp     viewport.Model
	in     textinput.Model
	chats  []core.ChatInfo
	sel    int
	syslog []string
	video  map[string]*videoView // chat key -> latest frame
	files  []core.Msg            // attachments of current chat (numbering for /save)
	msgs   []core.Msg            // messages of current chat (numbering for /r, /delete, /hide)
	ready  bool
}

func Run(c *core.Core) error {
	in := textinput.New()
	in.Placeholder = "wiadomość albo /pomoc"
	in.Focus()
	in.CharLimit = 4000
	m := &model{c: c, upd: c.Subscribe(), in: in, video: map[string]*videoView{}}
	p := tea.NewProgram(m, tea.WithAltScreen())
	c.OnVideoFrame = func(chat, from []byte, kind string, jpg []byte) {
		img, err := jpeg.Decode(bytes.NewReader(jpg))
		if err != nil {
			return
		}
		p.Send(frameMsg{chat: chat, from: from, kind: kind, img: img})
	}
	_, err := p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.waitUpdate(), tick())
}

func (m *model) waitUpdate() tea.Cmd {
	return func() tea.Msg {
		<-m.upd
		return refreshMsg{}
	}
}

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) curChat() *core.ChatInfo {
	if m.sel >= 0 && m.sel < len(m.chats) {
		return &m.chats[m.sel]
	}
	return nil
}

func (m *model) log(s string)    { m.syslog = append(m.syslog, stInfo.Render("· "+s)) }
func (m *model) logErr(s string) { m.syslog = append(m.syslog, stErr.Render("! "+s)) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.vp = viewport.New(m.w-30, m.h-4)
		m.ready = true
		m.refresh()
	case refreshMsg:
		m.refresh()
		return m, m.waitUpdate()
	case tickMsg:
		m.refresh()
		return m, tick()
	case frameMsg:
		m.video[string(msg.chat)] = &videoView{img: msg.img, from: msg.from, kind: msg.kind, when: time.Now()}
		m.refresh()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "tab", "shift+tab":
			if len(m.chats) > 0 {
				d := 1
				if msg.String() == "shift+tab" {
					d = len(m.chats) - 1
				}
				m.sel = (m.sel + d) % len(m.chats)
				if ch := m.curChat(); ch != nil {
					m.c.MarkChatRead(ch.ID)
				}
				m.refresh()
			}
		case "pgup":
			m.vp.ScrollUp(5)
		case "pgdown":
			m.vp.ScrollDown(5)
		case "enter":
			line := strings.TrimSpace(m.in.Value())
			m.in.SetValue("")
			if ch := m.curChat(); ch != nil {
				m.c.SendTyping(ch.ID, false)
			}
			if line != "" {
				if cmd := m.exec(line); cmd != nil {
					return m, cmd
				}
			}
			m.refresh()
		}
	}
	var cmd tea.Cmd
	oldVal := m.in.Value()
	m.in, cmd = m.in.Update(msg)
	newVal := m.in.Value()
	if oldVal != newVal && !strings.HasPrefix(newVal, "/") {
		if ch := m.curChat(); ch != nil {
			m.c.SendTyping(ch.ID, len(newVal) > 0)
		}
	}
	return m, cmd
}

func (m *model) exec(line string) tea.Cmd {
	if !strings.HasPrefix(line, "/") {
		ch := m.curChat()
		if ch == nil {
			m.logErr("brak czatu — /nowy <nazwa> albo /dolacz <kod>")
			return nil
		}
		if err := m.c.Send(ch.ID, line); err != nil {
			m.logErr(err.Error())
		}
		return nil
	}
	parts := strings.SplitN(line, " ", 2)
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	ch := m.curChat()
	switch parts[0] {
	case "/pomoc", "/help":
		m.log("/nowy <nazwa> · /dm <znajomy> · /zapros · /dolacz <kod> <pin> · /znajomy <kod> · /mojkod · /kto · /kick <nazwa> · /rekey · /zmien <nazwa> · /connector <adres> · /call · /rozlacz · /mic · /cam · /ekran · /quit")
		m.log("/wyslij <ścieżka> [podpis] · /zapisz <nr> · /szukaj <fraza> · /r <nr> <emoji> · /usun <nr> (u wszystkich) · /ukryj <nr> (u siebie) · /awatar <ścieżka>")
	case "/szukaj", "/search":
		if ch == nil || arg == "" {
			m.logErr("użycie: /szukaj <fraza>")
			break
		}
		res, err := m.c.SearchChat(ch.ID, arg)
		if err != nil {
			m.logErr(err.Error())
		} else {
			m.log(fmt.Sprintf("WYNIKI WYSZUKIWANIA (%d):", len(res)))
			for _, rm := range res {
				m.syslog = append(m.syslog, fmt.Sprintf(" [%s] %s: %s", rm.TS.Format("15:04"), rm.Name, rm.Text))
			}
		}
	case "/react", "/r":
		if ch == nil || arg == "" {
			m.logErr("użycie: /r <nr_wiadomości> <emoji>")
			break
		}
		f := strings.Fields(arg)
		if len(f) < 2 {
			m.logErr("użycie: /r <nr_wiadomości> <emoji>")
			break
		}
		n, err := strconv.Atoi(f[0])
		if err != nil || n < 1 || n > len(m.msgs) {
			m.logErr("nieprawidłowy numer wiadomości")
			break
		}
		target := m.msgs[n-1].ID
		if err := m.c.ReactMsg(ch.ID, target, f[1]); err != nil {
			m.logErr(err.Error())
		}
	case "/usun", "/delete":
		if ch == nil || arg == "" {
			m.logErr("użycie: /usun <nr_wiadomości> — usuwa u wszystkich w DAG")
			break
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > len(m.msgs) {
			m.logErr("nieprawidłowy numer wiadomości")
			break
		}
		target := m.msgs[n-1].ID
		if err := m.c.DeleteMsg(ch.ID, target); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("usunięto wiadomość u wszystkich")
		}
	case "/ukryj", "/hide":
		if ch == nil || arg == "" {
			m.logErr("użycie: /ukryj <nr_wiadomości> — ukrywa u siebie")
			break
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > len(m.msgs) {
			m.logErr("nieprawidłowy numer wiadomości")
			break
		}
		target := m.msgs[n-1].ID
		if err := m.c.HideMsg(ch.ID, target); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("ukryto wiadomość u siebie")
		}
	case "/quit":
		return tea.Quit
	case "/nowy", "/new":
		if arg == "" {
			m.logErr("podaj nazwę grupy")
			break
		}
		if _, err := m.c.CreateChat(arg); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("utworzono grupę „" + arg + "”")
		}
	case "/dm":
		fs, _ := m.c.St.Friends()
		found := false
		for _, f := range fs {
			if strings.EqualFold(f.Display(), arg) || strings.HasPrefix(strings.ToLower(f.Display()), strings.ToLower(arg)) {
				if _, err := m.c.CreateDirect(f); err != nil {
					m.logErr(err.Error())
				} else {
					m.log("czat z " + f.Display())
				}
				found = true
				break
			}
		}
		if !found {
			m.logErr("nie znam takiego znajomego (dodaj: /znajomy <kod>)")
		}
	case "/zapros", "/invite":
		if ch == nil {
			m.logErr("wybierz czat")
			break
		}
		code, pin, err := m.c.NewInvite(ch.ID)
		if err != nil {
			m.logErr(err.Error())
			break
		}
		m.log("KOD ZAPROSZENIA (jednorazowy, ważny 10 min):")
		m.syslog = append(m.syslog, code)
		m.log("PIN (podaj drugiej osobie innym kanałem): " + pin)
		m.syslog = append(m.syslog, qrBlock(code)...)
	case "/dolacz", "/join":
		f := strings.Fields(arg)
		if len(f) < 2 {
			m.logErr("użycie: /dolacz <kod> <pin>")
			break
		}
		if err := m.c.Redeem(f[0], f[1]); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("dołączanie… (zapraszający musi być online)")
		}
	case "/znajomy", "/friend":
		f, err := m.c.AddFriend(arg)
		if err != nil {
			m.logErr(err.Error())
		} else {
			m.log("dodano znajomego: " + f.Display())
		}
	case "/connector":
		if arg == "" {
			m.log("connector: " + m.c.Cfg.Connector + " — zmiana: /connector <adres>")
			break
		}
		url := core.NormalizeWS(arg)
		m.c.SetConnector(url)
		cfgFile, _ := boot.Dirs()
		cfg := m.c.Cfg
		if err := boot.SaveConfig(cfgFile, &cfg); err != nil {
			m.logErr("zapis konfiguracji: " + err.Error())
		}
		m.log("connector zmieniony na " + url + " — łączę…")
	case "/lang", "/jezyk":
		if arg == "" {
			m.log("język / language: " + m.c.Lang() + " — zmiana: /lang <pl|en>")
			break
		}
		if err := m.c.SetLang(arg); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("zmieniono język / language set to " + m.c.Lang())
		}
	case "/wyslij", "/send":
		if ch == nil || arg == "" {
			m.logErr("użycie: /wyslij <ścieżka> [podpis]")
			break
		}
		path, caption := splitPathArg(arg)
		if err := m.c.SendFilePath(ch.ID, path, caption); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("wysłano " + filepath.Base(path))
		}
	case "/zapisz", "/save":
		f := strings.Fields(arg)
		if len(f) == 0 {
			m.logErr("użycie: /zapisz <nr> [katalog] — numery są przy załącznikach")
			break
		}
		n, err := strconv.Atoi(f[0])
		if err != nil || n < 1 || n > len(m.files) {
			m.logErr("nie ma załącznika o takim numerze")
			break
		}
		dir := "."
		if len(f) > 1 {
			dir = expandPath(strings.Join(f[1:], " "))
		}
		at := m.files[n-1]
		p, err := m.c.SaveAttachment(at.Blob, at.File, dir)
		if err != nil {
			m.logErr(err.Error())
		} else {
			m.log("zapisano: " + p)
		}
	case "/awatar", "/avatar":
		if arg == "" {
			m.logErr("użycie: /awatar <ścieżka do PNG/JPEG/GIF-a> albo /awatar - żeby usunąć")
			break
		}
		if arg == "-" {
			if err := m.c.ClearAvatar(); err != nil {
				m.logErr(err.Error())
			} else {
				m.log("awatar usunięty")
			}
			break
		}
		if err := m.c.SetAvatarPath(expandPath(arg)); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("awatar ustawiony (przeskalowany i przekodowany)")
		}
	case "/mojkod", "/mycode", "/code":
		code := m.c.FriendCode()
		m.log("TWÓJ KOD ZNAJOMEGO (odcisk: " + identity.Fingerprint(m.c.ID.Pub) + "):")
		m.syslog = append(m.syslog, code)
		m.syslog = append(m.syslog, qrBlock(code)...)
	case "/kto", "/who":
		if ch == nil {
			break
		}
		for _, mem := range ch.Members {
			dot := "  "
			if m.c.Online(mem.Pub) || string(mem.Pub) == string(m.c.ID.Pub) {
				dot = stOnline.Render("● ")
			}
			m.syslog = append(m.syslog, dot+mem.Name+" "+stDim.Render(identity.Fingerprint(mem.Pub)))
		}
	case "/kick":
		if ch == nil || arg == "" {
			m.logErr("użycie: /kick <nazwa>")
			break
		}
		for _, mem := range ch.Members {
			if strings.EqualFold(mem.Name, arg) {
				if err := m.c.Kick(ch.ID, mem.Pub); err != nil {
					m.logErr(err.Error())
				} else {
					m.log("usunięto " + mem.Name + " + rotacja klucza")
				}
				break
			}
		}
	case "/rekey":
		if ch == nil {
			break
		}
		if err := m.c.Rekey(ch.ID); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("nowa epoka klucza")
		}
	case "/zmien", "/rename":
		if ch == nil || arg == "" {
			break
		}
		if err := m.c.Rename(ch.ID, arg); err != nil {
			m.logErr(err.Error())
		}
	case "/call":
		if ch == nil {
			break
		}
		if err := m.c.JoinCall(ch.ID); err != nil {
			m.logErr(err.Error())
		} else {
			m.log("dołączono do rozmowy (mikrofon aktywny)")
		}
	case "/rozlacz", "/leave", "/hangup":
		if ch != nil {
			m.c.LeaveCall(ch.ID)
			delete(m.video, string(ch.ID))
			m.log("rozłączono")
		}
	case "/mic":
		if ch != nil {
			m.c.ToggleMic(ch.ID)
		}
	case "/cam":
		if ch != nil {
			if err := m.c.ToggleCam(ch.ID, arg); err != nil {
				m.logErr(err.Error())
			}
		}
	case "/ekran", "/screen":
		if ch != nil {
			if err := m.c.ToggleScreen(ch.ID); err != nil {
				m.logErr(err.Error())
			}
		}
	default:
		m.logErr("nieznana komenda, /pomoc")
	}
	m.refresh()
	return nil
}

func (m *model) refresh() {
	if !m.ready {
		return
	}
	m.chats = m.c.Chats()
	if m.sel >= len(m.chats) {
		m.sel = 0
	}
	var b strings.Builder
	m.files = m.files[:0]
	if ch := m.curChat(); ch != nil {
		m.msgs = m.c.Messages(ch.ID)
		var preview *core.Msg
		for i, msg := range m.msgs {
			ts := stDim.Render(msg.TS.Format("15:04"))
			idxStr := stDim.Render(fmt.Sprintf("[%d] ", i+1))
			ackStr := formatACK(msg.ACK)
			reactStr := formatReactions(msg.Reactions)
			switch msg.Kind {
			case "info":
				b.WriteString(ts + " " + stDim.Render(msg.Text) + "\n")
			case "file":
				m.files = append(m.files, msg)
				state := stDim.Render("(pobieranie…)")
				if msg.Have {
					state = stDim.Render(fmt.Sprintf("[%d] /zapisz", len(m.files)))
					if attach.IsImage(msg.Mime) {
						msg := msg
						preview = &msg
					}
				}
				b.WriteString(fmt.Sprintf("%s%s %s: 📎 %s · %s %s%s%s\n", idxStr, ts, stName.Render(msg.Name),
					msg.File, attach.HumanSize(msg.Size), state, reactStr, ackStr))
				if msg.Text != "" {
					b.WriteString("        " + msg.Text + "\n")
				}
			default:
				txt := msg.Text
				if msg.Deleted {
					txt = stDim.Render("[Wiadomość usunięta]")
				}
				b.WriteString(idxStr + ts + " " + stName.Render(msg.Name) + ": " + txt + reactStr + ackStr + "\n")
			}
		}
		if typing := m.c.TypingPeers(ch.ID); len(typing) > 0 {
			b.WriteString(stInfo.Render("✍ "+strings.Join(typing, ", ")+" pisze...") + "\n")
		}
		// preview of last image — terminal renders via halfblocks
		if preview != nil {
			if data, ok := m.c.BlobBytes(preview.Blob); ok {
				if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					b.WriteString("\n" + stTitle.Render("🖼 "+preview.File) + "\n")
					b.WriteString(halfblocks(img, min(m.w-32, 80)))
				}
			}
		}
		// live video?
		if v := m.video[string(ch.ID)]; v != nil && time.Since(v.when) < 3*time.Second {
			who := "ja"
			if string(v.from) != string(m.c.ID.Pub) {
				st, _ := m.c.ChatState(ch.ID)
				_ = st
				who = identity.Fingerprint(v.from)[:9]
				for _, mem := range ch.Members {
					if string(mem.Pub) == string(v.from) {
						who = mem.Name
					}
				}
			}
			kind := "kamera"
			if v.kind == "s" {
				kind = "ekran"
			}
			b.WriteString("\n" + stTitle.Render(fmt.Sprintf("▶ %s (%s)", who, kind)) + "\n")
			b.WriteString(halfblocks(v.img, min(m.w-32, 100)))
		}
	}
	if len(m.syslog) > 0 {
		b.WriteString("\n")
		start := 0
		if len(m.syslog) > 60 {
			start = len(m.syslog) - 60
		}
		for _, s := range m.syslog[start:] {
			b.WriteString(s + "\n")
		}
	}
	m.vp.SetContent(b.String())
	m.vp.GotoBottom()
}

func (m *model) View() string {
	if !m.ready {
		return "ładowanie…"
	}
	// sidebar
	var sb strings.Builder
	sb.WriteString(stTitle.Render("CZATY") + "\n")
	for i, ch := range m.chats {
		line := ch.Name
		if len(ch.InCall) > 0 {
			line += " 📞"
		}
		if ch.Online > 0 {
			line += stOnline.Render(fmt.Sprintf(" ●%d", ch.Online))
		}
		if i == m.sel {
			line = stSel.Render(" " + line + " ")
		} else {
			line = " " + line
		}
		sb.WriteString(truncate(line, 27) + "\n")
	}
	sb.WriteString("\n" + stTitle.Render("ZNAJOMI") + "\n")
	fs, _ := m.c.St.Friends()
	sort.Slice(fs, func(i, j int) bool { return fs[i].Name < fs[j].Name })
	for _, f := range fs {
		dot := stDim.Render("○ ")
		if m.c.Online(f.Pub) {
			dot = stOnline.Render("● ")
		}
		sb.WriteString(dot + truncate(f.Display(), 24) + "\n")
	}
	side := lipgloss.NewStyle().Width(28).Height(m.h - 2).Render(sb.String())

	main := m.vp.View() + "\n" + m.in.View()

	status := stDim.Render(fmt.Sprintf(" %s · %s · %s", m.c.ID.Name, identity.Fingerprint(m.c.ID.Pub), m.callStatus()))
	body := lipgloss.JoinHorizontal(lipgloss.Top, side, stBorder.Render(""), " "+main)
	return status + "\n" + body
}

func (m *model) callStatus() string {
	ch := m.curChat()
	if ch == nil {
		return "brak czatu"
	}
	call := m.c.CallState(ch.ID)
	if call == nil {
		return "cisza"
	}
	bits := []string{}
	if call.Joined {
		bits = append(bits, "W ROZMOWIE")
		if call.Mic {
			bits = append(bits, "mic✓")
		} else {
			bits = append(bits, "mic✗")
		}
		if call.Cam {
			bits = append(bits, "cam✓")
		}
		if call.Screen {
			bits = append(bits, "ekran✓")
		}
	} else if len(call.Members) > 0 {
		bits = append(bits, fmt.Sprintf("trwa rozmowa (%d) — /call by dołączyć", len(call.Members)))
	}
	return strings.Join(bits, " ")
}

// splitPathArg splits "/send <path> [caption]". When full argument
// points to existing file, entire string is path — names with spaces work
// without quotes (quotes are handled as well).
func splitPathArg(arg string) (path, caption string) {
	if strings.HasPrefix(arg, `"`) {
		if i := strings.Index(arg[1:], `"`); i >= 0 {
			return expandPath(arg[1 : i+1]), strings.TrimSpace(arg[i+2:])
		}
	}
	if full := expandPath(arg); fileExists(full) {
		return full, ""
	}
	p, c, _ := strings.Cut(arg, " ")
	return expandPath(p), strings.TrimSpace(c)
}

func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- QR as unicode halfblocks ----

func qrBlock(content string) []string {
	q, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return []string{"(QR error)"}
	}
	grid := q.Bitmap()
	var lines []string
	for y := 0; y < len(grid); y += 2 {
		var row strings.Builder
		for x := 0; x < len(grid[y]); x++ {
			top := grid[y][x]
			bot := y+1 < len(grid) && grid[y+1][x]
			switch {
			case top && bot:
				row.WriteRune('█')
			case top:
				row.WriteRune('▀')
			case bot:
				row.WriteRune('▄')
			default:
				row.WriteRune(' ')
			}
		}
		lines = append(lines, row.String())
	}
	return lines
}

// ---- video frame as ANSI halfblock pixels ----

func halfblocks(img image.Image, cols int) string {
	if cols < 16 {
		cols = 16
	}
	b := img.Bounds()
	rows := b.Dy() * cols / b.Dx() / 2
	if rows < 4 {
		rows = 4
	}
	if rows > 40 {
		rows = 40
	}
	small := image.NewRGBA(image.Rect(0, 0, cols, rows*2))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, xdraw.Over, nil)
	var out strings.Builder
	for y := 0; y < rows*2; y += 2 {
		for x := 0; x < cols; x++ {
			tr, tg, tb, _ := small.At(x, y).RGBA()
			br, bg, bb, _ := small.At(x, y+1).RGBA()
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				tr>>8, tg>>8, tb>>8, br>>8, bg>>8, bb>>8)
		}
		out.WriteString("\x1b[0m\n")
	}
	return out.String()
}

func formatReactions(rm map[string]string) string {
	if len(rm) == 0 {
		return ""
	}
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
	sort.Strings(parts)
	return " [" + strings.Join(parts, " ") + "]"
}

func formatACK(status string) string {
	switch status {
	case "read":
		return stOnline.Render(" ✓✓")
	case "delivered":
		return stDim.Render(" ✓")
	default:
		return ""
	}
}
