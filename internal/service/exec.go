//go:build !windows

package service

// RunAsService is for Windows; elsewhere the init system runs the daemon as a
// plain process.
func RunAsService(func(stop <-chan struct{}) error) (bool, error) { return false, nil }
