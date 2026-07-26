package core

// Narzędzie deweloperskie (do usunięcia): buduje katalog danych z gotową
// historią, żeby dało się obejrzeć UI na emulatorze bez drugiego telefonu.
//   SZEPT_SEED=/tmp/seed go test ./internal/core -run TestSeedDevice -v

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"szept/internal/connector"
	"szept/internal/identity"
)

func seedPNG(t *testing.T, w, h int, seed int64) []byte {
	rnd := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				uint8(x * 255 / w), uint8(y * 255 / h), uint8(rnd.Intn(256)), 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSeedDevice(t *testing.T) {
	dir := os.Getenv("SZEPT_SEED")
	if dir == "" {
		t.Skip("SZEPT_SEED nie ustawione")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	url := os.Getenv("SZEPT_SEED_CONN")
	if url == "" {
		srv := httptest.NewServer(connector.NewServer())
		defer srv.Close()
		url = "ws" + strings.TrimPrefix(srv.URL, "http")
	}

	ida := identity.Create()
	ida.Name = "ala"
	if err := ida.Save(dir+"/identity.key", nil); err != nil {
		t.Fatal(err)
	}
	a, err := Open(Config{Connector: url, DataDir: dir}, ida)
	if err != nil {
		t.Fatal(err)
	}
	a.Start()
	defer a.Close()

	var b *Core
	if dirB := os.Getenv("SZEPT_SEED_B"); dirB != "" {
		// drugi katalog danych: podkładamy go na telefon, żeby obejrzeć widok
		// ODBIORCY dużego pliku (podgląd + „pobierz pełny plik”)
		os.MkdirAll(dirB, 0o700)
		idb := identity.Create()
		idb.Name = "bartek"
		if err := idb.Save(dirB+"/identity.key", nil); err != nil {
			t.Fatal(err)
		}
		var err error
		if b, err = Open(Config{Connector: url, DataDir: dirB}, idb); err != nil {
			t.Fatal(err)
		}
		b.Start()
		t.Cleanup(b.Close)
	} else {
		b = newCore(t, "bartek", url)
	}
	cz := newCore(t, "czarek", url)
	for _, p := range []*Core{b, cz} {
		if _, err := a.AddFriend(p.FriendCode()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.AddFriend(a.FriendCode()); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetAvatar(seedPNG(t, 200, 200, 1)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatar(seedPNG(t, 200, 200, 2)); err != nil {
		t.Fatal(err)
	}

	// czat na dwoje
	fs, _ := a.St.Friends()
	var bf = fs[0]
	for _, f := range fs {
		if f.Name == "bartek" {
			bf = f
		}
	}
	dm, err := a.CreateDirect(bf)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bartek widzi dm", 60*time.Second, func() bool {
		a.connectAll() // pierwsze wyszukanie peera może wypaść przed rejestracją
		_, err := b.ChatState(dm)
		return err == nil
	})
	a.Send(dm, "hej, działa ci już ta appka?")
	waitFor(t, "b ma wiadomość", 30*time.Second, func() bool {
		return len(b.Messages(dm)) > 1
	})
	b.Send(dm, "działa! ale scrollowanie było do niczego 😅")
	b.Send(dm, "zobacz co wczoraj zrobiłem")
	if err := b.SendFile(dm, "rysunek.png", seedPNG(t, 900, 600, 3), "mój rysunek"); err != nil {
		t.Fatal(err)
	}
	a.Send(dm, "ładne. wysyłam ci ten wielki plik, o którym mówiłem")
	big := testBigPNG(t)
	if err := a.SendFile(dm, "skan-mapy.png", big, "cała mapa, 13 MB"); err != nil {
		t.Fatal(err)
	}
	if err := a.SendFile(dm, "notatki.pdf",
		append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("notatki "), 400)...), ""); err != nil {
		t.Fatal(err)
	}

	// grupa: ala zakłada, pozostali wchodzą z zaproszenia
	grp, err := a.CreateChat("ekipa z osiedla")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*Core{b, cz} {
		code, pin, err := a.NewInvite(grp)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Redeem(code, pin); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "wchodzi do grupy", 25*time.Second, func() bool {
			st, err := p.ChatState(grp)
			return err == nil && st.IsMember(p.ID.Pub)
		})
	}
	b.Send(grp, "kto jedzie w weekend nad wodę?")
	cz.Send(grp, "ja wchodzę")
	a.Send(grp, "bierzcie kajaki, reszta na miejscu")
	cz.Send(grp, "a mogę wziąć psa?")
	b.SendFile(grp, "mapka.png", seedPNG(t, 640, 400, 5), "trasa")

	// profil ustawiony przed dołączeniem do czatów dochodzi tą samą drogą,
	// co w aplikacji (Start() woła to w pętli)
	for _, p := range []*Core{a, b, cz} {
		p.announceProfile()
	}
	waitFor(t, "ala ma wszystko", 30*time.Second, func() bool {
		return len(a.Messages(grp)) >= 7 && len(a.Messages(dm)) >= 7
	})
	time.Sleep(2 * time.Second) // domknięcie transferów blobów
	t.Logf("zasiane w %s: dm=%d grupa=%d", dir, len(a.Messages(dm)), len(a.Messages(grp)))
	if hold := os.Getenv("SZEPT_SEED_HOLD"); hold != "" {
		// zamykamy bartka (jego tożsamość przejmuje telefon) i trzymamy alę
		// online, żeby telefon mógł pobrać od niej pełny plik
		d, err := time.ParseDuration(hold)
		if err != nil {
			t.Fatal(err)
		}
		b.Close()
		cz.Close()
		t.Logf("ala online przez %s na %s", d, url)
		time.Sleep(d)
	}
}
