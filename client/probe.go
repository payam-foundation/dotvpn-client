package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-wire/transport"
)

// DefaultProbeTarget answers GET /generate_204 on almost every network.
const DefaultProbeTarget = "connectivitycheck.gstatic.com:80"

// Ranked is one probed candidate and what it cost.
type Ranked struct {
	Endpoint  transport.Endpoint
	Handshake time.Duration // dial to established session
	FirstByte time.Duration // data-plane request to response (0 without a target)
}

// Total is the ranking key.
func (r Ranked) Total() time.Duration { return r.Handshake + r.FirstByte }

// expandCandidates turns an endpoint with Params["ips"] into one endpoint per
// candidate address. The TLS name stays the original server name, so SNI and
// the certificate check are unchanged. Without "ips" it returns ep alone.
func expandCandidates(ep transport.Endpoint) []transport.Endpoint {
	addrs := transport.CandidateAddrs(ep)
	if len(addrs) == 1 && addrs[0] == ep.Addr {
		return []transport.Endpoint{ep}
	}
	out := make([]transport.Endpoint, 0, len(addrs))
	for _, a := range addrs {
		c := ep
		c.Addr, c.Host = a, ep.ServerName()
		c.Params = make(map[string]string, len(ep.Params))
		for k, v := range ep.Params {
			if k != "ips" {
				c.Params[k] = v
			}
		}
		out = append(out, c)
	}
	return out
}

// Probe dials every endpoint, at most conc at a time, verifies the data plane
// against target (skipped when empty) and returns the working ones, fastest
// first. Sessions are closed again; callers dial the winners themselves.
//
// ponytail: a probed session is thrown away and redialled by the race, one
// extra handshake per multi-IP endpoint per connect. Hand the live session
// over if that cost shows up.
func Probe(ctx context.Context, eps []transport.Endpoint, dialers map[transport.Kind]transport.Dialer, tok transport.TokenSource, target string, conc int) []Ranked {
	if conc <= 0 {
		conc = 4
	}
	var (
		mu  sync.Mutex
		out []Ranked
		wg  sync.WaitGroup
		sem = make(chan struct{}, conc)
	)
	for _, ep := range eps {
		d, ok := dialers[ep.Kind]
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			t0 := time.Now()
			s, err := d.Dial(pctx, ep, tok)
			if err != nil {
				return
			}
			defer s.Close()
			r := Ranked{Endpoint: ep, Handshake: time.Since(t0)}
			if target != "" {
				fb, err := verifyDataPlane(pctx, s, target)
				if err != nil {
					return
				}
				r.FirstByte = fb
			}
			mu.Lock()
			out = append(out, r)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Total() < out[j].Total() })
	return out
}

// verifyDataPlane proves the tunnel carries traffic end to end: one stream to
// target, GET /generate_204, expect 204. A TLS handshake alone does not (a
// censor can pass the handshake and drop everything after it).
//
// A reply from the node that refuses the destination (policy, busy, upstream
// unreachable) still proves the carrier and token work, so it is not a
// failure; only silence or a wrong answer is.
func verifyDataPlane(ctx context.Context, s transport.Session, target string) (time.Duration, error) {
	const budget = 8 * time.Second
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	t0 := time.Now()
	c, err := s.OpenStream(ctx, target)
	switch {
	case err == nil:
	case errors.Is(err, transport.ErrForbidden), errors.Is(err, transport.ErrBusy), errors.Is(err, transport.ErrBadGateway):
		return time.Since(t0), nil
	default:
		return 0, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(budget))
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(c, "GET /generate_204 HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0\r\nAccept: */*\r\nConnection: close\r\n\r\n", host); err != nil {
		return 0, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		return 0, fmt.Errorf("data-plane probe: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return 0, fmt.Errorf("data-plane probe: status %d, want 204", resp.StatusCode)
	}
	return time.Since(t0), nil
}
