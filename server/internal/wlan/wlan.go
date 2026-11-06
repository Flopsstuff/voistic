// Package wlan finds the Wi-Fi network this computer is on and the password it
// saved for it, so a button can be put on the same network without typing it.
package wlan

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
)

// ErrUnknown means the system doesn't say.
var ErrUnknown = errors.New("can't tell which Wi-Fi network this computer is on")

// Network is the one this computer is on. Guessed is set where the system hides
// the name and it's taken from the saved networks instead.
type Network struct {
	SSID    string
	Guessed bool
}

// profileKey reads the password from a Windows Wi-Fi profile, as netsh exports
// it. It's XML, so a password with & or < in it comes as entities, which the
// decoder turns back.
func profileKey(profile []byte) (string, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(profile))
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", false
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "keyMaterial" {
			var key string
			if err := decoder.DecodeElement(&key, &start); err != nil {
				return "", false
			}
			return strings.TrimSpace(key), true
		}
	}
}
