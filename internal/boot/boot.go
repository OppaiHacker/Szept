// Package boot: config file + identity file plumbing shared by TUI and GUI.
package boot

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"szept/internal/core"
	"szept/internal/crypt"
	"szept/internal/identity"
	"szept/internal/store"
)

func Dirs() (cfgFile, dataDir string) {
	c, _ := os.UserConfigDir()
	h, _ := os.UserHomeDir()
	dataDir = filepath.Join(h, ".local", "share", "szept")
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		dataDir = filepath.Join(xdg, "szept")
	} else if la := os.Getenv("LOCALAPPDATA"); runtime.GOOS == "windows" && la != "" {
		// %LOCALAPPDATA% on Windows; installs from before keep their old dir
		if _, err := os.Stat(dataDir); err != nil {
			dataDir = filepath.Join(la, "szept")
		}
	}
	if c == "" {
		c = h
	}
	return filepath.Join(c, "szept", "config.json"), dataDir
}

func LoadConfig(cfgFile string) (*core.Config, error) {
	b, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}
	var cfg core.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(cfgFile string, cfg *core.Config) error {
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(cfgFile, b, 0o600)
}

func IdentityPath(dataDir string) string { return filepath.Join(dataDir, "identity.key") }

func HasIdentity(dataDir string) bool {
	_, err := os.Stat(IdentityPath(dataDir))
	return err == nil
}

func CreateIdentity(dataDir, name string, pass []byte) (*identity.Identity, error) {
	id := identity.Create()
	id.Name = name
	if err := id.Save(IdentityPath(dataDir), pass); err != nil {
		return nil, err
	}
	return id, nil
}

// LoadIdentity otwiera tożsamość. Jeśli zmiana hasła przerwała się w połowie
// (został plik .new), kończy ją albo cofa zależnie od tego, którym kluczem
// jest już zaszyfrowana baza.
func LoadIdentity(dataDir string, pass []byte) (*identity.Identity, error) {
	path, db := IdentityPath(dataDir), filepath.Join(dataDir, "szept.db")
	if _, err := os.Stat(path + ".new"); err != nil {
		return identity.Load(path, pass)
	}
	if id, err := identity.Load(path+".new", pass); err == nil {
		if store.UsesKey(db, id.DBKey) {
			return id, os.Rename(path+".new", path)
		}
		os.Remove(path + ".new")
		return nil, errors.New("zmiana hasła nie została dokończona — zaloguj się starym hasłem")
	}
	id, err := identity.Load(path, pass)
	if err != nil {
		return nil, err
	}
	if !store.UsesKey(db, id.DBKey) {
		return nil, errors.New("hasło zostało zmienione — zaloguj się nowym hasłem")
	}
	os.Remove(path + ".new")
	return id, nil
}

// ChangePass szyfruje tożsamość nowym hasłem, a bazę nowym losowym kluczem
// (rekey to Store.Rekey otwartej bazy). Stara kopia pliku tożsamości ze
// starym hasłem nie otwiera wtedy obecnej bazy. Stare hasło jest weryfikowane
// wprost, bo literówka odcinałaby konto. Kolejność: nowy plik obok starego,
// rekey bazy (jedna transakcja), podmiana pliku; LoadIdentity sprząta po
// awarii między tymi krokami.
func ChangePass(dataDir string, oldPass, newPass []byte, rekey func([]byte) error) error {
	path := IdentityPath(dataDir)
	id, err := identity.Load(path, oldPass)
	if err != nil {
		return err
	}
	nid := *id
	nid.DBKey = crypt.NewKey()
	if err := nid.Save(path+".new", newPass); err != nil {
		return err
	}
	if err := rekey(nid.DBKey); err != nil {
		os.Remove(path + ".new")
		return err
	}
	return os.Rename(path+".new", path)
}

// ---- listy connectorów ----

// FetchConnectorLists pobiera listy i zwraca znalezione adresy ws://…
// Tylko https:// i bez przekierowań: lista po http:// mówi DPI, że ktoś
// używa Szeptu, a przekierowanie mogłoby skierować żądanie do sieci lokalnej.
func FetchConnectorLists(urls []string) (found []string, errs int) {
	cl := &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, url := range urls {
		if !strings.HasPrefix(strings.ToLower(url), "https://") {
			errs++
			continue
		}
		resp, err := cl.Get(url)
		if err != nil {
			errs++
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			errs++
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ws://") || strings.HasPrefix(line, "wss://") {
				found = append(found, core.NormalizeWS(line))
			}
		}
	}
	return found, errs
}

// AppendUnique dokłada wartość, jeśli jeszcze jej nie ma.
func AppendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// ---- konta (profile): osobne katalogi danych na jednym urządzeniu ----

// ProfileDir: katalog danych konta o podanej nazwie; "" = katalog domyślny.
func ProfileDir(name string) string {
	_, base := Dirs()
	if name == "" {
		return base
	}
	return filepath.Join(base, "profiles", name)
}

// Profiles: istniejące konta = podkatalogi profiles/ (z domyślnym na czele).
func Profiles() []string {
	out := []string{""}
	ents, _ := os.ReadDir(filepath.Join(ProfileDir(""), "profiles"))
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// ---- backup & restore ----
//
// Kopia to zip z identity.key i migawką bazy. Obie rzeczy są zaszyfrowane
// w spoczynku (klucz bazy pochodzi z seeda, seed chroni hasło tożsamości) —
// plik kopii bez hasła tożsamości czyta się tylko wtedy, gdy hasła nie było.

// Backup pisze zip: identity.key + szept.db (migawka przez snapshot).
func Backup(dataDir string, snapshot func(io.Writer) error, w io.Writer) error {
	z := zip.NewWriter(w)
	idBytes, err := os.ReadFile(IdentityPath(dataDir))
	if err != nil {
		return err
	}
	f, err := z.Create("identity.key")
	if err != nil {
		return err
	}
	if _, err := f.Write(idBytes); err != nil {
		return err
	}
	if f, err = z.Create("szept.db"); err != nil {
		return err
	}
	if err := snapshot(f); err != nil {
		return err
	}
	return z.Close()
}

// Restore rozpakowuje kopię do WSKAZANEGO katalogu (nowy profil) — nigdy nie
// nadpisuje otwartej bazy. Przełączenie na profil wymaga restartu aplikacji.
func Restore(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	want := map[string]bool{"identity.key": true, "szept.db": true}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	got := map[string]bool{}
	for _, f := range r.File {
		if !want[f.Name] {
			continue // obce wpisy (w tym ścieżki z ../) ignorujemy w całości
		}
		if got[f.Name] {
			return errors.New("uszkodzona kopia: " + f.Name + " występuje dwa razy")
		}
		if err := unzipEntry(f, filepath.Join(destDir, f.Name)); err != nil {
			return err
		}
		got[f.Name] = true
	}
	if len(got) != len(want) {
		return errors.New("to nie jest kopia szeptu (brak identity.key/szept.db)")
	}
	return nil
}

// maxRestoreBytes: sufit na rozpakowany wpis. „Kopia od znajomego” to realny
// scenariusz, a zip potrafi rozprężyć się dowolnie — czytamy strumieniowo
// (żadnego ReadAll całości do RAM) i przerywamy po przekroczeniu limitu.
const maxRestoreBytes = 512 << 20

func unzipEntry(f *zip.File, dest string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	// limit+1: gdy uda się przepisać o bajt więcej niż limit, wpis jest za duży
	n, err := io.Copy(out, io.LimitReader(src, maxRestoreBytes+1))
	if err != nil {
		return err
	}
	if n > maxRestoreBytes {
		os.Remove(dest)
		return fmt.Errorf("wpis %s w kopii jest za duży (limit %d MB)", f.Name, maxRestoreBytes>>20)
	}
	return out.Close()
}
