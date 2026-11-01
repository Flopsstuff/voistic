// Package pairing tracks the short window in which a new button may get a key.
package pairing

import (
	"context"
	"errors"
	"sync"
)

// ErrBusy means a window is already open.
var ErrBusy = errors.New("pairing is already in progress")

// Window lets one device pair while it's open. The zero value is closed.
type Window struct {
	mu     sync.Mutex
	paired chan string
}

// IsOpen reports whether a device can pair right now.
func (w *Window) IsOpen() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paired != nil
}

// Wait opens the window until a device pairs or ctx ends, and returns the
// device's id.
func (w *Window) Wait(ctx context.Context) (string, error) {
	paired, err := w.open()
	if err != nil {
		return "", err
	}
	select {
	case device := <-paired:
		return device, nil
	case <-ctx.Done():
		w.close()
		// A device may have claimed the window just as ctx ended.
		select {
		case device := <-paired:
			return device, nil
		default:
			return "", ctx.Err()
		}
	}
}

// Claim closes the window for device and reports whether it was open, so only
// the first device to knock gets in.
func (w *Window) Claim(device string) bool {
	w.mu.Lock()
	paired := w.paired
	w.paired = nil
	w.mu.Unlock()

	if paired == nil {
		return false
	}
	paired <- device // buffered, Wait may be gone
	return true
}

func (w *Window) open() (<-chan string, error) {
	w.mu.Lock()
	if w.paired != nil {
		w.mu.Unlock()
		return nil, ErrBusy
	}
	paired := make(chan string, 1)
	w.paired = paired
	w.mu.Unlock()
	return paired, nil
}

func (w *Window) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paired = nil
}
