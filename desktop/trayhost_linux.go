// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package main

import "github.com/godbus/dbus/v5"

// trayHostPresent reports whether something on the session bus can show a
// StatusNotifierItem. Stock GNOME has none until the AppIndicator extension
// is on; without a host the app keeps its window instead of vanishing.
func trayHostPresent() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
	return err == nil && has
}
