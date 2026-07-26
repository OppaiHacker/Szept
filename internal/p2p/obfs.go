package p2p

// Traffic obfuscation: every UDP datagram exchanged with a peer is scrambled
// with a per-connection ChaCha20 keystream, so on the wire it carries no STUN
// magic cookie, no DTLS record header and no fixed length — a DPI box sees
// only high-entropy noise instead of a WebRTC flow it can fingerprint and
// block. The shared key rides inside the Ed25519-signed offer (see sigPayload
// .Obf), so only the addressed peer can derive the keystream.
//
// Packets to the public STUN servers must stay real STUN, so those endpoints
// are learned from the ICE resolver (ResolveUDPAddr) and passed through in the
// clear. Everything else — host-candidate checks, peer connectivity STUN,
// DTLS/SCTP data — is peer traffic and gets obfuscated.
//
// Unauthenticated keystream (no MAC). DTLS underneath already
// authenticates, so a tampered datagram just decrypts to garbage DTLS drops;
// adding Poly1305 here would only duplicate that. 12-byte random nonce per
// datagram, safe to ~2^48 packets/key before birthday risk — a session never
// gets close, and the key rotates every Connect().

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/pion/transport/v4"
	"github.com/pion/transport/v4/stdnet"
	"golang.org/x/crypto/chacha20"
)

const (
	obfNonce  = chacha20.NonceSize // 12
	obfMaxPad = 64                 // random tail padding: perturbs the length histogram
)

// readPool: 64K scratch buffers for the deobfuscate-in-place read path.
var readPool = sync.Pool{New: func() any { b := make([]byte, 65536); return &b }}

// stunHostSet extracts the bare hostnames of ICE (STUN) servers, so
// ResolveUDPAddr can recognise which resolutions to keep in the clear.
func stunHostSet(urls []string) map[string]bool {
	set := map[string]bool{}
	for _, u := range urls {
		s := u
		if i := strings.IndexByte(s, ':'); i >= 0 {
			switch s[:i] {
			case "stun", "stuns", "turn", "turns":
				s = s[i+1:] // strip scheme
			}
		}
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			host = s // no port (pion adds the default); key on host alone
		}
		set[host] = true
	}
	return set
}

// obfNet wraps stdnet: UDP listen paths return obfuscating conns, and
// ResolveUDPAddr records the STUN server endpoints into a shared bypass set.
type obfNet struct {
	transport.Net
	key       []byte
	stunHosts map[string]bool
	bypass    *sync.Map // addrKey(net.Addr) -> struct{}: send/recv in the clear
}

func newObfNet(key []byte, stunHosts map[string]bool) (*obfNet, error) {
	if len(key) != chacha20.KeySize {
		return nil, errors.New("obfs: bad key length")
	}
	base, err := stdnet.NewNet()
	if err != nil {
		return nil, err
	}
	return &obfNet{Net: base, key: key, stunHosts: stunHosts, bypass: &sync.Map{}}, nil
}

// addrKey normalises an address to "ip:port" (folds ::ffff: IPv4 forms so a
// WriteTo destination and a ReadFrom source compare equal).
func addrKey(a net.Addr) string {
	if ua, ok := a.(*net.UDPAddr); ok {
		return net.JoinHostPort(ua.IP.String(), strconv.Itoa(ua.Port))
	}
	return a.String()
}

func (n *obfNet) ResolveUDPAddr(network, address string) (*net.UDPAddr, error) {
	a, err := n.Net.ResolveUDPAddr(network, address)
	if err == nil {
		if host, _, e := net.SplitHostPort(address); e == nil && n.stunHosts[host] {
			n.bypass.Store(addrKey(a), struct{}{}) // this exact endpoint stays real STUN
		}
	}
	return a, err
}

func (n *obfNet) ListenUDP(network string, laddr *net.UDPAddr) (transport.UDPConn, error) {
	c, err := n.Net.ListenUDP(network, laddr)
	if err != nil {
		return nil, err
	}
	return &obfUDPConn{UDPConn: c, key: n.key, bypass: n.bypass}, nil
}

func (n *obfNet) ListenPacket(network, address string) (net.PacketConn, error) {
	c, err := n.Net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	return &obfPacketConn{PacketConn: c, key: n.key, bypass: n.bypass}, nil
}

type obfUDPConn struct {
	transport.UDPConn
	key    []byte
	bypass *sync.Map
}

func (c *obfUDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	return obfReadFrom(c.UDPConn, c.key, c.bypass, p)
}
func (c *obfUDPConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return obfWriteTo(c.UDPConn, c.key, c.bypass, p, addr)
}

type obfPacketConn struct {
	net.PacketConn
	key    []byte
	bypass *sync.Map
}

func (c *obfPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	return obfReadFrom(c.PacketConn, c.key, c.bypass, p)
}
func (c *obfPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return obfWriteTo(c.PacketConn, c.key, c.bypass, p, addr)
}

func obfWriteTo(inner net.PacketConn, key []byte, bypass *sync.Map, p []byte, addr net.Addr) (int, error) {
	if _, clear := bypass.Load(addrKey(addr)); clear {
		return inner.WriteTo(p, addr)
	}
	w, err := obfSeal(key, p)
	if err != nil {
		return 0, err
	}
	if _, err := inner.WriteTo(w, addr); err != nil {
		return 0, err
	}
	return len(p), nil // report the caller's byte count, not the padded wire size
}

func obfReadFrom(inner net.PacketConn, key []byte, bypass *sync.Map, p []byte) (int, net.Addr, error) {
	bp := readPool.Get().(*[]byte)
	buf := *bp
	defer readPool.Put(bp)
	n, addr, err := inner.ReadFrom(buf)
	if err != nil {
		return n, addr, err
	}
	if _, clear := bypass.Load(addrKey(addr)); clear {
		return copy(p, buf[:n]), addr, nil
	}
	ln := obfOpen(key, buf[:n]) // decrypts in place, returns plaintext length
	if ln < 0 {
		return 0, addr, nil // undecryptable noise: hand up an empty datagram, DTLS ignores it
	}
	return copy(p, buf[obfNonce+2:obfNonce+2+ln]), addr, nil
}

// obfSeal: nonce(12) || ChaCha20( uint16 len || plaintext || random pad ).
func obfSeal(key, plaintext []byte) ([]byte, error) {
	if len(plaintext) > 0xffff {
		return nil, errors.New("obfs: datagram too large")
	}
	var pb [1]byte
	rand.Read(pb[:])
	pad := int(pb[0]) % (obfMaxPad + 1)
	out := make([]byte, obfNonce+2+len(plaintext)+pad)
	rand.Read(out[:obfNonce]) // random nonce: indistinguishable from the rest
	binary.BigEndian.PutUint16(out[obfNonce:], uint16(len(plaintext)))
	copy(out[obfNonce+2:], plaintext)
	rand.Read(out[obfNonce+2+len(plaintext):]) // random padding
	c, err := chacha20.NewUnauthenticatedCipher(key, out[:obfNonce])
	if err != nil {
		return nil, err
	}
	c.XORKeyStream(out[obfNonce:], out[obfNonce:])
	return out, nil
}

// obfOpen decrypts wire in place and returns the plaintext length, or -1 if
// the datagram is too short / declares a bogus length. Plaintext lives at
// wire[obfNonce+2 : obfNonce+2+len].
func obfOpen(key, wire []byte) int {
	if len(wire) < obfNonce+2 {
		return -1
	}
	c, err := chacha20.NewUnauthenticatedCipher(key, wire[:obfNonce])
	if err != nil {
		return -1
	}
	body := wire[obfNonce:]
	c.XORKeyStream(body, body)
	ln := int(binary.BigEndian.Uint16(body))
	if 2+ln > len(body) {
		return -1
	}
	return ln
}
