// App: everything the frontend can call. Every method here is bound by Wails
// and reachable from JS as `main.App.<Name>` (see api() in app.js).
//
// Rule of thumb for this layer: no logic, only translation. Byte slices become
// hex strings, core errors travel as-is, and anything larger than a few KB is
// served over HTTP instead (media.go).
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/wailsapp/wails/v3/pkg/application"

	"szept/internal/attach"
	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/identity"
	"szept/internal/media"
	"szept/internal/update"
	"szept/internal/version"
)

type App struct {
	app    *application.App
	win    application.Window
	notify func(id, title, body string) error // nil = no notifications here

	mu      sync.Mutex
	c       *core.Core
	cfg     *core.Config
	cfgFile string
	baseDir string
	pinned  bool // -data given: profiles are off, one fixed directory

	sel    string                    // hex id of the open chat ("" = none)
	selCh  map[string]string         // chat hex -> open text channel hex ("" = default)
	seenCh map[string]map[string]int // chat hex -> channel hex -> messages from others seen
	seen   map[string]int            // chat hex -> event count already shown
	booted bool
	prev   map[string]prevEntry // chat hex -> sidebar preview, see preview()
	bg     bool                 // Android activity stopped, see bgping.go
	bgSeen map[string]int       // chat hex -> event count when last checked in the background

	frames  map[string]*frame // live video tiles, see media.go
	frameNo uint64
	camStop func() // camera test in the settings, see CamTest

	dropped map[string]bool // paths dragged onto the window, see ReadDropped

	upd UpdateInfo      // newest release found, see CheckUpdate
	rel *update.Release // its signed file list, for InstallUpdate
}

func newApp(cfg *core.Config, cfgFile, baseDir string, pinned bool) *App {
	return &App{
		cfg: cfg, cfgFile: cfgFile, baseDir: baseDir, pinned: pinned,
		seen:   map[string]int{},
		selCh:  map[string]string{},
		seenCh: map[string]map[string]int{},
		frames: map[string]*frame{},
	}
}

func (a *App) shutdown() {
	if c, err := a.live(); err == nil {
		c.Close()
	}
}

// ---- helpers ----

func hexs(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return hex.EncodeToString(b)
}

func unhex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

var errNoCore = errors.New("nie zalogowano")

// live returns the running core, or an error the frontend can show.
// Bound methods run on their own goroutines and login happens on another, so
// the pointer itself needs the lock — callers that go on to take a.mu must
// call this first, never inside the critical section.
func (a *App) live() (*core.Core, error) {
	a.mu.Lock()
	c := a.c
	a.mu.Unlock()
	if c == nil {
		return nil, errNoCore
	}
	return c, nil
}

func (a *App) profileDir() string {
	if a.pinned {
		return a.baseDir
	}
	return boot.ProfileDir(a.cfg.Profile)
}

func (a *App) saveCfg() { _ = boot.SaveConfig(a.cfgFile, a.cfg) }

// ---- boot & login ----

type BootInfo struct {
	Stage     string   `json:"stage"` // setup | pass | ready
	Profile   string   `json:"profile"`
	Profiles  []string `json:"profiles"`
	Connector string   `json:"connector"`
	Accent    string   `json:"accent"`
	Pinned    bool     `json:"pinned"`
	Mobile    bool     `json:"mobile"` // Android: no window chrome, no self-update
}

// Boot: which screen to show. Safe to call again after a profile switch.
func (a *App) Boot() BootInfo {
	b := BootInfo{
		Profile: a.cfg.Profile, Connector: a.cfg.Connector,
		Accent: a.cfg.Accent, Pinned: a.pinned, Profiles: []string{},
		Mobile: mobile,
	}
	if !a.pinned {
		b.Profiles = boot.Profiles()
	}
	if c, err := a.live(); err == nil {
		b.Stage = "ready"
		if c.Linking() {
			b.Stage = "link" // waiting for the account device, see LinkSetup
		}
		return b
	}
	dir := a.profileDir()
	a.cfg.DataDir = dir
	switch {
	case !boot.HasIdentity(dir):
		b.Stage = "setup"
	default:
		b.Stage = "pass"
		// An unencrypted key means no password screen is needed at all.
		if id, err := boot.LoadIdentity(dir, nil); err == nil {
			if a.cfg.Connector == "" {
				b.Stage = "setup" // no connector yet — ask for it once
				break
			}
			if err := a.enter(id); err == nil {
				b.Stage = "ready"
			}
		}
	}
	return b
}

// Setup creates the identity for the active profile and logs into it.
func (a *App) Setup(name, connector, pass string) error {
	name, connector = strings.TrimSpace(name), strings.TrimSpace(connector)
	if name == "" || connector == "" {
		return errors.New("podaj nazwę i adres connectora")
	}
	a.cfg.Connector = core.NormalizeWS(connector)
	a.cfg.DataDir = a.profileDir()
	id, err := boot.LoadIdentity(a.cfg.DataDir, []byte(pass))
	if err != nil { // brand new profile
		if boot.HasIdentity(a.cfg.DataDir) {
			return errors.New("złe hasło")
		}
		if id, err = boot.CreateIdentity(a.cfg.DataDir, name, []byte(pass)); err != nil {
			return err
		}
	}
	a.saveCfg()
	return a.enter(id)
}

// Unlock opens an existing, password-protected identity.
func (a *App) Unlock(pass string) error {
	a.cfg.DataDir = a.profileDir()
	id, err := boot.LoadIdentity(a.cfg.DataDir, []byte(pass))
	if err != nil {
		return errors.New("złe hasło — spróbuj jeszcze raz")
	}
	return a.enter(id)
}

func (a *App) enter(id *identity.Identity) error {
	c, err := core.Open(*a.cfg, id)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.c = c
	a.prev = map[string]prevEntry{}
	a.mu.Unlock()
	c.OnVideoFrame = a.onFrame
	c.Start()
	if a.cfg.WebhookOn && a.cfg.WebhookPort > 0 {
		_ = c.StartWebhook(a.cfg.WebhookPort, c.WebhookToken())
	}
	go a.watch(c)
	return nil
}

// watch coalesces the core's event storm into at most ~12 repaints a second.
// DAG sync fires hundreds of notifications per second; the webview does not
// need to hear about each one.
func (a *App) watch(c *core.Core) {
	ch := c.Subscribe()
	for range ch {
		time.Sleep(80 * time.Millisecond)
		for drained := false; !drained; {
			select {
			case <-ch:
			default:
				drained = true
			}
		}
		a.bgPings(c)
		a.app.Event.Emit("sync")
	}
}

var profileName = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// UseProfile switches accounts. A live core is restarted rather than swapped:
// closing the database under running sync goroutines is not something core
// supports yet (same reasoning as the Fyne client).
func (a *App) UseProfile(name string) error {
	if a.pinned {
		return errors.New("konta są wyłączone przy uruchomieniu z -data")
	}
	if name != "" && !profileName.MatchString(name) {
		return errors.New("nazwa konta: małe litery, cyfry i myślniki (max 32)")
	}
	a.cfg.Profile = name
	a.saveCfg()
	if _, err := a.live(); err != nil {
		return nil // login screen: Boot() will pick the new directory up
	}
	return a.Restart()
}

// Restart starts a fresh copy of the executable (an installed update, a
// switched account) and quits this one.
func (a *App) Restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if c, err := a.live(); err == nil {
		c.StopWebhook() // free the port before the new process claims it
	}
	if err := exec.Command(exe, os.Args[1:]...).Start(); err != nil {
		return err
	}
	a.app.Quit()
	return nil
}

// UpdateInfo: a newer signed release ("" = none found yet) and whether it is
// already installed.
type UpdateInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Err       string `json:"err"`
}

// Update reports what the last check found.
func (a *App) Update() UpdateInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.upd
}

// CheckUpdate asks for the newest signed release without installing it.
// Version stays "" when this build is current (or a dev build).
func (a *App) CheckUpdate() (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := update.Latest(ctx)
	if err != nil {
		return UpdateInfo{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if update.Newer(r.Version, version.String()) && !(a.upd.Installed && a.upd.Version == r.Version) {
		a.rel, a.upd = r, UpdateInfo{Version: r.Version}
	}
	return a.upd, nil
}

// InstallUpdate puts the release CheckUpdate found over the running
// executable; it runs from the next start (see Restart).
func (a *App) InstallUpdate() (UpdateInfo, error) {
	a.mu.Lock()
	r, info := a.rel, a.upd
	a.mu.Unlock()
	if r == nil || info.Installed {
		return info, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	info.Err = ""
	if err := r.Install(ctx); err != nil {
		info.Err = err.Error()
	} else {
		info.Installed = true
	}
	a.mu.Lock()
	a.upd = info
	a.mu.Unlock()
	return info, nil
}

// updates: with auto-update on (the default), check shortly after start and
// then every six hours, install and tell the user once per version. Off, the
// app never asks GitHub by itself: only the button in About does.
func (a *App) updates() {
	update.Cleanup()
	for time.Sleep(15 * time.Second); ; time.Sleep(6 * time.Hour) {
		if a.cfg.NoUpdate {
			continue
		}
		before := a.Update()
		info, err := a.CheckUpdate()
		if err != nil || info.Version == "" || info.Version == before.Version {
			continue
		}
		info, _ = a.InstallUpdate()
		a.app.Event.Emit("update", info)
		body := "Zainstalowana. Uruchom szepta ponownie, żeby jej używać."
		if !info.Installed {
			body = "Nie udało się jej zainstalować, pobierz ją z github.com/OppaiHacker/Szept/releases."
		}
		if a.notify != nil && !a.cfg.NoNotify {
			_ = a.notify("szept-update", "Nowa wersja szepta: "+info.Version, body)
		}
	}
}

// ---- snapshot ----

type Me struct {
	Pub       string `json:"pub"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Status    string `json:"status"`
	Bio       string `json:"bio"`
	Style     string `json:"style"`
	Avatar    string `json:"avatar"`
	Banner    string `json:"banner"`
	Profile   string `json:"profile"`
	Connector string `json:"connector"`
	ConnUp    bool   `json:"connUp"`
	ConnErr   string `json:"connErr"`
	ConnVer   string `json:"connVer"`
	ConnRelay bool   `json:"connRelay"`
	Lang      string `json:"lang"`
	Accent    string `json:"accent"`
	Dev       bool   `json:"dev"`
}

type Chat struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Avatar     string              `json:"avatar"` // "" or a /avatar URL; group: its icon
	Banner     string              `json:"banner"` // group banner, "" = none
	Style      string              `json:"style"`  // group decoration
	Peer       string              `json:"peer"`   // direct chat: the other side's pubkey
	Direct     bool                `json:"direct"`
	Members    int                 `json:"members"`
	Online     int                 `json:"online"`
	InCall     []string            `json:"inCall"`
	Call       *CallInfo           `json:"call,omitempty"` // set only while you are in this chat's call
	IsOwner    bool                `json:"isOwner"`
	History    bool                `json:"history"` // members added now see earlier messages
	CallKey    string              `json:"callKey"` // the call above: chat ID + voice channel ID
	Channels   []ChanInfo          `json:"channels"`
	Voice      map[string][]string `json:"voice"`      // voice channel ID ("" = default) -> names in its call
	VFlags     map[string][]string `json:"vflags"`     // same keys and order: m mic off, d deafened, c camera, s screen
	ChanUnread map[string]int      `json:"chanUnread"` // text channel ID -> new messages
	Unread     int                 `json:"unread"`
	Preview    string              `json:"preview"`
	PreviewTS  int64               `json:"previewTS"`
}

type ChanInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // text | voice | cat
	Cat  string `json:"cat"`
	Arch bool   `json:"arch"`
}

type Friend struct {
	Pub  string `json:"pub"`
	Name string `json:"name"`
	// Alias: the name the peer announces for itself when it differs from the one
	// you gave them — a silent rename is exactly the socially engineered part.
	Alias  string `json:"alias,omitempty"`
	Online bool   `json:"online"`
	Relay  string `json:"relay"` // TURN relay the link goes through, "" = direct
	Avatar string `json:"avatar"`
	Status string `json:"status"`
	Style  string `json:"style"`
	Chat   string `json:"chat"` // existing direct chat, if any
}

type Snapshot struct {
	Me      Me       `json:"me"`
	Chats   []Chat   `json:"chats"`
	Friends []Friend `json:"friends"`
	Unread  int      `json:"unread"`
}

// Snapshot: one call feeds the sidebar, the friend list and the status bar.
func (a *App) Snapshot() (Snapshot, error) {
	c, err := a.live()
	if err != nil {
		return Snapshot{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	card, _ := c.St.Profile(c.ID.Pub)
	s := Snapshot{Me: Me{
		// a linked device shows the account: the key friends know, its face
		Pub: hexs(c.Account()), Name: c.ID.Name, Code: c.FriendCode(),
		Status: card.Status, Bio: card.Bio, Style: card.Style,
		Avatar: avatarURL(c, c.Account()), Banner: bannerURL(c, c.Account()),
		Profile: a.cfg.Profile, Connector: c.Cfg.Connector,
		ConnUp: c.ConnUp(), ConnErr: c.ConnErr(), ConnVer: c.ConnVersion(), ConnRelay: c.ConnRelay(), Lang: c.Lang(), Accent: a.cfg.Accent, Dev: a.cfg.DevMode,
	}, Chats: []Chat{}, Friends: []Friend{}}

	direct := map[string]string{} // peer hex -> chat hex
	for _, ci := range c.Chats() {
		id := hexs(ci.ID)
		n := c.St.EventCount(ci.ID)
		if !a.booted {
			a.seen[id] = n // first paint: history is not "unread"
		}
		ch := Chat{
			ID: id, Name: ci.Name, Members: len(ci.Members),
			Online: ci.Online, InCall: ci.InCall,
		}
		if ch.InCall == nil {
			ch.InCall = []string{}
		}
		if key := c.JoinedCall(); len(key) >= 32 && equalBytes(key[:32], ci.ID) {
			if call := c.CallState(key); call != nil {
				ch.Call = &CallInfo{Joined: true, Mic: call.Mic, Deaf: call.Deaf, Cam: call.Cam, Screen: call.Screen}
				ch.CallKey = hexs(key)
			}
		}
		ch.IsOwner = equalBytes(ci.Owner, c.ID.Pub)
		ch.History = !ci.NoHistory
		ch.Avatar, ch.Banner, ch.Style = blobURL(c, ci.Icon), blobURL(c, ci.Banner), ci.Style
		ch.Channels, ch.Voice, ch.VFlags = []ChanInfo{}, map[string][]string{}, map[string][]string{}
		for _, x := range ci.Channels {
			ch.Channels = append(ch.Channels, ChanInfo{ID: hexs(x.ID), Name: x.Name, Kind: x.Kind, Cat: hexs(x.Cat), Arch: x.Arch})
		}
		for k, names := range ci.Voice {
			ch.Voice[hexs([]byte(k))] = names
			ch.VFlags[hexs([]byte(k))] = ci.VFlags[k]
		}
		if ci.Direct { // a named group of two is still a group
			for _, m := range ci.Members {
				if !c.Mine(m.Pub) {
					ch.Direct, ch.Peer = true, hexs(m.Pub)
					ch.Avatar = avatarURL(c, m.Pub)
					direct[ch.Peer] = id
				}
			}
		}
		if n > a.seen[id] && id != a.sel {
			ch.Unread = n - a.seen[id]
		} else {
			a.seen[id] = n
		}
		p := a.preview(c, ci)
		ch.Preview, ch.PreviewTS = p.text, p.ts
		// channels with news: what others wrote since you last had it open
		if a.seenCh[id] == nil {
			a.seenCh[id] = map[string]int{}
		}
		ch.ChanUnread = map[string]int{}
		for k, n := range p.inCh {
			open := id == a.sel && k == a.selCh[id]
			if !a.booted || open || n < a.seenCh[id][k] {
				a.seenCh[id][k] = n
			} else if n > a.seenCh[id][k] {
				ch.ChanUnread[k] = n - a.seenCh[id][k]
			}
		}
		s.Chats = append(s.Chats, ch)
		s.Unread += ch.Unread
	}
	a.booted = true
	// newest conversation on top; never-used chats keep their creation order
	sort.SliceStable(s.Chats, func(i, j int) bool { return s.Chats[i].PreviewTS > s.Chats[j].PreviewTS })

	fs, _ := c.St.Friends()
	for _, f := range fs {
		st, _ := c.StatusOf(f.Pub)
		s.Friends = append(s.Friends, Friend{
			Pub: hexs(f.Pub), Name: f.Display(), Alias: f.Alias(), Online: c.Online(f.Pub), Relay: c.Relay(f.Pub),
			Avatar: avatarURL(c, f.Pub), Status: st, Style: c.StyleOf(f.Pub), Chat: direct[hexs(f.Pub)],
		})
	}
	sort.Slice(s.Friends, func(i, j int) bool {
		if s.Friends[i].Online != s.Friends[j].Online {
			return s.Friends[i].Online
		}
		return strings.ToLower(s.Friends[i].Name) < strings.ToLower(s.Friends[j].Name)
	})
	return s, nil
}

func equalBytes(a, b []byte) bool { return hexs(a) == hexs(b) }

type prevEntry struct {
	rev  uint64
	text string
	ts   int64
	inCh map[string]int // channel hex -> messages from others in it
}

// preview caches the sidebar line under the chat's store revision, so a
// refresh does not decrypt every chat's whole history again. Caller holds a.mu.
func (a *App) preview(c *core.Core, ci core.ChatInfo) prevEntry {
	key, rev := hexs(ci.ID), c.St.Rev(ci.ID)
	if p, ok := a.prev[key]; ok && p.rev == rev {
		return p
	}
	msgs := c.Messages(ci.ID)
	p := prevEntry{rev: rev, inCh: map[string]int{}}
	p.text, p.ts = preview(c, ci, msgs)
	for _, m := range msgs {
		if m.Kind != "info" && !c.Mine(m.Author) {
			p.inCh[hexs(m.Ch)]++
		}
	}
	a.prev[key] = p
	return p
}

// preview: last real message in a chat, formatted for the sidebar.
func preview(c *core.Core, ci core.ChatInfo, msgs []core.Msg) (string, int64) {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Kind == "info" {
			continue
		}
		who := ""
		switch {
		case c.Mine(m.Author):
			who = "Ty: "
		case len(ci.Members) > 2:
			who = m.Name + ": "
		}
		body := m.Text
		if m.Kind == "file" {
			body = "📎 " + m.File
			if m.Text != "" {
				body += " · " + m.Text
			}
		}
		return who + strings.ReplaceAll(body, "\n", " "), m.TS.Unix()
	}
	return "", 0
}

// ---- conversation ----

type Msg struct {
	ID        string            `json:"id"`
	Author    string            `json:"author"`
	Name      string            `json:"name"`
	Text      string            `json:"text"`
	TS        int64             `json:"ts"`
	Kind      string            `json:"kind"`
	Info      string            `json:"info,omitempty"`
	Mine      bool              `json:"mine"`
	Deleted   bool              `json:"deleted,omitempty"`
	Pinned    bool              `json:"pinned,omitempty"`
	Reply     string            `json:"reply,omitempty"`
	ReplyName string            `json:"replyName,omitempty"` // author and text of the answered message,
	ReplyText string            `json:"replyText,omitempty"` // even when it is outside the loaded page
	Reactions map[string]string `json:"reactions,omitempty"`
	ACK       string            `json:"ack,omitempty"`
	Avatar    string            `json:"avatar,omitempty"`

	Blob string `json:"blob,omitempty"`
	Mime string `json:"mime,omitempty"`
	File string `json:"file,omitempty"`
	Size int64  `json:"size,omitempty"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`
	Have bool   `json:"have,omitempty"`

	Prev     string `json:"prev,omitempty"`
	HavePrev bool   `json:"havePrev,omitempty"`
	Getting  bool   `json:"getting,omitempty"`
	Src      string `json:"src,omitempty"` // best image URL available right now
}

type Member struct {
	Pub    string `json:"pub"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
	Status string `json:"status"`
	Style  string `json:"style"`
	Online bool   `json:"online"`
	Owner  bool   `json:"owner"`
	Me     bool   `json:"me"`
	InCall bool   `json:"inCall"`
	Flags  string `json:"flags"` // in the call: m mic off, d deafened, c camera, s screen
	Relay  string `json:"relay"` // TURN relay the link goes through, "" = direct
}

type CallInfo struct {
	Joined  bool     `json:"joined"`
	Mic     bool     `json:"mic"`
	Deaf    bool     `json:"deaf"`
	Cam     bool     `json:"cam"`
	Screen  bool     `json:"screen"`
	Members []string `json:"members"`
}

type Conversation struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Avatar  string   `json:"avatar"`
	Direct  bool     `json:"direct"`
	Peer    string   `json:"peer"`
	Owner   string   `json:"owner"`
	IsOwner bool     `json:"isOwner"`
	Epoch   uint32   `json:"epoch"`
	Members []Member `json:"members"`
	Msgs    []Msg    `json:"msgs"`
	Pinned  []Msg    `json:"pinned"`
	Typing  []string `json:"typing"`
	Hidden  int      `json:"hidden"`
	Call    CallInfo `json:"call"`
	Online  int      `json:"online"`
	CallKey string   `json:"callKey"`
}

// OpenChannel picks the text channel of a group that Conversation shows and
// new messages go to.
func (a *App) OpenChannel(chat, ch string) {
	a.mu.Lock()
	a.selCh[chat] = ch
	a.mu.Unlock()
	if c, err := a.live(); err == nil {
		c.SetChannel(unhex(chat), unhex(ch))
	}
}

// AddChannel creates a channel or category of a group.
func (a *App) AddChannel(chat, kind, name, cat string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	_, err = c.AddChannel(unhex(chat), kind, name, unhex(cat))
	return err
}

// PutChannel renames, moves, archives or restores a channel or category of a
// group (id "" = the default of that kind), or with an empty name deletes it.
func (a *App) PutChannel(chat, id, kind, name, cat string, arch bool) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.PutChannel(unhex(chat), unhex(id), kind, name, unhex(cat), arch)
}

// Open selects a chat and clears its unread badge.
func (a *App) Open(chat string) {
	a.mu.Lock()
	a.sel = chat
	a.mu.Unlock()
	if c, err := a.live(); err == nil && chat != "" {
		id := unhex(chat)
		a.mu.Lock()
		a.seen[chat] = c.St.EventCount(id)
		a.mu.Unlock()
		c.MarkChatRead(id)
	}
}

// Conversation returns the open chat in full. Messages are capped at `limit`
// most recent items (0 = a sane default) — the frontend asks for more when the
// user scrolls to the top.
func (a *App) Conversation(chat string, limit int) (Conversation, error) {
	c, err := a.live()
	if err != nil {
		return Conversation{}, err
	}
	id := unhex(chat)
	if len(id) == 0 {
		return Conversation{}, errors.New("nieznana rozmowa")
	}
	var ci *core.ChatInfo
	for _, x := range c.Chats() {
		if hexs(x.ID) == chat {
			cp := x
			ci = &cp
			break
		}
	}
	if ci == nil {
		return Conversation{}, errors.New("nieznana rozmowa")
	}
	conv := Conversation{
		ID: chat, Name: ci.Name, Owner: hexs(ci.Owner), Epoch: ci.Epoch,
		IsOwner: equalBytes(ci.Owner, c.ID.Pub), Online: ci.Online,
		Members: []Member{}, Msgs: []Msg{}, Typing: c.TypingPeers(id),
		Hidden: c.HiddenCount(id),
	}
	if conv.Typing == nil {
		conv.Typing = []string{}
	}
	a.mu.Lock()
	selCh := a.selCh[chat]
	a.mu.Unlock()
	key := id // the call shown: the one you are in, else the default channel's
	if k := c.JoinedCall(); len(k) >= 32 && equalBytes(k[:32], id) {
		key = k
	}
	conv.CallKey = hexs(key)
	inCall := map[string]bool{}
	flags := map[string]string{}
	if call := c.CallState(key); call != nil {
		conv.Call = CallInfo{
			Joined: call.Joined, Mic: call.Mic, Deaf: call.Deaf,
			Cam: call.Cam, Screen: call.Screen, Members: []string{},
		}
		// devices show as their person, like in the member list
		for pub := range call.Members {
			who := hexs(c.Person(id, []byte(pub)))
			inCall[who] = true
			flags[who] = c.PeerFlags(key, []byte(pub))
			conv.Call.Members = append(conv.Call.Members, who)
		}
		if call.Joined {
			inCall[hexs(c.Account())] = true
			flags[hexs(c.Account())] = call.State()
		}
	}
	for _, m := range ci.Members {
		mine := c.Mine(m.Pub)
		status, _ := c.StatusOf(m.Pub)
		online := mine || c.Online(m.Pub)
		for _, d := range ci.Devices[string(m.Pub)] {
			online = online || c.Online(d)
		}
		conv.Members = append(conv.Members, Member{
			Pub: hexs(m.Pub), Name: m.Name, Avatar: avatarURL(c, m.Pub),
			Status: status, Style: c.StyleOf(m.Pub), Online: online,
			Owner: equalBytes(m.Pub, ci.Owner), Me: mine,
			InCall: inCall[hexs(m.Pub)], Flags: flags[hexs(m.Pub)], Relay: c.Relay(m.Pub),
		})
		if ci.Direct && !mine {
			conv.Direct, conv.Peer, conv.Avatar = true, hexs(m.Pub), avatarURL(c, m.Pub)
		}
	}
	sort.SliceStable(conv.Members, func(i, j int) bool {
		if conv.Members[i].Online != conv.Members[j].Online {
			return conv.Members[i].Online
		}
		return strings.ToLower(conv.Members[i].Name) < strings.ToLower(conv.Members[j].Name)
	})

	msgs := slices.DeleteFunc(c.Messages(id), func(m core.Msg) bool { return hexs(m.Ch) != selCh })
	if limit <= 0 {
		limit = 60
	}
	byID := map[string]core.Msg{}
	for _, m := range msgs {
		byID[string(m.ID)] = m
		if m.Pinned && !m.Deleted {
			conv.Pinned = append(conv.Pinned, toMsg(c, m))
		}
	}
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	for _, m := range msgs {
		out := toMsg(c, m)
		if r, ok := byID[string(m.Reply)]; ok {
			out.ReplyName, out.ReplyText = r.Name, r.Text
			if r.Kind == "file" && r.Text == "" {
				out.ReplyText = r.File
			}
		}
		conv.Msgs = append(conv.Msgs, out)
	}
	return conv, nil
}

func toMsg(c *core.Core, m core.Msg) Msg {
	out := Msg{
		ID: hexs(m.ID), Author: hexs(m.Who), Name: m.Name, Text: m.Text,
		TS: m.TS.Unix(), Kind: m.Kind, Info: m.Info, Deleted: m.Deleted, Pinned: m.Pinned, Reply: hexs(m.Reply),
		ACK: m.ACK, Mine: c.Mine(m.Author),
		Avatar: avatarURL(c, m.Who),
	}
	if m.Kind == "file" {
		out.Blob, out.Mime, out.File = hexs(m.Blob), m.Mime, m.File
		out.Size, out.W, out.H = m.Size, m.W, m.H
		out.Have, out.HavePrev, out.Getting = m.Have, m.HavePrev, m.Getting
		out.Prev = hexs(m.Prev)
		switch {
		case m.Have && strings.HasPrefix(m.Mime, "image/"):
			out.Src = "/blob/" + out.Blob
		case m.HavePrev:
			out.Src = "/blob/" + out.Prev
		}
	}
	if len(m.Reactions) > 0 {
		out.Reactions = map[string]string{}
		for pub, emoji := range m.Reactions {
			out.Reactions[hexs([]byte(pub))] = emoji
		}
	}
	return out
}

// ---- messaging ----

func (a *App) Send(chat, text string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Send(unhex(chat), text)
}

func (a *App) SendReply(chat, text, reply string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SendReply(unhex(chat), text, unhex(reply))
}

func (a *App) Pin(chat, msg string, unpin bool) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.PinMsg(unhex(chat), unhex(msg), unpin)
}

func (a *App) Typing(chat string, on bool) {
	if c, err := a.live(); err == nil {
		c.SendTyping(unhex(chat), on)
	}
}

func (a *App) React(chat, msg, emoji string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.ReactMsg(unhex(chat), unhex(msg), emoji)
}

func (a *App) DeleteMsg(chat, msg string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.DeleteMsg(unhex(chat), unhex(msg))
}

func (a *App) HideMsg(chat, msg string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.HideMsg(unhex(chat), unhex(msg))
}

func (a *App) UnhideAll(chat string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.UnhideAll(unhex(chat))
}

func (a *App) Search(chat, query string) ([]Msg, error) {
	c, err := a.live()
	if err != nil {
		return nil, err
	}
	hits, err := c.SearchChat(unhex(chat), query)
	if err != nil {
		return nil, err
	}
	out := []Msg{}
	for _, m := range hits {
		out = append(out, toMsg(c, m))
	}
	return out, nil
}

// ---- chats & members ----

// CreateGroup: history=false hides earlier messages from people added later.
func (a *App) CreateGroup(name string, history bool) (string, error) {
	c, err := a.live()
	if err != nil {
		return "", err
	}
	id, err := c.CreateChat(strings.TrimSpace(name), !history)
	return hexs(id), err
}

func (a *App) CreateDirect(pub string) (string, error) {
	c, err := a.live()
	if err != nil {
		return "", err
	}
	fs, _ := c.St.Friends()
	for _, f := range fs {
		if hexs(f.Pub) == pub {
			id, err := c.CreateDirect(f)
			return hexs(id), err
		}
	}
	return "", errors.New("nie znam takiego znajomego")
}

func (a *App) Rename(chat, name string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Rename(unhex(chat), strings.TrimSpace(name))
}

func (a *App) Kick(chat, pub string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Kick(unhex(chat), unhex(pub))
}

func (a *App) Leave(chat string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Leave(unhex(chat))
}

func (a *App) Rekey(chat string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Rekey(unhex(chat))
}

type InviteCode struct {
	Code string `json:"code"`
	Pin  string `json:"pin"`
	QR   string `json:"qr"`
}

func (a *App) NewInvite(chat string) (InviteCode, error) {
	c, err := a.live()
	if err != nil {
		return InviteCode{}, err
	}
	code, pin, err := c.NewInvite(unhex(chat))
	if err != nil {
		return InviteCode{}, err
	}
	return InviteCode{Code: code, Pin: pin, QR: a.QR(code)}, nil
}

func (a *App) Redeem(code, pin string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Redeem(strings.TrimSpace(code), strings.TrimSpace(pin))
}

func (a *App) AddFriend(code string) (string, error) {
	c, err := a.live()
	if err != nil {
		return "", err
	}
	f, err := c.AddFriend(strings.TrimSpace(code))
	if err != nil {
		return "", err
	}
	return f.Display(), nil
}

// Befriend adds a member of chat as a friend.
func (a *App) Befriend(chat, pub string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	st, err := c.ChatState(unhex(chat))
	if err != nil {
		return err
	}
	m := st.Members[string(unhex(pub))]
	if m == nil || m.Removed {
		return errors.New("tej osoby nie ma w czacie")
	}
	return c.Befriend(m.Pub, m.Name)
}

// KickFromCall disconnects a member from the call key (owner only).
func (a *App) KickFromCall(key, pub string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.KickFromCall(unhex(key), unhex(pub))
}

func (a *App) DelFriend(pub string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.St.DelFriend(unhex(pub))
}

// QR renders a code as a data URL so <img src> just works.
func (a *App) QR(text string) string {
	png, err := qrcode.Encode(text, qrcode.Medium, 512)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + b64(png)
}

// ---- profiles ----

type NickEntry struct {
	Name string `json:"name"`
	TS   int64  `json:"ts"`
}

type ProfileCard struct {
	Pub      string      `json:"pub"`
	Name     string      `json:"name"`
	Status   string      `json:"status"`
	Bio      string      `json:"bio"`
	Style    string      `json:"style"`
	Avatar   string      `json:"avatar"`
	Banner   string      `json:"banner"`
	Online   bool        `json:"online"`
	IsFriend bool        `json:"isFriend"`
	IsMe     bool        `json:"isMe"`
	Chat     string      `json:"chat"` // existing direct chat, if any
	History  []NickEntry `json:"history"`
	Version  string      `json:"version"` // what they said in "hello", "" = unknown
}

func (a *App) ProfileOf(pub string) (ProfileCard, error) {
	c, err := a.live()
	if err != nil {
		return ProfileCard{}, err
	}
	p := unhex(pub)
	status, bio := c.StatusOf(p)
	card := ProfileCard{
		Pub: pub, Status: status, Bio: bio, Style: c.StyleOf(p),
		Avatar: avatarURL(c, p), Banner: bannerURL(c, p),
		Online: c.Online(p), IsFriend: c.St.IsFriend(p),
		IsMe: c.Mine(p), History: []NickEntry{}, Version: c.PeerVersion(p),
	}
	if card.IsMe {
		card.Name, card.Online, card.Version = c.ID.Name, true, version.String()
	}
	for _, ci := range c.Chats() {
		for _, m := range ci.Members {
			if equalBytes(m.Pub, p) && card.Name == "" {
				card.Name = m.Name
			}
		}
		if ci.Direct {
			for _, m := range ci.Members {
				if equalBytes(m.Pub, p) {
					card.Chat = hexs(ci.ID)
				}
			}
		}
	}
	for _, n := range c.NickHistory(p) {
		card.History = append(card.History, NickEntry{Name: n.Name, TS: n.TS.Unix()})
	}
	return card, nil
}

func (a *App) SetNick(name string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SetNick(strings.TrimSpace(name))
}

func (a *App) SetStatus(text string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SetStatus(text)
}

func (a *App) SetStyle(s string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SetStyle(s)
}

func (a *App) SetBio(text string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SetBio(text)
}

// Crop: framing rectangle in source pixels, as produced by the crop overlay.
type Crop struct {
	X, Y, W, H int
}

func (cr Crop) rect() image.Rectangle {
	if cr.W <= 0 || cr.H <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(cr.X, cr.Y, cr.X+cr.W, cr.Y+cr.H)
}

// SetAvatar / SetBanner take a data: URL straight from the <input type=file>
// reader — the picture never touches disk on the way in.
func (a *App) SetAvatar(dataURL string, crop Crop) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	raw, err := decodeDataURL(dataURL, attach.MaxAvatarBytes)
	if err != nil {
		return err
	}
	return c.SetAvatar(raw, crop.rect())
}

func (a *App) ClearAvatar() error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.ClearAvatar()
}

func (a *App) SetBanner(dataURL string, crop Crop) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	raw, err := decodeDataURL(dataURL, attach.MaxAvatarBytes)
	if err != nil {
		return err
	}
	return c.SetBanner(raw, crop.rect())
}

func (a *App) ClearBanner() error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.ClearBanner()
}

// SetServerImage sets a group's icon or banner ("banner") from a data: URL;
// an empty URL clears it.
func (a *App) SetServerImage(chat, which, dataURL string, crop Crop) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	var raw []byte
	if dataURL != "" {
		if raw, err = decodeDataURL(dataURL, attach.MaxAvatarBytes); err != nil {
			return err
		}
	}
	return c.SetServerImage(unhex(chat), raw, crop.rect(), which == "banner")
}

// Notify shows a system notification unless they are turned off. One ID per
// chat, so a burst of pings replaces itself instead of piling up.
func (a *App) Notify(chat, title, body string) error {
	a.mu.Lock()
	bg := a.bg // Go posts its own pings then, see bgping.go
	a.mu.Unlock()
	if a.notify == nil || a.cfg.NoNotify || bg {
		return nil
	}
	return a.notify("szept-"+chat, title, body)
}

// SetHistory: owner only; on = people added from now on see earlier messages.
func (a *App) SetHistory(chat string, on bool) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SetHistory(unhex(chat), on)
}

func (a *App) SetServerStyle(chat, s string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.SetServerStyle(unhex(chat), s)
}

// ---- attachments ----

// filesDropped remembers the paths of a native file drop and tells the
// frontend, which pulls each one through ReadDropped into the composer.
func (a *App) filesDropped(paths []string) {
	a.mu.Lock()
	if a.dropped == nil {
		a.dropped = map[string]bool{}
	}
	for _, p := range paths {
		a.dropped[p] = true
	}
	a.mu.Unlock()
	a.app.Event.Emit("files-dropped", paths)
}

// ReadDropped returns one dropped file as a data URL. Only paths that came in
// through a real drop are served, once each, so page script cannot use this
// to read arbitrary files off the disk.
func (a *App) ReadDropped(path string) (string, error) {
	a.mu.Lock()
	ok := a.dropped[path]
	delete(a.dropped, path)
	a.mu.Unlock()
	if !ok {
		return "", errors.New("nieznany plik")
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("to nie jest plik")
	}
	if st.Size() > attach.MaxFileBytes {
		return "", fmt.Errorf("plik jest za duży (limit %s)", humanBytes(attach.MaxFileBytes))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "data:" + attach.SniffAny(raw) + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// SendData handles drag & drop and clipboard paste, where bytes arrive from JS.
func (a *App) SendData(chat, name, dataURL, caption string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	raw, err := decodeDataURL(dataURL, attach.MaxFileBytes)
	if err != nil {
		return err
	}
	return c.SendFile(unhex(chat), name, raw, caption)
}

func (a *App) SaveAs(hashHex, name string) (string, error) {
	c, err := a.live()
	if err != nil {
		return "", err
	}
	if mobile {
		// no directory picker on Android: stage the file, the frontend hands it to Downloads
		dir, err := outDir()
		if err != nil {
			return "", err
		}
		return c.SaveAttachment(unhex(hashHex), name, dir)
	}
	dir, err := a.app.Dialog.OpenFile().
		AttachToWindow(a.win).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		SetTitle("Zapisz w…").
		PromptForSingleSelection()
	if err != nil || dir == "" {
		return "", nil
	}
	return c.SaveAttachment(unhex(hashHex), name, dir)
}

func (a *App) Fetch(chat, hashHex, mime string, size int64, author string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	c.FetchBlob(unhex(chat), unhex(hashHex), mime, size, unhex(author))
	return nil
}

// ---- calls ----

func (a *App) JoinCall(chat string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.JoinCall(unhex(chat))
}

func (a *App) LeaveCall(chat string) {
	if c, err := a.live(); err == nil {
		c.LeaveCall(unhex(chat))
		a.dropFrames(chat)
	}
}

func (a *App) ToggleMic(chat string) {
	if c, err := a.live(); err == nil {
		c.ToggleMic(unhex(chat))
	}
}

func (a *App) ToggleDeafen(chat string) {
	if c, err := a.live(); err == nil {
		c.ToggleDeafen(unhex(chat))
	}
}

// ToggleCam / ToggleScreen: o is what was picked when starting (zero when
// stopping); it is remembered as the next start's preselection.
func (a *App) ToggleCam(chat string, o media.VideoOpts) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	if o.FPS > 0 {
		a.cfg.CamVideo, c.Cfg.CamVideo = o, o
		a.saveCfg()
	}
	return c.ToggleCam(unhex(chat), a.cfg.CamDevice)
}

func (a *App) ToggleScreen(chat string, display int, o media.VideoOpts) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	if o.FPS > 0 {
		a.cfg.ScreenVideo, c.Cfg.ScreenVideo = o, o
		a.saveCfg()
	}
	return c.ToggleScreen(unhex(chat), display)
}

// VideoPicks: the start dialog's preselection for camera and screen.
func (a *App) VideoPicks() map[string]media.VideoOpts {
	return map[string]media.VideoOpts{
		"cam": a.cfg.CamVideo.Or(core.DefCam), "screen": a.cfg.ScreenVideo.Or(core.DefScreen),
	}
}

// Displays: the monitors screen sharing can pick ("W×H"); empty when the
// system dialog picks instead (Wayland portal).
func (a *App) Displays() []string { return media.Displays() }

// SetFullscreen: a call tile shown over the whole screen.
func (a *App) SetFullscreen(on bool) {
	if on {
		a.win.Fullscreen()
	} else {
		a.win.UnFullscreen()
	}
}

func (a *App) SetOutputVolume(v float64) {
	if c, err := a.live(); err == nil {
		c.SetOutputVolume(v)
		a.cfg.OutputVol = v
		a.saveCfg()
	}
}

// Tones plays a UI cue ([Hz, start s] notes) on the chosen speaker.
func (a *App) Tones(notes [][2]float64) error {
	return media.Tones(a.cfg.SpeakerDevice, notes)
}

// SetMicGain: microphone volume 0..2, live in a call and in the mic test.
func (a *App) SetMicGain(v float64) {
	media.SetMicGain(float32(v))
	if c, err := a.live(); err == nil {
		c.Cfg.MicGain = v
		a.cfg.MicGain = v
		a.saveCfg()
	}
}

// MicTest shows your microphone level in the settings; see core.MicTest.
func (a *App) MicTest(on bool) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.MicTest(on)
}

// CamTest shows the camera in the settings: frames go to the "camtest" tile
// (/frame/camtest, event "camtest"), nothing is sent.
func (a *App) CamTest(on bool) error {
	a.mu.Lock()
	stop := a.camStop
	a.camStop = nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
	if !on {
		return nil
	}
	stop, err := media.StartCamera(a.cfg.CamDevice, a.cfg.CamVideo.Or(core.DefCam), func(jpg []byte) {
		a.mu.Lock()
		f := a.frames["camtest"]
		if f == nil {
			f = &frame{}
			a.frames["camtest"] = f
		}
		a.frameNo++
		seq := a.frameNo
		a.mu.Unlock()
		f.mu.Lock()
		f.jpeg, f.seq, f.last = jpg, seq, time.Now()
		f.mu.Unlock()
		a.app.Event.Emit("camtest", seq)
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.camStop = func() {
		stop()
		a.mu.Lock()
		delete(a.frames, "camtest")
		a.mu.Unlock()
	}
	a.mu.Unlock()
	return nil
}

func (a *App) MutePeer(pub string, mute bool) {
	if c, err := a.live(); err == nil {
		c.MuteVoicePeer(unhex(pub), mute)
	}
}

// SetVoice places a participant in the stereo field (spatial audio panel).
func (a *App) SetVoice(pub string, gain, pan float64) {
	if c, err := a.live(); err == nil {
		c.SetVoiceLevel(unhex(pub), float32(gain), float32(pan))
	}
}

// VoiceLevels: pubkey (hex) -> 0..1 for everyone audible in your call right now.
func (a *App) VoiceLevels() map[string]float32 {
	out := map[string]float32{}
	if c, err := a.live(); err == nil {
		for pub, v := range c.VoiceLevels() {
			out[hexs([]byte(pub))] = v
		}
	}
	return out
}

// ---- settings ----

type Settings struct {
	Connector      string          `json:"connector"`
	Connectors     []string        `json:"connectors"`
	ConnectorLists []string        `json:"connectorLists"`
	MediaDir       string          `json:"mediaDir"`
	Accent         string          `json:"accent"`
	OutputVol      float64         `json:"outputVol"`
	CamDevice      string          `json:"camDevice"`
	NoNotify       bool            `json:"noNotify"`
	NoUpdate       bool            `json:"noUpdate"`
	RelayOnly      bool            `json:"relayOnly"`
	QuietLookup    bool            `json:"quietLookup"`
	Lang           string          `json:"lang"`
	Profile        string          `json:"profile"`
	Profiles       []string        `json:"profiles"`
	Pinned         bool            `json:"pinned"`
	WebhookOn      bool            `json:"webhookOn"`
	WebhookPort    int             `json:"webhookPort"`
	WebhookToken   string          `json:"webhookToken"`
	DataDir        string          `json:"dataDir"`
	DevMode        bool            `json:"devMode"`
	MicDevice      string          `json:"micDevice"`
	SpeakerDevice  string          `json:"speakerDevice"`
	MicGain        float64         `json:"micGain"`
	Voice          media.VoiceOpts `json:"voice"`
	Devices        []DeviceInfo    `json:"devices"`
	Linked         bool            `json:"linked"` // this device joined another device's account
}

// AudioDevices: microphones and speakers for the voice settings.
func (a *App) AudioDevices() map[string][]media.Device {
	return map[string][]media.Device{"mics": media.Devices(true), "speakers": media.Devices(false)}
}

func (a *App) Settings() Settings {
	s := Settings{
		Connector: a.cfg.Connector, Connectors: a.cfg.Connectors,
		ConnectorLists: a.cfg.ConnectorLists, MediaDir: a.cfg.MediaDir,
		Accent: a.cfg.Accent, OutputVol: a.cfg.OutputVol, CamDevice: a.cfg.CamDevice,
		NoNotify: a.cfg.NoNotify, NoUpdate: a.cfg.NoUpdate, RelayOnly: a.cfg.RelayOnly, QuietLookup: a.cfg.QuietLookup, Profile: a.cfg.Profile, Pinned: a.pinned,
		WebhookOn: a.cfg.WebhookOn, WebhookPort: a.cfg.WebhookPort,
		DataDir: a.cfg.DataDir, DevMode: a.cfg.DevMode,
		MicDevice: a.cfg.MicDevice, SpeakerDevice: a.cfg.SpeakerDevice, MicGain: a.cfg.MicGain,
		Voice:    a.cfg.Voice,
		Profiles: []string{},
	}
	if s.Connectors == nil {
		s.Connectors = []string{}
	}
	if s.ConnectorLists == nil {
		s.ConnectorLists = []string{}
	}
	if !a.pinned {
		s.Profiles = boot.Profiles()
	}
	if c, err := a.live(); err == nil {
		s.Lang = c.Lang()
		s.WebhookToken = c.WebhookToken()
		s.Devices, s.Linked = devices(c), c.Linked()
	}
	return s
}

// SaveSettings writes the parts of the config the settings screen owns and
// applies the ones core needs to hear about immediately.
func (a *App) SaveSettings(s Settings) error {
	a.cfg.Connectors, a.cfg.ConnectorLists = s.Connectors, s.ConnectorLists
	a.cfg.MediaDir, a.cfg.Accent = s.MediaDir, s.Accent
	a.cfg.CamDevice, a.cfg.NoNotify = s.CamDevice, s.NoNotify
	a.cfg.NoUpdate = s.NoUpdate
	a.cfg.OutputVol = s.OutputVol
	a.cfg.RelayOnly = s.RelayOnly
	a.cfg.QuietLookup = s.QuietLookup
	a.cfg.DevMode = s.DevMode
	a.cfg.MicDevice, a.cfg.SpeakerDevice = s.MicDevice, s.SpeakerDevice
	a.cfg.Voice = s.Voice
	media.SetVoiceOpts(s.Voice) // live: a running call or mic test hears it now

	c, _ := a.live()
	if c != nil {
		c.Cfg.MicDevice, c.Cfg.SpeakerDevice = s.MicDevice, s.SpeakerDevice // next call
		c.Cfg.Voice = s.Voice
		c.SetRelayOnly(s.RelayOnly)
		c.SetQuietLookup(s.QuietLookup)
		if s.Connector != "" && core.NormalizeWS(s.Connector) != a.cfg.Connector {
			a.cfg.Connector = core.NormalizeWS(s.Connector)
			c.SetConnector(a.cfg.Connector)
		}
		if s.Lang != "" && s.Lang != c.Lang() {
			if err := c.SetLang(s.Lang); err != nil {
				return err
			}
		}
		c.SetOutputVolume(s.OutputVol)
		if err := a.applyWebhook(c, s); err != nil {
			a.saveCfg()
			return err
		}
	} else if s.Connector != "" {
		a.cfg.Connector = core.NormalizeWS(s.Connector)
	}
	a.saveCfg()
	a.app.Event.Emit("sync")
	return nil
}

func (a *App) applyWebhook(c *core.Core, s Settings) error {
	a.cfg.WebhookOn, a.cfg.WebhookPort = s.WebhookOn, s.WebhookPort
	if s.WebhookToken != "" {
		if err := c.SetWebhookToken(s.WebhookToken); err != nil {
			return err
		}
	}
	if !s.WebhookOn {
		c.StopWebhook()
		return nil
	}
	if s.WebhookPort <= 0 || s.WebhookToken == "" {
		return errors.New("webhook: podaj port i token")
	}
	c.StopWebhook()
	return c.StartWebhook(s.WebhookPort, s.WebhookToken)
}

// NewWebhookToken hands the settings screen a fresh random token.
func (a *App) NewWebhookToken() string { return core.NewWebhookToken() }

// RefreshConnectors pulls every configured list URL and merges new addresses in.
func (a *App) RefreshConnectors() (Settings, error) {
	found, errs := boot.FetchConnectorLists(a.cfg.ConnectorLists)
	for _, u := range found {
		a.cfg.Connectors = boot.AppendUnique(a.cfg.Connectors, u)
	}
	a.saveCfg()
	s := a.Settings()
	if errs > 0 && len(found) == 0 {
		return s, fmt.Errorf("nie udało się pobrać %d list", errs)
	}
	return s, nil
}

func (a *App) ChangePass(oldPass, newPass string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return boot.ChangePass(a.profileDir(), []byte(oldPass), []byte(newPass), c.St.Rekey)
}

// Backup writes the zip wherever the user points the native save dialog.
func (a *App) Backup() (string, error) {
	c, err := a.live()
	if err != nil {
		return "", err
	}
	file := "szept-" + time.Now().Format("2006-01-02") + ".zip"
	var path string
	if mobile {
		dir, err := outDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, file)
	} else if path, err = a.app.Dialog.SaveFile().
		AttachToWindow(a.win).
		SetFilename(file).
		SetMessage("Zapisz kopię").
		PromptForSingleSelection(); err != nil || path == "" {
		return "", nil
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := boot.Backup(a.cfg.DataDir, c.St.Snapshot, f); err != nil {
		return "", err
	}
	return path, nil
}

// Restore never overwrites live data — the copy lands as a new account.
func (a *App) Restore() (string, error) {
	if a.pinned {
		return "", errors.New("przywracanie wymaga trybu z kontami (bez -data)")
	}
	path, err := a.app.Dialog.OpenFile().
		AttachToWindow(a.win).
		AddFilter("Kopia szeptu", "*.zip").
		SetTitle("Wybierz kopię").
		PromptForSingleSelection()
	if err != nil || path == "" {
		return "", nil
	}
	name := "kopia-" + time.Now().Format("0102-1504")
	if err := boot.Restore(path, boot.ProfileDir(name)); err != nil {
		return "", err
	}
	return name, nil
}

// ---- window chrome (frameless title bar) ----

func (a *App) Minimise()  { a.win.Minimise() }
func (a *App) ToggleMax() { a.win.ToggleMaximise() }
func (a *App) CloseApp()  { a.app.Quit() }

// Version reports the build stamp shown in the About panel.
func (a *App) Version() string { return version.String() }

// ---- linked devices (docs/multi-device.md) ----

type DeviceInfo struct {
	Pub    string `json:"pub"`
	Name   string `json:"name"`
	TS     int64  `json:"ts"` // linked at, 0 for the account's first device
	Online bool   `json:"online"`
	Me     bool   `json:"me"` // the device this app runs on
}

// devices: the account's first device, then the linked ones.
func devices(c *core.Core) []DeviceInfo {
	acct := c.Account()
	out := []DeviceInfo{{Pub: hexs(acct), Name: "Pierwsze urządzenie", Online: true, Me: equalBytes(acct, c.ID.Pub)}}
	if !out[0].Me {
		out[0].Online = c.Online(acct)
	}
	for _, d := range c.Devices() {
		me := equalBytes(d.Pub, c.ID.Pub)
		out = append(out, DeviceInfo{Pub: hexs(d.Pub), Name: d.Name, TS: d.TS, Online: me || c.Online(d.Pub), Me: me})
	}
	return out
}

type LinkCode struct {
	Code string `json:"code"`
	QR   string `json:"qr"` // data URL
}

// NewLinkCode: a one-time code, also as a QR code, for linking a new device.
func (a *App) NewLinkCode() (LinkCode, error) {
	c, err := a.live()
	if err != nil {
		return LinkCode{}, err
	}
	code, err := c.NewLinkCode()
	if err != nil {
		return LinkCode{}, err
	}
	return LinkCode{Code: code, QR: a.QR(code)}, nil
}

func (a *App) Unlink(pub string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.Unlink(unhex(pub))
}

// LinkSetup: first run on a new device that joins an existing account. It
// gets its own key (name and friends come from the account) and asks the
// account device, which must be online, to link it. Boot reports "link"
// until the answer arrives.
func (a *App) LinkSetup(code, pass string) error {
	_, _, conn, err := core.ParseLinkCode(code)
	if err != nil {
		return err
	}
	if conn == "" {
		return errors.New("kod nie podaje connectora")
	}
	a.cfg.Connector = conn
	a.cfg.DataDir = a.profileDir()
	if boot.HasIdentity(a.cfg.DataDir) {
		return errors.New("to konto ma już klucz: połącz nowe konto albo nowe urządzenie")
	}
	id, err := boot.CreateIdentity(a.cfg.DataDir, "nowe urządzenie", []byte(pass))
	if err != nil {
		return err
	}
	a.saveCfg()
	if err := a.enter(id); err != nil {
		return err
	}
	return a.RetryLink(code)
}

// RetryLink sends the link request again, with a new code if the old expired.
func (a *App) RetryLink(code string) error {
	c, err := a.live()
	if err != nil {
		return err
	}
	return c.JoinAccount(code, deviceName())
}

// deviceName labels this device in the account's list.
func deviceName() string {
	if mobile {
		return "Telefon"
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "Komputer"
}
