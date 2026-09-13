// Package dsmenv validates that a request comes from an authenticated DSM
// administrator session.
package dsmenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// authenticateCGI is the DSM module that maps a session cookie to a user name.
const authenticateCGI = "/usr/syno/synoman/webman/modules/authenticate.cgi"

const (
	cacheTTL   = 30 * time.Second
	cgiTimeout = 5 * time.Second
	maxOutput  = 4096
	adminGroup = "administrators"
	devUser    = "dev"
)

// Authenticator checks DSM sessions and caches the result per cookie.
type Authenticator struct {
	bypass bool

	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	user    string
	ok      bool
	expires time.Time
}

// New builds an authenticator. The development bypass is only honoured when
// the daemon runs outside DSM.
func New() *Authenticator {
	bypass := config.DevMode() && os.Getenv("ETP_DEV_NO_DSM_AUTH") == "1"
	return &Authenticator{bypass: bypass, cache: map[string]entry{}}
}

// Bypassed reports whether DSM session checks are disabled for development.
func (a *Authenticator) Bypassed() bool { return a.bypass }

// Authenticate returns the DSM administrator behind the request.
func (a *Authenticator) Authenticate(ctx context.Context, request *http.Request) (string, *apperr.Error) {
	if a.bypass {
		return devUser, nil
	}
	cookie := request.Header.Get("Cookie")
	if strings.TrimSpace(cookie) == "" {
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
	if user, ok, cached := a.cached(cookie); cached {
		if !ok {
			return "", apperr.New(apperr.CodeDSMAuthRequired)
		}
		return user, nil
	}
	user, err := runCGI(ctx, authenticateCGI, cgiEnv(request))
	if err != nil {
		a.store(cookie, "", false)
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
	user = strings.TrimSpace(firstLine(user))
	if user == "" {
		a.store(cookie, "", false)
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
	if !isAdministrator(ctx, user) {
		a.store(cookie, user, false)
		return "", apperr.New(apperr.CodeDSMAuthForbidden)
	}
	a.store(cookie, user, true)
	return user, nil
}

func (a *Authenticator) cached(cookie string) (string, bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	found, ok := a.cache[cookie]
	if !ok || time.Now().After(found.expires) {
		delete(a.cache, cookie)
		return "", false, false
	}
	return found.user, found.ok, true
}

func (a *Authenticator) store(cookie, user string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) > 512 {
		a.cache = map[string]entry{}
	}
	a.cache[cookie] = entry{user: user, ok: ok, expires: time.Now().Add(cacheTTL)}
}

// cgiEnv synthesizes the CGI environment DSM modules expect.
func cgiEnv(request *http.Request) []string {
	host := request.Host
	serverName := host
	serverPort := "443"
	if name, port, err := net.SplitHostPort(host); err == nil {
		serverName = name
		serverPort = port
	}
	remoteAddr := request.RemoteAddr
	if name, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		remoteAddr = name
	}
	serverAddr := remoteAddr
	if localAddr, ok := request.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && localAddr != nil {
		if local, _, err := net.SplitHostPort(localAddr.String()); err == nil {
			serverAddr = local
		}
	}
	return []string{
		"GATEWAY_INTERFACE=CGI/1.1",
		"REQUEST_METHOD=GET",
		"SERVER_PROTOCOL=HTTP/1.1",
		"SERVER_SOFTWARE=easytier-pro-dsm",
		"SERVER_NAME=" + serverName,
		"SERVER_PORT=" + serverPort,
		"SERVER_ADDR=" + serverAddr,
		"REMOTE_ADDR=" + remoteAddr,
		"PATH_INFO=" + request.URL.Path,
		"QUERY_STRING=",
		"REQUEST_URI=" + request.URL.RequestURI(),
		"SCRIPT_NAME=" + request.URL.Path,
		"HTTP_COOKIE=" + request.Header.Get("Cookie"),
		"HTTP_HOST=" + host,
	}
}

// runCGI executes one DSM module with the synthesized environment.
func runCGI(parent context.Context, path string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, cgiTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append([]string{
		"PATH=/usr/syno/bin:/usr/syno/sbin:/usr/bin:/bin",
	}, env...)
	output, err := cmd.Output()
	if len(output) > maxOutput {
		output = output[:maxOutput]
	}
	if err != nil {
		return string(output), fmt.Errorf("authenticate.cgi: %w", err)
	}
	return string(output), nil
}

// isAdministrator reports whether user belongs to the DSM administrators group.
func isAdministrator(ctx context.Context, user string) bool {
	runCtx, cancel := context.WithTimeout(ctx, cgiTimeout)
	defer cancel()
	output, err := exec.CommandContext(runCtx, "id", "-nG", user).Output()
	if err != nil {
		return false
	}
	for _, group := range strings.Fields(string(output)) {
		if group == adminGroup {
			return true
		}
	}
	return false
}

func firstLine(value string) string {
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		return value[:index]
	}
	return value
}
