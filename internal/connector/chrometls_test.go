package connector

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// The Chrome hello must still complete a verified handshake and a WebSocket
// upgrade against a server that would prefer h2, and must look like Chrome
// (GREASE values) rather than Go.
func TestChromeTLSUpgrade(t *testing.T) {
	var hello *tls.ClientHelloInfo
	up := websocket.Upgrader{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(r.TLS.NegotiatedProtocol))
		c.Close()
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	srv.TLS.GetConfigForClient = func(h *tls.ClientHelloInfo) (*tls.Config, error) { hello = h; return nil, nil }
	tlsRoots = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	defer func() { tlsRoots = nil }()

	ws, _, err := clientDialer.Dial("wss"+strings.TrimPrefix(srv.URL, "https"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, proto, err := ws.ReadMessage(); err != nil || string(proto) == "h2" {
		t.Fatalf("ALPN %q, %v", proto, err)
	}
	if !slices.Equal(hello.SupportedProtos, []string{"http/1.1"}) {
		t.Fatalf("ALPN offered %q, want only http/1.1", hello.SupportedProtos)
	}
	grease := false
	for _, cs := range hello.CipherSuites {
		grease = grease || cs&0x0f0f == 0x0a0a
	}
	if !grease {
		t.Fatal("ClientHello has no GREASE: not the Chrome fingerprint")
	}
}
