package tun

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

// Bypass is the way out of the physical interface. Everything the client
// sends itself (transport sockets, control-plane HTTP, DNS for node names,
// direct flows) must use it, or it would be captured by its own tunnel.
//
// Create it before Run, while the default route is still the physical one.
type Bypass struct {
	// Iface is the interface that held the default route.
	Iface    *net.Interface
	upstream string
	resolver *net.Resolver
	// protect, when set, replaces interface binding: it exempts each socket
	// from the VPN (Android VpnService.protect).
	protect func(fd int) bool
	// dial replaces DialContext (tests).
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewBypass finds the physical interface. dns ("ip:port") is the resolver used
// outside the tunnel; empty means 1.1.1.1:53.
func NewBypass(dns string) (*Bypass, error) {
	if dns == "" {
		dns = "1.1.1.1:53"
	}
	// Connecting a UDP socket sends nothing; it only asks the kernel which
	// local address (hence interface) the default route uses.
	c, err := net.Dial("udp4", "9.9.9.9:53")
	if err != nil {
		return nil, errors.New("tun: no default route: " + err.Error())
	}
	local := c.LocalAddr().(*net.UDPAddr).IP
	_ = c.Close()
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for i := range ifs {
		addrs, _ := ifs[i].Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(local) {
				b := &Bypass{Iface: &ifs[i], upstream: dns}
				b.resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return b.dialer().DialContext(ctx, network, b.upstream)
				}}
				return b, nil
			}
		}
	}
	return nil, errors.New("tun: cannot find the interface of " + local.String())
}

// NewProtectedBypass is the Bypass for platforms where the OS, not the client,
// keeps sockets out of the tunnel: protect is called on every socket before it
// connects. dns is as for NewBypass.
func NewProtectedBypass(dns string, protect func(fd int) bool) *Bypass {
	if dns == "" {
		dns = "1.1.1.1:53"
	}
	b := &Bypass{upstream: dns, protect: protect}
	b.resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return b.dialer().DialContext(ctx, network, b.upstream)
	}}
	return b
}

// Control binds a socket to the physical interface. It fits
// transport.Options.Control and net.Dialer.Control.
func (b *Bypass) Control(network, address string, c syscall.RawConn) error {
	if b.protect != nil {
		ok := false
		if err := c.Control(func(fd uintptr) { ok = b.protect(int(fd)) }); err != nil {
			return err
		}
		if !ok {
			return errors.New("tun: socket protect refused")
		}
		return nil
	}
	if b.Iface == nil {
		return nil
	}
	return bindControl(b.Iface)(network, address, c)
}

// Resolver resolves names outside the tunnel.
func (b *Bypass) Resolver() *net.Resolver { return b.resolver }

func (b *Bypass) dialer() *net.Dialer {
	return &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second, Control: b.Control}
}

// DialContext dials outside the tunnel, resolving names outside it too.
func (b *Bypass) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if b.dial != nil {
		return b.dial(ctx, network, addr)
	}
	d := b.dialer()
	d.Resolver = b.resolver
	return d.DialContext(ctx, network, addr)
}
