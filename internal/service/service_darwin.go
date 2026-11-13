// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const plistPath = "/Library/LaunchDaemons/" + Label + ".plist"

// Install writes the LaunchDaemon and starts it. Needs root.
func Install(s Spec) error {
	if err := s.validate(); err != nil {
		return err
	}
	bootout()
	if err := os.WriteFile(plistPath, []byte(plist(s)), 0o644); err != nil {
		return err
	}
	if err := bootstrap(); err != nil {
		// launchctl often returns EIO (5) when the job is half-loaded; clear
		// and try once more before surfacing the error.
		bootout()
		time.Sleep(300 * time.Millisecond)
		if err2 := bootstrap(); err2 != nil {
			return err2
		}
	}
	_ = exec.Command("launchctl", "kickstart", "-k", "system/"+Label).Run()
	return nil
}

func bootout() {
	_ = exec.Command("launchctl", "bootout", "system/"+Label).Run()
	_ = exec.Command("launchctl", "unload", plistPath).Run() // older launchd
}

func bootstrap() error {
	out, err := exec.Command("launchctl", "bootstrap", "system", plistPath).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	// Already bootstrapped after a partial bootout: treat as success and let
	// kickstart refresh the process.
	if strings.Contains(msg, "already bootstrapped") || strings.Contains(msg, "service already loaded") {
		return nil
	}
	return fmt.Errorf("launchctl bootstrap system %s: %s", plistPath, msg)
}

// Uninstall stops the helper and removes its definition. Needs root.
func Uninstall() error {
	bootout()
	if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Query reports whether the helper is installed and running.
func Query() Status {
	_, err := os.Stat(plistPath)
	st := Status{Installed: err == nil}
	out, err := exec.Command("launchctl", "print", "system/"+Label).Output()
	st.Running = err == nil && strings.Contains(string(out), "state = running")
	return st
}
