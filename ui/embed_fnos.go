//go:build fnos

package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist-fnos
var dist embed.FS

// FS holds the interface assets, rooted so that index.html is at the top level
// the way a file server expects.
var FS fs.FS = mustSubFnos()

func mustSubFnos() fs.FS {
	sub, err := fs.Sub(dist, "dist-fnos")
	if err != nil {
		// The embed directive above cannot succeed without this directory, so a
		// failure here means the build produced nothing to embed.
		panic(err)
	}
	return sub
}
