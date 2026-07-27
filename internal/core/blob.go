package core

// Attachments and avatars. In the DAG (replicated, signed history)
// only metadata is stored: sha256 of content, mime type, name, size. Actual bytes travel
// separately on demand in chunks over the ctrl channel — the same E2E tunnel
// (WebRTC DTLS + verified signaling) as the rest of the conversation.
//
// Benefits:
//   - chat history remains lightweight (event with video is still ~300 bytes),
//   - recipient verifies hash, so content cannot be tampered with in transit,
//   - anyone without the hash cannot request the file.
//
// Trust in incoming bytes: sha256 must match what
// SIGNED event announced, and mime type must match magic bytes
// (attach.Verify). File name is generated locally.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
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
	blobChunk    = 16 << 10 // after base64 in JSON ~22 kB per message
	maxBlobParts = attach.MaxFileBytes/blobChunk + 2
	sendBufHigh  = 1 << 20 // above this queue size sender pauses
	wantRetry    = 25 * time.Second
	maxServing   = 3 // concurrent transfers per peer
	// asmBudget: total memory ceiling across all assembly buffers. Single
	// assembly holds up to MaxFileBytes, and requests can match count of
	// attachment events sent to us (~300 bytes each).
	asmBudget = 64 << 20
)

// blobWant: attachment not yet downloaded.
type blobWant struct {
	chat   []byte
	mime   string
	size   int64
	author []byte // sender — queried first
	asked  time.Time
}

// blobAsm: chunks being assembled.
type blobAsm struct {
	parts [][]byte
	got   int
	bytes int
	born  time.Time
}

// ---- sending ----

// SendFile validates a file, stores its content locally and announces it in chat.
// Recipients request bytes when they receive the event.
//
// Files above attach.PreviewMax do not auto-replicate: sender creates
// a thumbnail and only the thumbnail is broadcast. Full bytes remain with holders
// and travel only upon tapping "download" (see FetchBlob).
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

// SendFilePath: convenient wrapper for UI — reads file from disk with size limit.
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

// SaveAttachment saves downloaded attachment in directory dir and returns path.
// Name and extension derive from detected type, not wire metadata —
// file never lands on disk as .sh or .exe.
func (c *Core) SaveAttachment(hash []byte, name, dir string) (string, error) {
	data, ok := c.St.GetBlob(hash)
	if !ok {
		return "", errors.New("załącznik jeszcze się nie pobrał")
	}
	safe := attach.SafeName(name, attach.SniffAny(data))
	ext := filepath.Ext(safe)
	base := strings.TrimSuffix(safe, ext)
	path := filepath.Join(dir, safe)
	// O_EXCL instead of Stat+WriteFile: in shared dir (e.g. /tmp) someone
	// could place a symlink under predictable name overwriting arbitrary file.
	// O_EXCL does not follow symlinks — we take the next available name.
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

// BlobBytes returns attachment content if available locally.
func (c *Core) BlobBytes(hash []byte) ([]byte, bool) { return c.St.GetBlob(hash) }

// ---- avatars ----

// SetAvatar normalizes image (re-encoding strips appended payloads),
// stores it and announces it across all active chats. crop is the region the
// user framed; the zero value takes the center of the image.
func (c *Core) SetAvatar(data []byte, crop image.Rectangle) error {
	out, mime, err := attach.Avatar(data, crop)
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
	return c.SetAvatar(data, image.Rectangle{})
}

// ClearAvatar removes avatar (locally and for peers).
func (c *Core) ClearAvatar() error { return c.announceAvatar(nil, "") }

func (c *Core) announceAvatar(hash []byte, mime string) error {
	card, _ := c.St.Profile(c.ID.Pub)
	card.Blob, card.Mime = hash, mime
	if err := c.St.PutProfile(c.ID.Pub, card); err != nil {
		return err
	}
	// one event per chat — avatar is part of signed history, visible
	// to future joiners or currently offline peers
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

// AvatarOf returns avatar bytes, mime type and content hash; nil if not yet available.
// Hash serves as UI cache key — image update changes hash.
func (c *Core) AvatarOf(pub []byte) (data []byte, mime string, hash []byte) {
	a, ok := c.St.Profile(pub)
	if !ok {
		return nil, "", nil
	}
	data, ok = c.St.GetBlob(a.Blob)
	if !ok {
		return nil, "", nil
	}
	return data, a.Mime, a.Blob
}

// ---- profile card: status, description, banner ----

// MaxStatus / MaxBio: ceilings on network-provided profile text.
const (
	MaxStatus = MaxName
	MaxBio    = 400
)

// SetStatus sets the short line under the name ("" clears it).
func (c *Core) SetStatus(text string) error {
	text = safeName(strings.TrimSpace(text))
	if len(text) > MaxStatus {
		return fmt.Errorf("status może mieć najwyżej %d znaków", MaxStatus)
	}
	return c.editCard(func(p *store.ProfileMeta) { p.Status = text })
}

// SetBio sets the longer description on the profile ("" clears it).
func (c *Core) SetBio(text string) error {
	text = safeBio(strings.TrimSpace(text))
	if len(text) > MaxBio {
		return fmt.Errorf("opis może mieć najwyżej %d znaków", MaxBio)
	}
	return c.editCard(func(p *store.ProfileMeta) { p.Bio = text })
}

// SetBanner normalizes the wide image, stores it and announces it. crop is the
// region the user framed; the zero value takes the center of the image.
func (c *Core) SetBanner(data []byte, crop image.Rectangle) error {
	out, mime, err := attach.Banner(data, crop)
	if err != nil {
		return err
	}
	hash, err := c.St.PutBlob(out)
	if err != nil {
		return err
	}
	return c.editCard(func(p *store.ProfileMeta) { p.Banner, p.BannerMime = hash, mime })
}

// ClearBanner removes the banner (locally and for peers).
func (c *Core) ClearBanner() error {
	return c.editCard(func(p *store.ProfileMeta) { p.Banner, p.BannerMime = nil, "" })
}

// editCard: mutate own card in the index, then announce the whole card.
// One event kind carries status, description and banner together, so a peer
// missing earlier events still ends up with a consistent profile.
func (c *Core) editCard(f func(*store.ProfileMeta)) error {
	card, _ := c.St.Profile(c.ID.Pub)
	f(&card)
	if err := c.St.PutProfile(c.ID.Pub, card); err != nil {
		return err
	}
	var lastErr error
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		if st, err := c.ChatState(chat); err != nil || !st.IsMember(c.ID.Pub) {
			continue
		}
		if _, err := c.appendEvent(chat, event.KindProfile, cardBody(card)); err != nil {
			lastErr = err
		}
	}
	c.Notify()
	return lastErr
}

func cardBody(card store.ProfileMeta) event.Body {
	return event.Body{Text: card.Status, Bio: card.Bio,
		Blob: card.Banner, Mime: card.BannerMime, TS: time.Now().Unix()}
}

// StatusOf returns someone's status line and description (empty when unknown).
func (c *Core) StatusOf(pub []byte) (status, bio string) {
	card, ok := c.St.Profile(pub)
	if !ok {
		return "", ""
	}
	return card.Status, card.Bio
}

// BannerOf returns banner bytes, mime type and content hash (nil when absent
// or not downloaded yet).
func (c *Core) BannerOf(pub []byte) (data []byte, mime string, hash []byte) {
	card, ok := c.St.Profile(pub)
	if !ok {
		return nil, "", nil
	}
	data, ok = c.St.GetBlob(card.Banner)
	if !ok {
		return nil, "", nil
	}
	return data, card.BannerMime, card.Banner
}

// ---- want registry ----

// blobKind: event kinds whose body points at bytes we may need to fetch.
func blobKind(kind string) bool {
	return kind == event.KindFile || kind == event.KindAvatar || kind == event.KindProfile
}

// noteAttachment: after ingesting attachment event we decrypt it to
// read content hash and queue missing bytes.
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
	// profile index: friend list and member column do not need to traverse DAG.
	// Text arrives from the network, so it is cleansed here, once, before storage.
	if e.Kind == event.KindAvatar || e.Kind == event.KindProfile {
		card, _ := c.St.Profile(e.Author)
		if e.Kind == event.KindAvatar {
			card.Blob, card.Mime = b.Blob, b.Mime
		} else {
			card.Status, card.Bio = safeName(b.Text), safeBio(b.Bio)
			card.Banner, card.BannerMime = b.Blob, b.Mime
		}
		c.St.PutProfile(e.Author, card)
	}
	// large file: download thumbnail only, full bytes on explicit request
	if len(b.Prev) == sha256.Size {
		c.wantBlob(chat, b.Prev, b.PrevMime, 0, e.Author)
		return
	}
	if len(b.Blob) != sha256.Size || b.Size > attach.MaxFileBytes {
		return
	}
	if e.Kind == event.KindFile && b.Size > attach.PreviewMax {
		return // large file without preview (video, archive) — on request only
	}
	c.wantBlob(chat, b.Blob, b.Mime, b.Size, e.Author)
}

// FetchBlob: manual "download full file". Registers request and asks immediately,
// without delay — user just clicked button.
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

// Fetching: whether this blob is currently queued (UI shows "downloading...").
func (c *Core) Fetching(hash []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wants[string(hash)] != nil
}

// wantBlob registers missing blob and requests content immediately.
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

// askBlob asks one peer for content: first the author, then any online chat member.
// One at a time — files can be tens of MBs.
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

// retryWants: retrying after online peer set changes. force skips
// delay — used when someone just connected.
func (c *Core) retryWants(force bool) {
	c.mu.Lock()
	var hashes [][]byte
	for h, w := range c.wants {
		if force {
			w.asked = time.Time{}
		}
		hashes = append(hashes, []byte(h))
	}
	// drop stuck assembly buffers
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

// rescanAttachments rebuilds missing list on startup (registry is in-memory)
// and updates avatar index.
func (c *Core) rescanAttachments() {
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		evs, err := c.St.Events(chat)
		if err != nil {
			continue
		}
		event.SortCanonical(evs) // latest avatar wins
		for _, e := range evs {
			if blobKind(e.Kind) {
				c.noteAttachment(chat, e)
			}
		}
	}
}

// ---- transport ----

// serveBlob: someone requested an attachment. We provide it only to members of the chat
// in which the blob was announced.
// We don't verify whether this exact blob is in an event of this chat —
// hash is known only from decrypted history, so the hash itself acts as a pass. Verification
// would require decrypting the entire DAG on demand.
func (c *Core) serveBlob(p *p2p.Peer, m Ctrl) {
	st, err := c.ChatState(m.Chat)
	if err != nil || !st.IsMember(p.Pub) {
		return
	}
	data, ok := c.St.GetBlob(m.Blob)
	if !ok {
		return
	}
	// one peer cannot monopolize link: max maxServing concurrent sends
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
			// backpressure check: don't push 32 MB at once
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

// handleBlobPart assembles chunks only for blobs we requested.
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
		return // unrequested
	}
	held := 0
	for _, as := range c.incoming {
		held += as.bytes
	}
	if held+len(m.Part) > asmBudget {
		c.mu.Unlock()
		return // assembly memory budget exhausted
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
	// content must strictly match what signed event promised
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
