package service

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Install registers an auto-start Windows service and starts it. Needs
// Administrator.
func Install(s Spec) error {
	if err := s.validate(); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if old, err := m.OpenService(Name); err == nil { // reinstall
		stopAndWait(old)
		_ = old.Delete()
		old.Close()
	}
	sv, err := m.CreateService(Name, s.Exe, mgr.Config{
		DisplayName: "Dot VPN helper",
		StartType:   mgr.StartAutomatic,
	}, s.args()...)
	if err != nil {
		return err
	}
	defer sv.Close()
	return sv.Start()
}

// Uninstall stops and deletes the service. Needs Administrator.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	sv, err := m.OpenService(Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		return err
	}
	defer sv.Close()
	stopAndWait(sv)
	return sv.Delete()
}

func stopAndWait(sv *mgr.Service) {
	st, err := sv.Control(svc.Stop)
	for i := 0; err == nil && st.State != svc.Stopped && i < 50; i++ {
		time.Sleep(200 * time.Millisecond)
		st, err = sv.Query()
	}
}

// Query reports whether the helper is installed and running. Reading service
// state needs no elevation.
func Query() Status {
	m, err := mgr.Connect()
	if err != nil {
		return Status{}
	}
	defer m.Disconnect()
	sv, err := m.OpenService(Name)
	if err != nil {
		return Status{}
	}
	defer sv.Close()
	st, err := sv.Query()
	return Status{Installed: true, Running: err == nil && st.State == svc.Running}
}

type handler struct {
	run func(stop <-chan struct{}) error
}

func (h handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- h.run(stop) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				return false, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending}
				close(stop)
				<-done // the session restores routes and firewall before we report stopped
				return false, 0
			}
		}
	}
}

// RunAsService runs run under the service manager if this process was started
// by it, and reports whether it did. run must return once stop closes.
func RunAsService(run func(stop <-chan struct{}) error) (bool, error) {
	is, err := svc.IsWindowsService()
	if err != nil || !is {
		return false, err
	}
	return true, svc.Run(Name, handler{run})
}
