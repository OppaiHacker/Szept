// szept-connector: the rendezvous server ("tracker"). Stateless, zero-knowledge:
// sees pubkeys and IPs (unavoidable routing metadata), never message content.
// Run it on any box with a public address; TLS via --cert/--key or a reverse proxy.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"szept/internal/connector"
)

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
	listen := flag.String("listen", ":7777", "listen address (port, :port or host:port)")
	cert := flag.String("cert", "", "path to TLS cert (optional)")
	key := flag.String("key", "", "path to TLS key (optional)")
	seedsFlag := flag.String("seeds", "", "comma-separated list of seed connector URLs (e.g. ws://c2.szept.io:7777)")
	publicURLFlag := flag.String("public-url", "", "public URL of this connector for SNDP protocol (e.g. ws://c1.szept.io:7777)")
	maxPeersFlag := flag.Int("max-peers", 0, "max number of active peers (0 = no limit, exceeding limit triggers redirect)")
	meshSecretFlag := flag.String("mesh-secret", "", "shared secret for the connector mesh (empty = mesh off)")
	trustProxyFlag := flag.Bool("trust-proxy", false, "read the client IP from X-Forwarded-For (only behind your own reverse proxy)")
	flag.Parse()

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
	if *cert != "" && *key != "" {
		scheme = "wss"
	}
	log.Printf("szept-connector nasłuchuje na %s", ln.Addr())
	log.Printf("klienci łączą się adresem: %s://<publiczny-adres-tego-serwera>%s", scheme, portSuffix(ln.Addr().String()))
	log.Printf("test w przeglądarce: http://<adres>%s (strona statusu)", portSuffix(ln.Addr().String()))

	cServer := connector.NewServer()
	if *maxPeersFlag > 0 {
		cServer.SetMaxPeers(*maxPeersFlag)
		log.Printf("limit peerów ustawiony na: %d", *maxPeersFlag)
	}
	if *trustProxyFlag {
		cServer.SetTrustProxy(true)
		log.Print("ufam nagłówkowi X-Forwarded-For (--trust-proxy) — używaj TYLKO za własnym reverse proxy")
	}
	if *meshSecretFlag != "" {
		cServer.SetMeshSecret(*meshSecretFlag)
		log.Print("mesh (Sieć Cebulion) włączony — sąsiedzi muszą znać wspólny sekret")
	} else {
		log.Print("mesh (Sieć Cebulion) wyłączony (brak --mesh-secret)")
	}
	if *publicURLFlag != "" {
		cServer.SetPublicURL(*publicURLFlag)
	} else {
		cServer.SetPublicURL(scheme + "://" + ln.Addr().String())
	}
	if *seedsFlag != "" {
		seeds := strings.Split(*seedsFlag, ",")
		for i, s := range seeds {
			seeds[i] = strings.TrimSpace(s)
		}
		cServer.SetSeeds(seeds)
		log.Printf("zaimportowano %d connectorów startowych (SNDP)", len(seeds))
	}

	// ReadHeaderTimeout: without this, slow/hung requests keep connections open (Slowloris).
	srv := &http.Server{Handler: cServer, ReadHeaderTimeout: 10 * time.Second}
	if *cert != "" && *key != "" {
		log.Fatal(srv.ServeTLS(ln, *cert, *key))
	}
	log.Fatal(srv.Serve(ln))
}

func portSuffix(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return ":" + port
	}
	return ""
}
