package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"strings"
	"time"

	"szept/internal/event"
	"szept/internal/p2p"
	"szept/internal/store"
)

// Linked devices (docs/multi-device.md). The device that created the identity
// is the account. Another device gets its own key and a link proof signed by
// both keys (event.LinkProof). Every device is an ordinary member of every
// chat: each device adds its siblings to the chats it is in (ensureSiblings),
// so membership, keys, sync and calls need nothing new. Clients group members
// by account for display.
//
// Only the account links and unlinks. Unlinking asks each chat's owner to
// remove the device, which also rotates the key (enforceUnlinks).

// Device: one linked device of this account.
type Device struct {
	Pub   []byte `json:"pub"`
	Proof []byte `json:"proof"`
	Name  string `json:"name,omitempty"` // what the device called itself when linked
	TS    int64  `json:"ts,omitempty"`   // when it was linked
}

const (
	linkPrefix  = "SZL-"
	linkTTL     = 10 * time.Minute
	proofKey    = "link:proof"    // linked device: its own proof (names the account)
	devicesKey  = "link:devices"  // the account's linked devices, JSON []Device
	unlinkedKey = "link:unlinked" // account: devices cut off, JSON []Device, until no chat holds them
	pendingKey  = "link:pending"  // new device: the code and name of a JoinAccount not answered yet
)

type linkOffer struct {
	hash []byte // sha256 of the one-time secret in the code
	exp  time.Time
}

type linkRequest struct {
	Account, Secret []byte
	Name            string
}

func (c *Core) loadDevices() {
	acct := c.ID.Pub
	if p := c.St.Meta(proofKey); p != nil {
		if a := event.VerifyLink(p, c.ID.Pub); a != nil {
			acct = a
		}
	}
	var devs []Device
	_ = json.Unmarshal(c.St.Meta(devicesKey), &devs)
	// a link still waiting for the account survives a restart
	var req *linkRequest
	if code, name, ok := strings.Cut(string(c.St.Meta(pendingKey)), "\n"); ok && bytes.Equal(acct, c.ID.Pub) {
		if a, s, _, err := ParseLinkCode(code); err == nil {
			req = &linkRequest{Account: a, Secret: s, Name: name}
		}
	}
	c.devMu.Lock()
	c.acct, c.devs, c.linkReq = acct, devs, req
	c.devMu.Unlock()
}

// Account: the person this device belongs to (its own key unless linked).
func (c *Core) Account() []byte {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	return c.acct
}

// Linked reports whether this device was linked to another device's account.
func (c *Core) Linked() bool { return !bytes.Equal(c.Account(), c.ID.Pub) }

// Devices: the account's linked devices as this device knows them.
func (c *Core) Devices() []Device {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	return slices.Clone(c.devs)
}

// Mine reports whether pub is this device, its account or a sibling device.
func (c *Core) Mine(pub []byte) bool {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	if bytes.Equal(pub, c.ID.Pub) || bytes.Equal(pub, c.acct) {
		return true
	}
	return slices.ContainsFunc(c.devs, func(d Device) bool { return bytes.Equal(d.Pub, pub) })
}

// sibling: one of my other devices, with the proof needed to add it to a chat
// (nil for the account itself, which needs none).
type sibling struct{ pub, proof []byte }

func (c *Core) siblings() []sibling {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	var out []sibling
	if !bytes.Equal(c.acct, c.ID.Pub) {
		out = append(out, sibling{pub: c.acct})
	}
	for _, d := range c.devs {
		if !bytes.Equal(d.Pub, c.ID.Pub) {
			out = append(out, sibling{d.Pub, d.Proof})
		}
	}
	return out
}

// person: whom pub stands for in this chat. A linked device shows as its
// account, and so does every device of mine.
func (c *Core) person(st *event.State, pub []byte) []byte {
	if c.Mine(pub) {
		return c.Account()
	}
	if st != nil {
		if m := st.Members[string(pub)]; m != nil && m.Of != nil {
			return m.Of
		}
	}
	return pub
}

// Person: whom pub stands for in chat (see person).
func (c *Core) Person(chat, pub []byte) []byte {
	st, _ := c.ChatState(chat)
	return c.person(st, pub)
}

func (c *Core) saveDevices(devs []Device) error {
	b, _ := json.Marshal(devs)
	if err := c.St.PutMeta(devicesKey, b); err != nil {
		return err
	}
	c.devMu.Lock()
	c.devs = devs
	c.devMu.Unlock()
	return nil
}

// ---- account side ----

// NewLinkCode issues a one-time code (shown as a QR code) that links a new
// device to this account. Valid for 10 minutes; a new code replaces the old.
func (c *Core) NewLinkCode() (string, error) {
	if c.Linked() {
		return "", errors.New("nowe urządzenia łączy się z pierwszego urządzenia konta")
	}
	secret := makeSecret()
	h := sha256.Sum256(secret)
	c.devMu.Lock()
	c.linkOffer = &linkOffer{hash: h[:], exp: time.Now().Add(linkTTL)}
	c.devMu.Unlock()
	blob, _ := json.Marshal(map[string]any{"a": []byte(c.ID.Pub), "s": secret, "u": c.connURL()})
	return linkPrefix + base64.RawURLEncoding.EncodeToString(blob), nil
}

// linkTicket: whether hash opens the current link offer (see acceptPeer).
func (c *Core) linkTicket(hash []byte) bool {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	o := c.linkOffer
	return o != nil && time.Now().Before(o.exp) && subtle.ConstantTimeCompare(o.hash, hash) == 1
}

// handleLink: a new device holds our code and signed the link statement.
func (c *Core) handleLink(p *p2p.Peer, m Ctrl) {
	h := sha256.Sum256(m.Secret)
	if !c.linkTicket(h[:]) || c.Mine(p.Pub) {
		return
	}
	stmt := event.LinkStatement(c.ID.Pub, p.Pub)
	if !ed25519.Verify(p.Pub, stmt, m.Link) {
		return
	}
	c.devMu.Lock()
	c.linkOffer = nil // one device per code
	c.devMu.Unlock()
	proof := event.LinkProof(c.ID.Pub, ed25519.Sign(c.ID.Priv, stmt), m.Link)
	devs := append(c.Devices(), Device{Pub: p.Pub, Proof: proof, Name: safeName(m.Name), TS: time.Now().Unix()})
	if err := c.saveDevices(devs); err != nil {
		log.Printf("core: link: %v", err)
		return
	}
	fs, _ := c.St.Friends()
	p.SendCtrl(Ctrl{T: "linked", Link: proof, Devs: devs, Name: c.ID.Name, Friends: fs})
	c.pushDevices()
	go c.ensureSiblings()
	c.Notify()
}

// Unlink cuts a device off: it stops being one of mine, and every chat's
// owner is asked to remove it (enforceUnlinks).
func (c *Core) Unlink(pub []byte) error {
	if c.Linked() {
		return errors.New("urządzenia odłącza się z pierwszego urządzenia konta")
	}
	devs := c.Devices()
	i := slices.IndexFunc(devs, func(d Device) bool { return bytes.Equal(d.Pub, pub) })
	if i < 0 {
		return errors.New("nie ma takiego urządzenia")
	}
	if err := c.saveDevices(slices.Delete(slices.Clone(devs), i, i+1)); err != nil {
		return err
	}
	cut := append(c.unlinked(), devs[i])
	b, _ := json.Marshal(cut)
	if err := c.St.PutMeta(unlinkedKey, b); err != nil {
		return err
	}
	if p := c.P2P.Peer(pub); p != nil {
		p.Close()
	}
	c.pushDevices()
	go c.enforceUnlinks()
	c.Notify()
	return nil
}

func (c *Core) unlinked() []Device {
	var cut []Device
	_ = json.Unmarshal(c.St.Meta(unlinkedKey), &cut)
	return cut
}

// enforceUnlinks removes cut-off devices from every chat that still holds
// them: directly where we own the chat, otherwise by asking the owner when
// they are online. Runs again every round until no chat holds them.
func (c *Core) enforceUnlinks() {
	cut := c.unlinked()
	if len(cut) == 0 {
		return
	}
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		st, err := c.ChatState(chat)
		if err != nil || !st.IsMember(c.ID.Pub) {
			continue
		}
		for _, d := range cut {
			if !st.IsMember(d.Pub) {
				continue
			}
			if bytes.Equal(st.Owner, c.ID.Pub) {
				if err := c.removeDevice(chat, d.Pub, c.ID.Pub); err != nil {
					log.Printf("core: unlink: %v", err)
				}
			} else if p := c.P2P.Peer(st.Owner); p != nil {
				p.SendCtrl(Ctrl{T: "unlink", Chat: chat, Pub: d.Pub, Link: d.Proof})
			}
		}
	}
}

// ---- chat owner side ----

// handleUnlink: an account asks us, the owner, to remove one of its devices.
func (c *Core) handleUnlink(p *p2p.Peer, m Ctrl) {
	st, err := c.ChatState(m.Chat)
	if err != nil || !bytes.Equal(st.Owner, c.ID.Pub) || !st.IsMember(p.Pub) || !st.IsMember(m.Pub) {
		return
	}
	// only a device's own account may have it removed; the proof shows it
	// also for a device that joined through an invite and never said so
	if d := st.Members[string(m.Pub)]; !bytes.Equal(d.Of, p.Pub) && !bytes.Equal(event.VerifyLink(m.Link, m.Pub), p.Pub) {
		return
	}
	if err := c.removeDevice(m.Chat, m.Pub, p.Pub); err != nil {
		log.Printf("core: unlink from %x: %v", p.Pub[:4], err)
	}
}

// removeDevice kicks dev and rotates the key. A kick also removes everyone
// dev added, which includes its sibling devices (ensureSiblings), so those of
// acct are let back in.
func (c *Core) removeDevice(chat, dev, acct []byte) error {
	before, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	if err := c.Kick(chat, dev); err != nil {
		return err
	}
	after, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	for k, m := range after.Members {
		was := before.Members[k]
		if !m.Removed || was == nil || was.Removed || bytes.Equal(m.Pub, dev) {
			continue
		}
		if !bytes.Equal(m.Pub, acct) && !bytes.Equal(m.Of, acct) {
			continue
		}
		if _, err := c.appendEvent(chat, event.KindAdd, event.Body{Pub: m.Pub, Name: m.Name, Link: m.Link, TS: time.Now().Unix()}); err != nil {
			return err
		}
		c.pushKeys(chat, m.Pub)
	}
	return nil
}

// ---- new device side ----

// ParseLinkCode reads a code from NewLinkCode.
func ParseLinkCode(code string) (account, secret []byte, connector string, err error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, linkPrefix) {
		return nil, nil, "", errors.New("to nie jest kod łączenia urządzenia")
	}
	raw, err := base64.RawURLEncoding.DecodeString(code[len(linkPrefix):])
	if err != nil {
		return nil, nil, "", errors.New("uszkodzony kod")
	}
	var v struct {
		A []byte `json:"a"`
		S []byte `json:"s"`
		U string `json:"u"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.A) != ed25519.PublicKeySize || len(v.S) == 0 {
		return nil, nil, "", errors.New("uszkodzony kod")
	}
	return v.A, v.S, wsOrEmpty(v.U), nil
}

// JoinAccount links this fresh device to the account in code. The account
// device must be online; the link completes when its answer arrives
// (handleLinked). name labels this device in the account's device list.
func (c *Core) JoinAccount(code, name string) error {
	acct, secret, _, err := ParseLinkCode(code)
	if err != nil {
		return err
	}
	if c.Linked() || len(c.Devices()) > 0 {
		return errors.New("to urządzenie jest już połączone z kontem")
	}
	if len(c.chatIDs()) > 0 {
		return errors.New("łączyć można tylko nowe urządzenie, bez rozmów")
	}
	if bytes.Equal(acct, c.ID.Pub) {
		return errors.New("to kod tego urządzenia")
	}
	if err := c.St.PutMeta(pendingKey, []byte(strings.TrimSpace(code)+"\n"+name)); err != nil {
		return err
	}
	c.devMu.Lock()
	c.linkReq = &linkRequest{Account: acct, Secret: secret, Name: name}
	c.devMu.Unlock()
	if p := c.P2P.Peer(acct); p != nil {
		c.sendLink(p)
	} else {
		go c.ensurePeer(acct)
	}
	return nil
}

// Linking reports whether a JoinAccount is waiting for the account's answer.
func (c *Core) Linking() bool {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	return c.linkReq != nil
}

func (c *Core) pendingLink(pub []byte) *linkRequest {
	c.devMu.Lock()
	defer c.devMu.Unlock()
	if r := c.linkReq; r != nil && bytes.Equal(r.Account, pub) {
		return r
	}
	return nil
}

func (c *Core) sendLink(p *p2p.Peer) {
	if r := c.pendingLink(p.Pub); r != nil {
		sig := ed25519.Sign(c.ID.Priv, event.LinkStatement(r.Account, c.ID.Pub))
		p.SendCtrl(Ctrl{T: "link", Secret: r.Secret, Link: sig, Name: r.Name})
	}
}

// handleLinked: the account accepted us. From now on its chats come in as it
// adds this device to them.
func (c *Core) handleLinked(p *p2p.Peer, m Ctrl) {
	r := c.pendingLink(p.Pub)
	if r == nil || !bytes.Equal(event.VerifyLink(m.Link, c.ID.Pub), p.Pub) {
		return
	}
	if err := c.St.PutMeta(proofKey, m.Link); err != nil {
		log.Printf("core: linked: %v", err)
		return
	}
	c.St.PutMeta(pendingKey, nil)
	c.devMu.Lock()
	c.acct, c.linkReq = bytes.Clone(p.Pub), nil
	c.devMu.Unlock()
	c.takeDevices(p.Pub, m.Devs)
	for _, f := range m.Friends {
		if len(f.Pub) == ed25519.PublicKeySize && !c.Mine(f.Pub) && !c.St.IsFriend(f.Pub) {
			c.St.PutFriend(store.Friend{Pub: f.Pub, Name: safeName(f.Name), PeerName: safeName(f.PeerName), Connector: wsOrEmpty(f.Connector)})
		}
	}
	if m.Name != "" {
		c.SetNick(safeName(m.Name))
	}
	go c.connectAll()
	c.Notify()
}

// takeDevices keeps the device list the account sent, minus anything that
// does not prove to belong to it.
func (c *Core) takeDevices(acct []byte, in []Device) {
	var devs []Device
	for _, d := range in {
		if bytes.Equal(event.VerifyLink(d.Proof, d.Pub), acct) {
			devs = append(devs, Device{Pub: d.Pub, Proof: d.Proof, Name: safeName(d.Name), TS: d.TS})
		}
	}
	if err := c.saveDevices(devs); err != nil {
		log.Printf("core: devices: %v", err)
	}
}

// ---- both sides ----

// pushDevices tells my online devices the current name and, from the
// account, the device list.
func (c *Core) pushDevices() {
	for _, s := range c.siblings() {
		if p := c.P2P.Peer(s.pub); p != nil {
			c.sendDevices(p)
		}
	}
}

func (c *Core) sendDevices(p *p2p.Peer) {
	m := Ctrl{T: "devices", Name: c.ID.Name}
	if !c.Linked() {
		m.Devs = c.Devices()
		if m.Devs == nil {
			m.Devs = []Device{}
		}
	}
	p.SendCtrl(m)
}

// handleDevices: a sibling's name, and from the account the device list.
func (c *Core) handleDevices(p *p2p.Peer, m Ctrl) {
	if !c.Mine(p.Pub) {
		return
	}
	if m.Devs != nil && bytes.Equal(p.Pub, c.Account()) && c.Linked() {
		c.takeDevices(p.Pub, m.Devs)
	}
	if n := safeName(m.Name); n != "" && n != c.ID.Name {
		c.SetNick(n)
	}
	c.Notify()
}

// ensureSiblings adds my other devices to every chat I am in that does not
// list them yet. A device that was removed stays out: re-admitting is the
// owner's call (event fold, KindAdd).
func (c *Core) ensureSiblings() {
	sibs := c.siblings()
	if len(sibs) == 0 {
		return
	}
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		st, err := c.ChatState(chat)
		if err != nil || !st.IsMember(c.ID.Pub) {
			continue
		}
		for _, s := range sibs {
			if _, listed := st.Members[string(s.pub)]; listed {
				continue
			}
			if _, err := c.appendEvent(chat, event.KindAdd, event.Body{Pub: s.pub, Name: c.ID.Name, Link: s.proof, TS: time.Now().Unix()}); err != nil {
				log.Printf("core: add sibling: %v", err)
				break
			}
			c.pushKeys(chat, s.pub)
		}
	}
}
