// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/payam-foundation/dotvpn-client/internal/report"
	"github.com/payam-foundation/log-decypher/armor"
)

// ReportView is what the report dialog shows. The sealed blob stays on disk;
// only its armored text crosses to the window (for chat users).
type ReportView struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Preview string `json:"preview,omitempty"`
	Armor   string `json:"armor,omitempty"`
}

var reportID = regexp.MustCompile(`^[2-9A-Z]{8}$`)

func reportDir() string { return filepath.Join(os.TempDir(), "dotvpn-reports") }

// sweepReports removes reports a previous run left behind.
func sweepReports() { _ = os.RemoveAll(reportDir()) }

func reportPath(id string) (string, error) {
	if !reportID.MatchString(id) {
		return "", &helperError{Code: ipc.CodeInternal, Message: "bad report id"}
	}
	return filepath.Join(reportDir(), report.FileName(id)), nil
}

// BuildReport asks the helper for a report. With preview it returns only the
// plaintext; otherwise it writes the sealed file (0600) and returns its id.
func (s *AppService) BuildReport(ctx context.Context, windowMin int, note string, preview bool) (ReportView, error) {
	r, err := s.d.ipc.BuildReport(ctx, ipc.BuildReportParams{WindowMin: windowMin, Note: note, Preview: preview})
	if err != nil {
		return ReportView{}, toErr(err)
	}
	v := ReportView{ID: r.ID, Address: report.Address, Preview: r.Preview}
	if preview {
		return v, nil
	}
	path, err := reportPath(r.ID)
	if err != nil {
		return ReportView{}, err
	}
	if err := os.MkdirAll(reportDir(), 0o700); err != nil {
		return ReportView{}, toErr(err)
	}
	if err := os.WriteFile(path, r.Blob, 0o600); err != nil {
		return ReportView{}, toErr(err)
	}
	v.Armor = armor.Encode(r.Blob)
	return v, nil
}

// RevealReport shows the file in the OS file manager and opens a draft mail.
// mailto: cannot attach files, so the user drags the file in.
func (s *AppService) RevealReport(id string) error {
	path, err := reportPath(id)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer.exe", "/select,", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return &helperError{Code: ipc.CodeInternal, Message: err.Error()}
	}
	go cmd.Wait()
	q := url.Values{"subject": {"Support request " + id}, "body": {"Report " + id + "\n\n(attach r-" + id + ".dat)"}}
	u := "mailto:" + report.Address + "?" + encodeMailto(q)
	if err := s.d.app.Browser.OpenURL(u); err != nil {
		return &helperError{Code: ipc.CodeInternal, Message: err.Error()}
	}
	return nil
}

// DiscardReport deletes a built report.
func (s *AppService) DiscardReport(id string) {
	if path, err := reportPath(id); err == nil {
		_ = os.Remove(path)
	}
}

// encodeMailto is url.Values.Encode with %20 for spaces: some mail clients
// show "+" literally.
func encodeMailto(q url.Values) string {
	return strings.ReplaceAll(q.Encode(), "+", "%20")
}
