// Package service runs the server as a launchd agent, so it starts with the
// login and comes back after a crash. An agent rather than a daemon, because
// pressing keys only works inside the user's session.
package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Label names the agent to launchd. It's also the identifier the binary is
// signed with.
const Label = "app.voistic.server"

const defaultPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// Plist returns the agent's definition. launchd passes no user environment, so
// PATH is spelled out for the actions in the config.
func Plist(executable, log string) ([]byte, error) {
	var out bytes.Buffer
	escape := func(s string) string {
		var escaped bytes.Buffer
		xml.EscapeText(&escaped, []byte(s))
		return escaped.String()
	}
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, Label, escape(executable), defaultPath, escape(log), escape(log))
	return out.Bytes(), nil
}

// Install writes the agent and starts it, replacing an earlier one.
func Install(executable, log string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("install only works on macOS for now")
	}
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
	if runtime.GOOS != "darwin" {
		return errors.New("uninstall only works on macOS for now")
	}
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
