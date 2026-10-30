package core

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freePort: port, na którym da się nasłuchiwać w tej chwili.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestWebhookAuth(t *testing.T) {
	c := newCore(t, "ala", "ws://127.0.0.1:9/")
	chat, err := c.CreateChat("ekipa", false)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	if err := c.StartWebhook(port, "sekret"); err != nil {
		t.Fatal(err)
	}
	defer c.StopWebhook()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/send"
	post := func(token, body string) int {
		req, _ := http.NewRequest("POST", url, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		var resp *http.Response
		// serwer wstaje w tle — kilka prób zamiast sleepa na oko
		for i := 0; i < 40; i++ {
			resp, err = http.DefaultClient.Do(req)
			if err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
			req, _ = http.NewRequest("POST", url, strings.NewReader(body))
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post("", `{"chat":"ekipa","text":"hej"}`); got != http.StatusUnauthorized {
		t.Fatalf("bez tokenu: %d", got)
	}
	if got := post("zły", `{"chat":"ekipa","text":"hej"}`); got != http.StatusUnauthorized {
		t.Fatalf("zły token: %d", got)
	}
	if got := post("sekret", `{"chat":"nie ma","text":"hej"}`); got != http.StatusNotFound {
		t.Fatalf("nieznany czat: %d", got)
	}
	if got := post("sekret", `{"chat":"ekipa"}`); got != http.StatusBadRequest {
		t.Fatalf("brak tekstu: %d", got)
	}
	if got := post("sekret", `{"chat":"ekipa","text":"hej"}`); got != http.StatusNoContent {
		t.Fatalf("poprawne żądanie: %d", got)
	}
	if msgs := c.Messages(chat); len(msgs) == 0 || msgs[len(msgs)-1].Text != "hej" {
		t.Fatalf("wiadomość nie doszła do czatu: %+v", msgs)
	}

	c.StopWebhook()
	if c.WebhookOn() {
		t.Fatal("webhook nadal włączony po StopWebhook")
	}
}
