package wlan

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

// Current asks networksetup, and where macOS hides the name from a program
// without location access, takes the first of the preferred networks, which is
// the one joined last.
func Current() (Network, error) {
	device, err := wifiDevice()
	if err != nil {
		return Network{}, err
	}
	out, _ := exec.Command("networksetup", "-getairportnetwork", device).Output()
	if ssid, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "Current Wi-Fi Network: "); ok && ssid != "" {
		return Network{SSID: ssid}, nil
	}
	out, err = exec.Command("networksetup", "-listpreferredwirelessnetworks", device).Output()
	if err != nil {
		return Network{}, ErrUnknown
	}
	if ssid := firstPreferred(string(out)); ssid != "" {
		return Network{SSID: ssid, Guessed: true}, nil
	}
	return Network{}, ErrUnknown
}

// Password reads the keychain, which may ask the user to allow it.
func Password(ssid string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-D", "AirPort network password", "-a", ssid, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("no password for %s in the keychain", ssid)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func wifiDevice() (string, error) {
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return "", err
	}
	if device := wifiPort(string(out)); device != "" {
		return device, nil
	}
	return "", ErrUnknown
}

// wifiPort finds the device under "Hardware Port: Wi-Fi".
func wifiPort(listing string) string {
	scanner := bufio.NewScanner(strings.NewReader(listing))
	wifi := false
	for scanner.Scan() {
		line := scanner.Text()
		if port, ok := strings.CutPrefix(line, "Hardware Port: "); ok {
			wifi = port == "Wi-Fi" || port == "AirPort"
		} else if device, ok := strings.CutPrefix(line, "Device: "); ok && wifi {
			return device
		}
	}
	return ""
}

func firstPreferred(listing string) string {
	lines := strings.Split(listing, "\n")
	if len(lines) < 2 {
		return ""
	}
	return strings.TrimSpace(lines[1])
}
