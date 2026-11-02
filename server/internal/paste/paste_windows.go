package paste

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// These go straight to Win32, so the text never passes through a shell or a
// script, and a press takes no process start.
var (
	user32           = syscall.NewLazyDLL("user32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	sendInput        = user32.NewProc("SendInput")

	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	globalAlloc  = kernel32.NewProc("GlobalAlloc")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	globalFree   = kernel32.NewProc("GlobalFree")
	moveMemory   = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	inputKeyboard = 1
	keyEventUp    = 0x0002

	// V is a virtual key, not a character, so it works on a Russian layout too.
	vkControl = 0x11
	vkV       = 0x56

	// Another program may be holding the clipboard for a moment.
	clipboardTries = 40
	clipboardWait  = 25 * time.Millisecond
)

var virtualKeys = map[string]uint16{"enter": 0x0D, "tab": 0x09, "esc": 0x1B}

// keyInput is an INPUT holding a KEYBDINPUT, with the padding of the union in a
// 64-bit build.
type keyInput struct {
	kind  uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

func pasteText(ctx context.Context, text string) error {
	if err := setClipboard(ctx, text); err != nil {
		return err
	}
	return press(vkControl, vkV)
}

func pressKey(_ context.Context, name string) error {
	return press(virtualKeys[name])
}

// chord presses the keys in order and lets them go in reverse.
func chord(keys ...uint16) []keyInput {
	inputs := make([]keyInput, 0, 2*len(keys))
	for _, key := range keys {
		inputs = append(inputs, keyInput{kind: inputKeyboard, vk: key})
	}
	for i := len(keys) - 1; i >= 0; i-- {
		inputs = append(inputs, keyInput{kind: inputKeyboard, vk: keys[i], flags: keyEventUp})
	}
	return inputs
}

func press(keys ...uint16) error {
	inputs := chord(keys...)
	sent, _, err := sendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if sent != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput, the active window may be running as administrator: %w", err)
	}
	return nil
}

func setClipboard(ctx context.Context, text string) error {
	data, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	// The clipboard belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := waitForClipboard(ctx); err != nil {
		return err
	}
	defer closeClipboard.Call()
	if done, _, err := emptyClipboard.Call(); done == 0 {
		return fmt.Errorf("EmptyClipboard: %w", err)
	}

	size := uintptr(len(data)) * unsafe.Sizeof(data[0])
	memory, _, err := globalAlloc.Call(gmemMoveable, size)
	if memory == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	locked, _, err := globalLock.Call(memory)
	if locked == 0 {
		globalFree.Call(memory)
		return fmt.Errorf("GlobalLock: %w", err)
	}
	moveMemory.Call(locked, uintptr(unsafe.Pointer(&data[0])), size)
	globalUnlock.Call(memory)

	// The clipboard owns the memory from here, unless this fails.
	if handle, _, err := setClipboardData.Call(cfUnicodeText, memory); handle == 0 {
		globalFree.Call(memory)
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	return nil
}

func waitForClipboard(ctx context.Context) error {
	for range clipboardTries {
		if opened, _, _ := openClipboard.Call(0); opened != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(clipboardWait):
		}
	}
	return errors.New("another program is holding the clipboard")
}
