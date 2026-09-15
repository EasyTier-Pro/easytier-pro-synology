//go:build fnos

package platform

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/fnosenv"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/httpserver"
)

func init() {
	Current = Platform{
		Name:              "fnos",
		DisplayName:       "fnOS NAS",
		ListenAddr:        listenFnOS,
		NewAuth:           func(log *config.Logger) httpserver.Authenticator { return fnosenv.New(log) },
		AuthRequiredCode:  apperr.CodeFnOSAuthRequired,
		AuthForbiddenCode: apperr.CodeFnOSAuthForbidden,
	}
}

// listenFnOS creates the Unix socket the fnOS gateway proxies to.
func listenFnOS(paths config.Paths) (string, net.Listener, error) {
	sock := filepath.Join(paths.PkgDest, "app.sock")
	if override := os.Getenv("ETP_DEV_LISTEN"); config.DevMode() && override != "" {
		sock = override
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		return "", nil, err
	}
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return "", nil, fmt.Errorf("listen %s: %w", sock, err)
	}
	// The fnOS gateway (nginx worker) must be able to connect.
	if err := os.Chmod(sock, 0o666); err != nil {
		ln.Close()
		return "", nil, err
	}
	return sock, ln, nil
}
