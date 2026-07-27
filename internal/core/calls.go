package core

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"log"
	"strings"
	"time"

	"szept/internal/attach"
	"szept/internal/media"
	"szept/internal/p2p"
)

// Call state per chat. Media = full mesh: every participant sends frames to
// every other announced participant over unreliable data channels.
// Mesh is fine to ~8 people; an SFU is the upgrade path.
type Call struct {
	Chat    []byte
	Joined  bool
	Mic     bool
	Cam     bool
	Screen  bool
	Members map[string]bool // remote pubs who announced "join"
}

func chatLabel(kind string, chat []byte) string {
	return kind + ":" + base64.RawURLEncoding.EncodeToString(chat)
}

func parseLabel(label string) (kind string, chat []byte, ok bool) {
	i := strings.IndexByte(label, ':')
	if i < 0 {
		return "", nil, false
	}
	chat, err := base64.RawURLEncoding.DecodeString(label[i+1:])
	if err != nil || len(chat) != 32 {
		return "", nil, false
	}
	return label[:i], chat, true
}

func (c *Core) CallState(chat []byte) *Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[string(chat)]
}

func (c *Core) handleCallCtrl(p *p2p.Peer, m Ctrl) {
	st, err := c.ChatState(m.Chat)
	if err != nil || !st.IsMember(p.Pub) {
		return
	}
	c.mu.Lock()
	call := c.calls[string(m.Chat)]
	if call == nil {
		call = &Call{Chat: m.Chat, Members: map[string]bool{}}
		c.calls[string(m.Chat)] = call
	}
	switch m.Action {
	case "join":
		call.Members[string(p.Pub)] = true
	case "leave":
		delete(call.Members, string(p.Pub))
		if !call.Joined && len(call.Members) == 0 {
			delete(c.calls, string(m.Chat))
		}
	}
	c.mu.Unlock()
	c.Notify()
}

// broadcast a call control action to online chat members.
func (c *Core) callBroadcast(chat []byte, action string) {
	st, err := c.ChatState(chat)
	if err != nil {
		return
	}
	for _, m := range st.Members {
		if m.Removed || bytes.Equal(m.Pub, c.ID.Pub) {
			continue
		}
		if p := c.P2P.Peer(m.Pub); p != nil {
			p.SendCtrl(Ctrl{T: "call", Chat: chat, Action: action})
		}
	}
}

// mediaTargets: online peers announced in this call.
func (c *Core) mediaTargets(chat []byte) []*p2p.Peer {
	c.mu.Lock()
	call := c.calls[string(chat)]
	var pubs []string
	if call != nil {
		for pub := range call.Members {
			pubs = append(pubs, pub)
		}
	}
	c.mu.Unlock()
	var out []*p2p.Peer
	for _, pub := range pubs {
		if p := c.P2P.Peer([]byte(pub)); p != nil {
			out = append(out, p)
		}
	}
	return out
}

// JoinCall enters (or starts) the chat's voice call: announces, opens the
// speaker mixer and starts the microphone.
func (c *Core) JoinCall(chat []byte) error {
	c.mu.Lock()
	call := c.calls[string(chat)]
	if call == nil {
		call = &Call{Chat: chat, Members: map[string]bool{}}
		c.calls[string(chat)] = call
	}
	if call.Joined {
		c.mu.Unlock()
		return nil
	}
	call.Joined = true
	call.Mic = true
	c.mu.Unlock()

	player, err := media.StartPlayer()
	if err != nil {
		log.Printf("call: player: %v", err)
	} else {
		c.mu.Lock()
		c.voiceOut = player.Push
		c.mu.Unlock()
	}
	var seq uint32
	stopMic, err := media.StartVoice(func(pcm []byte) {
		c.mu.Lock()
		muted := !call.Mic
		c.mu.Unlock()
		if muted {
			return
		}
		seq++
		frame := make([]byte, 4+len(pcm))
		binary.BigEndian.PutUint32(frame, seq)
		copy(frame[4:], pcm)
		lbl := chatLabel("v", chat)
		for _, p := range c.mediaTargets(chat) {
			p.SendMedia(lbl, frame)
		}
	})
	if err != nil {
		log.Printf("call: mic: %v", err)
	}
	c.mu.Lock()
	prevPlayerClose := func() {}
	if player != nil {
		prevPlayerClose = player.Close
	}
	stop := func() {}
	if stopMic != nil {
		stop = stopMic
	}
	c.stopVoice = func() { stop(); prevPlayerClose() }
	c.mu.Unlock()

	c.callBroadcast(chat, "join")
	c.Notify()
	return nil
}

func (c *Core) LeaveCall(chat []byte) {
	c.mu.Lock()
	call := c.calls[string(chat)]
	if call == nil || !call.Joined {
		c.mu.Unlock()
		return
	}
	call.Joined = false
	call.Mic, call.Cam, call.Screen = false, false, false
	empty := len(call.Members) == 0
	if empty {
		delete(c.calls, string(chat))
	}
	sv, sc, ss := c.stopVoice, c.stopCam, c.stopScreen
	c.stopVoice, c.stopCam, c.stopScreen = nil, nil, nil
	c.voiceOut = nil
	c.mu.Unlock()
	for _, f := range []func(){sv, sc, ss} {
		if f != nil {
			f()
		}
	}
	for _, p := range c.P2P.Peers() {
		p.CloseMedia("v:")
		p.CloseMedia("c:")
		p.CloseMedia("s:")
	}
	c.callBroadcast(chat, "leave")
	c.Notify()
}

func (c *Core) LeaveAllCalls() {
	c.mu.Lock()
	var chats [][]byte
	for k, call := range c.calls {
		if call.Joined {
			chats = append(chats, []byte(k))
		}
	}
	c.mu.Unlock()
	for _, ch := range chats {
		c.LeaveCall(ch)
	}
}

func (c *Core) ToggleMic(chat []byte) {
	c.mu.Lock()
	if call := c.calls[string(chat)]; call != nil {
		call.Mic = !call.Mic
	}
	c.mu.Unlock()
	c.Notify()
}

// startVideoSource wires a JPEG-producing source into the mesh.
func (c *Core) startVideoSource(chat []byte, kind string, start func(onJPEG func([]byte)) (func(), error)) error {
	var frameID uint32
	stop, err := start(func(jpeg []byte) {
		frameID++
		c.sendVideoFrame(chat, kind, frameID, jpeg)
		// local echo so the sender sees own picture
		if cb := c.OnVideoFrame; cb != nil {
			cb(chat, c.ID.Pub, kind, jpeg)
		}
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	if kind == "c" {
		c.stopCam = stop
	} else {
		c.stopScreen = stop
	}
	c.mu.Unlock()
	return nil
}

func (c *Core) ToggleCam(chat []byte, device string) error {
	c.mu.Lock()
	call := c.calls[string(chat)]
	if call == nil || !call.Joined {
		c.mu.Unlock()
		return nil
	}
	turnOff := call.Cam
	call.Cam = !call.Cam
	stop := c.stopCam
	if turnOff {
		c.stopCam = nil
	}
	c.mu.Unlock()
	defer c.Notify()
	if turnOff {
		if stop != nil {
			stop()
		}
		return nil
	}
	return c.startVideoSource(chat, "c", func(cb func([]byte)) (func(), error) {
		return media.StartCamera(device, 12, cb)
	})
}

func (c *Core) ToggleScreen(chat []byte) error {
	c.mu.Lock()
	call := c.calls[string(chat)]
	if call == nil || !call.Joined {
		c.mu.Unlock()
		return nil
	}
	turnOff := call.Screen
	call.Screen = !call.Screen
	stop := c.stopScreen
	if turnOff {
		c.stopScreen = nil
	}
	c.mu.Unlock()
	defer c.Notify()
	if turnOff {
		if stop != nil {
			stop()
		}
		return nil
	}
	return c.startVideoSource(chat, "s", func(cb func([]byte)) (func(), error) {
		return media.StartScreen(8, cb)
	})
}

// ---- media frame transport ----

const chunkSize = 12 * 1024

func (c *Core) sendVideoFrame(chat []byte, kind string, frameID uint32, jpeg []byte) {
	total := (len(jpeg) + chunkSize - 1) / chunkSize
	if total > 255 {
		return // absurdly large frame, drop
	}
	lbl := chatLabel(kind, chat)
	targets := c.mediaTargets(chat)
	for idx := 0; idx < total; idx++ {
		end := (idx + 1) * chunkSize
		if end > len(jpeg) {
			end = len(jpeg)
		}
		part := jpeg[idx*chunkSize : end]
		buf := make([]byte, 6+len(part))
		binary.BigEndian.PutUint32(buf, frameID)
		buf[4] = byte(idx)
		buf[5] = byte(total)
		copy(buf[6:], part)
		for _, p := range targets {
			p.SendMedia(lbl, buf)
		}
	}
}

type assembly struct {
	parts [][]byte
	got   int
	total int
	born  time.Time
}

func (c *Core) onMedia(p *p2p.Peer, label string, data []byte) {
	kind, chat, ok := parseLabel(label)
	if !ok {
		return
	}
	// sender must be an active chat member *and* must have announced the call:
	// without this, a non-participant chat member can inject audio into speaker and video on screen
	st, err := c.ChatState(chat)
	if err != nil || !st.IsMember(p.Pub) {
		return
	}
	c.mu.Lock()
	call := c.calls[string(chat)]
	inCall := call != nil && call.Joined && call.Members[string(p.Pub)]
	c.mu.Unlock()
	if !inCall {
		return
	}
	switch kind {
	case "v":
		if len(data) <= 4 {
			return
		}
		c.mu.Lock()
		out := c.voiceOut
		c.mu.Unlock()
		if out != nil {
			out(string(p.Pub), data[4:])
		}
	case "c", "s":
		if len(data) <= 6 {
			return
		}
		frameID := binary.BigEndian.Uint32(data)
		idx, total := int(data[4]), int(data[5])
		if total == 0 || idx >= total {
			return
		}
		key := string(p.Pub) + label
		c.mu.Lock()
		frames := c.assemblers[key]
		if frames == nil {
			frames = map[uint32]*assembly{}
			c.assemblers[key] = frames
		}
		a := frames[frameID]
		if a == nil {
			a = &assembly{parts: make([][]byte, total), total: total, born: time.Now()}
			frames[frameID] = a
		}
		if a.total == total && a.parts[idx] == nil {
			a.parts[idx] = bytes.Clone(data[6:])
			a.got++
		}
		complete := a.got == a.total
		var jpeg []byte
		if complete {
			jpeg = bytes.Join(a.parts, nil)
			delete(frames, frameID)
		}
		// GC stale assemblies
		for id, old := range frames {
			if time.Since(old.born) > 3*time.Second {
				delete(frames, id)
			}
		}
		c.mu.Unlock()
		if complete {
			// header within dimension limits before UI invokes jpeg.Decode: frame is
			// ~3 MB, but JPEG can claim 65535x65535 px and decoder would try
			// allocating gigabytes (decompression bomb from call member)
			if attach.CheckDims(jpeg) != nil {
				return
			}
			if cb := c.OnVideoFrame; cb != nil {
				cb(chat, p.Pub, kind, jpeg)
			}
		}
	}
}
