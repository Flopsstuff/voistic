// Package firmware keeps the image each button should be running, so a button
// can fetch it over the LAN instead of being flashed over USB.
package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// imageMagic is the first byte of every ESP32 application image.
const imageMagic = 0xE9

// Info describes an image. The hash goes to the button in a signed answer, which
// is what makes the unsigned download safe.
type Info struct {
	Version string `json:"version"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// Store keeps one image per device in a folder.
type Store struct {
	Dir string
}

// Add makes the file at path the image for device.
func (s Store) Add(device, path, version string) (Info, error) {
	if version == "" {
		return Info{}, errors.New("no version")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	if len(data) == 0 || data[0] != imageMagic {
		return Info{}, fmt.Errorf("%s is not a firmware image", path)
	}
	sum := sha256.Sum256(data)
	info := Info{Version: version, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}

	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Info{}, err
	}
	if err := writeAtomically(s.imagePath(device), data); err != nil {
		return Info{}, err
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		return Info{}, err
	}
	return info, writeAtomically(s.infoPath(device), encoded)
}

// Get returns what's on offer for device, if anything.
func (s Store) Get(device string) (Info, bool) {
	data, err := os.ReadFile(s.infoPath(device))
	if err != nil {
		return Info{}, false
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, false
	}
	return info, true
}

// Open returns the image for device.
func (s Store) Open(device string) (io.ReadCloser, error) {
	return os.Open(s.imagePath(device))
}

func (s Store) imagePath(device string) string { return filepath.Join(s.Dir, device+".bin") }
func (s Store) infoPath(device string) string  { return filepath.Join(s.Dir, device+".json") }

func writeAtomically(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
