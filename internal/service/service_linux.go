// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"errors"
	"os"
	"os/exec"
)

const unitPath = "/etc/systemd/system/" + Name + ".service"

// Install writes the systemd unit and starts it. Needs root.
func Install(s Spec) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath, []byte(unit(s)), 0o644); err != nil {
		return err
	}
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return runCmd("systemctl", "enable", "--now", Name+".service")
}

// Uninstall stops the helper and removes its unit. Needs root.
func Uninstall() error {
	_ = exec.Command("systemctl", "disable", "--now", Name+".service").Run()
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return runCmd("systemctl", "daemon-reload")
}

// Query reports whether the helper is installed and running.
func Query() Status {
	_, err := os.Stat(unitPath)
	st := Status{Installed: err == nil}
	st.Running = exec.Command("systemctl", "is-active", "--quiet", Name+".service").Run() == nil
	return st
}
