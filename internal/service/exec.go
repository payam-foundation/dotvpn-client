//go:build !windows

package service

import (
	"fmt"
	"os/exec"
	"strings"
)

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RunAsService is for Windows; elsewhere the init system runs the daemon as a
// plain process.
func RunAsService(func(stop <-chan struct{}) error) (bool, error) { return false, nil }
