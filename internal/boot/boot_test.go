package boot

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestChangePass(t *testing.T) {
	dir := t.TempDir()
	id, err := CreateIdentity(dir, "ala", []byte("stare"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ChangePass(dir, []byte("złe"), []byte("nowe")); err == nil {
		t.Fatal("złe stare hasło przeszło")
	}
	if err := ChangePass(dir, []byte("stare"), []byte("nowe")); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIdentity(dir, []byte("nowe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Pub, id.Pub) || got.Name != "ala" {
		t.Fatal("tożsamość zmieniła się po zmianie hasła")
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
