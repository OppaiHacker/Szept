package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Signs with a throwaway key through the real ssh-keygen, so the parser is
// checked against OpenSSH's own output, then serves a fake release.
func TestLatest(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	pub, _ := os.ReadFile(key + ".pub")
	defer func(k string) { PubKey = k }(PubKey)
	PubKey = string(pub)

	files := map[string]string{"VERSION": "v1.2.3\n", Asset(): "binary"}
	var sums strings.Builder
	for n, b := range files {
		h := sha256.Sum256([]byte(b))
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + n + "\n")
	}
	files["SHA256SUMS"] = sums.String()
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0o644)
	if out, err := exec.Command("ssh-keygen", "-Y", "sign", "-f", key, "-n", "szept-release", filepath.Join(dir, "SHA256SUMS")).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	sig, _ := os.ReadFile(filepath.Join(dir, "SHA256SUMS.sig"))
	files["SHA256SUMS.sig"] = string(sig)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(b))
	}))
	defer srv.Close()
	Base = srv.URL + "/"

	r, err := Latest(context.Background())
	if err != nil || r.Version != "v1.2.3" {
		t.Fatal(r, err)
	}
	if _, err := r.fetch(context.Background(), Asset(), 1<<20); err != nil {
		t.Fatal(err)
	}

	files[Asset()] = "tampered"
	if _, err := r.fetch(context.Background(), Asset(), 1<<20); err == nil {
		t.Fatal("tampered binary accepted")
	}
	files["SHA256SUMS"] = strings.Replace(sums.String(), "VERSION", "VERSIOn", 1)
	if _, err := Latest(context.Background()); err == nil {
		t.Fatal("tampered sums accepted")
	}
	files["SHA256SUMS"] = sums.String()
	delete(files, "SHA256SUMS.sig")
	if _, err := Latest(context.Background()); err == nil {
		t.Fatal("unsigned release accepted")
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		v, cur string
		want   bool
	}{
		{"v0.2.0", "v0.1.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v9.0.0", "dev", false},
		{"v9.0.0", "0a43ea1", false},
		{"v1.0", "v0.1.0", false},
	} {
		if got := Newer(c.v, c.cur); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.v, c.cur, got)
		}
	}
}
