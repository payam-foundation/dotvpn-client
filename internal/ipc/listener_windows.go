// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
)

// Listen creates the helper's named pipe. The ACL grants SYSTEM, Administrators
// and the one user whose SID is owner; everyone else, including other
// interactive users and network logons, is denied by omission.
func Listen(addr, owner string) (net.Listener, error) {
	if strings.ContainsAny(owner, "();") {
		return nil, errors.New("ipc: bad owner SID")
	}
	sddl := "D:P(A;;GA;;;SY)(A;;GA;;;BA)"
	if owner != "" {
		sddl += "(A;;GA;;;" + owner + ")"
	}
	return winio.ListenPipe(addr, &winio.PipeConfig{SecurityDescriptor: sddl, MessageMode: false})
}

func dial(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, addr)
}
