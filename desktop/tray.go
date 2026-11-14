// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"embed"
	"runtime"
	"sync"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed icons/*.png
var iconFS embed.FS

var appIcon = mustIcon("icons/app.png")

func mustIcon(name string) []byte {
	b, err := iconFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}

// iconKind is which of the four Hop tray icons shows. Lime appears only in
// "on", per the guidelines.
func iconKind(up bool, st ipc.Status) string {
	if !up {
		return "off"
	}
	switch st.State {
	case ipc.StateConnecting, ipc.StateSwitching:
		return "busy"
	case ipc.StateConnected:
		return "on"
	case ipc.StateFailed:
		return "fail"
	}
	return "off"
}

// trayView owns the status item and rebuilds its menu when what it shows
// changes. A new Menu per change keeps every native call on Wails' own
// main-thread path (SetMenu) instead of mutating live items from our goroutine.
type trayView struct {
	supported bool

	mu   sync.Mutex
	item *application.SystemTray
	d    *desktop
	last viewKey
}

type viewKey struct {
	lang, icon, line, server, action string
}

func newTrayView() *trayView { return &trayView{supported: trayHostPresent()} }

func (t *trayView) install(d *desktop) {
	if !t.supported {
		return
	}
	t.d = d
	t.item = d.app.SystemTray.New()
	t.item.SetTooltip("Dot VPN")
	t.apply(viewKey{lang: d.lang(), icon: "off", line: tr(d.lang(), "helperDown"), action: "setup"})
}

// update refreshes the tray for the helper being up or down and its status.
func (t *trayView) update(lang string, up bool, st ipc.Status) {
	if !t.supported || t.item == nil {
		return
	}
	k := viewKey{lang: lang, icon: iconKind(up, st)}
	switch {
	case !up:
		k.line, k.action = tr(lang, "helperDown"), "setup"
	case !st.Configured:
		k.line, k.action = tr(lang, "noInvite"), "setup"
	default:
		k.line = stateLine(lang, st)
		k.server = st.Server
		switch {
		case st.State == ipc.StateConnected:
			k.action = "disconnect"
		case st.Running:
			k.action = "cancel"
		default:
			k.action = "connect"
		}
	}
	t.apply(k)
}

func (t *trayView) apply(k viewKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if k == t.last {
		return
	}
	t.last = k

	prefix := "icons/tray-color-"
	if runtime.GOOS == "darwin" {
		prefix = "icons/tray-mono-"
	}
	icon := mustIcon(prefix + k.icon + ".png")
	if runtime.GOOS == "darwin" {
		t.item.SetTemplateIcon(icon)
	} else {
		t.item.SetIcon(icon)
	}

	m := t.d.app.NewMenu()
	m.Add(k.line).SetEnabled(false)
	if k.server != "" {
		m.Add(k.server).SetEnabled(false)
	}
	m.AddSeparator()
	switch k.action {
	case "setup":
		m.Add(tr(k.lang, "setup")).OnClick(func(*application.Context) { t.d.showWindow() })
	default:
		m.Add(tr(k.lang, k.action)).OnClick(func(*application.Context) { go t.d.connectOrDisconnect() })
	}
	m.AddSeparator()
	m.Add(tr(k.lang, "show")).OnClick(func(*application.Context) { t.d.showWindow() })
	m.Add(tr(k.lang, "quit")).OnClick(func(*application.Context) { t.d.quit() })
	t.item.SetMenu(m)
}
