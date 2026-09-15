// Package platform abstracts the NAS-specific wiring between the shared daemon
// and the host operating system (Synology DSM or fnOS).
package platform

import (
	"net"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/httpserver"
)

// Platform carries the per-OS integration points the daemon needs.
type Platform struct {
	// Name is the build-time identifier ("dsm" or "fnos").
	Name string
	// DisplayName is used in enrollment key names and logs, e.g. "Synology NAS".
	DisplayName string
	// ListenAddr returns the address/socket path and a ready listener.
	ListenAddr func(paths config.Paths) (string, net.Listener, error)
	// NewAuth builds the session authenticator for the platform gateway.
	NewAuth func(log *config.Logger) httpserver.Authenticator
	// AuthRequiredCode / AuthForbiddenCode are the apperr codes the gateway
	// middleware maps to 401 / 403.
	AuthRequiredCode  string
	AuthForbiddenCode string
}

// Current is the platform selected at build time (dsm by default, fnos with
// the "fnos" build tag).
var Current Platform
