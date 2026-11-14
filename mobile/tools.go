// SPDX-License-Identifier: AGPL-3.0-only

//go:build tools

package mobile

// Keeps golang.org/x/mobile in go.mod: `gomobile bind` needs its bind package
// in the module graph.
import _ "golang.org/x/mobile/bind"
