package discovery

import (
	"net"
	"testing"
)

func TestInterfaceWithFindsTheCard(t *testing.T) {
	iface := interfaceWith(net.IPv4(127, 0, 0, 1))
	if iface == nil || iface.Flags&net.FlagLoopback == 0 {
		t.Errorf("loopback: %+v", iface)
	}
	if iface := interfaceWith(net.IPv4(203, 0, 113, 7)); iface != nil {
		t.Errorf("an address nobody has: %+v", iface)
	}
}

func TestPrimaryIsARealCard(t *testing.T) {
	iface, ip := primary()
	if iface == nil {
		t.Skip("no default route here")
	}
	if iface.Flags&net.FlagLoopback != 0 || ip.IsLoopback() {
		t.Errorf("%s %s", iface.Name, ip)
	}
}
