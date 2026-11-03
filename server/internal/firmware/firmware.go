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

	"voistic/server/internal/board"
)

// imageMagic is the first byte of every ESP32 application image.
const imageMagic = 0xE9

// Info describes an image. The hash goes to the button in a signed answer, which
// is what makes the unsigned download safe.
type Info struct {
	Version string `json:"version"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Board   string `json:"board,omitempty"` // what it was checked against, empty before boards
	File    string `json:"file,omitempty"`  // in Dir, such as voistic-25bc-iot-button-c6.bin
}

// Store keeps one image per device in a folder.
type Store struct {
	Dir string
}

// Add makes the file at path the image for device, a button on the board kind.
// An image for another chip is turned away, since it would only fail on the
// button, after the download. The file is named after both, so a look at the
// folder tells which board each image is for.
func (s Store) Add(device, kind, path, version string) (Info, error) {
	if version == "" {
		return Info{}, errors.New("no version")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	got, err := chipOf(data)
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", path, err)
	}
	want, known := board.Chip(kind)
	if !known {
		return Info{}, fmt.Errorf("%s is a %s, which this server has no chip for", device, kind)
	}
	if got != want {
		return Info{}, fmt.Errorf("%s is for %s, but %s runs on %s, which has %s",
			path, board.ChipName(got), device, kind, board.ChipName(want))
	}

	sum := sha256.Sum256(data)
	info := Info{
		Version: version,
		Size:    int64(len(data)),
		SHA256:  hex.EncodeToString(sum[:]),
		Board:   kind,
		File:    device + "-" + kind + ".bin",
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Info{}, err
	}
	previous, hadPrevious := s.Get(device)
	if err := writeAtomically(filepath.Join(s.Dir, info.File), data); err != nil {
		return Info{}, err
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		return Info{}, err
	}
	if err := writeAtomically(s.infoPath(device), encoded); err != nil {
		return Info{}, err
	}
	if hadPrevious && s.file(previous, device) != info.File {
		os.Remove(filepath.Join(s.Dir, s.file(previous, device)))
	}
	return info, nil
}

// chipOffset is where the extended header of an image keeps its chip ID, after
// the 8-byte image header and 4 bytes of flash pin settings.
const chipOffset = 12

// chipOf returns the chip an image was built for.
func chipOf(data []byte) (uint16, error) {
	if len(data) < chipOffset+2 || data[0] != imageMagic {
		return 0, errors.New("not a firmware image")
	}
	return uint16(data[chipOffset]) | uint16(data[chipOffset+1])<<8, nil
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
	info, ok := s.Get(device)
	if !ok {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(s.Dir, s.file(info, device)))
}

// file is where an image is, which before boards was just the device's name.
func (s Store) file(info Info, device string) string {
	if info.File != "" {
		return info.File
	}
	return device + ".bin"
}

func (s Store) infoPath(device string) string { return filepath.Join(s.Dir, device+".json") }

func writeAtomically(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
