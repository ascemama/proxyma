//go:build !embedded_frontend

package web

import "io/fs"

func assetFS() fs.FS {
	return nil
}
