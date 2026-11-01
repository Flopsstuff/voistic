package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestAddOffersTheImage(t *testing.T) {
	dir := t.TempDir()
	image := append([]byte{imageMagic}, "code"...)
	path := filepath.Join(dir, "firmware.bin")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(dir, "store")}
	if _, ok := store.Get("voistic-9af4"); ok {
		t.Fatal("offered something before Add")
	}
	if _, err := store.Add("voistic-9af4", path, "1.2"); err != nil {
		t.Fatal(err)
	}
	info, ok := store.Get("voistic-9af4")
	sum := sha256.Sum256(image)
	if !ok || info.Version != "1.2" || info.Size != 5 || info.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("info %+v, %v", info, ok)
	}
	file, err := store.Open("voistic-9af4")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if got, _ := io.ReadAll(file); string(got) != string(image) {
		t.Errorf("image %q", got)
	}
}

func TestAddRefusesWhatIsNotAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Dir: dir}).Add("voistic-9af4", path, "1.2"); err == nil {
		t.Error("no error")
	}
}
