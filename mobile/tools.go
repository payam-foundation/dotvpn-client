//go:build tools

package mobile

// Keeps golang.org/x/mobile in go.mod: `gomobile bind` needs its bind package
// in the module graph.
import _ "golang.org/x/mobile/bind"
