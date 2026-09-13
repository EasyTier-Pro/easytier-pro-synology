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
	log    *config.Logger

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
func New(log *config.Logger) *Authenticator {
	bypass := config.DevMode() && os.Getenv("ETP_DEV_NO_DSM_AUTH") == "1"
	return &Authenticator{bypass: bypass, log: log, cache: map[string]entry{}}
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
	user, result := a.sessionUser(ctx, request)
	a.store(cookie, user, result)
	switch result {
	case outcomeAuthenticated:
		return user, nil
	case outcomeForbidden:
		return "", apperr.New(apperr.CodeDSMAuthForbidden)
	default:
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
}

// maxSessionCookieValues and maxSessionAddresses bound the work of one session
// check. Both inputs come from the request and every combination runs
// authenticate.cgi as a child process, so an unauthenticated caller could
// otherwise force an unbounded number of spawns per request.
const (
	maxSessionCookieValues = 3
	maxSessionAddresses    = 3
)

// sessionUser resolves the request to a user. It reports the outcome of the
// best candidate found: an administrator if any candidate is one, otherwise a
// forbidden non-administrator, otherwise nothing.
//
// Two things have to be synthesized exactly: the Cookie header (see
// cookieCandidates) and the client address. authenticate.cgi is backed by
// SynoCgiIsAuthorized, which only accepts a session whose recorded login
// address matches REMOTE_ADDR, so the address nginx saw for the browser is not
// always the one the session was created with. Every plausible pair is tried in
// order.
//
// A candidate that authenticates as a non-administrator does not end the
// search: a browser can carry a non-administrator session beside an
// administrator one, and stopping at the first match would then report
// "forbidden" to the administrator.
func (a *Authenticator) sessionUser(ctx context.Context, request *http.Request) (string, outcome) {
	cookies := cookieCandidates(request.Header.Get("Cookie"))
	addresses := remoteAddrCandidates(request)
	if len(cookies) > maxSessionCookieValues {
		cookies = cookies[:maxSessionCookieValues]
	}
	if len(addresses) > maxSessionAddresses {
		addresses = addresses[:maxSessionAddresses]
	}
	nonAdmin := ""
	for index, cookie := range cookies {
		for addressIndex, address := range addresses {
			output, err := runCGI(ctx, authenticateCGI, cgiEnv(request, cookie, address))
			if err != nil {
				continue
			}
			user := strings.TrimSpace(firstLine(output))
			if user == "" {
				continue
			}
			if index > 0 || addressIndex > 0 {
				// Not the first guess: record it, since it reveals which
				// combination this deployment actually needs.
				a.log.Printf("DSM 会话校验使用了备用组合（Cookie 序号 %d，客户端地址 %s）", index, address)
			}
			if isAdministrator(ctx, user) {
				return user, outcomeAuthenticated
			}
			a.log.Printf("%s 不属于 administrators 组，继续尝试其它会话", user)
			nonAdmin = user
		}
	}
	if nonAdmin != "" {
		// A session resolved, it just is not an administrator.
		return nonAdmin, outcomeForbidden
	}
	return "", outcomeUnauthenticated
}

// remoteAddrCandidates lists the client addresses a DSM session may have been
// created with, most likely first.
func remoteAddrCandidates(request *http.Request) []string {
	direct := hostOf(request.RemoteAddr)
	forwarded := strings.TrimSpace(request.Header.Get("X-Real-IP"))
	local := ""
	if localAddr, ok := request.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && localAddr != nil {
		local = hostOf(localAddr.String())
	}
	candidates := make([]string, 0, 4)
	for _, address := range []string{forwarded, direct, local, "127.0.0.1"} {
		if address != "" && !slices.Contains(candidates, address) {
			candidates = append(candidates, address)
		}
	}
	return candidates
}

// hostOf strips the port from a host:port value, passing other values through.
func hostOf(value string) string {
	if name, _, err := net.SplitHostPort(value); err == nil {
		return name
	}
	return value
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
// normalized Cookie header of this attempt (see cookieCandidates) and
// remoteAddr the client address it is checked against (see
// remoteAddrCandidates).
func cgiEnv(request *http.Request, cookie, remoteAddr string) []string {
	host := request.Host
	serverName := host
	serverPort := "443"
	if name, port, err := net.SplitHostPort(host); err == nil {
		serverName = name
		serverPort = port
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

func firstLine(value string) string {
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		return value[:index]
	}
	return value
}
