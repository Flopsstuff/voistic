package recording

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloseWritesAPlayableWav(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	at := time.Date(2026, 11, 1, 17, 30, 5, 0, time.UTC)
	file, err := Create(dir, "voistic-9af4", 16000, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(make([]byte, 32000)); err != nil {
		t.Fatal(err)
	}
	path, length, err := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "voistic-9af4-20261101-173005.wav" {
		t.Errorf("path %s", path)
	}
	if length != time.Second {
		t.Errorf("length %v", length)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != headerBytes+32000 || string(data[:4]) != "RIFF" || string(data[36:40]) != "data" {
		t.Fatalf("bad file of %d bytes", len(data))
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != 32000 {
		t.Errorf("data size %d", got)
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != 16000 {
		t.Errorf("rate %d", got)
	}
}

func TestDiscardLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	file, err := Create(dir, "voistic-9af4", 16000, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	file.Discard()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
}

func TestRemoveTakesTheTranscriptToo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one.wav")
	for _, name := range []string{path, filepath.Join(dir, "one.txt"), filepath.Join(dir, "two.wav")} {
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "two.wav" {
		t.Errorf("left %v", entries)
	}
	if err := Remove(path); err != nil {
		t.Errorf("a second remove failed: %v", err)
	}
}
