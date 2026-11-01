// Package discovery advertises the server on the LAN over mDNS, so a button can
// find it without knowing its address.
package discovery

import (
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	service = "_voistic._tcp"
	// How often to notice a changed address, such as a new DHCP lease.
	refreshInterval = time.Minute
)

// Advertiser publishes the service until Stop is called.
type Advertiser struct {
	port int
	log  *slog.Logger
	stop chan struct{}

	mu     sync.Mutex
	server *mdns.Server
	ips    []net.IP
}

// Start begins advertising port.
func Start(port int, log *slog.Logger) (*Advertiser, error) {
	a := &Advertiser{port: port, log: log, stop: make(chan struct{})}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.restart(localIPs()); err != nil {
		return nil, err
	}
	go a.watchAddresses()
	return a, nil
}

// Stop ends advertising.
func (a *Advertiser) Stop() {
	close(a.stop)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		a.server.Shutdown()
		a.server = nil
	}
}

func (a *Advertiser) watchAddresses() {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
			a.mu.Lock()
			if ips := localIPs(); !sameIPs(ips, a.ips) {
				a.log.Info("addresses changed, updating mDNS", "ips", ips)
				if err := a.restart(ips); err != nil {
					a.log.Error("can't update mDNS", "err", err)
				}
			}
			a.mu.Unlock()
		}
	}
}

// restart replaces the server, since the library can't change a running one.
// Callers hold a.mu.
func (a *Advertiser) restart(ips []net.IP) error {
	if a.server != nil {
		a.server.Shutdown()
		a.server = nil
	}
	a.ips = ips
	if len(ips) == 0 {
		return nil // offline, watchAddresses retries once an address appears
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	hostname, _, _ = strings.Cut(hostname, ".") // drop .local, .home and the like

	zone, err := mdns.NewMDNSService(hostname, service, "", hostname+".local.", a.port, ips, []string{"v=1"})
	if err != nil {
		return err
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: zone})
	if err != nil {
		return err
	}
	a.server = server
	return nil
}

func localIPs() []net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if ip := ipNet.IP.To4(); ip != nil && !ip.IsLinkLocalUnicast() {
					ips = append(ips, ip)
				}
			}
		}
	}
	return ips
}

func sameIPs(a, b []net.IP) bool {
	return slices.EqualFunc(a, b, net.IP.Equal)
}
