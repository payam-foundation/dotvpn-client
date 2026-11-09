//go:build !darwin

package tun

import "net"

func defaultGateway4() net.IP { return nil }
