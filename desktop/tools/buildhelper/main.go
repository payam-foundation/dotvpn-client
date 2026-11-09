// Command buildhelper builds the CGO-free dotvpn binary into desktop/helper,
// where the app embeds it. Run through `go generate` in desktop/.
//
// If a go.work sits beside the client module (parent of github.com/.../dotvpn-client),
// it is used even when the caller set GOWORK=off — local wire Relays and friends
// need that workspace until they land on the pinned tag. CI has no such file and
// builds against go.mod's pin.
package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	clientRoot, err := filepath.Abs("..")
	if err != nil {
		log.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", "desktop/helper/dotvpn", "./cmd/dotvpn")
	cmd.Dir = clientRoot
	cmd.Env = append(stripEnv(os.Environ(), "GOWORK"), "CGO_ENABLED=0")
	if w := filepath.Join(filepath.Dir(clientRoot), "go.work"); fileExists(w) {
		cmd.Env = append(cmd.Env, "GOWORK="+w)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func stripEnv(env []string, key string) []string {
	prefix := key + "="
	out := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
