//go:build fnos

package platform

import (
	"fmt"
	"net"
	"os"
	"os/user"
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
	// Only the fnOS gateway (nginx worker) may reach the daemon: the API trusts
	// the gateway-injected identity headers, so the socket must not be
	// world-accessible or any local process could forge them. Group-ownership
	// goes to the nginx worker group when it can be found and we are root (only
	// root may chown to a foreign group); otherwise the socket keeps the
	// caller's own group, which on a real fnOS appliance is still a restricted
	// root group, and in development is simply the developer's group.
	if os.Geteuid() == 0 {
		if gid, gerr := gatewayGroupID(); gerr == nil {
			// A chown failure here is not fatal: the mode below still keeps the
			// socket private to root and its own group.
			_ = os.Chown(sock, 0, gid)
		}
	}
	if err := os.Chmod(sock, 0o660); err != nil {
		ln.Close()
		return "", nil, fmt.Errorf("chmod %s: %w", sock, err)
	}
	return sock, ln, nil
}

// gatewayGroupID resolves the group the fnOS nginx workers run as.
func gatewayGroupID() (int, error) {
	for _, name := range []string{"www-data", "nginx", "trim"} {
		if g, err := user.LookupGroup(name); err == nil {
			var gid int
			if _, err := fmt.Sscanf(g.Gid, "%d", &gid); err == nil {
				return gid, nil
			}
		}
	}
	return -1, fmt.Errorf("no nginx worker group found")
}
