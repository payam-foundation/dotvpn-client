// Package tun is the full-tunnel client front. It reads IP packets from a TUN
// device, terminates every TCP flow in a user-space gVisor stack, and sends the
// payload through the transport (or straight out of the physical interface for
// domestic destinations). DNS queries are answered by fakedns, so tunnelled
// names are resolved by the node, never by a local (poisonable) resolver.
//
// UDP other than DNS (QUIC, STUN, games, VoIP) goes through Config.Packet, one
// framed tunnel stream per flow (ADR-018), or straight out for direct
// destinations. Without Config.Packet it is dropped, which makes browsers fall
// back to TCP; nothing UDP ever leaves the physical interface unrouted.
//
// ponytail: UDP rides an ordered stream, so a lost carrier packet stalls the
// flow behind it (head-of-line blocking). Add QUIC datagrams on h3 if real-time
// media needs it. IPv6 is blackholed, not tunnelled.
package tun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	"github.com/payam-foundation/dotvpn-client/internal/fakedns"
	"github.com/payam-foundation/dotvpn-client/internal/router"
	"github.com/payam-foundation/dotvpn-wire/transport"
	"github.com/payam-foundation/dotvpn-wire/transport/udpframe"
)

// Dial opens an outbound connection; network is always "tcp".
type Dial func(ctx context.Context, network, addr string) (net.Conn, error)

// DialPacket opens a tunnelled UDP flow to addr; each Write and Read is one
// datagram (client.Engine.DialPacket).
type DialPacket func(ctx context.Context, addr string) (net.Conn, error)

// Config configures Run.
type Config struct {
	Name string
	MTU  int
	// Tunnel carries flows through the node (client.Engine.DialContext).
	Tunnel Dial
	// Packet carries non-DNS UDP flows through the node. Nil drops them.
	Packet DialPacket
	// Bypass is the physical-interface path (see NewBypass). It is used for
	// direct flows and to resolve direct names.
	Bypass *Bypass
	Router *router.Router
	// KillSwitch blocks all traffic that is neither tunnelled nor ours.
	KillSwitch bool
	// Device replaces the OS TUN device and skips OS configuration (tests).
	Device tun.Device
	Log    *slog.Logger
}

const (
	nicID  = 1
	offset = 16 // headroom wireguard-go's devices expect in packet buffers
)

type engine struct {
	cfg   Config
	ctx   context.Context
	log   *slog.Logger
	dev   tun.Device
	ep    *channel.Endpoint
	stack *stack.Stack
	dns   *fakedns.Server
}

// Run brings the tunnel up and blocks until ctx ends or the device fails.
// OS changes (addresses, routes, DNS, firewall) are undone before it returns.
func Run(ctx context.Context, cfg Config) (err error) {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1380
	}
	if cfg.MTU < 576 || cfg.MTU > 9000 {
		return fmt.Errorf("tun: mtu %d out of range 576-9000", cfg.MTU)
	}
	if cfg.Tunnel == nil || cfg.Bypass == nil {
		return errors.New("tun: Tunnel and Bypass are required")
	}
	dev := cfg.Device
	if dev == nil {
		if dev, err = createDevice(cfg.Name, cfg.MTU); err != nil {
			return fmt.Errorf("tun: create device: %w", err)
		}
		name, err := dev.Name()
		if err != nil {
			_ = dev.Close()
			return err
		}
		restore, err := configure(sysConfig{Name: name, MTU: cfg.MTU, Phys: cfg.Bypass.Iface, KillSwitch: cfg.KillSwitch, Log: cfg.Log})
		defer restore()
		if err != nil {
			_ = dev.Close()
			return fmt.Errorf("tun: configure %s: %w", name, err)
		}
		cfg.Log.Info("full tunnel up", "device", name, "kill_switch", cfg.KillSwitch)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e, err := newEngine(ctx, cfg, dev)
	if err != nil {
		_ = dev.Close()
		return err
	}
	return e.run(cancel)
}

func newEngine(ctx context.Context, cfg Config, dev tun.Device) (*engine, error) {
	e := &engine{cfg: cfg, ctx: ctx, log: cfg.Log, dev: dev}
	e.dns = fakedns.New(fakedns.Pool, cfg.Router.DirectName, func(ctx context.Context, name string) ([]netip.Addr, error) {
		ips, err := cfg.Bypass.Resolver().LookupNetIP(ctx, "ip4", name)
		if err == nil && len(ips) == 0 {
			err = fmt.Errorf("tun: no address for %s", name)
		}
		return ips, err
	})
	e.ep = channel.New(1024, uint32(cfg.MTU), "")
	e.stack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	sack := tcpip.TCPSACKEnabled(true)
	if err := e.stack.SetTransportProtocolOption(tcp.ProtocolNumber, &sack); err != nil {
		return nil, fmt.Errorf("tun: sack: %v", err)
	}
	if err := e.stack.CreateNIC(nicID, e.ep); err != nil {
		return nil, fmt.Errorf("tun: nic: %v", err)
	}
	// Accept packets for any destination and answer from that destination's
	// address, so one stack terminates flows to the whole internet.
	if err := e.stack.SetPromiscuousMode(nicID, true); err != nil {
		return nil, fmt.Errorf("tun: promiscuous: %v", err)
	}
	if err := e.stack.SetSpoofing(nicID, true); err != nil {
		return nil, fmt.Errorf("tun: spoofing: %v", err)
	}
	e.stack.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: nicID}})
	e.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcp.NewForwarder(e.stack, 0, 1024, e.onTCP).HandlePacket)
	e.stack.SetTransportProtocolHandler(udp.ProtocolNumber, udp.NewForwarder(e.stack, e.onUDP).HandlePacket)
	return e, nil
}

func (e *engine) run(cancel context.CancelFunc) error {
	errc := make(chan error, 2)
	go func() { errc <- e.readLoop() }()
	go func() { errc <- e.writeLoop() }()
	var err error
	select {
	case <-e.ctx.Done():
	case err = <-errc:
	}
	cancel()
	e.ep.Close()
	e.stack.Close()
	_ = e.dev.Close()
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	return err
}

// readLoop moves packets from the device into the stack.
func (e *engine) readLoop() error {
	n := e.dev.BatchSize()
	bufs := make([][]byte, n)
	sizes := make([]int, n)
	for i := range bufs {
		bufs[i] = make([]byte, offset+65535)
	}
	for {
		got, err := e.dev.Read(bufs, sizes, offset)
		if err != nil {
			if e.ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("tun: read: %w", err)
		}
		for i := range got {
			p := slices.Clone(bufs[i][offset : offset+sizes[i]])
			if len(p) == 0 || p[0]>>4 != 4 {
				continue // IPv6 and junk are dropped
			}
			pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(p)})
			e.ep.InjectInbound(ipv4.ProtocolNumber, pkt)
			pkt.DecRef()
		}
	}
}

// writeLoop moves packets from the stack to the device.
func (e *engine) writeLoop() error {
	for {
		pkt := e.ep.ReadContext(e.ctx)
		if pkt == nil {
			return e.ctx.Err()
		}
		v := pkt.ToView()
		pkt.DecRef()
		buf := make([]byte, offset+v.Size())
		_, _ = v.Read(buf[offset:])
		v.Release()
		if _, err := e.dev.Write([][]byte{buf}, offset); err != nil && e.ctx.Err() == nil {
			e.log.Debug("tun write", "err", err)
		}
	}
}

func addrPort(id stack.TransportEndpointID) netip.AddrPort {
	a, _ := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	return netip.AddrPortFrom(a, id.LocalPort)
}

func (e *engine) onTCP(r *tcp.ForwarderRequest) {
	dst := addrPort(r.ID())
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		r.Complete(true)
		return
	}
	r.Complete(false)
	go e.serveTCP(gonet.NewTCPConn(&wq, ep), dst)
}

func (e *engine) serveTCP(c *gonet.TCPConn, dst netip.AddrPort) {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	up, err := e.dialFlow(ctx, dst)
	cancel()
	if err != nil {
		e.log.Debug("flow failed", "err", err)
		_ = c.Close()
		return
	}
	transport.Splice(c, up)
}

// dialFlow picks the path of one flow: tunnel by name for fake addresses,
// direct for domestic addresses, tunnel by address for everything else.
func (e *engine) dialFlow(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	ip, port := dst.Addr(), strconv.Itoa(int(dst.Port()))
	if e.dns.InPool(ip) {
		name, ok := e.dns.Name(ip)
		if !ok {
			return nil, fmt.Errorf("tun: stale fake address %s", ip)
		}
		return e.cfg.Tunnel(ctx, "tcp", net.JoinHostPort(name, port))
	}
	if e.cfg.Router.DirectIP(ip) || e.dns.IsDirectIP(ip) {
		return e.cfg.Bypass.DialContext(ctx, "tcp", dst.String())
	}
	return e.cfg.Tunnel(ctx, "tcp", dst.String())
}

func (e *engine) onUDP(r *udp.ForwarderRequest) {
	dst := addrPort(r.ID())
	if dst.Port() != 53 && e.cfg.Packet == nil {
		return // dropped, see the package comment
	}
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		return
	}
	c := gonet.NewUDPConn(&wq, ep)
	if dst.Port() == 53 {
		go e.serveDNS(c)
		return
	}
	go e.serveUDP(c, dst)
}

// udpIdle ends a flow with no datagram in either direction.
const udpIdle = 30 * time.Second

// serveUDP relays one client UDP flow to the path dialFlowUDP picks.
func (e *engine) serveUDP(c *gonet.UDPConn, dst netip.AddrPort) {
	defer c.Close()
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	up, err := e.dialFlowUDP(ctx, dst)
	cancel()
	if err != nil {
		e.log.Debug("udp flow failed", "dst", dst, "err", err)
		return
	}
	defer up.Close()
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	go func() { // node -> client
		buf := make([]byte, 65535)
		for {
			n, err := up.Read(buf)
			if err != nil {
				_ = c.Close() // unblocks the other direction
				return
			}
			last.Store(time.Now().UnixNano())
			if _, err := c.Write(buf[:n]); err != nil {
				return
			}
		}
	}()
	buf := make([]byte, 65535) // client -> node
	for {
		_ = c.SetReadDeadline(time.Now().Add(udpIdle))
		n, err := c.Read(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && time.Since(time.Unix(0, last.Load())) < udpIdle {
				continue // the other direction is still active
			}
			return
		}
		last.Store(time.Now().UnixNano())
		if _, err := up.Write(buf[:n]); err != nil && !errors.Is(err, udpframe.ErrTooLarge) {
			return // an oversize datagram is dropped, like an MTU black hole
		}
	}
}

// dialFlowUDP mirrors dialFlow for datagrams.
func (e *engine) dialFlowUDP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	ip, port := dst.Addr(), strconv.Itoa(int(dst.Port()))
	if e.dns.InPool(ip) {
		name, ok := e.dns.Name(ip)
		if !ok {
			return nil, fmt.Errorf("tun: stale fake address %s", ip)
		}
		return e.cfg.Packet(ctx, net.JoinHostPort(name, port))
	}
	if e.cfg.Router.DirectIP(ip) || e.dns.IsDirectIP(ip) {
		return e.cfg.Bypass.DialContext(ctx, "udp", dst.String())
	}
	return e.cfg.Packet(ctx, dst.String())
}

func (e *engine) serveDNS(c *gonet.UDPConn) {
	defer c.Close()
	var wg sync.WaitGroup
	defer wg.Wait()
	buf := make([]byte, 4096)
	for {
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		q := slices.Clone(buf[:n])
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
			defer cancel()
			if a := e.dns.Answer(ctx, q); a != nil {
				_, _ = c.Write(a)
			}
		})
	}
}
