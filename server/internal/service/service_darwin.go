package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Install writes the agent and starts it, replacing an earlier one.
func Install(executable, log string) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	definition, err := Plist(executable, log)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, definition, 0o644); err != nil {
		return err
	}
	exec.Command("launchctl", "bootout", target()).Run() // fine if it wasn't loaded
	return launchctl("bootstrap", domain(), path)
}

// Uninstall stops the agent and removes it.
func Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	exec.Command("launchctl", "bootout", target()).Run() // fine if it wasn't loaded
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }
func target() string { return domain() + "/" + Label }

func launchctl(args ...string) error {
	output, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", args[0], err, bytes.TrimSpace(output))
	}
	return nil
}

// Restart restarts the agent, if it's installed, so it runs a new binary. It
// reports whether there was one.
func Restart() (bool, error) {
	if exec.Command("launchctl", "print", target()).Run() != nil {
		return false, nil
	}
	return true, launchctl("kickstart", "-k", target())
}
