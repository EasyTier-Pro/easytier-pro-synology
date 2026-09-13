// Package ui bundles the static management interface. DSM serves the same
// files from the package directory; the embedded copy keeps the interface
// usable when the daemon runs outside DSM.
package ui

import "embed"

// FS holds the interface assets.
//
//go:embed index.html app.js styles.css lib pages images
var FS embed.FS
