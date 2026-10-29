//go:build !android

package mobile

import (
	"errors"

	"golang.zx2c4.com/wireguard/tun"
)

func deviceFromFD(int) (tun.Device, error) {
	return nil, errors.New("mobile: only available on Android")
}
