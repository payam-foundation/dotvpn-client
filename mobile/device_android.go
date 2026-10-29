//go:build android

package mobile

import (
	"golang.zx2c4.com/wireguard/tun"

	inttun "github.com/payam-foundation/dotvpn-client/internal/tun"
)

func deviceFromFD(fd int) (tun.Device, error) { return inttun.DeviceFromFD(fd) }
