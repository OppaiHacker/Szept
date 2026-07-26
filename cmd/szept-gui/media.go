package main

// Załączniki, awatary i galeria: dekodowanie z podręcznym cache, odtwarzanie
// GIF-ów (fyne nie animuje ich sam), kafelki plików w rozmowie i wybór tego,
// co wysyłamy — siatka miniatur z dysku albo systemowy wybór plików.

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
	// maxAnims: animujemy tylko kilka ostatnich GIF-ów. Klatki trzymamy
	// rozpakowane w RAM, więc czat pełen gifów inaczej zjadłby telefon.
	maxAnims = 6
	// cacheMax: tyle rozpakowanych obrazków/GIF-ów trzymamy pod ręką.
	cacheMax = 16
	tickStep = 80 * time.Millisecond

	avatarSmall = float32(30) // przy dymku wiadomości
	avatarRow   = float32(38) // lista znajomych / członkowie
	avatarBig   = float32(48) // wiersz czatu
	avatarHuge  = float32(96) // strona profilu

	// galleryMax: ile plików pokazujemy w siatce. Katalog ze zdjęciami potrafi
	// mieć tysiące pozycji, a miniatura każdej to dekodowanie JPEG-a.
	galleryMax = 30
	thumbSide  = float32(104)
	// thumbMax: powyżej tego nie robimy miniatury — dekodowanie 40-megapikselowego
	// zdjęcia na telefonie to sekundy i setki megabajtów RAM-u.
	thumbMax = 12 << 20
)

type gifClip struct {
	frames []image.Image
	delays []time.Duration
}

// anim: jeden odtwarzany GIF. Wszystkie chodzą z jednego tickera.
type anim struct {
	clip *gifClip
	img  *canvas.Image
	idx  int
	acc  time.Duration
}

// startAnims odpala jedyny w aplikacji zegar animacji. Lista u.anims jest
// przebudowywana przy każdym renderMessages — i jedno, i drugie dzieje się
// w wątku UI (fyne.Do), więc nie ma tu wyścigu.
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

// cacheGet/cachePut: prymitywny cache dekodowanych treści, klucz = skrót bloba
// (+ sufiks wariantu). Kasujemy na ślepo przy przepełnieniu — dekodowanie
// kosztuje milisekundy, więc pudło w cache nikogo nie zaboli.
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

// still: pojedynczy obrazek (pierwsza klatka GIF-a, gdy animacji nie ma).
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

// clip: klatki GIF-a gotowe do odtwarzania (nil, gdy się nie da — wtedy UI
// pokazuje sam obrazek statyczny).
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

// circleMask wycina z obrazka koło (awatary) — fyne nie umie przycinać.
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
			// nieprzezroczystość liczona z premultiplied RGBA
			dst.Set(x, y, color.NRGBA{
				R: uint8(cr * 0xff / ca), G: uint8(cg * 0xff / ca),
				B: uint8(cb * 0xff / ca), A: uint8(ca >> 8),
			})
		}
	}
	return dst
}

// ---- awatary ----

// avatarView: kółko z awatarem albo — gdy go nie ma — z inicjałem.
func (u *ui) avatarView(pub []byte, name string, size float32) fyne.CanvasObject {
	sz := fyne.NewSize(size, size)
	data, mime, hash := u.c.AvatarOf(pub)
	if len(data) > 0 {
		key := fmt.Sprintf("%x", hash) // zmiana awatara = nowy skrót = nowy wpis w cache
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
	// brak awatara: kółko w kolorze autora z pierwszą literą nazwy
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

// avatarPage: podgląd + wybór pliku. Obrazek jest normalizowany w core
// (przekodowanie), więc tutaj tylko podajemy bajty.
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

// ---- wybór plików ----

// showFileDlg pokazuje okno plików w rozsądnym rozmiarze.
// Kolejność jest istotna: fyne 2.8 w Resize() woła MinSize(), które sięga do
// okna tworzonego dopiero w Show() — Resize przed Show to panika (nil pointer).
// Na telefonie Show() oddaje sterowanie natywnemu pickerowi i okna fyne w ogóle
// nie ma, więc tam nie ruszamy rozmiaru.
func (u *ui) showFileDlg(fd *dialog.FileDialog) {
	fd.Show()
	if !u.mobile {
		fd.Resize(u.dlgSize(760, 560))
	}
}

// pickFile otwiera systemowy wybór pliku i wczytuje go z limitem rozmiaru.
// mimes zawęża listę: na Androidzie fyne przekłada filtr na EXTRA_MIME_TYPES,
// więc "image/*|video/*" daje wybieraczkę zdjęć, a nie listę dokumentów.
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

// readLimit: plik z dysku z twardym sufitem rozmiaru.
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

// ---- strona „załącznik” ----

// attachPage: to, co widzi użytkownik po dotknięciu spinacza. Siatka miniatur
// z folderu multimediów u góry, systemowy wybór niżej. Podpis bierzemy z pola
// wiadomości, żeby dało się wysłać zdjęcie z komentarzem jak na Discordzie.
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

// ---- galeria w aplikacji ----

// mediaDir: folder, po którym chodzi siatka miniatur.
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

// defaultMediaDirs: typowe miejsca ze zdjęciami. Android trzyma je w /sdcard,
// pulpit w ~/Pictures albo ~/Obrazy.
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

// galleryFile: jedna pozycja siatki.
type galleryFile struct {
	path string
	name string
	size int64
	mod  time.Time
}

// scanMedia: pliki multimedialne z katalogu, najnowsze pierwsze. Rozpoznajemy
// po rozszerzeniu — sniffowanie magicznych bajtów w tysiącu plików byłoby
// wolniejsze niż całe otwarcie ekranu; treść i tak sprawdza attach.Check.
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

// galleryGrid: siatka miniatur z folderu multimediów. Gdy Android nie da nam
// dostępu do pamięci (uprawnienie „Pliki i multimedia” jest do włączenia tylko
// z ustawień systemu — aplikacja bez JNI nie umie o nie poprosić), pokazujemy
// wprost, co zrobić, i zostawiamy systemowy wybór jako drogę obok.
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

// loadThumbs dekoduje miniatury w tle, jedna po drugiej, i wstawia je do
// gotowych kafelków. Synchronicznie w wątku UI byłaby to sekundowa zwiecha —
// każdy plik to pełne dekodowanie obrazu.
func (u *ui) loadThumbs(files []galleryFile, slots []*fyne.Container) {
	for i, f := range files {
		if isVideoExt(f.name) || f.size > thumbMax {
			continue // filmu bez dekodera nie podejrzymy (bez ffmpeg)
		}
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		img := thumbImage(data)
		if img == nil {
			continue
		}
		// Bez cache — galerię otwiera się rzadko, a dekodowanie leci
		// w tle. Cache trzeba by jeszcze pilnować przed wyścigiem z wątkiem UI.
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

// thumbImage: miniatura z pełnych bajtów. ApproxBiLinear, nie CatmullRom —
// przy stu pikselach różnicy nie widać, a kosztuje kilka razy mniej.
func thumbImage(data []byte) image.Image {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	// kwadrat ze środka kadru — siatka ma wyglądać jak siatka, nie jak
	// zbiór prostokątów z pustymi pasami
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

// tile: kafelek galerii — miniatura (dosypywana z loadThumbs) i nazwa pliku.
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

// mediaDirPage: zmiana folderu galerii — wpisany ręcznie albo wybrany systemowo.
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

// ---- zapis i otwieranie ----

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

// openExternal zapisuje załącznik do katalogu tymczasowego i oddaje go
// systemowemu odtwarzaczowi. Nazwa i rozszerzenie pochodzą z wykrytego typu,
// nie z sieci — nie odpalamy tu niczego samodzielnie.
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

// fetchFull: „pobierz pełny plik”. Bajty leżą u nadawcy (albo u kogoś, kto już
// je ma) — jeśli nikt z nich nie jest online, prośba poczeka w kolejce.
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

// ---- kafelek załącznika w rozmowie ----

// attachView rysuje kafelek załącznika; animate = ta wiadomość dostała slot
// na odtwarzanie GIF-a (patrz renderMessages).
func (u *ui) attachView(m core.Msg, animate bool) fyne.CanvasObject {
	label := m.File
	if label == "" {
		label = "załącznik"
	}
	head := canvas.NewText(trunc(label, 34)+"  ·  "+attach.HumanSize(m.Size), cFg)
	head.TextSize = fs(12)

	// duży plik: mamy podgląd, pełne bajty na przycisk
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
		// wideo odtwarza system, zwykły plik oddajemy do zapisu — fyne nie ma
		// dekodera, a wciąganie ffmpeg do klienta czatu to nie jest mały koszt
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

// previewImage: miniatura dużego pliku (osobny blob, dlatego osobna ścieżka).
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

// fileIcon: ikona wg typu — obraz, film, dźwięk albo zwykły plik.
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

// previewSize skaluje podgląd tak, by mieścił się w dymku.
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
