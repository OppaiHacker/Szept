package identity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"szept/internal/crypt"
)

// A v1 file (no database key) still opens and yields the old seed-derived key.
func TestLoadV1(t *testing.T) {
	id := Create()
	path := filepath.Join(t.TempDir(), "id")
	salt := crypt.RandBytes(16)
	box, _ := crypt.Seal(crypt.PassKey(nil, salt), append(id.Priv.Seed(), "ala"...), []byte(magicV1))
	os.WriteFile(path, append(append([]byte(magicV1), salt...), box...), 0o600)
	got, err := Load(path, nil)
	if err != nil || got.Name != "ala" || !bytes.Equal(got.DBKey, crypt.Derive(id.Priv.Seed(), "szept-storage-v1")) {
		t.Fatal(err, got)
	}
	got.Save(path, nil)
	again, err := Load(path, nil)
	if err != nil || again.Name != "ala" || !bytes.Equal(again.DBKey, got.DBKey) {
		t.Fatal("v2 roundtrip", err)
	}
}
