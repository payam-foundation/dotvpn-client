package tun

import (
	"net"
	"os/exec"
	"strings"
)

// defaultGateway4 reads the IPv4 default gateway from the routing table.
func defaultGateway4() net.IP {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "gateway:") {
			continue
		}
		return net.ParseIP(strings.TrimSpace(strings.TrimPrefix(line, "gateway:")))
	}
	return nil
}
