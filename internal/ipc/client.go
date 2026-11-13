package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client talks to the helper. Each call dials, so a helper restart never
// leaves a stale connection behind.
type Client struct {
	// Addr is the socket path or pipe name; empty means DefaultAddr.
	Addr string
	// Timeout bounds one call; default 30s. Connect and Disconnect can take a
	// few seconds while routes change.
	Timeout time.Duration
}

func (c *Client) addr() string {
	if c.Addr != "" {
		return c.Addr
	}
	return DefaultAddr()
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	return dial(ctx, c.addr())
}

func (c *Client) call(ctx context.Context, method string, params, out any) error {
	d := c.Timeout
	if d == 0 {
		d = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	resp, err := roundTrip(conn, bufio.NewReader(conn), method, params)
	if err != nil {
		return err
	}
	if out != nil && resp.Result != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

func roundTrip(conn net.Conn, br *bufio.Reader, method string, params any) (*response, error) {
	req := request{V: Version, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		req.Params = b
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("ipc: helper closed the connection: %w", err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return &resp, nil
}

// Status returns the current status.
func (c *Client) Status(ctx context.Context) (st Status, err error) {
	return st, c.call(ctx, MethodStatus, nil, &st)
}

// Connect starts the session with the saved settings.
func (c *Client) Connect(ctx context.Context) error { return c.call(ctx, MethodConnect, nil, nil) }

// Disconnect stops the session.
func (c *Client) Disconnect(ctx context.Context) error {
	return c.call(ctx, MethodDisconnect, nil, nil)
}

// ImportInvite saves a dv:// invite as the active account.
func (c *Client) ImportInvite(ctx context.Context, uri string) (r ImportResult, err error) {
	return r, c.call(ctx, MethodImport, uri, &r)
}

// Servers lists the ladder.
func (c *Client) Servers(ctx context.Context) (sv []Server, err error) {
	return sv, c.call(ctx, MethodServers, nil, &sv)
}

// ProbeServers dials each ladder rung once and returns servers with latency.
func (c *Client) ProbeServers(ctx context.Context) (sv []Server, err error) {
	cc := *c
	if cc.Timeout == 0 || cc.Timeout < 20*time.Second {
		cc.Timeout = 25 * time.Second
	}
	return sv, cc.call(ctx, MethodProbe, nil, &sv)
}

// Settings returns the helper settings.
func (c *Client) Settings(ctx context.Context) (st Settings, err error) {
	return st, c.call(ctx, MethodGetSettings, nil, &st)
}

// SetSettings saves settings; they apply at the next connect.
func (c *Client) SetSettings(ctx context.Context, st Settings) error {
	return c.call(ctx, MethodSetSettings, st, nil)
}

// Unblock removes leftover kill-switch rules.
func (c *Client) Unblock(ctx context.Context) error { return c.call(ctx, MethodUnblock, nil, nil) }

// Account returns the safe subscription view from the helper.
func (c *Client) Account(ctx context.Context) (a Account, err error) {
	return a, c.call(ctx, MethodAccount, nil, &a)
}

// Relays lists curated and opted-in community relays.
func (c *Client) Relays(ctx context.Context) (rs []Relay, err error) {
	return rs, c.call(ctx, MethodRelays, nil, &rs)
}

// Ads fetches active ads for a client slot (home|relay).
func (c *Client) Ads(ctx context.Context, slot, relayID string) (ads []Ad, err error) {
	return ads, c.call(ctx, MethodAds, map[string]string{"slot": slot, "relayId": relayID}, &ads)
}

// AddRelay opts into a community/private relay by id or dvr:// URI.
func (c *Client) AddRelay(ctx context.Context, idOrURI string) error {
	return c.call(ctx, MethodAddRelay, idOrURI, nil)
}

// RemoveRelay drops an opted-in relay id.
func (c *Client) RemoveRelay(ctx context.Context, id string) error {
	return c.call(ctx, MethodRemoveRelay, id, nil)
}

// ReportDiagnostics submits a scrubbed support bundle via the helper.
func (c *Client) ReportDiagnostics(ctx context.Context) (r ReportResult, err error) {
	err = c.call(ctx, MethodReport, nil, &r)
	return
}

// Logout clears the saved invite/account and stops any session.
func (c *Client) Logout(ctx context.Context) error {
	return c.call(ctx, MethodLogout, nil, nil)
}

// Subscribe streams status changes, starting with the current one, until ctx
// ends or the helper goes away; the channel is then closed.
func (c *Client) Subscribe(ctx context.Context) (<-chan Status, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 4096)
	if _, err := roundTrip(conn, br, MethodSubscribe, nil); err != nil {
		_ = conn.Close()
		return nil, err
	}
	out := make(chan Status, 1)
	var once sync.Once
	stop := func() { once.Do(func() { _ = conn.Close() }) }
	go func() {
		<-ctx.Done()
		stop()
	}()
	go func() {
		defer close(out)
		defer stop()
		for {
			line, err := br.ReadBytes('\n')
			if err != nil {
				return
			}
			var st Status
			if json.Unmarshal(line, &st) != nil {
				return
			}
			select {
			case out <- st:
			default: // slow reader: drop the stale one
				select {
				case <-out:
				default:
				}
				out <- st
			}
		}
	}()
	return out, nil
}
