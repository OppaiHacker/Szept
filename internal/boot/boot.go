// Package boot: config file + identity file plumbing shared by TUI and GUI.
package boot

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"szept/internal/core"
	"szept/internal/identity"
)

func Dirs() (cfgFile, dataDir string) {
	c, _ := os.UserConfigDir()
	h, _ := os.UserHomeDir()
	dataDir = filepath.Join(h, ".local", "share", "szept")
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		dataDir = filepath.Join(xdg, "szept")
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

func LoadIdentity(dataDir string, pass []byte) (*identity.Identity, error) {
	return identity.Load(IdentityPath(dataDir), pass)
}

// ChangePass przeszyfrowuje plik tożsamości nowym hasłem. Stare hasło jest
// weryfikowane wprost — bez tego literówka odcinałaby konto na zawsze.
// Klucz bazy pochodzi z seeda (nie z hasła), więc baza zostaje nietknięta.
func ChangePass(dataDir string, oldPass, newPass []byte) error {
	id, err := identity.Load(IdentityPath(dataDir), oldPass)
	if err != nil {
		return err
	}
	return id.Save(IdentityPath(dataDir), newPass)
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
	found := 0
	for _, f := range r.File {
		if !want[f.Name] {
			continue // obce wpisy (w tym ścieżki z ../) ignorujemy w całości
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(src, 1<<31))
		src.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, f.Name), data, 0o600); err != nil {
			return err
		}
		found++
	}
	if found != 2 {
		return errors.New("to nie jest kopia szeptu (brak identity.key/szept.db)")
	}
	return nil
}
