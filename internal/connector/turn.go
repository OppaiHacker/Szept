package connector

// TURN relay. Every connector can relay peer traffic, so a client that does
// not want its contacts to learn its IP (relay-only mode), or one stuck behind
// a carrier-grade NAT, still gets through. The connector doubles as the STUN
// server, so clients no longer need Google's.
//
// Credentials are TURN REST style (expiry:user + HMAC), handed out only to
// sockets that passed the signed register, and refreshed over the WebSocket.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pion/turn/v5"
	"golang.org/x/time/rate"
)

const (
	turnCredTTL        = 24 * time.Hour
	maxAllocsPerClient = 64 // one per relayed peer connection
)

// ICE is a STUN/TURN server as handed to clients.
type ICE struct {
	URLs []string `json:"urls"`
	User string   `json:"user,omitempty"`
	Pass string   `json:"pass,omitempty"`
}

type turnRelay struct {
	url    string // host:port
	secret string
	srv    *turn.Server

	mu     sync.Mutex
	allocs map[string]int // userID -> live allocations
}

// StartTURN runs a TURN server on UDP addr (e.g. ":443"), relaying from
// relayIP, the connector's public address; host:port is what clients dial.
// rateBytes caps the relayed traffic of all allocations together, in bytes
// per second each way (0 = no cap): a connector on a home router must not
// lose its owner's uplink to strangers' video calls.
func (s *Server) StartTURN(addr string, relayIP net.IP, hostPort string, rateBytes int) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	t := &turnRelay{url: hostPort, secret: hex.EncodeToString(secret), allocs: map[string]int{}}
	// the realm goes out in the 401 to any unauthenticated probe: name the
	// host the prober already dialed, never the software
	realm, _, err := net.SplitHostPort(hostPort)
	if err != nil || realm == "" {
		realm = hostPort
	}
	t.srv, err = turn.NewServer(turn.ServerConfig{
		Realm:       realm,
		AuthHandler: turn.LongTermTURNRESTAuthHandler(t.secret, nil),
		QuotaHandler: func(username, _ string, _ net.Addr) bool {
			t.mu.Lock()
			defer t.mu.Unlock()
			return t.allocs[turnUserID(username)] < maxAllocsPerClient
		},
		EventHandler: turn.EventHandler{
			OnAllocationCreated: func(_, _ net.Addr, _, userID, _ string, _ net.Addr, _ int) {
				t.mu.Lock()
				t.allocs[userID]++
				t.mu.Unlock()
			},
			OnAllocationDeleted: func(_, _ net.Addr, _, userID, _ string) {
				t.mu.Lock()
				if t.allocs[userID]--; t.allocs[userID] <= 0 {
					delete(t.allocs, userID)
				}
				t.mu.Unlock()
			},
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: pc,
			RelayAddressGenerator: &cappedRelay{
				RelayAddressGeneratorStatic: &turn.RelayAddressGeneratorStatic{RelayAddress: relayIP, Address: "0.0.0.0"},
				in:                          newCap(rateBytes),
				out:                         newCap(rateBytes),
			},
			// the relay must not become a door into the connector's own network
			PermissionHandler: func(_ net.Addr, peer net.IP) bool { return publicIP(peer) },
		}},
	})
	if err != nil {
		pc.Close()
		return err
	}
	s.mu.Lock()
	s.turn = t
	s.mu.Unlock()
	log.Printf("TURN/STUN na udp %s (przekaźnik %s)", addr, relayIP)
	return nil
}

// cappedRelay hands out relay sockets that share one byte budget per
// direction. Over budget, datagrams are dropped, never queued: media copes
// with loss and SCTP backs off, while a queue would only add latency.
type cappedRelay struct {
	*turn.RelayAddressGeneratorStatic
	in, out *rate.Limiter // nil = unlimited
}

func newCap(bytesPerSec int) *rate.Limiter {
	if bytesPerSec <= 0 {
		return nil
	}
	return rate.NewLimiter(rate.Limit(bytesPerSec), max(bytesPerSec, 64<<10))
}

func (g *cappedRelay) AllocatePacketConn(cfg turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	pc, addr, err := g.RelayAddressGeneratorStatic.AllocatePacketConn(cfg)
	if err != nil || (g.in == nil && g.out == nil) {
		return pc, addr, err
	}
	return &cappedConn{PacketConn: pc, in: g.in, out: g.out}, addr, nil
}

type cappedConn struct {
	net.PacketConn
	in, out *rate.Limiter
}

func (c *cappedConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c.out != nil && !c.out.AllowN(time.Now(), len(b)) {
		return len(b), nil // dropped
	}
	return c.PacketConn.WriteTo(b, addr)
}

func (c *cappedConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(b)
		if err != nil || c.in == nil || c.in.AllowN(time.Now(), n) {
			return n, addr, err
		}
	}
}

// turnUserID: the part after "expiry:" in a TURN REST username.
func turnUserID(username string) string {
	for i := 0; i < len(username); i++ {
		if username[i] == ':' {
			return username[i+1:]
		}
	}
	return username
}

// iceFor returns fresh relay credentials for a registered key, or nil.
func (s *Server) iceFor(pub []byte) *ICE {
	s.mu.Lock()
	t := s.turn
	s.mu.Unlock()
	if t == nil {
		return nil
	}
	id := sha256.Sum256(pub)
	user, pass, err := turn.GenerateLongTermTURNRESTCredentials(t.secret, hex.EncodeToString(id[:8]), turnCredTTL)
	if err != nil {
		return nil
	}
	return &ICE{
		URLs: []string{"stun:" + t.url, fmt.Sprintf("turn:%s?transport=udp", t.url)},
		User: user, Pass: pass,
	}
}
