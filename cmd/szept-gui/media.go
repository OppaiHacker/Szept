package main

// Attachments, avatars, and gallery: cached decoding, playing
// GIFs (fyne does not animate them natively), file tiles in chat, and selecting
// outbound files — local thumbnail grid or system file picker.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	xdraw "golang.org/x/image/draw"

	"szept/internal/attach"
	"szept/internal/boot"
	"szept/internal/core"
)

const (
	// maxAnims: animate only the last few GIFs. Frames are stored
	// unpacked in RAM, so a chat full of GIFs would exhaust phone memory.
	maxAnims = 6
	// cacheMax: number of unpacked images/GIFs kept in memory.
	cacheMax = 16
	tickStep = 80 * time.Millisecond

	avatarSmall = float32(30) // next to message bubble
	avatarRow   = float32(38) // friend list / members
	avatarBig   = float32(48) // chat row
	avatarHuge  = float32(96) // profile page

	// galleryMax: max files shown in grid. Photo directories can
	// hold thousands of items, and each thumbnail requires JPEG decoding.
	galleryMax = 30
	thumbSide  = float32(104)
	// thumbMax: thumbnail limit — decoding a 40-megapixel
	// photo on a phone costs seconds and hundreds of megabytes of RAM.
	thumbMax = 12 << 20
)

type gifClip struct {
	frames []image.Image
	delays []time.Duration
}

// anim: single animated GIF. All instances advance from a single ticker.
type anim struct {
	clip *gifClip
	img  *canvas.Image
	idx  int
	acc  time.Duration
}

// startAnims launches the application's single animation timer. The u.anims list
// is rebuilt during every renderMessages — both run on the UI thread (fyne.Do),
// so there is no race condition here.
func (u *ui) startAnims() {
	go func() {
		for range time.Tick(tickStep) {
			fyne.Do(u.stepAnims)
		}
	}()
}

func (u *ui) stepAnims() {
	for _, a := range append(append([]*anim{}, u.anims...), u.msgAnims...) {
		a.acc += tickStep
		for a.acc >= a.clip.delays[a.idx] {
			a.acc -= a.clip.delays[a.idx]
			a.idx = (a.idx + 1) % len(a.clip.frames)
		}
		if a.img.Image != a.clip.frames[a.idx] {
			a.img.Image = a.clip.frames[a.idx]
			a.img.Refresh()
		}
	}
}

// cacheGet/cachePut: primitive cache for decoded content, key = blob hash
// (+ variant suffix). Blind cache wipe on overflow — decoding
// takes milliseconds, so cache misses cause no visible lag.
func (u *ui) cacheGet(key string) any { return u.decoded[key] }

func (u *ui) cachePut(key string, v any) {
	if len(u.decoded) >= cacheMax {
		for k := range u.decoded {
			delete(u.decoded, k)
			if len(u.decoded) < cacheMax {
				break
			}
		}
	}
	u.decoded[key] = v
}

// still: static image (first GIF frame when animation is disabled).
func (u *ui) still(key string, data []byte, round bool) image.Image {
	ck := key + "|s"
	if round {
		ck += "r"
	}
	if v, ok := u.cacheGet(ck).(image.Image); ok {
		return v
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	if round {
		img = circleMask(img)
	}
	u.cachePut(ck, img)
	return img
}

// clip: GIF frames ready for playback (nil if unsupported — UI
// falls back to static image).
func (u *ui) clip(key string, data []byte, round bool) *gifClip {
	ck := key + "|g"
	if round {
		ck += "r"
	}
	if v, ok := u.cacheGet(ck).(*gifClip); ok {
		return v
	}
	frames, delays, err := attach.GifFrames(data)
	if err != nil || len(frames) < 2 {
		return nil
	}
	for i := range delays {
		if delays[i] <= 0 {
			delays[i] = 100 * time.Millisecond
		}
		if round {
			frames[i] = circleMask(frames[i])
		}
	}
	c := &gifClip{frames: frames, delays: delays}
	u.cachePut(ck, c)
	return c
}

// circleMask crops a circle from an image (avatars) — fyne cannot clip.
func circleMask(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	r := cx
	if cy < r {
		r = cy
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := dx*dx + dy*dy
			if d > r*r {
				continue
			}
			cr, cg, cb, ca := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if ca == 0 {
				continue
			}
			// opacity calculated from premultiplied RGBA
			dst.Set(x, y, color.NRGBA{
				R: uint8(cr * 0xff / ca), G: uint8(cg * 0xff / ca),
				B: uint8(cb * 0xff / ca), A: uint8(ca >> 8),
			})
		}
	}
	return dst
}

// ---- avatars ----

// avatarView: circle with avatar or — if absent — initial letter.
func (u *ui) avatarView(pub []byte, name string, size float32) fyne.CanvasObject {
	sz := fyne.NewSize(size, size)
	data, mime, hash := u.c.AvatarOf(pub)
	if len(data) > 0 {
		key := fmt.Sprintf("%x", hash) // avatar change = new hash = new cache entry
		if attach.Animated(mime) && len(u.anims)+len(u.msgAnims) < maxAnims {
			if cl := u.clip(key, data, true); cl != nil {
				ci := canvas.NewImageFromImage(cl.frames[0])
				ci.FillMode = canvas.ImageFillContain
				ci.SetMinSize(sz)
				u.anims = append(u.anims, &anim{clip: cl, img: ci})
				return container.NewGridWrap(sz, ci)
			}
		}
		if img := u.still(key, data, true); img != nil {
			ci := canvas.NewImageFromImage(img)
			ci.FillMode = canvas.ImageFillContain
			ci.SetMinSize(sz)
			return container.NewGridWrap(sz, ci)
		}
	}
	// no avatar: circle in author color with first letter of name
	circle := canvas.NewCircle(authorColor(pub))
	letter := "?"
	for _, r := range name {
		letter = strings.ToUpper(string(r))
		break
	}
	t := canvas.NewText(letter, cBg)
	t.TextSize = size * 0.45
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewGridWrap(sz, container.NewStack(circle, container.NewCenter(t)))
}

// avatarPage: preview + file selection. Image is normalized in core
// (re-encoding), so here we only provide bytes.
func (u *ui) avatarPage() {
	preview := container.NewStack(u.avatarView(u.c.ID.Pub, u.c.ID.Name, avatarHuge*1.4))
	redraw := func() {
		preview.Objects = []fyne.CanvasObject{
			u.avatarView(u.c.ID.Pub, u.c.ID.Name, avatarHuge*1.4)}
		preview.Refresh()
		u.refresh()
	}
	set := func(data []byte) {
		if err := u.c.SetAvatar(data); err != nil {
			u.fail(err)
			return
		}
		redraw()
		u.toast("Awatar zmieniony")
	}
	pickSys := bigButton("Wybierz z galerii", theme.FolderOpenIcon(), func() {
		u.pickImage(attach.MaxAvatarBytes, func(_ string, data []byte) { set(data) })
	})
	del := widget.NewButtonWithIcon("Usuń awatar", theme.DeleteIcon(), func() {
		if err := u.c.ClearAvatar(); err != nil {
			u.fail(err)
			return
		}
		redraw()
	})
	grid := u.galleryGrid(true, func(path string) {
		data, err := readLimit(path, attach.MaxAvatarBytes)
		if err != nil {
			u.fail(err)
			return
		}
		set(data)
	})
	u.page("Awatar", container.NewVBox(
		container.NewCenter(preview), vspace(8), pickSys, del,
		mutedWrap("PNG, JPEG, WebP albo GIF (animowany zostaje animowany). Obrazek jest "+
			"przycinany do kwadratu, skalowany do "+fmt.Sprint(attach.AvatarSide)+
			" px i kodowany od nowa — nic poza samą grafiką nie przechodzi dalej."),
		vspace(6), section("Z FOLDERU "+strings.ToUpper(filepath.Base(u.mediaDir()))), grid,
	))
}

// ---- file selection ----

// showFileDlg displays file dialog in a reasonable size.
// Order matters: fyne 2.8 calls MinSize() inside Resize(), which accesses
// window created in Show() — Resize before Show causes nil pointer panic.
// On mobile, Show() delegates to native picker and there is no fyne window.
func (u *ui) showFileDlg(fd *dialog.FileDialog) {
	fd.Show()
	if !u.mobile {
		fd.Resize(u.dlgSize(760, 560))
	}
}

// pickFile opens system file picker and reads file with size limit.
// mimes restricts filter: on Android fyne maps filter to EXTRA_MIME_TYPES,
// so "image/*|video/*" opens media picker instead of doc list.
func (u *ui) pickFile(limit int64, mimes []string, done func(name string, data []byte)) {
	fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, limit+1))
		if err != nil {
			u.fail(err)
			return
		}
		if int64(len(data)) > limit {
			u.fail(fmt.Errorf("plik przekracza limit %s", attach.HumanSize(limit)))
			return
		}
		done(r.URI().Name(), data)
	}, u.w)
	if len(mimes) > 0 {
		fd.SetFilter(storage.NewMimeTypeFileFilter(mimes))
	}
	u.showFileDlg(fd)
}

func (u *ui) pickImage(limit int64, done func(string, []byte)) {
	u.pickFile(limit, []string{"image/*"}, done)
}

// readLimit: file from disk with a hard size cap.
func readLimit(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("plik ma %s — limit to %s",
			attach.HumanSize(fi.Size()), attach.HumanSize(limit))
	}
	return os.ReadFile(path)
}

// ---- attachment page ----

// attachPage: user view after tapping paperclip. Thumbnail grid
// from media folder at top, system picker below. Caption is taken
// from message field to allow sending image with comment like Discord.
func (u *ui) attachPage() {
	ch := u.cur()
	if ch == nil {
		u.sysErr("Najpierw wybierz czat z listy.")
		return
	}
	caption := strings.TrimSpace(u.entry.Text)
	send := func(name string, data []byte) {
		if err := u.c.SendFile(ch.ID, name, data, caption); err != nil {
			u.fail(err)
			return
		}
		u.entry.SetText("")
		u.popPage()
		u.msgSig = ""
		u.refresh()
	}
	sendPath := func(path string) {
		data, err := readLimit(path, attach.MaxFileBytes)
		if err != nil {
			u.fail(err)
			return
		}
		send(filepath.Base(path), data)
	}

	rows := container.NewVBox()
	if caption != "" {
		rows.Add(container.NewPadded(mutedWrap("Podpis: " + caption)))
	}
	rows.Add(section("Z FOLDERU " + strings.ToUpper(filepath.Base(u.mediaDir()))))
	rows.Add(u.galleryGrid(false, sendPath))
	rows.Add(navRow(theme.FolderOpenIcon(), "Zmień folder", u.mediaDir(), u.mediaDirPage))
	rows.Add(section("SYSTEMOWY WYBÓR"))
	rows.Add(navRow(theme.MediaPhotoIcon(), "Zdjęcia i filmy", "Galeria Androida", func() {
		u.pickFile(attach.MaxFileBytes, []string{"image/*", "video/*"}, send)
	}))
	rows.Add(navRow(theme.FileIcon(), "Dowolny plik",
		"PDF, archiwum, cokolwiek — limit "+attach.HumanSize(attach.MaxFileBytes), func() {
			u.pickFile(attach.MaxFileBytes, nil, send)
		}))
	rows.Add(vspace(6))
	rows.Add(mutedWrap(fmt.Sprintf(
		"Pliki do %s rozchodzą się same. Większe wysyłają się jako podgląd — "+
			"reszta dociąga się dopiero, gdy ktoś naciśnie „pobierz”, i wymaga, "+
			"żeby nadawca był online.", attach.HumanSize(attach.PreviewMax))))
	u.page("Załącznik", rows)
}

// ---- in-app gallery ----

// mediaDir: directory scanned by thumbnail grid.
func (u *ui) mediaDir() string {
	if u.cfg.MediaDir != "" {
		return u.cfg.MediaDir
	}
	for _, d := range defaultMediaDirs() {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return defaultMediaDirs()[0]
}

// defaultMediaDirs: typical media locations. Android keeps them in /sdcard,
// desktop in ~/Pictures or ~/Obrazy.
func defaultMediaDirs() []string {
	if mobileUI {
		return []string{
			"/sdcard/DCIM/Camera", "/sdcard/DCIM", "/sdcard/Pictures",
			"/sdcard/Download", "/storage/emulated/0/DCIM",
		}
	}
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "Pictures"), filepath.Join(home, "Obrazy"),
		filepath.Join(home, "Downloads"), home,
	}
}

// galleryFile: single item in grid.
type galleryFile struct {
	path string
	name string
	size int64
	mod  time.Time
}

// scanMedia: media files from directory, newest first. Identified
// by extension — sniffing magic bytes in thousand files would be
// slower than opening screen; content is validated by attach.Check anyway.
func scanMedia(dir string, imagesOnly bool) ([]galleryFile, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	exts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true,
		".gif": true, ".webp": true}
	if !imagesOnly {
		exts[".mp4"] = true
		exts[".webm"] = true
	}
	var out []galleryFile
	for _, e := range ents {
		if e.IsDir() || !exts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, galleryFile{
			path: filepath.Join(dir, e.Name()), name: e.Name(),
			size: fi.Size(), mod: fi.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	if len(out) > galleryMax {
		out = out[:galleryMax]
	}
	return out, nil
}

// galleryGrid: thumbnail grid from media folder. When Android denies
// storage access ("Files and media" permission can only be enabled from
// system settings without JNI), show instructions and leave system picker.
func (u *ui) galleryGrid(imagesOnly bool, pick func(path string)) fyne.CanvasObject {
	dir := u.mediaDir()
	files, err := scanMedia(dir, imagesOnly)
	if err != nil {
		msg := "Nie mogę zajrzeć do " + dir + " (" + err.Error() + ")."
		if os.IsPermission(err) || mobileUI {
			msg = "Android nie dał dostępu do " + dir + ". Włącz uprawnienie " +
				"„Pliki i multimedia” w ustawieniach systemu (Aplikacje → szept → " +
				"Uprawnienia) albo użyj systemowego wyboru niżej."
		}
		return container.NewPadded(card(mutedWrap(msg)))
	}
	if len(files) == 0 {
		return container.NewPadded(card(mutedWrap("W " + dir + " nie ma zdjęć ani filmów.")))
	}
	cols := 3
	if !u.mobile {
		cols = 5
	}
	grid := container.NewGridWithColumns(cols)
	var slots []*fyne.Container
	for _, f := range files {
		f := f
		slot := container.NewStack(container.NewCenter(widget.NewIcon(galleryIcon(f.name))))
		slots = append(slots, slot)
		grid.Add(u.tile(f, slot, func() { pick(f.path) }))
	}
	go u.loadThumbs(files, slots)
	return container.NewPadded(grid)
}

// loadThumbs decodes thumbnails in background, one by one, and injects into
// prepared tiles. Synchronously on UI thread would freeze UI for seconds —
// every file requires full image decode.
func (u *ui) loadThumbs(files []galleryFile, slots []*fyne.Container) {
	for i, f := range files {
		if isVideoExt(f.name) || f.size > thumbMax {
			continue // video without decoder cannot be previewed (no ffmpeg)
		}
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		img := thumbImage(data)
		if img == nil {
			continue
		}
		// No cache — gallery opened rarely, and decoding runs
		// in background. Cache would need synchronization with UI thread.
		slot := slots[i]
		fyne.Do(func() {
			ci := canvas.NewImageFromImage(img)
			ci.FillMode = canvas.ImageFillContain
			ci.SetMinSize(fyne.NewSize(thumbSide-8, thumbSide-8))
			slot.Objects = []fyne.CanvasObject{ci}
			slot.Refresh()
		})
	}
}

// thumbImage: thumbnail from full bytes. ApproxBiLinear, not CatmullRom —
// unnoticeable difference at 100px, costs fraction of CPU.
func thumbImage(data []byte) image.Image {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	// center crop square — grid should look uniform, not like
	// collection of rectangles with letterboxing
	b := src.Bounds()
	n := b.Dx()
	if b.Dy() < n {
		n = b.Dy()
	}
	crop := image.Rect(0, 0, n, n).Add(image.Point{
		X: b.Min.X + (b.Dx()-n)/2, Y: b.Min.Y + (b.Dy()-n)/2})
	side := int(thumbSide * 2)
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, crop, xdraw.Over, nil)
	return dst
}

func galleryIcon(name string) fyne.Resource {
	if isVideoExt(name) {
		return theme.FileVideoIcon()
	}
	return theme.FileImageIcon()
}

// tile: gallery tile — thumbnail (populated by loadThumbs) and filename.
func (u *ui) tile(f galleryFile, slot *fyne.Container, tap func()) fyne.CanvasObject {
	label := mutedText(trunc(f.name, 14), 9)
	bg := canvas.NewRectangle(cSurface)
	bg.CornerRadius = 8
	cell := container.NewStack(bg, container.NewBorder(nil,
		container.NewCenter(label), nil, nil, container.NewPadded(slot)))
	return tappable(container.NewGridWrap(fyne.NewSize(thumbSide, thumbSide+18), cell), tap, nil)
}

func isVideoExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".mkv", ".mov":
		return true
	}
	return false
}

// mediaDirPage: change gallery folder — manual entry or system select.
func (u *ui) mediaDirPage() {
	e := widget.NewEntry()
	e.SetText(u.mediaDir())
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	check := func(dir string) {
		found, err := scanMedia(dir, false)
		if err != nil {
			status.SetText("Nie da się czytać: " + err.Error())
			return
		}
		status.SetText(fmt.Sprintf("OK — widzę %d plików multimedialnych.", len(found)))
	}
	check(e.Text)
	save := func() {
		dir := strings.TrimSpace(e.Text)
		u.cfg.MediaDir = dir
		if err := boot.SaveConfig(u.cfgFile, u.cfg); err != nil {
			u.fail(err)
			return
		}
		u.c.Cfg.MediaDir = dir
		u.popPage()
		u.refresh()
		u.toast("Folder zapisany")
	}
	e.OnSubmitted = func(string) { save() }
	e.OnChanged = func(s string) { check(strings.TrimSpace(s)) }

	quick := container.NewVBox()
	for _, d := range defaultMediaDirs() {
		d := d
		if _, err := os.Stat(d); err != nil {
			continue
		}
		quick.Add(navRow(theme.FolderIcon(), d, "", func() {
			e.SetText(d)
			check(d)
		}))
	}
	browse := widget.NewButtonWithIcon("Wybierz systemowo", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFolderOpen(func(l fyne.ListableURI, err error) {
			if err != nil || l == nil {
				return
			}
			e.SetText(l.Path())
			check(l.Path())
		}, u.w)
		u.showFileDlg(fd)
	})
	u.page("Folder z multimediami", container.NewVBox(e, status,
		bigButton("Zapisz", theme.ConfirmIcon(), save), browse,
		vspace(6), section("SKRÓTY"), quick,
		vspace(6),
		mutedWrap("Na Androidzie odczyt /sdcard wymaga uprawnienia „Pliki i multimedia”, "+
			"które włącza się w ustawieniach systemu. Bez niego zostaje systemowy wybór "+
			"plików — ten działa zawsze.")))
}

// ---- save and open ----

func (u *ui) saveAttachment(m core.Msg) {
	data, ok := u.c.BlobBytes(m.Blob)
	if !ok {
		u.toast("Najpierw pobierz plik")
		return
	}
	fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil {
			return
		}
		defer w.Close()
		if _, err := w.Write(data); err != nil {
			u.fail(err)
			return
		}
		u.toast("Zapisane: " + m.File)
	}, u.w)
	fd.SetFileName(m.File)
	u.showFileDlg(fd)
}

// openExternal saves attachment to temp directory and delegates to
// system viewer. Name and extension come from detected type,
// not network — we don't execute anything directly.
func (u *ui) openExternal(m core.Msg) {
	path, err := u.c.SaveAttachment(m.Blob, m.File, os.TempDir())
	if err != nil {
		u.fail(err)
		return
	}
	if err := u.a.OpenURL(&url.URL{Scheme: "file", Path: path}); err != nil {
		u.fail(fmt.Errorf("nie udało się otworzyć pliku: %w", err))
	}
}

// fetchFull: "download full file". Bytes live with sender (or peer who
// already has them) — if none are online, request waits in queue.
func (u *ui) fetchFull(m core.Msg) {
	ch := u.cur()
	if ch == nil {
		return
	}
	u.c.FetchBlob(ch.ID, m.Blob, m.Mime, m.Size, m.Author)
	u.msgSig = ""
	u.refresh()
	if !u.c.Online(m.Author) {
		u.toast("Nadawca jest offline — pobiorę, gdy się pojawi")
		return
	}
	u.toast("Pobieram " + m.File)
}

// ---- attachment tile in chat ----

// attachView renders attachment tile; animate = message received slot
// for GIF playback (see renderMessages).
func (u *ui) attachView(m core.Msg, animate bool) fyne.CanvasObject {
	label := m.File
	if label == "" {
		label = "załącznik"
	}
	head := canvas.NewText(trunc(label, 34)+"  ·  "+attach.HumanSize(m.Size), cFg)
	head.TextSize = fs(12)

	// large file: we have preview, full bytes on button
	if !m.Have && m.HavePrev {
		body := container.NewVBox()
		if img := u.previewImage(m); img != nil {
			body.Add(img)
		}
		body.Add(head)
		if m.Getting {
			body.Add(mutedText("pobieranie…", 10))
		} else {
			body.Add(widget.NewButtonWithIcon("Pobierz pełny plik", theme.DownloadIcon(),
				func() { u.fetchFull(m) }))
			body.Add(mutedText("podgląd · pełny plik u nadawcy", 10))
		}
		return body
	}
	if !m.Have {
		state := "pobieranie… (nadawca musi być online)"
		var act fyne.CanvasObject = mutedText(state, 10)
		if !m.Getting {
			act = widget.NewButtonWithIcon("Pobierz ("+attach.HumanSize(m.Size)+")",
				theme.DownloadIcon(), func() { u.fetchFull(m) })
		}
		return container.NewVBox(container.NewHBox(
			widget.NewIcon(fileIcon(m.Mime)), hspace(2), container.NewCenter(head)), act)
	}

	data, _ := u.c.BlobBytes(m.Blob)
	save := widget.NewButtonWithIcon("Zapisz", theme.DocumentSaveIcon(), func() { u.saveAttachment(m) })
	open := widget.NewButtonWithIcon("Otwórz", theme.MediaPlayIcon(), func() { u.openExternal(m) })
	buttons := container.NewHBox(open, save, layout.NewSpacer())

	if !attach.IsImage(m.Mime) {
		// video played by system, regular file offered to save — fyne has no
		// decoder, and pulling ffmpeg into chat client is significant bloat
		return container.NewVBox(
			container.NewHBox(widget.NewIcon(fileIcon(m.Mime)), hspace(2),
				container.NewCenter(head)),
			buttons)
	}

	key := fmt.Sprintf("%x", m.Blob)
	w, h := previewSize(m.W, m.H, u.mobile)
	var ci *canvas.Image
	if animate && attach.Animated(m.Mime) {
		if cl := u.clip(key, data, false); cl != nil {
			ci = canvas.NewImageFromImage(cl.frames[0])
			u.msgAnims = append(u.msgAnims, &anim{clip: cl, img: ci})
		}
	}
	if ci == nil {
		img := u.still(key, data, false)
		if img == nil {
			return container.NewVBox(head, mutedText("(nie udało się odczytać obrazu)", 10), buttons)
		}
		ci = canvas.NewImageFromImage(img)
	}
	ci.FillMode = canvas.ImageFillContain
	ci.SetMinSize(fyne.NewSize(w, h))
	return container.NewVBox(container.NewGridWrap(fyne.NewSize(w, h), ci), head, buttons)
}

// previewImage: thumbnail of large file (separate blob, hence separate path).
func (u *ui) previewImage(m core.Msg) fyne.CanvasObject {
	data, ok := u.c.BlobBytes(m.Prev)
	if !ok {
		return nil
	}
	img := u.still(fmt.Sprintf("%x", m.Prev), data, false)
	if img == nil {
		return nil
	}
	w, h := previewSize(m.W, m.H, u.mobile)
	ci := canvas.NewImageFromImage(img)
	ci.FillMode = canvas.ImageFillContain
	ci.SetMinSize(fyne.NewSize(w, h))
	return container.NewGridWrap(fyne.NewSize(w, h), ci)
}

// fileIcon: icon by type — image, video, audio or generic file.
func fileIcon(mime string) fyne.Resource {
	switch {
	case attach.IsImage(mime):
		return theme.FileImageIcon()
	case attach.IsVideo(mime):
		return theme.FileVideoIcon()
	case strings.HasPrefix(mime, "audio/"):
		return theme.FileAudioIcon()
	case strings.HasPrefix(mime, "text/"):
		return theme.FileTextIcon()
	}
	return theme.FileIcon()
}

// previewSize scales preview to fit inside bubble.
func previewSize(w, h int, mobile bool) (float32, float32) {
	maxW, maxH := float32(320), float32(260)
	if mobile {
		maxW, maxH = 220, 200
	}
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	fw, fh := float32(w), float32(h)
	if fw > maxW {
		fh *= maxW / fw
		fw = maxW
	}
	if fh > maxH {
		fw *= maxH / fh
		fh = maxH
	}
	return fw, fh
}
