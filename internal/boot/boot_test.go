package boot

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"szept/internal/crypt"
	"szept/internal/identity"
	"szept/internal/store"
)

func TestChangePass(t *testing.T) {
	dir := t.TempDir()
	id, err := CreateIdentity(dir, "ala", []byte("stare"))
	if err != nil {
		t.Fatal(err)
	}
	oldCopy, _ := os.ReadFile(IdentityPath(dir))
	db := filepath.Join(dir, "szept.db")
	st, err := store.Open(db, id.StorageKey())
	if err != nil {
		t.Fatal(err)
	}
	st.PutMeta("nick", []byte("ala"))
	st.PutChatKey([]byte("chat"), 1, []byte("k1"))
	if err := ChangePass(dir, []byte("złe"), []byte("nowe"), st.Rekey); err == nil {
		t.Fatal("złe stare hasło przeszło")
	}
	if err := ChangePass(dir, []byte("stare"), []byte("nowe"), st.Rekey); err != nil {
		t.Fatal(err)
	}
	st.Close()
	got, err := LoadIdentity(dir, []byte("nowe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Pub, id.Pub) || got.Name != "ala" {
		t.Fatal("tożsamość zmieniła się po zmianie hasła")
	}
	if store.UsesKey(db, id.StorageKey()) || !store.UsesKey(db, got.StorageKey()) {
		t.Fatal("baza nie dostała nowego klucza")
	}
	st, err = store.Open(db, got.StorageKey())
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := st.ChatKeys([]byte("chat"))
	if string(st.Meta("nick")) != "ala" || string(keys[1]) != "k1" {
		t.Fatal("dane zginęły przy rekey")
	}
	st.Close()
	if _, err := store.Open(db, id.StorageKey()); err != store.ErrWrongKey {
		t.Fatalf("stary klucz: %v", err)
	}

	// stara kopia pliku ze starym hasłem nie otwiera bazy
	os.WriteFile(IdentityPath(dir)+".bak", oldCopy, 0o600)
	stale, err := identity.Load(IdentityPath(dir)+".bak", []byte("stare"))
	if err != nil || store.UsesKey(db, stale.StorageKey()) {
		t.Fatal("stara kopia nadal otwiera bazę")
	}
}

// Awaria między krokami ChangePass: LoadIdentity kończy albo cofa zmianę.
func TestChangePassCrash(t *testing.T) {
	dir := t.TempDir()
	path, db := IdentityPath(dir), filepath.Join(dir, "szept.db")
	id, _ := CreateIdentity(dir, "ala", []byte("stare"))
	st, _ := store.Open(db, id.StorageKey())
	st.Close()

	// przed rekey: nowe hasło odrzucone, stare działa, .new znika
	nid := *id
	nid.DBKey = crypt.NewKey()
	nid.Save(path+".new", []byte("nowe"))
	if _, err := LoadIdentity(dir, []byte("nowe")); err == nil {
		t.Fatal("nowe hasło przed rekey")
	}
	nid.Save(path+".new", []byte("nowe"))
	if _, err := LoadIdentity(dir, []byte("stare")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".new"); err == nil {
		t.Fatal(".new zostało")
	}

	// po rekey: stare hasło odrzucone, nowe kończy zmianę
	nid.Save(path+".new", []byte("nowe"))
	st, _ = store.Open(db, id.StorageKey())
	st.Rekey(nid.DBKey)
	st.Close()
	if _, err := LoadIdentity(dir, []byte("stare")); err == nil {
		t.Fatal("stare hasło po rekey")
	}
	if _, err := LoadIdentity(dir, []byte("nowe")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(dir, []byte("nowe")); err != nil {
		t.Fatal("zmiana nie zapisana", err)
	}
}

func TestBackupRestore(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateIdentity(dir, "ala", nil); err != nil {
		t.Fatal(err)
	}
	db := []byte("udawana-baza")
	var buf bytes.Buffer
	snap := func(w io.Writer) error { _, err := w.Write(db); return err }
	if err := Backup(dir, snap, &buf); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "kopia.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "restored")
	if err := Restore(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "szept.db"))
	if err != nil || !bytes.Equal(got, db) {
		t.Fatalf("baza po restore: %v %q", err, got)
	}
	if _, err := LoadIdentity(dest, nil); err != nil {
		t.Fatalf("tożsamość po restore: %v", err)
	}
}
