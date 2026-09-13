// Package ui bundles the built management interface. DSM serves the same files
// from the package directory; the embedded copy keeps the interface usable when
// the daemon runs outside DSM.
//
// The embedded tree is the build output, not the sources: run `npm run build` in
// this directory before building the daemon, which scripts/build-spk.sh does.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS holds the interface assets, rooted so that index.html is at the top level
// the way a file server expects.
var FS fs.FS = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed directive above cannot succeed without this directory, so a
		// failure here means the build produced nothing to embed.
		panic(err)
	}
	return sub
}
