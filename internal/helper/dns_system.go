// SPDX-License-Identifier: AGPL-3.0-only

package helper

import (
	"bufio"
	"net"
	"os"
	"strings"
)

// readSystemDNS returns the first real nameserver from /etc/resolv.conf, if
// any. Windows/macOS often still populate this file for compatibility. The
// value is host-only (no port); use bypassDNS for a dialable address. The
// tunnel's fake resolver (198.18.0.0/15) is skipped so Settings never shows
// it as "system DNS" while connected.
func readSystemDNS() string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "nameserver") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ns := fields[1]
		if ns == "" || ns == "system" || isFakeTunHost(ns) {
			continue
		}
		return ns
	}
	return ""
}

const bypassDNSFallback = "1.1.1.1:53"

// bypassDNS returns a dialable "ip:port" for the physical-path resolver used
// by the tunnel bypass (control plane + node names). Never returns the
// preset keyword "system" or the tunnel's fake DNS (198.18.0.0/15).
func bypassDNS(preset, dns string) string {
	switch preset {
	case "cloudflare":
		return "1.1.1.1:53"
	case "google":
		return "8.8.8.8:53"
	case "quad9":
		return "9.9.9.9:53"
	case "custom":
		if dns != "" && dns != "system" && !isFakeTunHost(dns) {
			return ensureDNSPort(dns)
		}
		return bypassDNSFallback
	}
	// system / empty / unknown: prefer a real host resolver, else Cloudflare.
	if dns != "" && dns != "system" && !isFakeTunHost(dns) {
		return ensureDNSPort(dns)
	}
	if ns := readSystemDNS(); ns != "" && ns != "system" && !isFakeTunHost(ns) {
		return ensureDNSPort(ns)
	}
	return bypassDNSFallback
}

func isFakeTunHost(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	_, cidr, err := net.ParseCIDR("198.18.0.0/15")
	return err == nil && cidr.Contains(ip)
}

func ensureDNSPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "53")
}
