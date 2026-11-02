package service

import (
	"os"
	"path/filepath"
	"testing"
)

// The real schtasks has to accept the definition, which nothing else can check.
func TestInstallRegistersATask(t *testing.T) {
	if os.Getenv("CI") == "" {
		t.Skip("touches the scheduled tasks of this user")
	}
	dir := t.TempDir()
	if err := Install(filepath.Join(dir, "voistic.exe"), filepath.Join(dir, "serve.log")); err == nil {
		t.Fatal("no error without voisticw.exe")
	}
	// A program that exits at once, so the task has nothing left running.
	system := filepath.Join(os.Getenv("SystemRoot"), "System32", "whoami.exe")
	data, err := os.ReadFile(system)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, background), data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(filepath.Join(dir, "voistic.exe"), filepath.Join(dir, "serve.log")); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}
