// Package paste works the keyboard of this computer: it types a transcript into
// whatever window is active and presses single keys, on macOS and Windows.
package paste

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Keys names the keys an action can press.
var Keys = []string{"enter", "tab", "esc"}

// IsKey reports whether name is one of Keys.
func IsKey(name string) bool { return slices.Contains(Keys, name) }

// UnpasteWithin is how long after a paste Unpaste still erases it. The longer
// it is, the likelier the cursor has moved or more was typed since.
const UnpasteWithin = 2 * time.Minute

// ErrNothingToUnpaste means there was no paste to erase: none yet, already
// erased, or too long ago.
var ErrNothingToUnpaste = errors.New("nothing to unpaste")

// The last paste, which Unpaste erases.
var (
	lastMu    sync.Mutex
	lastChars int
	lastAt    time.Time
)

// Text puts text on the clipboard, where it stays, and pastes it. Line breaks
// become spaces, so a paste is always one line: Claude Code folds pasted lines
// into a placeholder that one backspace removes whole, and Unpaste counts on
// one backspace a character.
func Text(ctx context.Context, text string) error {
	text = strings.Join(strings.FieldsFunc(text, isLineBreak), " ")
	if text == "" {
		return nil
	}
	if err := pasteText(ctx, text); err != nil {
		return err
	}
	lastMu.Lock()
	lastChars, lastAt = utf8.RuneCountInString(text), time.Now()
	lastMu.Unlock()
	return nil
}

func isLineBreak(r rune) bool { return r == '\n' || r == '\r' }

// Unpaste erases the last paste with one backspace for each character, all
// sent at once, and returns how many. It counts on the cursor being right after
// the paste. Each paste can be erased once, within UnpasteWithin.
func Unpaste(ctx context.Context) (int, error) {
	lastMu.Lock()
	chars, at := lastChars, lastAt
	lastChars = 0
	lastMu.Unlock()
	if chars == 0 || time.Since(at) > UnpasteWithin {
		return 0, ErrNothingToUnpaste
	}
	return chars, pressBackspace(ctx, chars)
}

// Key presses one of Keys.
func Key(ctx context.Context, name string) error {
	if !IsKey(name) {
		return fmt.Errorf("unknown key %q", name)
	}
	return pressKey(ctx, name)
}
