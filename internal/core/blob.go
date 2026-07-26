package core

// Załączniki i awatary. W DAG-u (a więc w replikowanej, podpisanej historii)
// leży tylko metryczka: sha256 treści, typ, nazwa, rozmiar. Same bajty jadą
// osobno, na żądanie, kawałkami po kanale ctrl — czyli tym samym E2E tunelem
// (WebRTC DTLS + weryfikowany sygnaling), co reszta rozmowy.
//
// Dzięki temu:
//   - historia czatu zostaje lekka (event z filmem ma nadal ~300 bajtów),
//   - odbiorca sprawdza skrót, więc nikt nie podmieni treści po drodze,
//   - kto nie zna skrótu, nie ma jak poprosić o plik.
//
// Zaufanie do przychodzących bajtów: sha256 musi się zgadzać z tym, co
// zapowiedział PODPISANY event, a typ musi zgadzać się z magicznymi bajtami
// (attach.Verify). Nazwę pliku i tak generujemy sami.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"szept/internal/attach"
	"szept/internal/event"
	"szept/internal/p2p"
	"szept/internal/store"
)

const (
	blobChunk    = 16 << 10 // po base64 w JSON-ie ~22 kB na wiadomość
	maxBlobParts = attach.MaxFileBytes/blobChunk + 2
	sendBufHigh  = 1 << 20 // powyżej tylu bajtów w kolejce nadawca czeka
	wantRetry    = 25 * time.Second
	maxServing   = 3 // równoległych wysyłek na jednego peera
	// asmBudget: łączny sufit pamięci na wszystkie składanki naraz. Pojedyncza
	// składanka mieści do MaxFileBytes, a zamówień może być tyle, ile zdarzeń
	// z załącznikiem ktoś nam wyśle (jedno waży ~300 bajtów).
	asmBudget = 64 << 20
)

// blobWant: załącznik, którego jeszcze nie mamy.
type blobWant struct {
	chat   []byte
	mime   string
	size   int64
	author []byte // ten, kto go wysłał — pytamy najpierw jego
	asked  time.Time
}

// blobAsm: kawałki składane w całość.
type blobAsm struct {
	parts [][]byte
	got   int
	bytes int
	born  time.Time
}

// ---- wysyłanie ----

// SendFile waliduje plik, zapisuje jego treść lokalnie i ogłasza go w czacie.
// Odbiorcy sami poproszą o bajty, gdy dostaną event.
//
// Pliki powyżej attach.PreviewMax nie rozchodzą się same: nadawca robi
// miniaturę i tylko ona jedzie do wszystkich. Pełne bajty leżą u tych, którzy
// je mają, i wędrują dopiero po naciśnięciu „pobierz” (patrz FetchBlob).
func (c *Core) SendFile(chat []byte, name string, data []byte, caption string) error {
	meta, err := attach.Check(data, name)
	if err != nil {
		return err
	}
	hash, err := c.St.PutBlob(data)
	if err != nil {
		return err
	}
	body := event.Body{
		Blob: hash, Mime: meta.Mime, Name: meta.Name, Size: meta.Size,
		W: meta.W, H: meta.H, Text: caption, TS: time.Now().Unix(),
	}
	if meta.Size > attach.PreviewMax {
		if prev, _, _, err := attach.Preview(data, meta.Mime); err == nil {
			if ph, err := c.St.PutBlob(prev); err == nil {
				body.Prev, body.PrevMime = ph, "image/jpeg"
			}
		}
	}
	_, err = c.appendEvent(chat, event.KindFile, body)
	return err
}

// SendFilePath: wygodne opakowanie dla UI — czyta plik z dysku z limitem.
func (c *Core) SendFilePath(chat []byte, path string, caption string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() > attach.MaxFileBytes {
		return fmt.Errorf("plik ma %s — limit to %s",
			attach.HumanSize(fi.Size()), attach.HumanSize(attach.MaxFileBytes))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return c.SendFile(chat, fi.Name(), data, caption)
}

// SaveAttachment zapisuje pobrany załącznik w katalogu dir i zwraca ścieżkę.
// Nazwa i rozszerzenie pochodzą z wykrytego typu, nie z tego, co przyszło
// po sieci — plik nigdy nie wyląduje na dysku jako .sh czy .exe.
func (c *Core) SaveAttachment(hash []byte, name, dir string) (string, error) {
	data, ok := c.St.GetBlob(hash)
	if !ok {
		return "", errors.New("załącznik jeszcze się nie pobrał")
	}
	safe := attach.SafeName(name, attach.SniffAny(data))
	ext := filepath.Ext(safe)
	base := strings.TrimSuffix(safe, ext)
	path := filepath.Join(dir, safe)
	// O_EXCL zamiast Stat+WriteFile: w katalogu współdzielonym (np. /tmp) ktoś
	// podstawia dowiązanie pod przewidywalną nazwę i zapis idzie w cudzy plik.
	// O_EXCL nie pójdzie za dowiązaniem — po prostu bierzemy następną nazwę.
	for i := 1; ; i++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.Write(data)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			return path, werr
		}
		if !os.IsExist(err) || i >= 100 {
			return "", err
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
	}
}

// BlobBytes zwraca treść załącznika, jeśli już ją mamy.
func (c *Core) BlobBytes(hash []byte) ([]byte, bool) { return c.St.GetBlob(hash) }

// ---- awatary ----

// SetAvatar normalizuje obrazek (przekodowanie = koniec z doklejonym
// ładunkiem), zapisuje go i ogłasza w każdym czacie, w którym jesteśmy.
func (c *Core) SetAvatar(data []byte) error {
	out, mime, err := attach.Avatar(data)
	if err != nil {
		return err
	}
	hash, err := c.St.PutBlob(out)
	if err != nil {
		return err
	}
	return c.announceAvatar(hash, mime)
}

func (c *Core) SetAvatarPath(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() > attach.MaxAvatarBytes {
		return fmt.Errorf("obrazek ma %s — limit to %s",
			attach.HumanSize(fi.Size()), attach.HumanSize(attach.MaxAvatarBytes))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return c.SetAvatar(data)
}

// ClearAvatar kasuje awatar (u nas i u pozostałych).
func (c *Core) ClearAvatar() error { return c.announceAvatar(nil, "") }

func (c *Core) announceAvatar(hash []byte, mime string) error {
	if err := c.St.PutAvatar(c.ID.Pub, store.AvatarMeta{Blob: hash, Mime: mime}); err != nil {
		return err
	}
	// jeden event na czat — awatar jest częścią podpisanej historii, więc widzą
	// go też ci, którzy dołączą później albo są teraz offline
	var lastErr error
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		st, err := c.ChatState(chat)
		if err != nil || !st.IsMember(c.ID.Pub) {
			continue
		}
		if _, err := c.appendEvent(chat, event.KindAvatar,
			event.Body{Blob: hash, Mime: mime, TS: time.Now().Unix()}); err != nil {
			lastErr = err
		}
	}
	c.Notify()
	return lastErr
}

// AvatarOf zwraca bajty awatara, jego typ i skrót treści; nil, gdy go (jeszcze)
// nie mamy. Skrót nadaje się na klucz cache w UI — zmiana obrazka zmienia skrót.
func (c *Core) AvatarOf(pub []byte) (data []byte, mime string, hash []byte) {
	a, ok := c.St.Avatar(pub)
	if !ok {
		return nil, "", nil
	}
	data, ok = c.St.GetBlob(a.Blob)
	if !ok {
		return nil, "", nil
	}
	return data, a.Mime, a.Blob
}

// ---- rejestr braków ----

// noteAttachment: po przyjęciu eventu z załącznikiem odszyfrowujemy go, żeby
// poznać skrót treści, i zamawiamy brakujące bajty.
func (c *Core) noteAttachment(chat []byte, e *event.Event) {
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		return
	}
	key, ok := keys[e.Epoch]
	if !ok {
		return
	}
	b, err := e.Decrypt(key)
	if err != nil {
		return
	}
	if e.Kind == event.KindAvatar {
		// indeks awatarów: lista znajomych nie musi rozwijać DAG-a
		c.St.PutAvatar(e.Author, store.AvatarMeta{Blob: b.Blob, Mime: b.Mime})
	}
	// duży plik: ściągamy sam podgląd, pełne bajty tylko na wyraźne życzenie
	if len(b.Prev) == sha256.Size {
		c.wantBlob(chat, b.Prev, b.PrevMime, 0, e.Author)
		return
	}
	if len(b.Blob) != sha256.Size || b.Size > attach.MaxFileBytes {
		return
	}
	if e.Kind == event.KindFile && b.Size > attach.PreviewMax {
		return // duży plik bez podglądu (film, archiwum) — tylko na życzenie
	}
	c.wantBlob(chat, b.Blob, b.Mime, b.Size, e.Author)
}

// FetchBlob: ręczne „pobierz pełny plik”. Rejestruje brak i pyta natychmiast,
// bez odczekania — użytkownik właśnie nacisnął przycisk.
func (c *Core) FetchBlob(chat, hash []byte, mime string, size int64, author []byte) {
	if len(hash) != sha256.Size || c.St.HasBlob(hash) {
		return
	}
	c.wantBlob(chat, hash, mime, size, author)
	c.mu.Lock()
	if w := c.wants[string(hash)]; w != nil {
		w.asked = time.Time{}
	}
	c.mu.Unlock()
	c.askBlob(hash)
}

// Fetching: czy ten blob jest właśnie w kolejce (UI pokazuje „pobieram…”).
func (c *Core) Fetching(hash []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wants[string(hash)] != nil
}

// wantBlob rejestruje brak i od razu prosi o treść.
func (c *Core) wantBlob(chat, hash []byte, mime string, size int64, author []byte) {
	if c.St.HasBlob(hash) {
		return
	}
	c.mu.Lock()
	if _, dup := c.wants[string(hash)]; dup {
		c.mu.Unlock()
		return
	}
	c.wants[string(hash)] = &blobWant{
		chat: bytes.Clone(chat), mime: mime, size: size, author: bytes.Clone(author),
	}
	c.mu.Unlock()
	c.askBlob(hash)
}

// askBlob pyta o treść jednego peera: najpierw autora, potem kogokolwiek
// z czatu, kto jest online. Jeden na raz — plik potrafi ważyć dziesiątki MB.
func (c *Core) askBlob(hash []byte) {
	c.mu.Lock()
	w := c.wants[string(hash)]
	if w == nil || time.Since(w.asked) < wantRetry {
		c.mu.Unlock()
		return
	}
	w.asked = time.Now()
	chat, author := w.chat, w.author
	c.mu.Unlock()

	st, err := c.ChatState(chat)
	if err != nil {
		return
	}
	var target *p2p.Peer
	if len(author) > 0 && !bytes.Equal(author, c.ID.Pub) && st.IsMember(author) {
		target = c.P2P.Peer(author)
	}
	if target == nil {
		for _, m := range st.Members {
			if m.Removed || bytes.Equal(m.Pub, c.ID.Pub) {
				continue
			}
			if p := c.P2P.Peer(m.Pub); p != nil {
				target = p
				break
			}
		}
	}
	if target != nil {
		target.SendCtrl(Ctrl{T: "blobreq", Chat: chat, Blob: hash})
	}
}

// retryWants: ponawianie po zmianie tego, kto jest online. force pomija
// odczekanie — używane, gdy właśnie ktoś wszedł na łącze.
func (c *Core) retryWants(force bool) {
	c.mu.Lock()
	var hashes [][]byte
	for h, w := range c.wants {
		if force {
			w.asked = time.Time{}
		}
		hashes = append(hashes, []byte(h))
	}
	// porzuć składanki, które utknęły
	for h, a := range c.incoming {
		if time.Since(a.born) > 5*time.Minute {
			delete(c.incoming, h)
		}
	}
	c.mu.Unlock()
	for _, h := range hashes {
		c.askBlob(h)
	}
}

// rescanAttachments odtwarza listę braków po starcie programu (rejestr żyje
// w pamięci) i uzupełnia indeks awatarów.
func (c *Core) rescanAttachments() {
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		evs, err := c.St.Events(chat)
		if err != nil {
			continue
		}
		event.SortCanonical(evs) // najświeższy awatar ma wygrać
		for _, e := range evs {
			if e.Kind == event.KindFile || e.Kind == event.KindAvatar {
				c.noteAttachment(chat, e)
			}
		}
	}
}

// ---- transport ----

// serveBlob: ktoś prosi o załącznik. Dajemy go tylko członkowi czatu, w którym
// blob został ogłoszony.
// ponytail: nie sprawdzamy, czy ten konkretny blob wisi w evencie tego czatu —
// skrót zna się wyłącznie z odszyfrowanej historii, więc sam skrót działa tu
// jak przepustka. Weryfikacja wymagałaby odszyfrowania całego DAG-a na żądanie.
func (c *Core) serveBlob(p *p2p.Peer, m Ctrl) {
	st, err := c.ChatState(m.Chat)
	if err != nil || !st.IsMember(p.Pub) {
		return
	}
	data, ok := c.St.GetBlob(m.Blob)
	if !ok {
		return
	}
	// jeden peer nie zajmie nam łącza w kółko: naraz najwyżej maxServing wysyłek
	c.mu.Lock()
	if c.serving[string(p.Pub)] >= maxServing {
		c.mu.Unlock()
		return
	}
	c.serving[string(p.Pub)]++
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			c.serving[string(p.Pub)]--
			c.mu.Unlock()
		}()
		total := (len(data) + blobChunk - 1) / blobChunk
		for idx := 0; idx < total; idx++ {
			end := (idx + 1) * blobChunk
			if end > len(data) {
				end = len(data)
			}
			// przeciwdziałanie zapchaniu bufora: 32 MB nie wpychamy naraz
			for i := 0; p.BufferedCtrl() > sendBufHigh && i < 500; i++ {
				time.Sleep(20 * time.Millisecond)
			}
			if err := p.SendCtrl(Ctrl{
				T: "blobpart", Chat: m.Chat, Blob: m.Blob,
				Idx: idx, Total: total, Part: data[idx*blobChunk : end],
			}); err != nil {
				return
			}
		}
	}()
}

// handleBlobPart składa kawałki tylko dla blobów, o które sami prosiliśmy.
func (c *Core) handleBlobPart(p *p2p.Peer, m Ctrl) {
	if m.Total <= 0 || m.Total > maxBlobParts || m.Idx < 0 || m.Idx >= m.Total ||
		len(m.Part) == 0 || len(m.Part) > blobChunk {
		return
	}
	key := string(m.Blob)
	c.mu.Lock()
	w := c.wants[key]
	if w == nil || !bytes.Equal(w.chat, m.Chat) {
		c.mu.Unlock()
		return // nikt tego nie zamawiał
	}
	held := 0
	for _, as := range c.incoming {
		held += as.bytes
	}
	if held+len(m.Part) > asmBudget {
		c.mu.Unlock()
		return // budżet pamięci na składanki wyczerpany
	}
	a := c.incoming[key]
	if a == nil || len(a.parts) != m.Total {
		a = &blobAsm{parts: make([][]byte, m.Total), born: time.Now()}
		c.incoming[key] = a
	}
	if a.parts[m.Idx] == nil {
		a.parts[m.Idx] = bytes.Clone(m.Part)
		a.got++
		a.bytes += len(m.Part)
	}
	over := a.bytes > attach.MaxFileBytes
	done := a.got == m.Total
	var data []byte
	if done || over {
		delete(c.incoming, key)
		if done && !over {
			data = bytes.Join(a.parts, nil)
		}
	}
	mime := w.mime
	c.mu.Unlock()
	if data == nil {
		return
	}
	// treść musi być dokładnie tym, co obiecywał podpisany event
	if sum := sha256.Sum256(data); !bytes.Equal(sum[:], m.Blob) {
		return
	}
	if err := attach.Verify(data, mime); err != nil {
		return
	}
	if _, err := c.St.PutBlob(data); err != nil {
		return
	}
	c.mu.Lock()
	delete(c.wants, key)
	c.mu.Unlock()
	c.Notify()
}
