//go:build fnos

package config

import (
	"errors"
	"os"
	"path/filepath"
)

// daemonBinaryName is the file name of the daemon under PkgDest/bin. The
// supervise loop re-executes this path, so it must match the binary the
// package build installs for the platform.
const daemonBinaryName = "easytier-pro-fnos"

// ResolvePaths reads the fnOS package env (TRIM_APPDEST / TRIM_PKGVAR),
// falling back to ETP_DEV_ROOT for development.
func ResolvePaths() (Paths, error) {
	pkgDest := os.Getenv("TRIM_APPDEST")
	pkgVar := os.Getenv("TRIM_PKGVAR")
	if devRoot := DevRoot(); devRoot != "" {
		pkgDest = filepath.Join(devRoot, "target")
		pkgVar = filepath.Join(devRoot, "var")
	}
	if pkgDest == "" || pkgVar == "" {
		return Paths{}, errors.New("TRIM_APPDEST and TRIM_PKGVAR must be set")
	}
	return Paths{PkgDest: pkgDest, PkgVar: pkgVar}, nil
}
