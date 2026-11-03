package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voistic/server/internal/board"
)

func TestAddOffersTheImage(t *testing.T) {
	dir := t.TempDir()
	image := header(13) // ESP32-C6
	path := filepath.Join(dir, "firmware.bin")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(dir, "store")}
	if _, ok := store.Get("voistic-9af4"); ok {
		t.Fatal("offered something before Add")
	}
	if _, err := store.Add("voistic-9af4", board.IoTButtonC6, path, "1.2"); err != nil {
		t.Fatal(err)
	}
	info, ok := store.Get("voistic-9af4")
	sum := sha256.Sum256(image)
	if !ok || info.Version != "1.2" || info.Size != int64(len(image)) || info.SHA256 != hex.EncodeToString(sum[:]) ||
		info.File != "voistic-9af4-iot-button-c6.bin" {
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
	if _, err := (Store{Dir: dir}).Add("voistic-9af4", board.XiaoS3, path, "1.2"); err == nil {
		t.Error("no error")
	}
}

// header is the start of an image for a chip, as the build writes it.
func header(chip byte) []byte {
	data := make([]byte, 24)
	data[0] = imageMagic
	data[chipOffset] = chip
	return append(data, "code"...)
}

func TestAddRefusesAnImageForAnotherChip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "firmware.bin")
	if err := os.WriteFile(path, header(9), 0o600); err != nil { // ESP32-S3
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(dir, "store")}
	_, err := store.Add("voistic-25bc", board.IoTButtonC6, path, "1.2")
	if err == nil || !strings.Contains(err.Error(), "ESP32-S3") {
		t.Fatalf("error %v", err)
	}
	if _, ok := store.Get("voistic-25bc"); ok {
		t.Error("offered after all")
	}
}

func TestOlderEntriesStillOpen(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: dir}
	os.WriteFile(filepath.Join(dir, "voistic-9af4.bin"), []byte("old"), 0o600)
	os.WriteFile(filepath.Join(dir, "voistic-9af4.json"), []byte(`{"version":"1","size":3,"sha256":"x"}`), 0o600)
	file, err := store.Open("voistic-9af4")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()

	path := filepath.Join(dir, "new.bin")
	os.WriteFile(path, header(9), 0o600)
	if _, err := store.Add("voistic-9af4", board.XiaoS3, path, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "voistic-9af4.bin")); !os.IsNotExist(err) {
		t.Error("the old image file is still there")
	}
}
