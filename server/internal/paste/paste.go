// Package paste types a transcript into whatever window is active on this Mac.
package paste

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// The text never goes through AppleScript, only the keystroke does, so nothing
// a person says can turn into a script.
//
// Key code 9 is the physical V key. A keystroke "v" means a character, which a
// Russian layout doesn't have.
const pressPaste = `tell application "System Events" to key code 9 using command down`

// run starts a command with stdin and returns what it wrote to stderr on failure.
var run = func(ctx context.Context, stdin string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// pbcopy reads Cyrillic as garbage unless the locale says UTF-8, and a
	// service doesn't inherit one.
	cmd.Env = append(os.Environ(), "LANG=en_US.UTF-8", "LC_CTYPE=UTF-8")
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Text puts text on the clipboard, where it stays, and presses Cmd+V. Pressing a
// key needs the Accessibility permission for the app that started the server.
func Text(ctx context.Context, text string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("pasting only works on macOS")
	}
	if text == "" {
		return nil
	}
	if err := run(ctx, text, "pbcopy"); err != nil {
		return err
	}
	return run(ctx, "", "osascript", "-e", pressPaste)
}
