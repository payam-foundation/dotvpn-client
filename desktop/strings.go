package main

import (
	"fmt"
	"strings"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
)

// The tray and notifications are drawn by the OS, outside the webview, so
// their copy lives here. Everything inside the window is in frontend/src/i18n.
// Copy follows design/GUIDELINES.md.
var copyFor = map[string]map[string]string{
	"en": {
		"off":         "Off",
		"connecting":  "Connecting",
		"switching":   "Switching route",
		"connected":   "Connected",
		"failed":      "Failed",
		"connect":     "Connect",
		"disconnect":  "Disconnect",
		"cancel":      "Cancel",
		"show":        "Show Dot VPN",
		"quit":        "Quit",
		"setup":       "Set up Dot VPN",
		"ms":          "ms",
		"fallback":    "Couldn't reach the server. Trying the next route.",
		"blocked":     "Traffic stays blocked until a route works.",
		"failedBody":  "Couldn't connect. Open Dot VPN for details.",
		"helperDown":  "Dot VPN isn’t set up",
		"noInvite":    "Add an invite to connect",
		"notifyTitle": "Dot VPN",
	},
	"fa": {
		"off":         "خاموش",
		"connecting":  "در حال اتصال",
		"switching":   "تغییر مسیر",
		"connected":   "متصل",
		"failed":      "ناموفق",
		"connect":     "اتصال",
		"disconnect":  "قطع اتصال",
		"cancel":      "لغو",
		"show":        "نمایش Dot VPN",
		"quit":        "خروج",
		"setup":       "راه‌اندازی Dot VPN",
		"ms":          "میلی‌ثانیه",
		"fallback":    "سرور در دسترس نبود. مسیر بعدی امتحان می‌شود.",
		"blocked":     "تا برقراری مسیر، ترافیک مسدود می‌ماند.",
		"failedBody":  "اتصال برقرار نشد. برای جزئیات Dot VPN را باز کنید.",
		"helperDown":  "Dot VPN راه‌اندازی نشده",
		"noInvite":    "برای اتصال یک دعوت‌نامه اضافه کنید",
		"notifyTitle": "Dot VPN",
	},
}

func tr(lang, key string) string {
	if m, ok := copyFor[lang]; ok {
		return m[key]
	}
	return copyFor["en"][key]
}

// persianDigits swaps Latin digits for Persian ones in prose; hosts, carriers
// and invites keep Latin digits per the guidelines.
func persianDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '۰' + (r - '0')
		}
		return r
	}, s)
}

// stateWord is the one-word state, e.g. "Connected".
func stateWord(lang string, st ipc.Status) string { return tr(lang, string(st.State)) }

// stateLine is the tray's first row: "Connected · h3 · 42 ms".
func stateLine(lang string, st ipc.Status) string {
	parts := []string{stateWord(lang, st)}
	if st.State == ipc.StateConnected && st.Carrier != "" {
		parts = append(parts, st.Carrier)
		if st.LatencyMS > 0 {
			ms := fmt.Sprintf("%d", st.LatencyMS)
			if lang == "fa" {
				ms = persianDigits(ms)
			}
			parts = append(parts, ms+" "+tr(lang, "ms"))
		}
	}
	return strings.Join(parts, " · ")
}
