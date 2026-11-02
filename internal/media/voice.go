package media

import (
	"sync"
	"time"

	"szept/internal/media/opus"
)

// Receiver decodes the Opus packets of every peer in a call using an adaptive jitter buffer.
type Receiver struct {
	mu   sync.Mutex
	srcs map[string]*rx
}

type rx struct {
	dec *opus.Decoder
	jb  *jitterBuf
	at  time.Time
}

func NewReceiver() *Receiver { return &Receiver{srcs: map[string]*rx{}} }

// Decode buffers the packet in the adaptive jitter buffer and returns decoded PCM.
// Out-of-order packets are reordered smoothly, missing frames leverage in-band FEC and PLC.
func (r *Receiver) Decode(src string, seq uint32, pkt []byte) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.srcs[src]
	if s == nil {
		s = &rx{dec: opus.NewDecoder(), jb: newJitterBuf()}
		r.srcs[src] = s
	}
	s.at = time.Now()
	s.jb.Push(seq, pkt)
	return s.jb.Pop(s.dec)
}

// LossReport returns the estimated packet loss percentage for each active remote peer.
func (r *Receiver) LossReport() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	report := map[string]int{}
	now := time.Now()
	for src, s := range r.srcs {
		if now.Sub(s.at) < 3*time.Second {
			report[src] = s.jb.LossPercent()
		}
	}
	return report
}
