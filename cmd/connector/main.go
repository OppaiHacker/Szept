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
	listen := flag.String("listen", ":7777", "adres nasłuchu (port, :port lub host:port)")
	cert := flag.String("cert", "", "ścieżka do cert TLS (opcjonalnie)")
	key := flag.String("key", "", "ścieżka do klucza TLS (opcjonalnie)")
	flag.Parse()

	addr := normalizeListen(*listen)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Typowy przypadek VPS/NAT: publiczny IP nie jest przypisany do interfejsu
		// (cloud 1:1 NAT). Bindujemy wtedy na wszystkie interfejsy na tym samym porcie.
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

	srv := &http.Server{Handler: connector.NewServer()}
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
