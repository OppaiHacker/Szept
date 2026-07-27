package core

// Webhook: lokalny most dla botów i skryptów. Nasłuch TYLKO na 127.0.0.1 —
// wiadomość wysyła każdy proces na tym komputerze, który zna token.
//
//	curl -X POST http://127.0.0.1:PORT/send \
//	  -H "Authorization: Bearer TOKEN" \
//	  -d '{"chat":"nazwa czatu","text":"treść"}'

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"szept/internal/crypt"
)

// NewWebhookToken: 32 losowe bajty w base64url — do wklejenia w skrypt.
func NewWebhookToken() string {
	return base64.RawURLEncoding.EncodeToString(crypt.RandBytes(32))
}

func (c *Core) WebhookOn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.webhook != nil
}

func (c *Core) StartWebhook(port int, token string) error {
	c.StopWebhook()
	if token == "" || port <= 0 {
		return fmt.Errorf("webhook: potrzebny port i token")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(auth), []byte(token)) != 1 {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		var req struct {
			Chat string `json:"chat"`
			Text string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil ||
			req.Chat == "" || req.Text == "" {
			http.Error(w, "want {\"chat\":..,\"text\":..}", http.StatusBadRequest)
			return
		}
		for _, ci := range c.Chats() {
			if ci.Name == req.Chat {
				if err := c.Send(ci.ID, req.Text); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.Error(w, "no such chat", http.StatusNotFound)
	})
	srv := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	c.mu.Lock()
	c.webhook = srv
	c.mu.Unlock()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("webhook: %v", err)
		}
	}()
	return nil
}

func (c *Core) StopWebhook() {
	c.mu.Lock()
	srv := c.webhook
	c.webhook = nil
	c.mu.Unlock()
	if srv != nil {
		srv.Close()
	}
}
