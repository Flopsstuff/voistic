// Package recording writes the voice a button streams into WAV files.
package recording

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	headerBytes   = 44
	bytesPerFrame = 2 // 16-bit mono
	partSuffix    = ".part"
)

// File is a recording in progress. It stays a .part file until Close, so a
// finished name always holds a complete WAV.
type File struct {
	file  *os.File
	path  string
	rate  int
	bytes int64
}

// Create starts a recording of 16-bit mono audio at rate in dir, which is
// created if needed. The folder is private, since it holds what people said.
func Create(dir, device string, rate int, at time.Time) (*File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s-%s.wav", device, at.Format("20060102-150405"))
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path+partSuffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	// The sizes aren't known yet, so Close fills the header in.
	if _, err := file.Write(make([]byte, headerBytes)); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return &File{file: file, path: path, rate: rate}, nil
}

// Write appends raw little-endian samples.
func (f *File) Write(samples []byte) (int, error) {
	n, err := f.file.Write(samples)
	f.bytes += int64(n)
	return n, err
}

// Close finishes the file and returns its path and length.
func (f *File) Close() (string, time.Duration, error) {
	data := f.bytes - f.bytes%bytesPerFrame
	if data > 0xFFFFFFFF-headerBytes {
		f.Discard()
		return "", 0, errors.New("recording too long for a WAV")
	}
	if _, err := f.file.WriteAt(header(uint32(data), f.rate), 0); err != nil {
		f.Discard()
		return "", 0, err
	}
	if err := f.file.Close(); err != nil {
		os.Remove(f.file.Name())
		return "", 0, err
	}
	if err := os.Rename(f.file.Name(), f.path); err != nil {
		return "", 0, err
	}
	frames := data / bytesPerFrame
	return f.path, time.Duration(frames) * time.Second / time.Duration(f.rate), nil
}

// Discard drops the recording.
func (f *File) Discard() {
	f.file.Close()
	os.Remove(f.file.Name())
}

// Remove deletes a recording and the transcript next to it.
func Remove(path string) error {
	transcript := strings.TrimSuffix(path, filepath.Ext(path)) + ".txt"
	return errors.Join(removeIfThere(path), removeIfThere(transcript))
}

func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func header(data uint32, rate int) []byte {
	out := make([]byte, 0, headerBytes)
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, headerBytes-8+data)
	out = append(out, "WAVEfmt "...)
	out = binary.LittleEndian.AppendUint32(out, 16)                         // fmt chunk size
	out = binary.LittleEndian.AppendUint16(out, 1)                          // PCM
	out = binary.LittleEndian.AppendUint16(out, 1)                          // mono
	out = binary.LittleEndian.AppendUint32(out, uint32(rate))               // frames per second
	out = binary.LittleEndian.AppendUint32(out, uint32(rate*bytesPerFrame)) // bytes per second
	out = binary.LittleEndian.AppendUint16(out, bytesPerFrame)              // bytes per frame
	out = binary.LittleEndian.AppendUint16(out, 8*bytesPerFrame)            // bits per sample
	out = append(out, "data"...)
	return binary.LittleEndian.AppendUint32(out, data)
}
