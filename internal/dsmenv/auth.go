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
	"slices"
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

// sessionCookie is the DSM cookie that binds a request to a login session.
const sessionCookie = "id"

// Authenticator checks DSM sessions and caches the result per cookie.
type Authenticator struct {
	bypass bool

	mu    sync.Mutex
	cache map[string]entry
}

// outcome is the cached result of one session check.
type outcome int

const (
	outcomeUnauthenticated outcome = iota
	outcomeAuthenticated
	outcomeForbidden
)

type entry struct {
	user    string
	result  outcome
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
	if user, result, cached := a.cached(cookie); cached {
		switch result {
		case outcomeAuthenticated:
			return user, nil
		case outcomeForbidden:
			return "", apperr.New(apperr.CodeDSMAuthForbidden)
		default:
			return "", apperr.New(apperr.CodeDSMAuthRequired)
		}
	}
	user, ok := sessionUser(ctx, request)
	if !ok {
		a.store(cookie, "", outcomeUnauthenticated)
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
	if !isAdministrator(ctx, user) {
		a.store(cookie, user, outcomeForbidden)
		return "", apperr.New(apperr.CodeDSMAuthForbidden)
	}
	a.store(cookie, user, outcomeAuthenticated)
	return user, nil
}

// sessionUser returns the authenticated DSM user of one request, or false when
// no session cookie of the request resolves to a user.
func sessionUser(ctx context.Context, request *http.Request) (string, bool) {
	for _, cookie := range cookieCandidates(request.Header.Get("Cookie")) {
		output, err := runCGI(ctx, authenticateCGI, cgiEnv(request, cookie))
		if err != nil {
			continue
		}
		if user := strings.TrimSpace(firstLine(output)); user != "" {
			return user, true
		}
	}
	return "", false
}

// cookie is one name/value pair of a Cookie header.
type cookie struct {
	name  string
	value string
}

// cookieCandidates normalizes the Cookie header for authenticate.cgi.
//
// authenticate.cgi resolves "id" to the LAST occurrence in the header, but a
// browser can hold several "id" cookies at once: a stale one left behind by an
// earlier login or by a different cookie path, next to the current one. The
// order the browser happens to send then decides the outcome, and whenever a
// stale value comes last every request is rejected as unauthenticated, which
// the interface reports as an expired DSM session.
//
// Normalizing to exactly one "id" per attempt and trying every value removes
// that ordering dependency. The last value is what authenticate.cgi would have
// used, so it is attempted first.
func cookieCandidates(header string) []string {
	pairs := parseCookie(header)
	identifiers := make([]string, 0, 1)
	others := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair.name == sessionCookie {
			if !slices.Contains(identifiers, pair.value) {
				identifiers = append(identifiers, pair.value)
			}
			continue
		}
		others = append(others, pair.name+"="+pair.value)
	}
	if len(identifiers) == 0 {
		return []string{strings.Join(others, "; ")}
	}
	candidates := make([]string, 0, len(identifiers))
	for index := len(identifiers) - 1; index >= 0; index-- {
		parts := append([]string{sessionCookie + "=" + identifiers[index]}, others...)
		candidates = append(candidates, strings.Join(parts, "; "))
	}
	return candidates
}

// parseCookie splits a Cookie header into pairs, dropping empty names.
func parseCookie(header string) []cookie {
	parsed := make([]cookie, 0, 4)
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		parsed = append(parsed, cookie{name: name, value: strings.TrimSpace(value)})
	}
	return parsed
}

func (a *Authenticator) cached(cookie string) (string, outcome, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	found, ok := a.cache[cookie]
	if !ok || time.Now().After(found.expires) {
		delete(a.cache, cookie)
		return "", outcomeUnauthenticated, false
	}
	return found.user, found.result, true
}

func (a *Authenticator) store(cookie, user string, result outcome) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) > 512 {
		a.cache = map[string]entry{}
	}
	a.cache[cookie] = entry{user: user, result: result, expires: time.Now().Add(cacheTTL)}
}

// cgiEnv synthesizes the CGI environment DSM modules expect. cookie is the
// normalized Cookie header of this attempt, which may differ from the one the
// browser sent (see cookieCandidates).
func cgiEnv(request *http.Request, cookie string) []string {
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
	// DSM's nginx proxies the local API over loopback, so the direct peer is
	// the proxy. X-Real-IP carries the browser address it saw.
	if forwarded := strings.TrimSpace(request.Header.Get("X-Real-IP")); forwarded != "" && isLoopback(remoteAddr) {
		remoteAddr = forwarded
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
		"HTTP_COOKIE=" + cookie,
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

// isLoopback reports whether an address belongs to this machine.
func isLoopback(host string) bool {
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func firstLine(value string) string {
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		return value[:index]
	}
	return value
}
