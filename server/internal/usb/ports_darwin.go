package usb

import "path/filepath"

// Ports lists the serial ports a button can be on. The ESP32's own USB shows up
// as a usbmodem, and IOKit, which could tell its vendor, needs cgo.
func Ports() ([]string, error) {
	return filepath.Glob("/dev/cu.usbmodem*")
}
