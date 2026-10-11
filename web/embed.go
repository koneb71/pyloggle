// Package web embeds the built single-page UI (web/dist). Run `make web` (or the Docker build)
// to populate dist; a bare checkout only carries a placeholder so `go build` always works.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
