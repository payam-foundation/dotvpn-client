package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
)

// Serve serves b on ln until ctx ends or the listener fails, then closes it
// and every open connection.
func Serve(ctx context.Context, ln net.Listener, b Backend) error {
	s := &server{b}
	var mu sync.Mutex
	conns := map[net.Conn]struct{}{}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		mu.Lock()
		for c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		mu.Lock()
		conns[c] = struct{}{}
		mu.Unlock()
		go func() {
			s.handle(ctx, c)
			_ = c.Close()
			mu.Lock()
			delete(conns, c)
			mu.Unlock()
		}()
	}
}

type server struct{ Backend }

func (s *server) handle(ctx context.Context, c net.Conn) {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	enc := json.NewEncoder(c)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(response{Error: &Error{Code: CodeInternal, Message: "bad request: " + err.Error()}})
			return
		}
		if req.V != Version {
			_ = enc.Encode(response{Error: &Error{Code: CodeVersion, Message: "protocol version mismatch: update the app or the helper"}})
			return
		}
		if req.Method == MethodSubscribe {
			s.stream(ctx, c, enc)
			return
		}
		res, err := s.call(req)
		var resp response
		if err != nil {
			resp.Error = asError(err)
		} else if resp.Result, err = json.Marshal(res); err != nil {
			resp.Error = asError(err)
		}
		if enc.Encode(resp) != nil {
			return
		}
	}
}

// stream sends the current status, then every change, until the client closes
// the connection.
func (s *server) stream(ctx context.Context, c net.Conn, enc *json.Encoder) {
	ev, cancel := s.Backend.Subscribe()
	defer cancel()
	if enc.Encode(response{}) != nil || enc.Encode(s.Backend.Status()) != nil {
		return
	}
	gone := make(chan struct{})
	go func() { // the client sends nothing more; a read ends when it hangs up
		_, _ = c.Read(make([]byte, 1))
		close(gone)
	}()
	for {
		select {
		case st := <-ev:
			if enc.Encode(st) != nil {
				return
			}
		case <-gone:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *server) call(req request) (any, error) {
	b := s.Backend
	switch req.Method {
	case MethodStatus:
		return b.Status(), nil
	case MethodConnect:
		return nil, b.Connect()
	case MethodDisconnect:
		b.Disconnect()
		return nil, nil
	case MethodImport:
		var uri string
		if err := json.Unmarshal(req.Params, &uri); err != nil {
			return nil, Coded(CodeBadInvite, errors.New("invite must be a string"))
		}
		return b.ImportInvite(uri)
	case MethodServers:
		return b.Servers(), nil
	case MethodProbe:
		return b.ProbeServers()
	case MethodGetSettings:
		return b.Settings(), nil
	case MethodSetSettings:
		var st Settings
		if err := json.Unmarshal(req.Params, &st); err != nil {
			return nil, err
		}
		return nil, b.SetSettings(st)
	case MethodUnblock:
		return nil, b.Unblock()
	case MethodAccount:
		return b.Account()
	case MethodRelays:
		return b.Relays()
	case MethodAds:
		var p struct {
			Slot    string `json:"slot"`
			RelayID string `json:"relayId"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, err
			}
		}
		return b.Ads(p.Slot, p.RelayID)
	case MethodAddRelay:
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, err
		}
		return nil, b.AddRelay(id)
	case MethodRemoveRelay:
		var id string
		if err := json.Unmarshal(req.Params, &id); err != nil {
			return nil, err
		}
		return nil, b.RemoveRelay(id)
	case MethodLogout:
		return nil, b.Logout()
	case MethodReport:
		return b.ReportDiagnostics()
	}
	return nil, errors.New("unknown method " + req.Method)
}
