// Package console implements the EasyTier Console client used by this device:
// device authorization, session refresh, enrollment keys, networks and nodes.
//
// It is a direct port of the OpenWrt client's console.sh so that both clients
// observe the same Console contract.
package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// maxResponseBytes bounds every Console response the daemon buffers.
const maxResponseBytes = 4 << 20

// Client talks to EasyTier Console on behalf of this device.
type Client struct {
	store *config.Store
	http  *http.Client
	log   *config.Logger

	// mu serializes session refresh and logout, mirroring the file lock the
	// OpenWrt client uses across processes.
	mu sync.Mutex
}

// New returns a Console client bound to the given state store.
func New(store *config.Store, log *config.Logger) *Client {
	return &Client{
		store: store,
		log:   log,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConnsPerHost:   4,
				ResponseHeaderTimeout: 20 * time.Second,
			},
		},
	}
}

// consoleURL resolves the configured Console base URL.
func (c *Client) consoleURL() (string, *apperr.Error) {
	settings, err := c.store.Settings()
	if err != nil {
		return "", apperr.New(apperr.CodeStateUnavailable)
	}
	base := settings.ConsoleURL
	if base == "" {
		base = config.DefaultConsoleURL
	}
	if !config.ValidConsoleURL(base, settings.AllowInsecureConsole) {
		return "", apperr.New(apperr.CodeInvalidConsoleURL)
	}
	return config.NormalizeConsoleURL(base), nil
}

// Session returns the stored session, if any.
func (c *Client) session() (Session, bool) {
	data, err := os.ReadFile(c.store.Paths().SessionFile())
	if err != nil {
		return Session{}, false
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, false
	}
	return session, true
}

// LoggedIn reports whether a Console session is stored.
func (c *Client) LoggedIn() bool {
	_, ok := c.session()
	return ok
}

func (c *Client) writeSession(session Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return config.AtomicWrite(c.store.Paths().SessionFile(), data, 0o600)
}

// ClearSession removes the Console session and any pending device login.
func (c *Client) ClearSession() {
	paths := c.store.Paths()
	os.Remove(paths.SessionFile())
	os.Remove(paths.DeviceAuthFile())
}

func now() int64 { return time.Now().Unix() }

// SessionReady ensures the access token is present and fresh enough, exactly
// like the OpenWrt client: tokens within 60 seconds of expiry are refreshed.
func (c *Client) SessionReady(ctx context.Context) *apperr.Error {
	session, ok := c.session()
	if !ok {
		return apperr.New(apperr.CodeNotAuthenticated)
	}
	if session.ExpiresAt == 0 {
		return nil
	}
	if session.ExpiresAt-now() > 60 {
		return nil
	}
	return c.RefreshSession(ctx, false)
}

// RefreshSession exchanges the refresh token for a new access token. When
// force is false the refresh is skipped if the token is still fresh; a refresh
// another caller already performed is detected by comparing refresh tokens.
func (c *Client) RefreshSession(ctx context.Context, force bool) *apperr.Error {
	observed, _ := c.session()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.session()
	if ok && current.RefreshToken != "" && observed.RefreshToken != "" &&
		current.RefreshToken != observed.RefreshToken {
		return nil
	}
	if !force && ok && current.ExpiresAt != 0 && current.ExpiresAt-now() > 60 {
		return nil
	}
	return c.refreshLocked(ctx)
}

func (c *Client) refreshLocked(ctx context.Context) *apperr.Error {
	session, ok := c.session()
	if !ok || !config.ValidSecret(session.RefreshToken) {
		c.ClearSession()
		return apperr.New(apperr.CodeNotAuthenticated)
	}
	form := url.Values{"refresh_token": {session.RefreshToken}}
	status, body, aerr := c.publicCall(ctx, http.MethodPost, "/api/v1/auth/device/refresh",
		[]byte(form.Encode()), "application/x-www-form-urlencoded")
	if aerr != nil {
		return aerr
	}
	switch {
	case status >= 200 && status < 300:
	case status == http.StatusBadRequest, status == http.StatusUnauthorized:
		var oauthError struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &oauthError)
		if oauthError.Error == "invalid_grant" {
			c.ClearSession()
		}
		return apperr.New(apperr.CodeNotAuthenticated)
	default:
		return apperr.New(apperr.CodeConsoleUnreachable)
	}
	token, aerr := parseTokenResponse(body)
	if aerr != nil {
		return aerr
	}
	if !config.ValidSecret(token.AccessToken) {
		return apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	if token.RefreshToken == "" {
		token.RefreshToken = session.RefreshToken
	}
	if err := c.writeSession(token); err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	return nil
}

func parseTokenResponse(body []byte) (Session, *apperr.Error) {
	var raw struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		IDToken      string      `json:"id_token"`
		TokenType    string      `json:"token_type"`
		Scope        string      `json:"scope"`
		ExpiresIn    json.Number `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Session{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	expiresIn := int64(0)
	if value, err := raw.ExpiresIn.Int64(); err == nil && value > 0 {
		expiresIn = value
	}
	obtainedAt := now()
	return Session{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		IDToken:      raw.IDToken,
		TokenType:    raw.TokenType,
		Scope:        raw.Scope,
		ExpiresIn:    expiresIn,
		ObtainedAt:   obtainedAt,
		ExpiresAt:    obtainedAt + expiresIn,
	}, nil
}

// request performs an authenticated Console request and replays it once after
// a session refresh when Console answers 401.
func (c *Client) request(ctx context.Context, method, path string, body []byte, contentType, idempotencyKey string) (int, []byte, *apperr.Error) {
	if aerr := c.SessionReady(ctx); aerr != nil {
		return 0, nil, aerr
	}
	status, payload, aerr := c.authenticatedCall(ctx, method, path, body, contentType, idempotencyKey)
	if aerr != nil {
		return 0, nil, aerr
	}
	if status == http.StatusUnauthorized {
		if aerr := c.RefreshSession(ctx, true); aerr != nil {
			return 0, nil, aerr
		}
		status, payload, aerr = c.authenticatedCall(ctx, method, path, body, contentType, idempotencyKey)
		if aerr != nil {
			return 0, nil, aerr
		}
	}
	return status, payload, nil
}

func (c *Client) authenticatedCall(ctx context.Context, method, path string, body []byte, contentType, idempotencyKey string) (int, []byte, *apperr.Error) {
	session, ok := c.session()
	if !ok || !config.ValidSecret(session.AccessToken) {
		return 0, nil, apperr.New(apperr.CodeNotAuthenticated)
	}
	return c.call(ctx, method, path, session.AccessToken, body, contentType, idempotencyKey)
}

func (c *Client) publicCall(ctx context.Context, method, path string, body []byte, contentType string) (int, []byte, *apperr.Error) {
	return c.call(ctx, method, path, "", body, contentType, "")
}

func (c *Client) call(ctx context.Context, method, path, accessToken string, body []byte, contentType, idempotencyKey string) (int, []byte, *apperr.Error) {
	base, aerr := c.consoleURL()
	if aerr != nil {
		return 0, nil, aerr
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return 0, nil, apperr.New(apperr.CodeInvalidRequest)
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	client := *c.http
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return checkRedirect(next, via[0])
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, apperr.New(apperr.CodeConsoleUnreachable)
		}
		return 0, nil, apperr.New(apperr.CodeConsoleUnreachable)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, apperr.New(apperr.CodeConsoleUnreachable)
	}
	return response.StatusCode, payload, nil
}

// requireStatus converts a non-success Console status into an API error using
// the caller's failure code.
func requireStatus(status int, failureCode string) *apperr.Error {
	if isSuccess(status) {
		return nil
	}
	return apperr.New(failureCode)
}

// isSuccess reports whether a Console status code means success.
func isSuccess(status int) bool { return status >= 200 && status < 300 }

// decodeJSON unmarshals a Console payload into target.
func decodeJSON(payload []byte, target any) *apperr.Error {
	if err := json.Unmarshal(payload, target); err != nil {
		return apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	return nil
}

// workspaceID returns the active workspace, which every network call needs.
func (c *Client) workspaceID() (string, *apperr.Error) {
	settings, err := c.store.Settings()
	if err != nil {
		return "", apperr.New(apperr.CodeStateUnavailable)
	}
	if !config.ValidUUID(settings.ActiveWorkspaceID) {
		return "", apperr.New(apperr.CodeNoWorkspace)
	}
	return settings.ActiveWorkspaceID, nil
}

func tenantPath(workspaceID, suffix string) string {
	return fmt.Sprintf("/api/v1/tenants/%s%s", workspaceID, suffix)
}

// checkRedirect keeps a Console request on the scheme it was validated with:
// an HTTPS endpoint never downgrades, and authorization is only replayed to
// the same origin.
func checkRedirect(next *http.Request, original *http.Request) error {
	if original.URL.Scheme == "https" && next.URL.Scheme != "https" {
		return errors.New("refusing to follow a redirect to an insecure scheme")
	}
	if next.URL.Scheme != "https" && next.URL.Scheme != "http" {
		return errors.New("refusing to follow a redirect to an unsupported scheme")
	}
	if next.URL.Host != original.URL.Host && next.Header.Get("Authorization") != "" {
		return errors.New("refusing to send credentials to another host")
	}
	return nil
}

// errNoEnrollmentKey marks a missing or unusable enrollment key.
var errNoEnrollmentKey = errors.New("no usable enrollment key")
