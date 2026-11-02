// Package paste works the keyboard of this computer: it types a transcript into
// whatever window is active and presses single keys, on macOS and Windows.
package paste

import (
	"context"
	"fmt"
	"slices"
)

// Keys names the keys an action can press.
var Keys = []string{"enter", "tab", "esc"}

// IsKey reports whether name is one of Keys.
func IsKey(name string) bool { return slices.Contains(Keys, name) }

// Text puts text on the clipboard, where it stays, and pastes it.
func Text(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	return pasteText(ctx, text)
}

// Key presses one of Keys.
func Key(ctx context.Context, name string) error {
	if !IsKey(name) {
		return fmt.Errorf("unknown key %q", name)
	}
	return pressKey(ctx, name)
}
