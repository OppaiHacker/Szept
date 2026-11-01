package media

import (
	"sync"
	"time"

	"szept/internal/media/opus"
)

// Receiver decodes the Opus packets of every peer in a call. Packets come
// over an unordered, lossy channel numbered by the sender: late ones are
// dropped, and a short gap is filled from the next packet's FEC and PLC.
type Receiver struct {
	mu   sync.Mutex // ponytail: one lock for all peers, per-peer if calls get big
	srcs map[string]*rx
}

type rx struct {
	dec *opus.Decoder
	seq uint32
	at  time.Time
}

func NewReceiver() *Receiver { return &Receiver{srcs: map[string]*rx{}} }

const maxConceal = 3 // frames (60 ms); a longer gap is a new talk spurt

// Decode returns PCM for packet seq from src, or nil for a late duplicate.
func (r *Receiver) Decode(src string, seq uint32, pkt []byte) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.srcs[src]
	if s == nil {
		s = &rx{dec: opus.NewDecoder()}
		r.srcs[src] = s
	}
	// after a pause (gate closed) or a rejoin that restarted the counter,
	// take the stream as it comes (reordering never spans 10 packets)
	fresh := time.Since(s.at) > time.Second || s.seq-seq < 1<<31 && s.seq-seq > 10
	if !fresh && seq-s.seq-1 >= 1<<31 { // seq <= s.seq, mod 2^32
		return nil
	}
	var out []byte
	if gap := seq - s.seq - 1; !fresh && gap > 0 && gap <= maxConceal {
		for range gap - 1 {
			pcm, _ := s.dec.Decode(nil, false)
			out = append(out, pcm...)
		}
		pcm, _ := s.dec.Decode(pkt, true) // the frame right before pkt
		out = append(out, pcm...)
	}
	s.seq, s.at = seq, time.Now()
	pcm, err := s.dec.Decode(pkt, false)
	if err != nil {
		return out
	}
	return append(out, pcm...)
}
