// szept: terminalowy klient komunikatora.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/identity"
	"szept/internal/tui"
)

func prompt(r *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptPass(label string) []byte {
	fmt.Print(label + ": ")
	p, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return nil
	}
	return p
}

func main() {
	dataFlag := flag.String("data", "", "katalog danych (domyślnie ~/.local/share/szept)")
	connFlag := flag.String("connector", "", "adres connectora, np. ws://serwer:7777")
	flag.Parse()

	cfgFile, dataDir := boot.Dirs()
	if *dataFlag != "" {
		dataDir = *dataFlag
	}
	cfg, _ := boot.LoadConfig(cfgFile)
	if cfg == nil {
		cfg = &core.Config{}
	}
	if *connFlag != "" {
		cfg.Connector = *connFlag
	}
	cfg.DataDir = dataDir

	r := bufio.NewReader(os.Stdin)
	var id *identity.Identity
	var err error

	if !boot.HasIdentity(dataDir) {
		fmt.Println("== szept: pierwsze uruchomienie ==")
		name := ""
		for name == "" {
			name = prompt(r, "Twoja nazwa", "")
		}
		for cfg.Connector == "" {
			cfg.Connector = prompt(r, "Adres connectora (np. ws://1.2.3.4:7777)", "")
		}
		pass := promptPass("Hasło do klucza (Enter = bez hasła)")
		if len(pass) > 0 {
			again := promptPass("Powtórz hasło")
			if string(pass) != string(again) {
				fmt.Println("Hasła się różnią.")
				os.Exit(1)
			}
		}
		id, err = boot.CreateIdentity(dataDir, name, pass)
		if err != nil {
			fmt.Println("Błąd tworzenia tożsamości:", err)
			os.Exit(1)
		}
		if err := boot.SaveConfig(cfgFile, cfg); err != nil {
			fmt.Println("Błąd zapisu konfiguracji:", err)
			os.Exit(1)
		}
		fmt.Println("Twój odcisk klucza:", identity.Fingerprint(id.Pub))
	} else {
		// try empty passphrase silently first
		id, err = boot.LoadIdentity(dataDir, nil)
		for err != nil {
			pass := promptPass("Hasło do klucza")
			id, err = boot.LoadIdentity(dataDir, pass)
			if err != nil {
				fmt.Println("Nie udało się odszyfrować klucza.")
			}
		}
		if cfg.Connector == "" {
			cfg.Connector = prompt(r, "Adres connectora", "")
			boot.SaveConfig(cfgFile, cfg)
		}
	}

	c, err := core.Open(*cfg, id)
	if err != nil {
		fmt.Println("Błąd otwarcia bazy:", err)
		os.Exit(1)
	}
	defer c.Close()
	c.Start()

	if err := tui.Run(c); err != nil {
		fmt.Println("TUI:", err)
		os.Exit(1)
	}
}
