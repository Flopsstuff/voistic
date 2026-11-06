package wlan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

var ssidLine = regexp.MustCompile(`(?m)^\s*SSID\s*:\s*(.+?)\s*$`)

// Current asks netsh. Its labels are translated, but "SSID" stays.
func Current() (Network, error) {
	out, err := exec.Command("netsh", "wlan", "show", "interfaces").Output()
	if err != nil {
		return Network{}, ErrUnknown
	}
	if match := ssidLine.FindStringSubmatch(string(out)); match != nil {
		return Network{SSID: match[1]}, nil
	}
	return Network{}, ErrUnknown
}

// Password exports the profile with its key, since the XML, unlike netsh's
// listing, isn't translated.
func Password(ssid string) (string, error) {
	dir, err := os.MkdirTemp("", "voistic-wlan")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := exec.Command("netsh", "wlan", "export", "profile", "name="+ssid, "key=clear", "folder="+dir).Run(); err != nil {
		return "", fmt.Errorf("no saved profile for %s", ssid)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.xml"))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if key, ok := profileKey(data); ok {
			return key, nil
		}
	}
	return "", fmt.Errorf("no password saved for %s", ssid)
}
