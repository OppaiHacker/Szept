package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

var (
	mu      sync.RWMutex
	current = "pl"
	data    = map[string]map[string]string{}
)

func init() {
	load("pl")
	load("en")
}

func load(lang string) {
	b, err := localeFS.ReadFile("locales/" + lang + ".json")
	if err != nil {
		return
	}
	var m map[string]string
	if json.Unmarshal(b, &m) == nil {
		data[lang] = m
	}
}

// SetLang sets active language ("pl" or "en"). Returns current active language.
func SetLang(lang string) string {
	mu.Lock()
	defer mu.Unlock()
	if lang == "en" || lang == "pl" {
		current = lang
	}
	return current
}

// Lang returns current active language.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T translates a message key with optional format arguments.
func T(key string, args ...any) string {
	mu.RLock()
	lang := current
	m := data[lang]
	msg, ok := m[key]
	mu.RUnlock()
	if !ok {
		// fallback to en
		mu.RLock()
		if mEn := data["en"]; mEn != nil {
			if msgEn, okEn := mEn[key]; okEn {
				msg = msgEn
			} else {
				msg = key
			}
		} else {
			msg = key
		}
		mu.RUnlock()
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}
