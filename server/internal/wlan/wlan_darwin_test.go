package wlan

import "testing"

func TestWifiPort(t *testing.T) {
	listing := "\nHardware Port: Ethernet\nDevice: en1\nEthernet Address: x\n\nHardware Port: Wi-Fi\nDevice: en0\nEthernet Address: y\n"
	if got := wifiPort(listing); got != "en0" {
		t.Errorf("wifiPort = %q", got)
	}
}

func TestFirstPreferred(t *testing.T) {
	if got := firstPreferred("Preferred networks on en0:\n\tHome Net\n\tCafe\n"); got != "Home Net" {
		t.Errorf("firstPreferred = %q", got)
	}
	if got := firstPreferred("Preferred networks on en0:\n"); got != "" {
		t.Errorf("firstPreferred = %q", got)
	}
}
