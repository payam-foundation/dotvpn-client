// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// SystemPath is where the installed helper binary lives: a location only an
// administrator can write, because the service runs it as root. Running it
// from a download folder or the app bundle would let any local user replace
// it and gain root.
func SystemPath() string {
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		return filepath.Join(pf, "Dot VPN", "dotvpn.exe")
	case "darwin":
		return "/Library/PrivilegedHelperTools/" + Label
	}
	return "/usr/local/lib/dotvpn/dotvpn"
}

// Place copies the binary at src to SystemPath and returns that path. Needs
// root. The service must be stopped first on Windows, which locks a running
// exe.
func Place(src string) (string, error) {
	dst := SystemPath()
	return dst, place(src, dst)
}

func place(src, dst string) error {
	if a, err := os.Stat(src); err == nil {
		if b, err := os.Stat(dst); err == nil && os.SameFile(a, b) {
			return nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy helper: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
