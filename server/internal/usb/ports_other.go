//go:build !darwin

package usb

import (
	"strings"

	"go.bug.st/serial/enumerator"
)

// espressif is the USB vendor of the ESP32's own USB port.
const espressif = "303A"

// Ports lists the serial ports a button can be on.
func Ports() ([]string, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, port := range ports {
		if port.IsUSB && strings.EqualFold(port.VID, espressif) {
			names = append(names, port.Name)
		}
	}
	return names, nil
}
