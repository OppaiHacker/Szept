package connector

// Client side of wss://. Go's crypto/tls ClientHello has a fingerprint of its
// own (JA3/JA4): to DPI it reads as "some Go program", and next to a connector
// that is one more reason to block. uTLS sends Chrome's hello instead, with
// ALPN limited to http/1.1 — what Chrome itself offers when it opens a
// WebSocket, and what keeps the server from choosing h2, which gorilla cannot
// speak.

import (
	"context"
	"crypto/x509"
	"net"

	"github.com/gorilla/websocket"
	utls "github.com/refraction-networking/utls"
)

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// chromeTLS wraps a TCP dialer into a TLS dialer for websocket.Dialer's
// NetDialTLSContext. The certificate is verified as usual against addr's host.
func chromeTLS(dial dialFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
		if err != nil {
			return nil, err
		}
		exts := spec.Extensions[:0]
		for _, e := range spec.Extensions {
			switch e := e.(type) {
			case *utls.ALPNExtension:
				e.AlpnProtocols = []string{"http/1.1"}
			case *utls.ApplicationSettingsExtension, *utls.ApplicationSettingsExtensionNew:
				continue // ALPS only accompanies h2; Chrome drops it for a WebSocket
			}
			exts = append(exts, e)
		}
		spec.Extensions = exts
		raw, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		uc := utls.UClient(raw, &utls.Config{ServerName: host, RootCAs: tlsRoots}, utls.HelloCustom)
		if err := uc.ApplyPreset(&spec); err != nil {
			raw.Close()
			return nil, err
		}
		if err := uc.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return uc, nil
	}
}

// tlsRoots: nil = the system pool. Tests point it at httptest's CA.
var tlsRoots *x509.CertPool

var plainDial dialFunc = (&net.Dialer{}).DialContext

// clientDialer: the dialer for client → connector sockets.
var clientDialer = func() *websocket.Dialer {
	d := *websocket.DefaultDialer
	d.NetDialTLSContext = chromeTLS(plainDial)
	return &d
}()
