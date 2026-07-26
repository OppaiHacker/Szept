// Package boot: config file + identity file plumbing shared by TUI and GUI.
package boot

import (
	"encoding/json"
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
