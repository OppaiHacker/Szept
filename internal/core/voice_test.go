package core

import (
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"szept/internal/connector"
)

// Two headless cores in a voice call: audio flows both ways, and a loss
// report from the listener reaches the talker without locking up its core.
func TestVoiceCallLossReport(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)
	chat, err := a.CreateChat("g", false)
	if err != nil {
		t.Fatal(err)
	}
	code, pin, err := a.NewInvite(chat)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Redeem(code, pin); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b joins", 20*time.Second, func() bool {
		st, err := b.ChatState(chat)
		return err == nil && st.IsMember(b.ID.Pub) && b.Online(a.ID.Pub)
	})

	// a talks: a 440 Hz tone in 20 ms frames (VoiceIn skips the mic and denoiser)
	a.Cfg.Voice.NoGate = true
	var stopTalk atomic.Bool
	var talk sync.WaitGroup
	a.VoiceIn = func(cb func([]byte)) (func(), error) {
		talk.Add(1)
		go func() {
			defer talk.Done()
			pcm := make([]byte, 960*2)
			for n := 0; !stopTalk.Load(); n++ {
				for i := 0; i < 960; i++ {
					v := int16(8000 * math.Sin(2*math.Pi*440*float64(n*960+i)/48000))
					pcm[2*i], pcm[2*i+1] = byte(v), byte(uint16(v)>>8)
				}
				cb(pcm)
				time.Sleep(20 * time.Millisecond)
			}
		}()
		return func() { stopTalk.Store(true); talk.Wait() }, nil
	}
	a.VoiceOut = func(string, []byte) {}
	b.VoiceIn = func(func([]byte)) (func(), error) { return func() {}, nil }
	var heard atomic.Int64
	b.VoiceOut = func(src string, pcm []byte) {
		if src == string(a.ID.Pub) {
			heard.Add(int64(len(pcm)))
		}
	}
	if err := b.JoinCall(chat); err != nil {
		t.Fatal(err)
	}
	if err := a.JoinCall(chat); err != nil {
		t.Fatal(err)
	}
	defer a.LeaveCall(chat)
	defer b.LeaveCall(chat)
	waitFor(t, "b hears a", 20*time.Second, func() bool { return heard.Load() > 48000*2 })

	// b reports loss to a; a's core must stay responsive
	b.P2P.Peer(a.ID.Pub).SendCtrl(Ctrl{T: "call", Chat: chat, Action: "loss", Status: "10"})
	time.Sleep(300 * time.Millisecond)
	done := make(chan struct{})
	go func() { a.JoinedCall(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a's core deadlocked on a loss report")
	}
	before := heard.Load()
	waitFor(t, "audio keeps flowing", 10*time.Second, func() bool { return heard.Load() > before+48000 })
}
