package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// devRootEnv points the daemon at a throwaway directory tree. It is only set
// outside DSM (local development and integration testing).
const devRootEnv = "ETP_DEV_ROOT"

// Paths holds the two Synology package roots the daemon works with.
//
// PkgDest is replaced on every package upgrade and only holds the shipped
// files; PkgVar survives upgrades and holds all mutable state.
type Paths struct {
	PkgDest string
	PkgVar  string
}

// ResolvePaths reads the package roots from the environment. When ETP_DEV_ROOT
// is set it wins over the DSM variables so the daemon can run on any machine.
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

// DevMode reports whether the daemon runs outside DSM.
func DevMode() bool {
	return DevRoot() != ""
}

func (p Paths) RuntimeDir() string    { return filepath.Join(p.PkgVar, "runtime") }
func (p Paths) StateDir() string      { return filepath.Join(p.PkgVar, "state") }
func (p Paths) SecretsDir() string    { return filepath.Join(p.StateDir(), "secrets") }
func (p Paths) LogsDir() string       { return filepath.Join(p.PkgVar, "logs") }
func (p Paths) RunDir() string        { return filepath.Join(p.PkgVar, "run") }
func (p Paths) OperationsDir() string { return filepath.Join(p.StateDir(), "operations") }

// ArchiveCacheDir holds the runtime archives that were already downloaded and
// verified. It sits beside the runtime because both belong to the package
// variable tree, which survives package upgrades.
func (p Paths) ArchiveCacheDir() string { return filepath.Join(p.PkgVar, "cache") }

func (p Paths) SettingsFile() string       { return filepath.Join(p.StateDir(), "settings.json") }
func (p Paths) MachineIDFile() string      { return filepath.Join(p.StateDir(), "machine-id") }
func (p Paths) BootstrapTokenFile() string { return filepath.Join(p.SecretsDir(), "bootstrap-token") }
func (p Paths) SessionFile() string        { return filepath.Join(p.SecretsDir(), "console-session.json") }
func (p Paths) DeviceAuthFile() string     { return filepath.Join(p.StateDir(), "device-auth.json") }
func (p Paths) DownloadStatusFile() string {
	return filepath.Join(p.StateDir(), "download-status.json")
}
func (p Paths) ConnectionTransactionFile() string {
	return filepath.Join(p.StateDir(), "connection-transaction.json")
}
func (p Paths) UpdateTransactionFile() string {
	return filepath.Join(p.StateDir(), "update-transaction.json")
}
func (p Paths) DaemonLogFile() string { return filepath.Join(p.LogsDir(), "daemon.log") }
func (p Paths) PidFile() string       { return filepath.Join(p.RunDir(), "daemon.pid") }
func (p Paths) CorePIDFile() string   { return filepath.Join(p.RunDir(), "core.pid") }

func (p Paths) CoreBinary() string   { return filepath.Join(p.RuntimeDir(), "easytier-core") }
func (p Paths) CLIbinary() string    { return filepath.Join(p.RuntimeDir(), "easytier-cli") }
func (p Paths) DaemonBinary() string { return filepath.Join(p.PkgDest, "bin", "easytier-pro-dsm") }
func (p Paths) UIDir() string        { return filepath.Join(p.PkgDest, "ui") }

// EnsureDirs creates the state tree with owner-only permissions.
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.RuntimeDir(), p.StateDir(), p.SecretsDir(), p.LogsDir(), p.RunDir(), p.OperationsDir(), p.ArchiveCacheDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}
