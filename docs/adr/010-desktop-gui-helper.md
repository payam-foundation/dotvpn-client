# ADR-010: Desktop GUI over a privileged helper

Status: accepted

## Context

The client was CLI-only and full tunnel needs root or Administrator. Users want a tray app, not a terminal. The lab drives the `dotvpn` flags, and `AGENTS.md` requires the `dotvpn` binary to stay CGO-free and static.

## Decision

- **Helper.** `dotvpn daemon` runs as a service (launchd, systemd, Windows service) installed once with `dotvpn service install`. It owns the config, the engine, the TUN and the kill switch through `internal/daemon.Session`. The foreground flags use the same `Session`, so there is one code path. On start with no active session it runs the existing `Unblock`, so crash leftovers are cleaned automatically.
- **IPC.** `internal/ipc`: versioned JSON requests plus an event stream over a unix socket (macOS, Linux) or a named pipe (Windows). Only the installing user may connect. The GUI never sees tokens or account keys; the helper returns a redacted view.
- **GUI.** `desktop/` is a nested module (own `go.mod`) using Wails v3 and a Svelte/TypeScript frontend on `design/tokens.css`. It imports only the `ipc` types, so the CGO webview dependency stays out of the `dotvpn` binary. It is unprivileged. The only elevated step is `service install`, run through the OS prompt (`osascript`, `pkexec`, UAC).
- **Helper delivery.** The CGO-free `dotvpn` binary is embedded in the app (`go generate` in `desktop/` builds it). Setup extracts it to a private temp dir and runs `dotvpn service install` elevated; that copies it to a root-owned path (`/Library/PrivilegedHelperTools/org.dotvpn.helper`, `/usr/local/lib/dotvpn/dotvpn`, `%ProgramFiles%\Dot VPN\dotvpn.exe`) and points the service there. The service never runs a binary from a user-writable location, which would be a local root escalation. Updating the app and pressing setup again replaces the helper.
- **CLI.** `connect`, `disconnect`, `status`, `import` are thin IPC clients. The legacy flags (`-tun`, `-kill-switch`, `-unblock`, `-invite`) stay for the lab and scripts.
- **Wails v3**, not v2 plus `fyne.io/systray`: v3 has window, tray, dock, autostart, single-instance and URL-scheme APIs in one run loop. v2 has no tray, and a second run loop on macOS is a known problem.
- **Notifications** need a bundle identifier on macOS. A bare binary (`go run`, a dev build) runs without them.

## Consequences

- The tunnel and kill switch outlive the GUI. Quitting the app does not drop the tunnel.
- One admin prompt at install, none per connect.
- The Windows pipe ACL adds `github.com/Microsoft/go-winio`; `x/sys` cannot set a pipe security descriptor cleanly.
- The GUI build needs CGO, WebView2 (Windows) or WebKitGTK (Linux). CI builds it natively on all three.
- GNOME has no tray host without the AppIndicator extension. The app then keeps its window and says so in Settings.

## Rejected

- Privileged GUI: a prompt per connect and no kill switch after a GUI crash.
- Deleting the CLI: the lab and scripts depend on it.
