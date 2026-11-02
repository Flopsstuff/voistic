// Package discovery advertises the server on the LAN over mDNS, so a button can
// find it without knowing its address.
package discovery

import (
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	service = "_voistic._tcp"
	// How often to notice a changed address, such as a new DHCP lease.
	refreshInterval = time.Minute
	// A public address, only to ask which way the default route goes. A UDP dial
	// sends nothing.
	routeProbe = "1.1.1.1:53"
)

// Advertiser publishes the service until Stop is called.
type Advertiser struct {
	port int
	log  *slog.Logger
	stop chan struct{}

	mu     sync.Mutex
	server *mdns.Server
	ip     net.IP
}

// Start begins advertising port.
func Start(port int, log *slog.Logger) (*Advertiser, error) {
	a := &Advertiser{port: port, log: log, stop: make(chan struct{})}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.restart(primary()); err != nil {
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

// Refresh starts advertising anew on a new socket and returns where buttons find
// this server, or "offline". A socket can go deaf with nothing to show for it, so
// pairing doesn't rely on the one that's there.
func (a *Advertiser) Refresh() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.restart(primary()); err != nil {
		a.log.Error("can't update mDNS", "err", err)
	}
	if a.ip == nil {
		return "offline"
	}
	return net.JoinHostPort(a.ip.String(), strconv.Itoa(a.port))
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
			if iface, ip := primary(); !ip.Equal(a.ip) {
				a.log.Info("address changed, updating mDNS", "ip", ip)
				if err := a.restart(iface, ip); err != nil {
					a.log.Error("can't update mDNS", "err", err)
				}
			}
			a.mu.Unlock()
		}
	}
}

// restart replaces the server, since the library can't change a running one.
// Callers hold a.mu.
func (a *Advertiser) restart(iface *net.Interface, ip net.IP) error {
	if a.server != nil {
		a.server.Shutdown()
		a.server = nil
	}
	a.ip = ip
	if iface == nil {
		return nil // offline, watchAddresses retries once an address appears
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	hostname, _, _ = strings.Cut(hostname, ".") // drop .local, .home and the like

	zone, err := mdns.NewMDNSService(hostname, service, "", hostname+".local.", a.port, []net.IP{ip}, []string{"v=1"})
	if err != nil {
		return err
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: zone, Iface: iface})
	if err != nil {
		return err
	}
	a.server = server
	a.log.Info("advertising over mDNS", "interface", iface.Name, "ip", ip)
	return nil
}

// primary finds the network card the default route leaves through, the one the
// buttons are on, with its address. Only that card counts. The virtual ones of
// WSL or a VM can't be reached from the LAN, and left to itself the system may
// join the mDNS group on one of them, so the server would hear nothing.
func primary() (*net.Interface, net.IP) {
	conn, err := net.Dial("udp4", routeProbe)
	if err != nil {
		return nil, nil
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP.To4()
	iface := interfaceWith(ip)
	if iface == nil {
		return nil, nil
	}
	return iface, ip
}

func interfaceWith(ip net.IP) *net.Interface {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range interfaces {
		addrs, err := interfaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.Equal(ip) {
				return &interfaces[i]
			}
		}
	}
	return nil
}
