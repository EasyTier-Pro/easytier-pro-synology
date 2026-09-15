package config

import (
	"os"
	"strings"
)

// devRootEnv points the daemon at a throwaway directory tree. It is only set
// outside the NAS platform (local development and integration testing).
const devRootEnv = "ETP_DEV_ROOT"

// DevRoot returns the development root override, if any.
func DevRoot() string {
	return strings.TrimSpace(os.Getenv(devRootEnv))
}

// DevMode reports whether the daemon runs outside the NAS platform.
func DevMode() bool {
	return DevRoot() != ""
}
