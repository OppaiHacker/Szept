package media

import (
	"sync"
	"time"

	"szept/internal/media/opus"
)

// packetEntry holds an Opus encoded packet in the jitter buffer.
type packetEntry struct {
	seq uint32
	pkt []byte
	at  time.Time
}

// jitterBuf is a per-peer packet-level adaptive jitter buffer.
type jitterBuf struct {
	mu        sync.Mutex
	entries   map[uint32]packetEntry
	playSeq   uint32
	started   bool
	target    int           // target buffer size in frames (1..8, i.e. 20..160 ms)
	jitter    float64       // RFC 3550 jitter estimate in seconds
	lastArrAt time.Time
	lastArrSeq uint32
	lastActive time.Time

	// Loss tracking over recent window
	statRcvd int
	statLost int
	statAt   time.Time
	lastLoss int
}

func newJitterBuf() *jitterBuf {
	return &jitterBuf{
		entries: map[uint32]packetEntry{},
		target:  2, // start with 40 ms
		statAt:  time.Now(),
	}
}

// Push stores a newly arrived packet into the buffer. Out-of-order packets
// naturally find their slot without packet drop.
func (j *jitterBuf) Push(seq uint32, pkt []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	j.lastActive = now

	// Estimate inter-arrival jitter (RFC 3550)
	if !j.lastArrAt.IsZero() {
		transit := now.Sub(j.lastArrAt).Seconds()
		expected := float64(int32(seq-j.lastArrSeq)) * 0.020
		d := transit - expected
		if d < 0 {
			d = -d
		}
		// J = J + (|D| - J) / 16
		j.jitter += (d - j.jitter) / 16.0

		// Target frames: 1 frame (20ms) safety margin + ceil(jitter / 20ms), clamped [1, 8]
		t := 1 + int(j.jitter/0.020+0.99)
		if t < 1 {
			t = 1
		} else if t > 8 {
			t = 8
		}
		j.target = t
	}
	j.lastArrAt = now
	j.lastArrSeq = seq

	if !j.started {
		j.playSeq = seq
		j.started = true
	} else if now.Sub(j.statAt) > time.Second*2 {
		total := j.statRcvd + j.statLost
		if total > 0 {
			j.lastLoss = (j.statLost * 100) / total
		} else {
			j.lastLoss = 0
		}
		j.statRcvd, j.statLost = 0, 0
		j.statAt = now
	}

	// Rejoin or restart counter check
	if seq-j.playSeq > 1<<31 && j.playSeq-seq > 50 {
		j.playSeq = seq
		clear(j.entries)
	}

	// Store copy of packet
	if len(j.entries) < 32 {
		cp := make([]byte, len(pkt))
		copy(cp, pkt)
		j.entries[seq] = packetEntry{seq: seq, pkt: cp, at: now}
	}
}

// Pop decodes and retrieves the next 20 ms PCM frame. It handles:
// - Exact packet hit
// - FEC from next packet if current is lost
// - PLC concealment if both are missing
func (j *jitterBuf) Pop(dec *opus.Decoder) []byte {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !j.started || len(j.entries) == 0 {
		return nil
	}

	// Clean up entries older than 800 ms
	now := time.Now()
	for s, e := range j.entries {
		if now.Sub(e.at) > 800*time.Millisecond {
			delete(j.entries, s)
		}
	}

	// If buffer is accumulating excessive delay, fast-forward playSeq
	if len(j.entries) > j.target+4 {
		// drift compensation: skip old frames
		for s := range j.entries {
			if s > j.playSeq && (s-j.playSeq) < 1<<31 {
				j.playSeq = s
				break
			}
		}
	}

	curSeq := j.playSeq
	j.playSeq++

	if e, ok := j.entries[curSeq]; ok {
		delete(j.entries, curSeq)
		j.statRcvd++
		pcm, _ := dec.Decode(e.pkt, false)
		return pcm
	}

	// curSeq is missing: check if curSeq+1 is present to decode in-band FEC
	if next, ok := j.entries[curSeq+1]; ok {
		j.statLost++
		pcm, err := dec.Decode(next.pkt, true)
		if err == nil && len(pcm) > 0 {
			return pcm
		}
	}

	// Neither curSeq nor FEC is available: perform PLC concealment
	j.statLost++
	pcm, _ := dec.Decode(nil, false)
	return pcm
}

// LossPercent returns estimated packet loss percentage over recent window.
func (j *jitterBuf) LossPercent() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastLoss
}

