// Package attach: walidacja i normalizacja załączników (obrazy, GIF-y, wideo).
//
// Zasada bezpieczeństwa: typ pliku bierzemy WYŁĄCZNIE z magicznych bajtów
// (net/http.DetectContentType), nigdy z nazwy ani z pola przysłanego przez
// drugą stronę. Nazwa pliku jest odcinana od ścieżki i dostaje rozszerzenie
// wynikające z wykrytego typu — plik "foto.png" będący w środku HTML-em albo
// skryptem nie przejdzie, a "x.png.sh" nie zapisze się jako .sh.
//
// Awatar idzie o krok dalej: jest w całości dekodowany i PRZEKODOWANY naszym
// enkoderem (PNG albo GIF), więc do bazy trafiają bajty wyprodukowane przez
// nas, a nie przez użytkownika. Doklejony ładunek (polyglot ZIP/HTML/JS,
// EXIF, komentarze, profile ICC) po prostu nie przeżywa tej operacji.
package attach

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/jpeg" // enkoder miniatur; import rejestruje też dekoder JPEG
	"image/png"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// MaxFileBytes: sufit załącznika w czacie. Blob leci przez kanał ctrl
	// i ląduje w bbolt jako jedna wartość — stąd rozsądny, nie kosmiczny limit.
	// ponytail: gdy komuś zabraknie, wtedy dopiero streaming do pliku na dysku.
	MaxFileBytes = 32 << 20
	// PreviewMax: powyżej tego rozmiaru nie replikujemy całego pliku. Nadawca
	// robi miniaturę, ona idzie do wszystkich od razu, a pełne bajty wędrują
	// dopiero na żądanie (i tylko wtedy, gdy ktoś, kto je ma, jest online).
	PreviewMax = 10 << 20
	// PreviewSide: dłuższy bok miniatury.
	PreviewSide = 720
	// MaxAvatarBytes: limit tego, co użytkownik wybiera z dysku.
	MaxAvatarBytes = 4 << 20
	// MaxAvatarOut: limit po normalizacji (to jest to, co replikujemy).
	MaxAvatarOut = 1 << 20
	// AvatarSide: awatar jest kwadratowy, przycinany do środka.
	AvatarSide = 256

	maxGifFrames = 300
	maxDim       = 8000
	maxPixels    = 20_000_000 // ~80 MB RGBA w szczycie — zapora na bomby dekompresyjne
	sniffLen     = 512
)

// Meta: to, co o załączniku wiemy po walidacji (i co ląduje w evencie DAG).
type Meta struct {
	Name string
	Mime string
	Size int64
	W, H int
}

var (
	imageMimes = map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}
	// Wideo tylko kontenerowo: nie mamy dekodera, więc nie udajemy walidacji
	// zawartości. Chroni nas to, że plik nigdy nie jest uruchamiany, a nazwa
	// i rozszerzenie pochodzą od nas.
	// ponytail: mkv ma to samo EBML co webm i wykryje się jako video/webm — i dobrze.
	videoMimes = map[string]string{
		"video/mp4":  ".mp4",
		"video/webm": ".webm",
	}
)

func IsImage(mime string) bool { _, ok := imageMimes[mime]; return ok }
func IsVideo(mime string) bool { _, ok := videoMimes[mime]; return ok }

// IsMedia: typ, który sami rysujemy albo oddajemy odtwarzaczowi. Wszystko inne
// jest „zwykłym plikiem” — pokazujemy kafelek i pozwalamy zapisać na dysk.
func IsMedia(mime string) bool { return IsImage(mime) || IsVideo(mime) }

// Animated: czy typ może być animowany (GIF) — UI odpala wtedy odtwarzacz klatek.
func Animated(mime string) bool { return mime == "image/gif" }

// Ext zwraca rozszerzenie wymuszone przez wykryty typ ("" dla nieznanego).
func Ext(mime string) string {
	if e, ok := imageMimes[mime]; ok {
		return e
	}
	return videoMimes[mime]
}

// SniffAny wykrywa typ z magicznych bajtów — bez białej listy. Nazwa z sieci
// nigdy nie decyduje o typie ani o rozszerzeniu zapisanego pliku.
func SniffAny(data []byte) string {
	head := data
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	mime := http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i >= 0 { // "text/plain; charset=utf-8"
		mime = mime[:i]
	}
	return mime
}

// Sniff zwraca typ tylko dla mediów, które umiemy wyświetlić (obraz/wideo);
// dla wszystkiego innego "". Wołaj tam, gdzie chodzi o rysowanie, nie o zapis.
func Sniff(data []byte) string {
	if mime := SniffAny(data); IsMedia(mime) {
		return mime
	}
	return ""
}

// SafeName: nazwa do pokazania i do zapisu na dysku. Ucina katalogi, znaki
// sterujące i separatory oraz przycina długość.
//
// Rozszerzenie: dla mediów wymuszone przez wykryty typ (plik „foto.png” będący
// skryptem nie zapisze się jako .sh). Dla zwykłych plików zostaje rozszerzenie
// z nazwy — inaczej PDF czy ZIP nie dałoby się otworzyć — ale przepuszczamy
// wyłącznie znaki alfanumeryczne i zostawiamy dokładnie jedną kropkę, więc
// „raport.pdf.exe” nie udaje PDF-a. Sami nigdy nic nie uruchamiamy.
func SafeName(name, mime string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f: // znaki sterujące
			return -1
		case strings.ContainsRune(`/:*?"<>|`, r):
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".") // ".bashrc", "..", ukryte pliki
	ext := Ext(mime)
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		if ext == "" {
			ext = cleanExt(name[i+1:])
			if mediaExt(ext) {
				// treść nie jest medium (inaczej Ext by je nazwał), więc nazwa
				// nie będzie tego udawać — HTML nie zapisze się jako .png
				ext = ""
			}
		}
		name = name[:i]
	}
	name = strings.ReplaceAll(name, ".", "_") // koniec z podwójnym rozszerzeniem
	if name == "" {
		name = "zalacznik"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	if ext == "" {
		ext = ".bin"
	}
	return name + ext
}

// mediaExt: rozszerzenie, które nasze UI potraktowałoby jako obraz albo film.
func mediaExt(ext string) bool {
	for _, m := range []map[string]string{imageMimes, videoMimes} {
		for _, e := range m {
			if e == ext {
				return true
			}
		}
	}
	return ext == ".jpeg" // jedyny alias, którego nie ma w tabelach
}

// cleanExt: rozszerzenie zwykłego pliku — same litery i cyfry, do 10 znaków.
func cleanExt(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
		if b.Len() >= 10 {
			break
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "." + b.String()
}

// Check waliduje załącznik przed wysłaniem: limit rozmiaru, typ z magicznych
// bajtów, a dla obrazów pełne dekodowanie (plik musi naprawdę być obrazem).
// Pliki inne niż media przechodzą bez zaglądania w treść — nie udajemy, że
// umiemy zwalidować PDF czy archiwum; nigdy ich nie uruchamiamy.
func Check(data []byte, name string) (Meta, error) {
	if len(data) == 0 {
		return Meta{}, errors.New("pusty plik")
	}
	if len(data) > MaxFileBytes {
		return Meta{}, fmt.Errorf("plik ma %s — limit to %s",
			HumanSize(int64(len(data))), HumanSize(MaxFileBytes))
	}
	mime := SniffAny(data)
	m := Meta{Name: SafeName(name, mime), Mime: mime, Size: int64(len(data))}
	if !IsImage(mime) {
		return m, nil
	}
	_, w, h, err := imageDims(data)
	if err != nil {
		return Meta{}, err
	}
	m.W, m.H = w, h
	// pełne dekodowanie po stronie nadawcy: uszkodzony albo podrobiony obraz
	// wychodzi na jaw u nas, a nie u odbiorcy
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return Meta{}, fmt.Errorf("to nie jest poprawny obraz: %w", err)
	}
	return m, nil
}

// Verify sprawdza blob przyjęty od peera: typ musi zgadzać się z tym, co
// zapowiedział podpisany event, a obraz musi mieć sensowny nagłówek.
// Pełnego dekodowania tu nie robimy — i tak zrobi je UI przy rysowaniu, a
// wymuszanie go na każdym przychodzącym pliku to darmowy wektor na CPU.
func Verify(data []byte, mime string) error {
	if len(data) == 0 || len(data) > MaxFileBytes {
		return errors.New("blob poza limitem")
	}
	if got := SniffAny(data); got != mime {
		return fmt.Errorf("typ nie zgadza się z zapowiedzianym (%q vs %q)", got, mime)
	}
	if IsImage(mime) {
		if _, _, _, err := imageDims(data); err != nil {
			return err
		}
	}
	return nil
}

// imageDims czyta nagłówek obrazu i pilnuje limitów rozmiaru (bomby).
func imageDims(data []byte) (format string, w, h int, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, fmt.Errorf("nieczytelny nagłówek obrazu: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxDim || cfg.Height > maxDim ||
		cfg.Width*cfg.Height > maxPixels {
		return "", 0, 0, fmt.Errorf("obraz %dx%d poza limitem (max %d px na bok, %d MPx)",
			cfg.Width, cfg.Height, maxDim, maxPixels/1_000_000)
	}
	return format, cfg.Width, cfg.Height, nil
}

// Preview robi miniaturę dużego obrazka: dłuższy bok PreviewSide px, JPEG.
// Tylko to replikuje się do wszystkich, gdy plik przekracza PreviewMax.
// Zwraca błąd dla nie-obrazów — filmu bez dekodera nie podejrzymy, a wciąganie
// ffmpeg do klienta czatu to nie jest mały koszt (ponytail).
func Preview(data []byte, mime string) (out []byte, w, h int, err error) {
	if !IsImage(mime) {
		return nil, 0, 0, errors.New("podgląd tylko dla obrazów")
	}
	if _, _, _, err := imageDims(data); err != nil {
		return nil, 0, 0, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := src.Bounds()
	w, h = b.Dx(), b.Dy()
	if w > PreviewSide || h > PreviewSide {
		if w >= h {
			h, w = h*PreviewSide/w, PreviewSide
		} else {
			w, h = w*PreviewSide/h, PreviewSide
		}
	}
	if w < 1 || h < 1 {
		return nil, 0, 0, errors.New("obraz bez wymiarów")
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 78}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), w, h, nil
}

// Avatar normalizuje obrazek na awatar: kwadrat AvatarSide px, przekodowany
// naszym enkoderem. GIF zostaje GIF-em (animacja przeżywa), reszta wychodzi
// jako PNG. Zwraca gotowe bajty i ich typ.
func Avatar(data []byte) ([]byte, string, error) {
	if len(data) == 0 {
		return nil, "", errors.New("pusty plik")
	}
	if len(data) > MaxAvatarBytes {
		return nil, "", fmt.Errorf("awatar ma %s — limit to %s",
			HumanSize(int64(len(data))), HumanSize(MaxAvatarBytes))
	}
	mime := Sniff(data)
	if !IsImage(mime) {
		return nil, "", errors.New("awatar musi być obrazem: PNG, JPEG, GIF albo WebP")
	}
	if _, _, _, err := imageDims(data); err != nil {
		return nil, "", err
	}
	if mime == "image/gif" {
		return avatarGIF(data)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("to nie jest poprawny obraz: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, square(src, AvatarSide)); err != nil {
		return nil, "", err
	}
	if buf.Len() > MaxAvatarOut {
		return nil, "", errors.New("awatar po przeskalowaniu wciąż za duży — użyj prostszego obrazka")
	}
	return buf.Bytes(), "image/png", nil
}

// avatarGIF: dekoduje wszystkie klatki, składa je (GIF trzyma różnice, nie
// pełne obrazki), skaluje i koduje od nowa. Gdy wynik nie mieści się w limicie,
// wyrzuca co drugą klatkę i próbuje ponownie.
func avatarGIF(data []byte) ([]byte, string, error) {
	frames, delays, err := GifFrames(data)
	if err != nil {
		return nil, "", err
	}
	for {
		g := &gif.GIF{LoopCount: 0}
		for i, fr := range frames {
			pal := image.NewPaletted(image.Rect(0, 0, AvatarSide, AvatarSide), palette.Plan9)
			draw.FloydSteinberg.Draw(pal, pal.Bounds(), square(fr, AvatarSide), image.Point{})
			g.Image = append(g.Image, pal)
			d := int(delays[i] / (10 * time.Millisecond))
			if d < 2 {
				d = 2 // < 20 ms to w praktyce "tak szybko jak się da"
			}
			g.Delay = append(g.Delay, d)
			g.Disposal = append(g.Disposal, gif.DisposalNone)
		}
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, g); err != nil {
			return nil, "", err
		}
		if buf.Len() <= MaxAvatarOut {
			return buf.Bytes(), "image/gif", nil
		}
		if len(frames) <= 1 {
			return nil, "", errors.New("animowany awatar za duży — użyj krótszego GIF-a")
		}
		frames, delays = decimate(frames, delays)
	}
}

// decimate zostawia co drugą klatkę, sumując czasy wyświetlania.
func decimate(frames []image.Image, delays []time.Duration) ([]image.Image, []time.Duration) {
	var of []image.Image
	var od []time.Duration
	for i := 0; i < len(frames); i += 2 {
		d := delays[i]
		if i+1 < len(delays) {
			d += delays[i+1]
		}
		of = append(of, frames[i])
		od = append(od, d)
	}
	return of, od
}

// square przycina obraz do kwadratu (środek) i skaluje do side px.
func square(src image.Image, side int) image.Image {
	b := src.Bounds()
	n := b.Dx()
	if b.Dy() < n {
		n = b.Dy()
	}
	crop := image.Rect(0, 0, n, n).Add(image.Point{
		X: b.Min.X + (b.Dx()-n)/2,
		Y: b.Min.Y + (b.Dy()-n)/2,
	})
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Over, nil)
	return dst
}

// GifFrames dekoduje GIF-a na listę gotowych do wyświetlenia klatek.
// Składa je z uwzględnieniem pola Disposal — surowe klatki z gif.DecodeAll to
// często tylko łatki na fragmencie kadru.
func GifFrames(data []byte) ([]image.Image, []time.Duration, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("uszkodzony GIF: %w", err)
	}
	if len(g.Image) == 0 {
		return nil, nil, errors.New("GIF bez klatek")
	}
	if len(g.Image) > maxGifFrames {
		return nil, nil, fmt.Errorf("GIF ma %d klatek — limit to %d", len(g.Image), maxGifFrames)
	}
	if g.Config.Width <= 0 || g.Config.Height <= 0 ||
		g.Config.Width*g.Config.Height*len(g.Image) > maxPixels {
		return nil, nil, errors.New("GIF poza limitem rozmiaru")
	}
	rect := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	cur := image.NewRGBA(rect)
	var out []image.Image
	var delays []time.Duration
	for i, fr := range g.Image {
		var backup *image.RGBA
		if i < len(g.Disposal) && g.Disposal[i] == gif.DisposalPrevious {
			backup = image.NewRGBA(rect)
			draw.Draw(backup, rect, cur, rect.Min, draw.Src)
		}
		draw.Draw(cur, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		snap := image.NewRGBA(rect)
		draw.Draw(snap, rect, cur, rect.Min, draw.Src)
		out = append(out, snap)
		d := 100 * time.Millisecond
		if i < len(g.Delay) && g.Delay[i] > 0 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		delays = append(delays, d)
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(cur, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				draw.Draw(cur, rect, backup, rect.Min, draw.Src)
			}
		}
	}
	return out, delays, nil
}

// HumanSize formatuje bajty na potrzeby UI.
func HumanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
