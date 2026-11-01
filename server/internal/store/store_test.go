package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutPersistsPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("a", Device{Key: "k", Seq: 7}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if device, ok := reopened.Device("a"); !ok || device.Seq != 7 || device.Key != "k" {
		t.Errorf("device = %+v, %v", device, ok)
	}
}

func TestPutKeepsMemoryOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("a", Device{Seq: 1}); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := s.Put("a", Device{Seq: 2}); err == nil {
		t.Fatal("want a write error")
	}
	if device, _ := s.Device("a"); device.Seq != 1 {
		t.Errorf("Seq = %d, want 1", device.Seq)
	}
}

func TestOpenRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("want an error")
	}
}
