//go:build !darwin && !windows

package wlan

import (
	"fmt"
	"os/exec"
	"strings"
)

// Current asks NetworkManager. Terse output escapes : and \ unless told not
// to, which would garble a name or a password that has them.
func Current() (Network, error) {
	out, err := exec.Command("nmcli", "--escape", "no", "-t", "-f", "active,ssid", "dev", "wifi").Output()
	if err != nil {
		return Network{}, ErrUnknown
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if ssid, ok := strings.CutPrefix(line, "yes:"); ok && ssid != "" {
			return Network{SSID: ssid}, nil
		}
	}
	return Network{}, ErrUnknown
}

// Password reads the connection NetworkManager keeps under the network's name.
func Password(ssid string) (string, error) {
	out, err := exec.Command("nmcli", "--escape", "no", "-s", "-g", "802-11-wireless-security.psk", "connection", "show", ssid).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("no password saved for %s", ssid)
	}
	return strings.TrimSpace(string(out)), nil
}
