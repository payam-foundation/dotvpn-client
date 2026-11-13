// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/helper"
	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/report"
	"github.com/payam-foundation/dotvpn-client/internal/service"
)

const usage = `usage: dotvpn <command>

  connect | disconnect | status      drive the helper
  import <dv://...>              save an invite in the helper
  report [--window 60m] [--note TEXT] --out FILE
                                     write an encrypted report to email to report@dotvpn.org
  service install|uninstall|status   manage the helper (needs root/Administrator to change)
  daemon                             run the helper (the service runs this)
  version

Without a command, dotvpn runs in the foreground; see dotvpn -h.
`

// subcommand runs the named command and reports whether args named one.
func subcommand(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	rest := args[1:]
	switch args[0] {
	case "daemon":
		return true, daemonCmd(ctx, rest)
	case "service":
		return true, serviceCmd(rest)
	case "status", "connect", "disconnect", "import", "report":
		return true, clientCmd(ctx, args[0], rest)
	case "help":
		fmt.Print(usage)
		return true, nil
	}
	return false, nil
}

func daemonConfigPath() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "dotvpn", "client.yaml")
	}
	return "/etc/dotvpn/client.yaml"
}

func daemonCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	cfg := fs.String("config", daemonConfigPath(), "config file")
	owner := fs.String("owner", "", "user id (SID on Windows) allowed to connect besides root")
	addr := fs.String("addr", ipc.DefaultAddr(), "socket path or pipe name")
	verbose := fs.Bool("v", false, "debug logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lvl := slog.LevelInfo
	if *verbose {
		lvl = slog.LevelDebug
	}
	log := report.Tee(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(log)
	runHelper := func(ctx context.Context) error {
		helper.AppVersion = version
		ln, err := ipc.Listen(*addr, *owner)
		if err != nil {
			return err
		}
		log.Info("helper ready", "addr", *addr)
		return helper.New(log, *cfg).Run(ctx, ln)
	}
	if ok, err := service.RunAsService(func(stop <-chan struct{}) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() { <-stop; cancel() }()
		return runHelper(ctx)
	}); ok {
		return err
	}
	return runHelper(ctx)
}

func serviceCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("service: install, uninstall or status")
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		owner := fs.String("owner", "", "user id (SID on Windows) allowed to connect; default is the invoking user")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return err
		}
		if *owner == "" {
			*owner = invokingUser()
		}
		_ = service.Uninstall() // frees the installed binary (Windows locks it) before it is replaced
		if exe, err = service.Place(exe); err != nil {
			return err
		}
		if err := service.Install(service.Spec{Exe: exe, Config: daemonConfigPath(), Owner: *owner}); err != nil {
			return err
		}
		fmt.Println("helper installed and running")
		return nil
	case "uninstall":
		if err := service.Uninstall(); err != nil {
			return err
		}
		if err := os.Remove(service.SystemPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Println("helper removed")
		return nil
	case "status":
		st := service.Query()
		fmt.Printf("installed=%t running=%t\n", st.Installed, st.Running)
		return nil
	}
	return fmt.Errorf("service: unknown action %q", args[0])
}

// invokingUser is the user who should talk to the helper: the one behind sudo
// if any, else the current user. Root alone yields "" (root-only socket).
func invokingUser() string {
	if v := os.Getenv("SUDO_UID"); v != "" {
		return v
	}
	u, err := user.Current()
	if err != nil || (runtime.GOOS != "windows" && u.Uid == "0") {
		return ""
	}
	return u.Uid // a SID on Windows
}

func clientCmd(ctx context.Context, name string, args []string) error {
	c := &ipc.Client{}
	switch name {
	case "status":
		st, err := c.Status(ctx)
		if err != nil {
			return helperErr(err)
		}
		fmt.Println(statusLine(st))
		return nil
	case "disconnect":
		return helperErr(c.Disconnect(ctx))
	case "import":
		if len(args) != 1 {
			return errors.New("import: one dv:// invite")
		}
		_, err := c.ImportInvite(ctx, args[0])
		return helperErr(err)
	case "report":
		return reportCmd(ctx, c, args)
	}
	ev, err := c.Subscribe(ctx)
	if err != nil {
		return helperErr(err)
	}
	if err := c.Connect(ctx); err != nil {
		return helperErr(err)
	}
	timeout := time.After(60 * time.Second)
	for {
		select {
		case st, ok := <-ev:
			if !ok {
				return errors.New("helper went away")
			}
			switch st.State {
			case ipc.StateConnected:
				fmt.Println(statusLine(st))
				return nil
			case ipc.StateFailed:
				return fmt.Errorf("%s", st.Error)
			}
		case <-timeout:
			return errors.New("still connecting; run dotvpn status")
		case <-ctx.Done():
			return nil
		}
	}
}

func statusLine(st ipc.Status) string {
	parts := []string{string(st.State)}
	if st.Server != "" {
		parts = append(parts, "via "+st.Carrier, st.Server, fmt.Sprintf("%dms", st.LatencyMS))
	}
	if st.FullTunnel {
		parts = append(parts, "full tunnel")
	}
	if st.KillSwitch {
		parts = append(parts, "kill switch")
	}
	if st.Error != "" {
		parts = append(parts, st.Error)
	}
	return strings.Join(parts, "  ")
}

// helperErr turns "socket missing" into the thing to do about it.
func helperErr(err error) error {
	if err == nil {
		return nil
	}
	var e *ipc.Error
	if errors.As(err, &e) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "connection refused") {
		return fmt.Errorf("cannot reach the helper (%w); run `sudo dotvpn service install`", err)
	}
	return err
}

// reportCmd asks the helper for a sealed report and writes it to --out.
func reportCmd(ctx context.Context, c *ipc.Client, args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	window := fs.Duration("window", time.Hour, "log window, 30m or 60m")
	note := fs.String("note", "", "what went wrong (max 1 KB, sealed with the report)")
	out := fs.String("out", "", "file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("report: --out FILE is required")
	}
	r, err := c.BuildReport(ctx, ipc.BuildReportParams{WindowMin: int(window.Minutes()), Note: *note})
	if err != nil {
		return helperErr(err)
	}
	if err := os.WriteFile(*out, r.Blob, 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s (report %s); email it to %s\n", *out, r.ID, report.Address)
	return nil
}
