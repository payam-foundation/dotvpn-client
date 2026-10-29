package client

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

// DialFunc opens an outbound connection. Engine.DialContext fits, and so does
// a Router that sends some destinations direct.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// LocalProxy is a mixed SOCKS5 + HTTP proxy listener: the first byte of each
// connection decides the protocol. It forwards everything through Dial.
//
// With Auth set every client must authenticate: SOCKS5 username/password
// (RFC 1929) or HTTP "Proxy-Authorization: Basic". Auth is required when the
// listener is reachable off-host; on loopback it is optional.
type LocalProxy struct {
	Dial DialFunc
	Log  *slog.Logger
	// Auth reports whether the credentials are valid; nil disables auth.
	Auth func(user, pass string) bool
}

// StaticAuth returns an Auth func that accepts exactly one user and password,
// compared in constant time.
func StaticAuth(user, pass string) func(string, string) bool {
	return func(u, p string) bool {
		// Both comparisons always run so timing does not reveal which field matched.
		ok := subtle.ConstantTimeCompare([]byte(u), []byte(user))
		ok &= subtle.ConstantTimeCompare([]byte(p), []byte(pass))
		return ok == 1
	}
}

// Serve accepts on ln until it fails or ctx ends.
func (p *LocalProxy) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go p.handle(ctx, c)
	}
}

func (p *LocalProxy) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

func (p *LocalProxy) handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	if first[0] == 5 {
		p.socks5(ctx, c, br)
		return
	}
	p.httpProxy(ctx, c, br)
}

// --- SOCKS5 (CONNECT only; username/password auth when Auth is set) ---

func (p *LocalProxy) socks5(ctx context.Context, c net.Conn, br *bufio.Reader) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}
	want := byte(0) // no authentication
	if p.Auth != nil {
		want = 2 // username/password
	}
	if !contains(methods, want) {
		_, _ = c.Write([]byte{5, 0xff})
		return
	}
	if _, err := c.Write([]byte{5, want}); err != nil {
		return
	}
	if want == 2 && !p.socksAuth(c, br) {
		return
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return
	}
	if req[0] != 5 || req[1] != 1 {
		socksReply(c, 7) // command not supported
		return
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		n, err := br.ReadByte()
		if err != nil {
			return
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = string(b)
	default:
		socksReply(c, 8)
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(br, pb); err != nil {
		return
	}
	addr := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))
	up, err := p.dial(ctx, addr)
	if err != nil {
		socksReply(c, socksCode(err))
		return
	}
	socksReply(c, 0)
	pipe(c, br, up)
}

// socksAuth runs the RFC 1929 sub-negotiation and reports whether the client
// authenticated. A failure is answered with status 1 and the caller closes.
func (p *LocalProxy) socksAuth(c net.Conn, br *bufio.Reader) bool {
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer c.SetReadDeadline(time.Time{}) //nolint:errcheck
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(br, hdr); err != nil || hdr[0] != 1 {
		return false
	}
	user := make([]byte, hdr[1])
	if _, err := io.ReadFull(br, user); err != nil {
		return false
	}
	n, err := br.ReadByte()
	if err != nil {
		return false
	}
	pass := make([]byte, n)
	if _, err := io.ReadFull(br, pass); err != nil {
		return false
	}
	if !p.Auth(string(user), string(pass)) {
		_, _ = c.Write([]byte{1, 1})
		return false
	}
	_, err = c.Write([]byte{1, 0})
	return err == nil
}

func socksReply(c net.Conn, code byte) {
	_, _ = c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
}

func socksCode(err error) byte {
	switch {
	case errors.Is(err, transport.ErrForbidden):
		return 2
	case errors.Is(err, transport.ErrBusy):
		return 1
	case errors.Is(err, context.DeadlineExceeded):
		return 6
	}
	return 4
}

func contains(b []byte, v byte) bool {
	for _, x := range b {
		if x == v {
			return true
		}
	}
	return false
}

// --- HTTP proxy: CONNECT and absolute-URI forwarding ---

func (p *LocalProxy) httpProxy(ctx context.Context, c net.Conn, br *bufio.Reader) {
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		if p.Auth != nil && !p.httpAuthorized(req) {
			_, _ = c.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"dotvpn\"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return
		}
		if req.Method == http.MethodConnect {
			addr := withPort(req.Host, "443")
			up, err := p.dial(ctx, addr)
			if err != nil {
				_, _ = fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", httpCode(err), http.StatusText(httpCode(err)))
				return
			}
			_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			pipe(c, br, up)
			return
		}
		if !p.forwardHTTP(ctx, c, req) {
			return
		}
	}
}

// httpAuthorized checks "Proxy-Authorization: Basic" against Auth.
func (p *LocalProxy) httpAuthorized(req *http.Request) bool {
	raw, ok := strings.CutPrefix(req.Header.Get("Proxy-Authorization"), "Basic ")
	if !ok {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	user, pass, _ := strings.Cut(string(b), ":")
	return p.Auth(user, pass)
}

func httpCode(err error) int {
	if errors.Is(err, transport.ErrForbidden) {
		return http.StatusForbidden
	}
	return http.StatusBadGateway
}

// forwardHTTP relays one plain-HTTP request over a fresh tunnel (Connection:
// close semantics) and always reports the client connection as not reusable.
func (p *LocalProxy) forwardHTTP(ctx context.Context, c net.Conn, req *http.Request) bool {
	if req.URL.Host == "" {
		_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n"))
		return false
	}
	up, err := p.dial(ctx, withPort(req.URL.Host, "80"))
	if err != nil {
		code := httpCode(err)
		_, _ = fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", code, http.StatusText(code))
		return false
	}
	defer up.Close()
	req.RequestURI = ""
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	req.Close = true
	out := req.Clone(ctx)
	out.URL.Scheme, out.URL.Host = "", ""
	out.RequestURI = out.URL.RequestURI()
	if err := out.Write(up); err != nil {
		return false
	}
	resp, err := http.ReadResponse(bufio.NewReader(up), req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	resp.Close = true
	_ = resp.Write(c)
	return false
}

func withPort(hostport, def string) string {
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	return net.JoinHostPort(strings.Trim(hostport, "[]"), def)
}

func (p *LocalProxy) dial(ctx context.Context, addr string) (net.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := p.Dial(dctx, "tcp", addr)
	if err != nil {
		p.log().Debug("dial failed", "dest", addr, "err", err)
	}
	return c, err
}

// pipe relays between the client connection (with its buffered reader) and up.
func pipe(c net.Conn, br *bufio.Reader, up net.Conn) {
	transport.Splice(&bufferedConn{Conn: c, r: br}, up)
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func (b *bufferedConn) CloseWrite() error {
	if cw, ok := b.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
