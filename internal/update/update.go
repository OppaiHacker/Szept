// Package update finds, verifies and installs new desktop releases.
//
// A release counts only when its SHA256SUMS carries an SSH signature
// (ssh-keygen -Y sign, namespace "szept-release") by the key below. GitHub
// alone can't ship code to clients: a hijacked account or CI run produces an
// unsigned release that every client ignores. The signature covers a VERSION
// file too, so an old signed release can't pose as a new one.
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// PubKey signs releases; the private half is ~/.ssh/szept_release on the
// maintainer's machine, never in CI (see `make sign-release`).
var PubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIuzOeNYOb2nUZTvZCsDlDS9vHiF3iZMtKgK7vhNx9bs"

const namespace = "szept-release"

// Base is where release files live; a var so tests can point it elsewhere.
var Base = "https://github.com/OppaiHacker/Szept/releases/latest/download/"

var client = &http.Client{Timeout: 5 * time.Minute}

// Release: the newest signed release, files mapped to their SHA-256.
type Release struct {
	Version string
	sums    map[string]string
}

// Asset: the desktop binary built for this platform. On Windows it is named
// "portable" so people do not take it for the installer (szept-setup-*). On
// Linux it follows the GTK the running build links (flavor_gtk*.go).
func Asset() string {
	if runtime.GOOS == "windows" {
		return "szept-portable-windows-" + runtime.GOARCH + ".exe"
	}
	return "szept-wails-" + runtime.GOOS + "-" + runtime.GOARCH + flavor
}

// Latest fetches and verifies the newest release's metadata.
func Latest(ctx context.Context) (*Release, error) {
	sums, err := get(ctx, "SHA256SUMS", 64<<10)
	if err != nil {
		return nil, err
	}
	sig, err := get(ctx, "SHA256SUMS.sig", 16<<10)
	if err != nil {
		return nil, fmt.Errorf("release has no signature: %w", err)
	}
	if err := Verify(sums, sig); err != nil {
		return nil, err
	}
	r := &Release{sums: map[string]string{}}
	for _, line := range strings.Split(string(sums), "\n") {
		if h, name, ok := strings.Cut(line, "  "); ok {
			r.sums[strings.TrimPrefix(name, "*")] = h
		}
	}
	v, err := r.fetch(ctx, "VERSION", 64)
	if err != nil {
		return nil, err
	}
	r.Version = strings.TrimSpace(string(v))
	if _, ok := parse(r.Version); !ok {
		return nil, fmt.Errorf("bad release version %q", r.Version)
	}
	return r, nil
}

// Newer: is release version v later than the running cur? A cur that isn't
// a release tag (dev builds) never updates.
func Newer(v, cur string) bool {
	a, ok1 := parse(v)
	b, ok2 := parse(cur)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var n [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 || !strings.HasPrefix(v, "v") {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// Install downloads this platform's binary, checks it against the signed
// sums and swaps it in for the running executable. The new version runs
// from the next start.
func (r *Release) Install(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	bin, err := r.fetch(ctx, Asset(), 512<<20)
	if err != nil {
		return err
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	// A build linked against libraries this system lacks dies in the dynamic
	// loader before main. Find out now, while the working binary is in place.
	if runtime.GOOS == "linux" {
		if err := starts(ctx, tmp); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	// Windows won't overwrite a running .exe but will rename it.
	if runtime.GOOS == "windows" {
		os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		if runtime.GOOS == "windows" {
			os.Rename(exe+".old", exe)
		}
		return err
	}
	return nil
}

// starts runs the binary with -h, which exits 0 from flag parsing before any
// window opens.
func starts(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-h").CombinedOutput()
	if err != nil {
		return fmt.Errorf("nowa wersja nie uruchamia się na tym systemie: %v %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// Cleanup removes the executable Windows left behind after the last update.
func Cleanup() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

// fetch downloads a file listed in the sums and checks its hash.
func (r *Release) fetch(ctx context.Context, name string, max int64) ([]byte, error) {
	want, ok := r.sums[name]
	if !ok {
		return nil, fmt.Errorf("%s is not in the signed sums", name)
	}
	b, err := get(ctx, name, max)
	if err != nil {
		return nil, err
	}
	if h := sha256.Sum256(b); hex.EncodeToString(h[:]) != want {
		return nil, fmt.Errorf("%s: checksum mismatch", name)
	}
	return b, nil
}

func get(ctx context.Context, name string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", Base+name, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s: too large", name)
	}
	return b, err
}

// Verify checks an armored `ssh-keygen -Y sign` signature over msg against
// PubKey (format: PROTOCOL.sshsig in OpenSSH).
func Verify(msg, armored []byte) error {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(PubKey))
	if err != nil {
		return err
	}
	blk, _ := pem.Decode(armored)
	if blk == nil || blk.Type != "SSH SIGNATURE" || !bytes.HasPrefix(blk.Bytes, []byte("SSHSIG")) {
		return errors.New("signature: not an SSH signature")
	}
	var s struct {
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		HashAlg   string
		Signature []byte
	}
	if err := ssh.Unmarshal(blk.Bytes[6:], &s); err != nil {
		return err
	}
	if s.Version != 1 || s.Namespace != namespace || !bytes.Equal(s.PublicKey, pub.Marshal()) {
		return errors.New("signature: wrong key or namespace")
	}
	var h []byte
	switch s.HashAlg {
	case "sha512":
		x := sha512.Sum512(msg)
		h = x[:]
	case "sha256":
		x := sha256.Sum256(msg)
		h = x[:]
	default:
		return errors.New("signature: unknown hash " + s.HashAlg)
	}
	var sig ssh.Signature
	if err := ssh.Unmarshal(s.Signature, &sig); err != nil {
		return err
	}
	signed := append([]byte("SSHSIG"), ssh.Marshal(struct {
		Namespace, Reserved, HashAlg string
		Hash                         []byte
	}{namespace, "", s.HashAlg, h})...)
	return pub.Verify(signed, &sig)
}
