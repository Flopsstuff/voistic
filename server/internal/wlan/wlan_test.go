package wlan

import "testing"

func TestProfileKeyDecodesXML(t *testing.T) {
	profile := []byte(`<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
	<name>Home &amp; Garden</name>
	<MSM><security><sharedKey>
		<keyType>passPhrase</keyType>
		<protected>false</protected>
		<keyMaterial>a&amp;b&lt;c&gt;d&quot;e</keyMaterial>
	</sharedKey></security></MSM>
</WLANProfile>`)
	if got, ok := profileKey(profile); !ok || got != `a&b<c>d"e` {
		t.Errorf("profileKey = %q, %v", got, ok)
	}
	if _, ok := profileKey([]byte(`<WLANProfile><name>Open</name></WLANProfile>`)); ok {
		t.Error("an open network has no key")
	}
}
