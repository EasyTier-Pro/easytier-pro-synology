package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// defaultDeviceCodeTTL and defaultPollInterval match the OpenWrt client.
const (
	defaultDeviceCodeTTL = 600
	defaultPollInterval  = 5
)

// AuthStatusResult is the answer of GET /api/auth/status.
type AuthStatusResult struct {
	LoggedIn          bool   `json:"logged_in"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	WorkspaceID       string `json:"workspace_id,omitempty"`
	DeviceAuthPending bool   `json:"device_auth_pending"`
}

// DeviceAuthStartResult is the answer of POST /api/auth/device/start.
type DeviceAuthStartResult struct {
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

// DeviceAuthPollResult is the answer of POST /api/auth/device/poll. Exactly
// one of Pending and Authenticated is set.
type DeviceAuthPollResult struct {
	Pending       bool            `json:"pending,omitempty"`
	RetryAfter    int64           `json:"retry_after,omitempty"`
	Authenticated bool            `json:"authenticated,omitempty"`
	Account       json.RawMessage `json:"account,omitempty"`
}

// AuthStatus reports the stored Console session.
func (c *Client) AuthStatus() (AuthStatusResult, *apperr.Error) {
	settings, err := c.store.Settings()
	if err != nil {
		return AuthStatusResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	result := AuthStatusResult{WorkspaceID: settings.ActiveWorkspaceID}
	if session, ok := c.session(); ok {
		result.LoggedIn = true
		result.ExpiresAt = session.ExpiresAt
	}
	if _, err := os.Stat(c.store.Paths().DeviceAuthFile()); err == nil {
		result.DeviceAuthPending = true
	}
	return result, nil
}

// AuthStart begins the device authorization flow.
func (c *Client) AuthStart(ctx context.Context) (DeviceAuthStartResult, *apperr.Error) {
	if err := c.store.Paths().EnsureDirs(); err != nil {
		return DeviceAuthStartResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	form := url.Values{"client_id": {""}, "scope": {"openid profile email"}}
	status, body, aerr := c.publicCall(ctx, http.MethodPost, "/api/v1/auth/device",
		[]byte(form.Encode()), "application/x-www-form-urlencoded")
	if aerr != nil {
		return DeviceAuthStartResult{}, aerr
	}
	if !isSuccess(status) {
		return DeviceAuthStartResult{}, apperr.New(apperr.CodeDeviceAuthFailed)
	}
	var raw struct {
		DeviceCode              string      `json:"device_code"`
		UserCode                string      `json:"user_code"`
		VerificationURI         string      `json:"verification_uri"`
		VerificationURIComplete string      `json:"verification_uri_complete"`
		ExpiresIn               json.Number `json:"expires_in"`
		Interval                json.Number `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return DeviceAuthStartResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	if !config.ValidSecret(raw.DeviceCode) || raw.UserCode == "" || raw.VerificationURI == "" {
		return DeviceAuthStartResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	expiresIn := positiveInt(raw.ExpiresIn, defaultDeviceCodeTTL)
	interval := positiveInt(raw.Interval, defaultPollInterval)
	if interval < defaultPollInterval {
		interval = defaultPollInterval
	}
	state := DeviceAuth{
		DeviceCode: raw.DeviceCode,
		ExpiresAt:  now() + expiresIn,
		Interval:   interval,
		NextPoll:   now(),
	}
	if err := c.saveDeviceAuth(state); err != nil {
		return DeviceAuthStartResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	return DeviceAuthStartResult{
		UserCode:                raw.UserCode,
		VerificationURI:         raw.VerificationURI,
		VerificationURIComplete: raw.VerificationURIComplete,
		ExpiresIn:               expiresIn,
		Interval:                interval,
	}, nil
}

// AuthPoll asks Console whether the user finished signing in.
func (c *Client) AuthPoll(ctx context.Context) (DeviceAuthPollResult, *apperr.Error) {
	state, ok := c.deviceAuth()
	if !ok {
		return DeviceAuthPollResult{}, apperr.New(apperr.CodeNoDeviceAuth)
	}
	if state.ExpiresAt <= 0 || state.Interval <= 0 || state.NextPoll < 0 {
		c.removeDeviceAuth()
		return DeviceAuthPollResult{}, apperr.New(apperr.CodeInvalidAuthState)
	}
	current := now()
	if current >= state.ExpiresAt {
		c.removeDeviceAuth()
		return DeviceAuthPollResult{}, apperr.New(apperr.CodeExpiredToken)
	}
	if current < state.NextPoll {
		return DeviceAuthPollResult{Pending: true, RetryAfter: state.NextPoll - current}, nil
	}
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {state.DeviceCode},
		"client_id":   {""},
	}
	state.NextPoll = current + state.Interval
	_ = c.saveDeviceAuth(state)
	status, body, aerr := c.publicCall(ctx, http.MethodPost, "/api/v1/auth/device/token",
		[]byte(form.Encode()), "application/x-www-form-urlencoded")
	if aerr != nil {
		return DeviceAuthPollResult{}, aerr
	}
	if isSuccess(status) {
		session, aerr := parseTokenResponse(body)
		if aerr != nil {
			return DeviceAuthPollResult{}, aerr
		}
		if !config.ValidSecret(session.AccessToken) {
			return DeviceAuthPollResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
		}
		c.mu.Lock()
		writeErr := c.writeSession(session)
		c.mu.Unlock()
		if writeErr != nil {
			return DeviceAuthPollResult{}, apperr.New(apperr.CodeStateUnavailable)
		}
		c.removeDeviceAuth()
		account, aerr := c.AuthMe(ctx)
		if aerr != nil {
			return DeviceAuthPollResult{}, apperr.New(apperr.CodeAccountLookupFailed)
		}
		return DeviceAuthPollResult{Authenticated: true, Account: account}, nil
	}
	var oauthError struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &oauthError)
	switch oauthError.Error {
	case "", "authorization_pending":
		return DeviceAuthPollResult{Pending: true, RetryAfter: state.Interval}, nil
	case "slow_down":
		state.Interval += 5
		state.NextPoll = current + state.Interval
		_ = c.saveDeviceAuth(state)
		return DeviceAuthPollResult{Pending: true, RetryAfter: state.Interval}, nil
	case "expired_token", "access_denied":
		c.removeDeviceAuth()
		return DeviceAuthPollResult{}, apperr.New(oauthError.Error)
	default:
		return DeviceAuthPollResult{}, apperr.New(apperr.CodeDeviceAuthFailed)
	}
}

// AuthMe returns the signed-in account as Console sent it.
func (c *Client) AuthMe(ctx context.Context) (json.RawMessage, *apperr.Error) {
	status, body, aerr := c.request(ctx, http.MethodGet, "/api/v1/auth/me", nil, "application/json", "")
	if aerr != nil {
		return nil, aerr
	}
	if status != http.StatusOK {
		return nil, apperr.New(apperr.CodeNotAuthenticated)
	}
	var probe any
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	return json.RawMessage(body), nil
}

// Logout clears the stored Console session. The configured connection and any
// running tunnel are left untouched.
func (c *Client) Logout() *apperr.Error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ClearSession()
	return nil
}

func (c *Client) deviceAuth() (DeviceAuth, bool) {
	data, err := os.ReadFile(c.store.Paths().DeviceAuthFile())
	if err != nil {
		return DeviceAuth{}, false
	}
	var state DeviceAuth
	if err := json.Unmarshal(data, &state); err != nil {
		return DeviceAuth{}, false
	}
	return state, true
}

func (c *Client) saveDeviceAuth(state DeviceAuth) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return config.AtomicWrite(c.store.Paths().DeviceAuthFile(), data, 0o600)
}

func (c *Client) removeDeviceAuth() {
	os.Remove(c.store.Paths().DeviceAuthFile())
}

// positiveInt parses a JSON number, falling back to fallback.
func positiveInt(value json.Number, fallback int64) int64 {
	parsed, err := value.Int64()
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
