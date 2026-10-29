//go:build android

package tun

import (
	"errors"
	"log/slog"
	"net"
	"syscall"

	"golang.zx2c4.com/wireguard/tun"
)

// On Android the VpnService owns the device, routes, DNS and the kill switch
// (always-on VPN with "block connections without VPN"). The client only reads
// and writes packets, so every OS-configuration hook is a no-op or an error.

var errAndroid = errors.New("tun: the device comes from VpnService on Android")

func createDevice(string, int) (tun.Device, error) { return nil, errAndroid }

// bindControl is unused: Android bypass sockets go through NewProtectedBypass.
func bindControl(*net.Interface) func(network, address string, c syscall.RawConn) error {
	return func(string, string, syscall.RawConn) error { return errAndroid }
}

func configure(sysConfig) (func(), error) { return func() {}, errAndroid }

func unblock(*slog.Logger) error { return nil }

// DeviceFromFD wraps the descriptor returned by VpnService.Builder.establish
// (detached, so Close closes it). Pass it as Config.Device.
func DeviceFromFD(fd int) (tun.Device, error) {
	d, _, err := tun.CreateUnmonitoredTUNFromFD(fd)
	if err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return d, nil
}
