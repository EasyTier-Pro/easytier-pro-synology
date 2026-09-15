//go:build !fnos

package platform

import (
	"fmt"
	"net"
	"os"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/dsmenv"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/httpserver"
)

// listenAddress is the loopback address the DSM reverse proxy forwards to.
const listenAddress = "127.0.0.1:15890"

func init() {
	Current = Platform{
		Name:              "dsm",
		DisplayName:       "Synology NAS",
		ListenAddr:        listenDSM,
		NewAuth:           func(log *config.Logger) httpserver.Authenticator { return dsmenv.New(log) },
		AuthRequiredCode:  apperr.CodeDSMAuthRequired,
		AuthForbiddenCode: apperr.CodeDSMAuthForbidden,
	}
}

func listenDSM(paths config.Paths) (string, net.Listener, error) {
	addr := listenAddress
	if override := os.Getenv("ETP_DEV_LISTEN"); config.DevMode() && override != "" {
		addr = override
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	return addr, ln, nil
}
