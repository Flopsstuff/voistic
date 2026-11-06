package main

import (
	"os"
	"testing"
)

func TestWriteTempKeepsTheImage(t *testing.T) {
	path, err := writeTemp([]byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if data, _ := os.ReadFile(path); string(data) != "image" {
		t.Errorf("wrote %q", data)
	}
}

func TestWriteTempReportsAFailure(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent/voistic")
	t.Setenv("TMP", `Z:\nonexistent\voistic`)
	if _, err := writeTemp([]byte("image")); err == nil {
		t.Error("a temporary folder that isn't there should fail")
	}
}
