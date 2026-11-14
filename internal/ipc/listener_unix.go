// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Listen creates the helper's socket. owner is the os/user Uid (numeric here,
// a SID on Windows) of the one user besides root who may connect: the socket is
// chowned to it with mode 0600. Empty leaves it root-only.
//
// ponytail: access control is the file mode, not SO_PEERCRED. That is enough
// while one user owns the helper; add a peer-credential check if several users
// ever share one.
func Listen(addr, owner string) (net.Listener, error) {
	dir := filepath.Dir(addr)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// A leftover socket from a crash would make Listen fail; a live helper
	// would still be answering on it.
	if c, err := net.Dial("unix", addr); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("ipc: another helper is listening on %s", addr)
	}
	_ = os.Remove(addr)
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if owner != "" && os.Geteuid() == 0 {
		uid, err := strconv.Atoi(owner)
		if err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("ipc: owner %q is not a uid", owner)
		}
		if err := os.Chown(addr, uid, -1); err != nil {
			_ = ln.Close()
			return nil, err
		}
	}
	return ln, nil
}

func dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}
