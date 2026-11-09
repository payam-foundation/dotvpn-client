package daemon

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ipEchoURLs are plain-text IP echoes that are not fronted by Cloudflare.
// api.ipify.org sits on CF anycast; when the tunnel also lands on CF the body
// can be a CF edge address instead of the real exit.
var ipEchoURLs = []string{
	"https://checkip.amazonaws.com",
	"https://ifconfig.me/ip",
}

// LookupPublicIP asks a simple plaintext IP echo over dial (tunnel or direct).
func LookupPublicIP(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tr := &http.Transport{
		DialContext:           dial,
		Proxy:                 nil,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 8 * time.Second}
	var last error
	for _, rawURL := range ipEchoURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			last = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		ip, err := readIPBody(resp)
		_ = resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if ip != "" {
			return ip, nil
		}
	}
	if last != nil {
		return "", last
	}
	return "", nil
}

func readIPBody(resp *http.Response) (string, error) {
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(raw))
	if net.ParseIP(ip) == nil {
		return "", nil
	}
	return ip, nil
}
