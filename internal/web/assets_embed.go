//go:build embedded_frontend

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedDist embed.FS

func assetFS() fs.FS {
	dist, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil
	}
	return dist
}
