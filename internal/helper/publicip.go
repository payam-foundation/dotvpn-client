package helper

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/daemon"
)

var (
	publicIPMu   sync.Mutex
	publicIPVal  string
	publicIPAt   time.Time
	publicIPOnce sync.Once
)

func (h *Helper) cachedPublicIP() string {
	publicIPOnce.Do(func() { go refreshPublicIP() })
	publicIPMu.Lock()
	defer publicIPMu.Unlock()
	if time.Since(publicIPAt) > 2*time.Minute {
		go refreshPublicIP()
	}
	return publicIPVal
}

func refreshPublicIP() {
	ip, err := daemon.LookupPublicIP(context.Background(), (&net.Dialer{Timeout: 5 * time.Second}).DialContext)
	if err != nil || ip == "" {
		return
	}
	publicIPMu.Lock()
	publicIPVal, publicIPAt = ip, time.Now()
	publicIPMu.Unlock()
}
