// Package dsmenv validates that a request comes from an authenticated DSM
// administrator session.
//
// The check is delegated to DSM: the daemon asks the appliance's own Web API,
// over loopback and carrying the caller's cookies, whether the session may call
// an administrative API. DSM owns the session store and the privilege rules, so
// re-implementing either here would only be able to approximate them.
//
// The earlier implementation ran the authenticate.cgi module directly with a
// synthesized CGI environment. Measured on DSM 7.2 that rejects a real browser
// session even when every environment field matches, so it was abandoned; a
// session created by a normal login only resolves inside DSM's own request
// pipeline.
package dsmenv

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// adminAPI is a DSM API that only an administrator may call, used as the
// authorization oracle: DSM answers it successfully for an administrator and
// with an error for everyone else.
const adminAPI = "/webapi/entry.cgi?api=SYNO.Core.User&version=1&method=list"

const (
	cacheTTL       = 30 * time.Second
	requestTimeout = 10 * time.Second
	maxBody        = 64 << 10
	devUser        = "dev"

	// authenticatedUser is reported for a session that passed every check.
	// DSM's administrative APIs do not disclose which account owns a session,
	// so the identity itself stays with DSM.
	authenticatedUser = "administrator"
)

// DSM error codes that decide the outcome of the probe.
const (
	// dsmCodeNoPermission is returned when the session is valid but the account
	// may not call an administrative API.
	dsmCodeNoPermission = 105
)

// defaultEndpoints are tried, in order, when the reverse proxy did not report
// the address it was reached on.
var defaultEndpoints = []string{"https://127.0.0.1:5001", "https://127.0.0.1:443"}

// Authenticator checks DSM sessions and caches the result per cookie.
type Authenticator struct {
	bypass bool
	log    *config.Logger

	mu       sync.Mutex
	cache    map[string]entry
	endpoint string

	// client talks to loopback only. The DSM certificate is self-signed, and
	// the connection never leaves the appliance, so verification is disabled
	// deliberately.
	client *http.Client
}

type outcome int

const (
	outcomeUnauthenticated outcome = iota
	outcomeAuthenticated
	outcomeForbidden
)

type entry struct {
	result  outcome
	expires time.Time
}

// New builds an authenticator. The development bypass is only honoured when the
// daemon runs outside DSM.
func New(log *config.Logger) *Authenticator {
	bypass := config.DevMode() && os.Getenv("ETP_DEV_NO_DSM_AUTH") == "1"
	return &Authenticator{
		bypass: bypass,
		log:    log,
		cache:  map[string]entry{},
		client: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
			// A DSM listener answers in place; a redirect would mean something
			// else is answering on this port.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// logf and logErrorf keep the logger optional, so the package stays usable
// without one (tests, tools).
func (a *Authenticator) logf(format string, args ...any) {
	if a.log != nil {
		a.log.Printf(format, args...)
	}
}

func (a *Authenticator) logErrorf(format string, args ...any) {
	if a.log != nil {
		a.log.Errorf(format, args...)
	}
}

// Bypassed reports whether DSM session checks are disabled for development.
func (a *Authenticator) Bypassed() bool { return a.bypass }

// Authenticate reports the DSM administrator behind the request.
func (a *Authenticator) Authenticate(ctx context.Context, request *http.Request) (string, *apperr.Error) {
	if a.bypass {
		return devUser, nil
	}
	cookie := strings.TrimSpace(request.Header.Get("Cookie"))
	if cookie == "" {
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
	if result, cached := a.cached(cookie); cached {
		return a.result(result)
	}
	result := a.probe(ctx, request, cookie)
	a.store(cookie, result)
	return a.result(result)
}

func (a *Authenticator) result(result outcome) (string, *apperr.Error) {
	switch result {
	case outcomeAuthenticated:
		return authenticatedUser, nil
	case outcomeForbidden:
		return "", apperr.New(apperr.CodeDSMAuthForbidden)
	default:
		return "", apperr.New(apperr.CodeDSMAuthRequired)
	}
}

// probe asks DSM whether the session may call an administrative API.
func (a *Authenticator) probe(ctx context.Context, request *http.Request, cookie string) outcome {
	for _, endpoint := range a.endpoints(request) {
		result, err := a.ask(ctx, endpoint, cookie)
		if err != nil {
			// Nothing that speaks the DSM Web API is listening here.
			continue
		}
		a.remember(endpoint)
		if result.forbidden {
			a.logf("DSM 会话已通过校验，但该账号不是管理员")
			return outcomeForbidden
		}
		if !result.success {
			a.logf("DSM 会话校验未通过（错误码 %d）", result.code)
			return outcomeUnauthenticated
		}
		return outcomeAuthenticated
	}
	a.logErrorf("无法连接 DSM 本机 Web API，无法校验登录会话")
	return outcomeUnauthenticated
}

// endpoints lists the loopback addresses to try, most likely first. The reverse
// proxy reports the address the browser actually reached, which is the one DSM
// itself is listening on.
func (a *Authenticator) endpoints(request *http.Request) []string {
	a.mu.Lock()
	cached := a.endpoint
	a.mu.Unlock()
	if cached != "" {
		return []string{cached}
	}
	candidates := make([]string, 0, len(defaultEndpoints)+1)
	if scheme := strings.TrimSpace(request.Header.Get("X-DSM-Scheme")); scheme != "" {
		if port := strings.TrimSpace(request.Header.Get("X-DSM-Port")); port != "" {
			if _, err := strconv.Atoi(port); err == nil {
				candidates = append(candidates, fmt.Sprintf("%s://127.0.0.1:%s", scheme, port))
			}
		}
	}
	return append(candidates, defaultEndpoints...)
}

func (a *Authenticator) remember(endpoint string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.endpoint = endpoint
}

// probeResult is the part of a DSM answer that decides the outcome.
type probeResult struct {
	success   bool
	forbidden bool
	code      int
}

// call performs one probe. A non-nil error means the endpoint could not be
// reached or did not answer with a DSM envelope, so another one may be tried.
func (a *Authenticator) ask(ctx context.Context, endpoint, cookie string) (probeResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+adminAPI, nil)
	if err != nil {
		return probeResult{}, err
	}
	request.Header.Set("Cookie", cookie)
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return probeResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody))
	if err != nil {
		return probeResult{}, err
	}
	if response.StatusCode != http.StatusOK {
		return probeResult{}, fmt.Errorf("dsm api answered %d", response.StatusCode)
	}
	var payload struct {
		Success bool `json:"success"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return probeResult{}, fmt.Errorf("dsm api answered a non-JSON body: %w", err)
	}
	result := probeResult{success: payload.Success}
	if payload.Error != nil {
		result.code = payload.Error.Code
		result.forbidden = payload.Error.Code == dsmCodeNoPermission
	}
	return result, nil
}

func (a *Authenticator) cached(cookie string) (outcome, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	found, ok := a.cache[cookie]
	if !ok || time.Now().After(found.expires) {
		delete(a.cache, cookie)
		return outcomeUnauthenticated, false
	}
	return found.result, true
}

func (a *Authenticator) store(cookie string, result outcome) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) > 512 {
		a.cache = map[string]entry{}
	}
	a.cache[cookie] = entry{result: result, expires: time.Now().Add(cacheTTL)}
}
