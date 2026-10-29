package client

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// echoDial answers every tunnelled connection with an in-memory echo peer.
func echoDial(_ context.Context, _, _ string) (net.Conn, error) {
	a, b := net.Pipe()
	go func() { _, _ = io.Copy(b, b) }()
	return a, nil
}

func startProxy(t *testing.T, auth func(string, string) bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &LocalProxy{Dial: echoDial, Auth: auth}
	go func() { _ = p.Serve(ctx, ln) }()
	return ln.Addr().String()
}

func socksHandshake(t *testing.T, addr string, methods []byte, creds [2]string) (net.Conn, byte) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write(append([]byte{5, byte(len(methods))}, methods...))
	rep := make([]byte, 2)
	if _, err := io.ReadFull(c, rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] == 2 {
		msg := append([]byte{1, byte(len(creds[0]))}, creds[0]...)
		msg = append(msg, byte(len(creds[1])))
		msg = append(msg, creds[1]...)
		_, _ = c.Write(msg)
		st := make([]byte, 2)
		if _, err := io.ReadFull(c, st); err != nil {
			t.Fatal(err)
		}
		return c, st[1]
	}
	return c, rep[1]
}

func socksConnect(t *testing.T, c net.Conn) {
	t.Helper()
	_, _ = c.Write([]byte{5, 1, 0, 3, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0, 80})
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0 {
		t.Fatalf("connect reply %v err %v", rep, err)
	}
	_, _ = c.Write([]byte("ping"))
	got := make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "ping" {
		t.Fatalf("echo %q err %v", got, err)
	}
}

func TestSocks5Auth(t *testing.T) {
	open := startProxy(t, nil)
	c, m := socksHandshake(t, open, []byte{0}, [2]string{})
	if m != 0 {
		t.Fatalf("no-auth method = %d", m)
	}
	socksConnect(t, c)

	secured := startProxy(t, StaticAuth("alice", "s3cret"))
	if _, m := socksHandshake(t, secured, []byte{0}, [2]string{}); m != 0xff {
		t.Errorf("no-auth client accepted by authed proxy: method %#x", m)
	}
	if _, st := socksHandshake(t, secured, []byte{0, 2}, [2]string{"alice", "wrong"}); st == 0 {
		t.Error("wrong password accepted")
	}
	if _, st := socksHandshake(t, secured, []byte{0, 2}, [2]string{"bob", "s3cret"}); st == 0 {
		t.Error("wrong user accepted")
	}
	c, st := socksHandshake(t, secured, []byte{0, 2}, [2]string{"alice", "s3cret"})
	if st != 0 {
		t.Fatalf("valid credentials rejected: status %d", st)
	}
	socksConnect(t, c)
}

func httpConnectStatus(t *testing.T, addr, authz string) int {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	req := "CONNECT example:443 HTTP/1.1\r\nHost: example:443\r\n"
	if authz != "" {
		req += "Proxy-Authorization: " + authz + "\r\n"
	}
	_, _ = c.Write([]byte(req + "\r\n"))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestHTTPProxyAuth(t *testing.T) {
	basic := func(u, p string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p))
	}
	open := startProxy(t, nil)
	if got := httpConnectStatus(t, open, ""); got != 200 {
		t.Errorf("open proxy status %d", got)
	}
	secured := startProxy(t, StaticAuth("alice", "s3cret"))
	for _, tc := range []struct {
		name, authz string
		want        int
	}{
		{"missing", "", 407},
		{"wrong password", basic("alice", "nope"), 407},
		{"bearer scheme", "Bearer abc", 407},
		{"garbage base64", "Basic !!!", 407},
		{"valid", basic("alice", "s3cret"), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpConnectStatus(t, secured, tc.authz); got != tc.want {
				t.Errorf("status %d, want %d", got, tc.want)
			}
		})
	}
}

func TestStaticAuth(t *testing.T) {
	a := StaticAuth("u", "p")
	for _, tc := range []struct {
		u, p string
		ok   bool
	}{{"u", "p", true}, {"u", "", false}, {"", "p", false}, {"u", "pp", false}, {strings.Repeat("u", 300), "p", false}} {
		if a(tc.u, tc.p) != tc.ok {
			t.Errorf("(%q,%q) = %v", tc.u, tc.p, !tc.ok)
		}
	}
}
