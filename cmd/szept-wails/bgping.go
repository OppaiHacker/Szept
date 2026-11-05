package main

import (
	"regexp"
	"strings"

	"szept/internal/core"
)

// Background pings, Android only. While the activity is stopped the page may
// be gone (Android destroys activities freely; the foreground service keeps
// the process and Go alive), so Go posts the notifications that notifyPings
// in the frontend (store/notify.ts) would. While a.bg is set the page's own Notify calls are dropped,
// so a ping is never shown twice.

// setBG is called on android:ActivityStopped (true) and ActivityStarted (false).
func (a *App) setBG(on bool) {
	c, _ := a.live()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.bg = on
	a.bgSeen = map[string]int{}
	if on && c != nil {
		for _, ci := range c.Chats() {
			a.bgSeen[hexs(ci.ID)] = c.St.EventCount(ci.ID)
		}
	}
}

// bgPings runs after each coalesced core event batch, see watch.
func (a *App) bgPings(c *core.Core) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.bg || a.notify == nil || a.cfg.NoNotify {
		return
	}
	for _, ci := range c.Chats() {
		id, n := hexs(ci.ID), c.St.EventCount(ci.ID)
		was, known := a.bgSeen[id]
		a.bgSeen[id] = n
		if known && n <= was {
			continue
		}
		msgs := c.Messages(ci.ID)
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.Kind == "info" {
				continue
			}
			if !c.Mine(m.Author) && (ci.Direct || pingsMe(m.Text, c.ID.Name)) {
				_ = a.notify("szept-"+id, ci.Name, a.preview(c, ci).text)
			}
			break
		}
	}
}

// pingsMe mirrors pingsMe in frontend/src/lib/text.ts: @name or @everyone/@here as a whole word.
func pingsMe(text, me string) bool {
	if !strings.Contains(text, "@") {
		return false
	}
	t := strings.ToLower(text)
	for _, n := range []string{strings.ToLower(me), "everyone", "here"} {
		if n != "" && regexp.MustCompile(`(^|[\s(])@`+regexp.QuoteMeta(n)+`($|[\s.,!?;:)])`).MatchString(t) {
			return true
		}
	}
	return false
}
