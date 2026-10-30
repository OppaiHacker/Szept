// szept-connector: the rendezvous server ("tracker"). Stateless, zero-knowledge:
// sees pubkeys and IPs (unavoidable routing metadata), never message content.
// Run it on any box with a public address. Best camouflage: --domain (wss on
// 443 with a Let's Encrypt cert) plus the secret path, optionally --decoy.
package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"szept/internal/connector"
)

// secretPath returns the path clients must dial. "auto" keeps a random one in
// connector.path so it survives restarts; "off" accepts any path.
func secretPath(flagVal string) string {
	switch flagVal {
	case "off":
		return ""
	case "auto":
		if b, err := os.ReadFile("connector.path"); err == nil && len(strings.TrimSpace(string(b))) > 1 {
			return strings.TrimSpace(string(b))
		}
		b := make([]byte, 12)
		rand.Read(b)
		p := "/" + hex.EncodeToString(b)
		if err := os.WriteFile("connector.path", []byte(p+"\n"), 0o600); err != nil {
			log.Fatalf("nie mogę zapisać connector.path: %v", err)
		}
		return p
	}
	return "/" + strings.TrimPrefix(flagVal, "/")
}

// normalizeListen accepts sloppy inputs: "7777", ":7777", "1.2.3.4:7777",
// "ws://1.2.3.4:7777/". Returns a net.Listen-ready "host:port".
func normalizeListen(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"ws://", "wss://", "http://", "https://"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimSuffix(s, "/")
	if s != "" && !strings.Contains(s, ":") {
		s = ":" + s // bare port
	}
	return s
}

func main() {
	listen := flag.String("listen", "", "listen address (port, :port or host:port; default :7777, or :443 with --domain)")
	domain := flag.String("domain", "", "serve wss:// on this domain with an automatic Let's Encrypt certificate (needs port 443)")
	cert := flag.String("cert", "", "path to TLS cert (optional)")
	key := flag.String("key", "", "path to TLS key (optional)")
	pathFlag := flag.String("path", "auto", "secret WebSocket path (auto = random, kept in connector.path; off = any path)")
	decoyFlag := flag.String("decoy", "", "URL of a real website to show everyone who is not a Szept client (default: 404)")
	seedsFlag := flag.String("seeds", "", "comma-separated connector URLs to join the Cebulion mesh through")
	publicURLFlag := flag.String("public-url", "", "URL other connectors dial to reach this one (default: derived from --domain)")
	maxPeersFlag := flag.Int("max-peers", 0, "max number of active peers (0 = no limit, exceeding limit triggers redirect)")
	meshFlag := flag.Bool("mesh", true, "join the open Cebulion mesh")
	meshSecretFlag := flag.String("mesh-secret", "", "make the mesh private: only nodes with this secret are neighbours")
	privateGossip := flag.Bool("private-gossip", false, "never tell anyone this node's neighbours (except --mesh-secret peers) and ask neighbours not to pass this node on: hidden from mesh walkers, grows through --seeds only")
	turnRate := flag.Int("turn-rate", 2048, "relay bandwidth cap in KB/s each way, all users together (0 = no cap)")
	turnFlag := flag.String("turn", "auto", "TURN/STUN relay on UDP at the listen port: auto = on when the public address is known (--domain/--public-url), off, or the public IP to relay from")
	trustProxyFlag := flag.Bool("trust-proxy", false, "read the client IP from X-Forwarded-For (only behind your own reverse proxy)")
	flag.Parse()

	if *listen == "" {
		*listen = ":7777"
		if *domain != "" {
			*listen = ":443"
		}
	}
	addr := normalizeListen(*listen)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Typical VPS/NAT case: public IP is not bound directly to the interface
		// (cloud 1:1 NAT). We bind to all interfaces on the same port instead.
		if _, port, splitErr := net.SplitHostPort(addr); splitErr == nil && port != "" {
			log.Printf("nie mogę nasłuchiwać na %s (%v) — próbuję :%s (wszystkie interfejsy)", addr, err, port)
			addr = ":" + port
			ln, err = net.Listen("tcp", addr)
		}
	}
	if err != nil {
		log.Fatalf("nie mogę nasłuchiwać na %s: %v", addr, err)
	}

	scheme := "ws"
	if *domain != "" || *cert != "" && *key != "" {
		scheme = "wss"
	}
	path := secretPath(*pathFlag)
	host := "<publiczny-adres-tego-serwera>" + portSuffix(ln.Addr().String())
	if *domain != "" {
		host = *domain
		if p := portSuffix(ln.Addr().String()); p != ":443" {
			host += p
		}
	}
	log.Printf("szept-connector nasłuchuje na %s", ln.Addr())
	log.Printf("klienci łączą się adresem: %s://%s%s", scheme, host, path)
	if scheme == "ws" {
		log.Print("UWAGA: bez TLS rejestracja idzie jawnym tekstem i DPI rozpozna connector — użyj --domain albo --cert/--key")
	}
	if path == "" {
		log.Print("UWAGA: --path off — każdy, kto otworzy WebSocket na tym porcie, rozpozna connector")
	}

	cServer := connector.NewServer()
	cServer.SetPath(path)
	if *decoyFlag != "" {
		u, err := url.Parse(*decoyFlag)
		if err != nil || u.Host == "" {
			log.Fatalf("zły --decoy: %q", *decoyFlag)
		}
		rp := httputil.NewSingleHostReverseProxy(u)
		director := rp.Director
		rp.Director = func(r *http.Request) { director(r); r.Host = u.Host }
		cServer.SetDecoy(rp)
	}
	if *maxPeersFlag > 0 {
		cServer.SetMaxPeers(*maxPeersFlag)
		log.Printf("limit peerów ustawiony na: %d", *maxPeersFlag)
	}
	if *trustProxyFlag {
		cServer.SetTrustProxy(true)
		log.Print("ufam nagłówkowi X-Forwarded-For (--trust-proxy) — używaj TYLKO za własnym reverse proxy")
	}
	switch {
	case !*meshFlag:
		cServer.SetMeshOff()
		log.Print("mesh (Sieć Cebulion) wyłączony (--mesh=false)")
	case *meshSecretFlag != "":
		cServer.SetMeshSecret(*meshSecretFlag)
		log.Print("mesh (Sieć Cebulion) prywatny — sąsiedzi muszą znać --mesh-secret")
	default:
		log.Print("mesh (Sieć Cebulion) otwarty")
	}
	if *privateGossip && *meshFlag {
		cServer.SetPrivateGossip()
		log.Print("--private-gossip: nie podaję sąsiadów i proszę ich, żeby nie podawali dalej mojego adresu")
	}
	// a bind address ([::]:7777) is not an address anyone can dial, so it is
	// never announced
	switch {
	case *publicURLFlag != "":
		cServer.SetPublicURL(*publicURLFlag)
	case *domain != "":
		cServer.SetPublicURL(scheme + "://" + host + path)
	default:
		log.Print("brak --public-url: inne connectory nie dodadzą tego węzła do meshu")
	}
	if *seedsFlag != "" {
		seeds := strings.Split(*seedsFlag, ",")
		for i, s := range seeds {
			seeds[i] = strings.TrimSpace(s)
		}
		cServer.SetSeeds(seeds)
		log.Printf("zaimportowano %d connectorów startowych (SNDP)", len(seeds))
	}

	if host, relayIP := turnAddr(*turnFlag, *domain, *publicURLFlag); relayIP != nil {
		port := strings.TrimPrefix(portSuffix(ln.Addr().String()), ":")
		if err := cServer.StartTURN(":"+port, relayIP, net.JoinHostPort(host, port), *turnRate<<10); err != nil {
			log.Printf("TURN nie wystartował: %v", err)
		}
	} else if *turnFlag != "off" {
		log.Print("TURN wyłączony: podaj --domain, --public-url albo --turn <publiczny IP>")
	}

	// ReadHeaderTimeout: without this, slow/hung requests keep connections open (Slowloris).
	srv := &http.Server{Handler: cServer, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	if *domain != "" {
		// TLS-ALPN-01 runs on 443 itself, no port 80 needed
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(*domain),
			Cache:      autocert.DirCache("autocert"),
		}
		srv.TLSConfig = m.TLSConfig()
		log.Fatal(srv.Serve(tls.NewListener(ln, srv.TLSConfig)))
	}
	if *cert != "" && *key != "" {
		log.Fatal(srv.ServeTLS(ln, *cert, *key))
	}
	log.Fatal(srv.Serve(ln))
}

// turnAddr picks the host clients dial for TURN and the public IPv4 the relay
// announces. nil = no relay.
func turnAddr(flagVal, domain, publicURL string) (string, net.IP) {
	host := flagVal
	switch flagVal {
	case "off":
		return "", nil
	case "auto":
		host = domain
		if u, err := url.Parse(publicURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	}
	if host == "" {
		return "", nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return host, ip
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		log.Printf("TURN: nie mogę rozwiązać %s: %v", host, err)
		return "", nil
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return host, ip4
		}
	}
	return host, ips[0]
}

func portSuffix(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return ":" + port
	}
	return ""
}
