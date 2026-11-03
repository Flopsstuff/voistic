package paste

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The text never goes through AppleScript, only the keystroke does, so nothing
// a person says can turn into a script.
//
// Key code 9 is the physical V key. A keystroke "v" means a character, which a
// Russian layout doesn't have.
const pressPaste = `tell application "System Events" to key code 9 using command down`

var keyCodes = map[string]int{"enter": 36, "tab": 48, "esc": 53}

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

// Pressing a key needs the Accessibility permission for the app that started
// the server.
func pasteText(ctx context.Context, text string) error {
	if err := run(ctx, text, "pbcopy"); err != nil {
		return err
	}
	return run(ctx, "", "osascript", "-e", pressPaste)
}

// pressBackspace runs one script, so the presses don't each start a process.
// Key code 51 is Delete, the Mac's backspace.
func pressBackspace(ctx context.Context, count int) error {
	script := fmt.Sprintf("tell application \"System Events\"\nrepeat %d times\nkey code 51\nend repeat\nend tell", count)
	return run(ctx, "", "osascript", "-e", script)
}

func pressKey(ctx context.Context, name string) error {
	script := fmt.Sprintf(`tell application "System Events" to key code %d`, keyCodes[name])
	return run(ctx, "", "osascript", "-e", script)
}
