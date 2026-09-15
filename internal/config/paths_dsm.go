package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// devRootEnv points the daemon at a throwaway directory tree. It is only set
// outside the NAS platform (local development and integration testing).
const devRootEnv = "ETP_DEV_ROOT"

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

// DevRoot returns the development root override, if any.
func DevRoot() string {
	return strings.TrimSpace(os.Getenv(devRootEnv))
}

// DevMode reports whether the daemon runs outside the NAS platform.
func DevMode() bool {
	return DevRoot() != ""
}
