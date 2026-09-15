//go:build !fnos

package config

import (
	"errors"
	"os"
	"path/filepath"
)

// daemonBinaryName is the file name of the daemon under PkgDest/bin. The
// supervise loop re-executes this path, so it must match the binary the
// package build installs for the platform.
const daemonBinaryName = "easytier-pro-dsm"

// ResolvePaths reads the Synology package roots from the environment. When
// ETP_DEV_ROOT is set it wins over the DSM variables so the daemon can run on
// any machine.
func ResolvePaths() (Paths, error) {
	pkgDest := os.Getenv("SYNOPKG_PKGDEST")
	pkgVar := os.Getenv("SYNOPKG_PKGVAR")
	if devRoot := DevRoot(); devRoot != "" {
		pkgDest = filepath.Join(devRoot, "target")
		pkgVar = filepath.Join(devRoot, "var")
	}
	if pkgDest == "" || pkgVar == "" {
		return Paths{}, errors.New("SYNOPKG_PKGDEST and SYNOPKG_PKGVAR must be set")
	}
	return Paths{PkgDest: pkgDest, PkgVar: pkgVar}, nil
}
