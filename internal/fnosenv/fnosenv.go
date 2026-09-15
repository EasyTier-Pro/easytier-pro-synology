// Package fnosenv integrates with the fnOS gateway identity headers.
//
// The fnOS gateway terminates the user session and injects X-Trim-Isadmin /
// X-Trim-Username before proxying to the app's Unix socket, so the daemon only
// ever sees requests the gateway already authenticated.
package fnosenv

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

const (
	isAdminHeader  = "X-Trim-Isadmin"
	usernameHeader = "X-Trim-Username"

	// maxUsernameBytes caps the identity so a hostile gateway cannot fill the
	// log with an arbitrarily long value.
	maxUsernameBytes = 64

	devUser = "dev"

	// fallbackUser is reported when the gateway authenticates an administrator
	// but does not forward the account name.
	fallbackUser = "admin"
)

// Authenticator trusts the identity headers injected by the fnOS gateway.
type Authenticator struct {
	bypass bool
	log    *config.Logger
}

// New enables the dev bypass only in dev mode with an explicit opt-in.
func New(log *config.Logger) *Authenticator {
	return &Authenticator{
		bypass: config.DevMode() && os.Getenv("ETP_DEV_NO_FNOS_AUTH") == "1",
		log:    log,
	}
}

// Bypassed reports whether gateway identity checks are disabled for
// development.
func (a *Authenticator) Bypassed() bool { return a.bypass }

// Authenticate reports the administrator the fnOS gateway identified, or why
// the request may not use the API.
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (string, *apperr.Error) {
	if a.bypass {
		return devUser, nil
	}
	switch r.Header.Get(isAdminHeader) {
	case "true":
		name := sanitizeUsername(r.Header.Get(usernameHeader))
		if name == "" {
			name = fallbackUser
		}
		return name, nil
	case "":
		return "", apperr.New(apperr.CodeFnOSAuthRequired)
	default:
		return "", apperr.New(apperr.CodeFnOSAuthForbidden)
	}
}

// sanitizeUsername strips control characters and caps the identity at
// maxUsernameBytes: the value ends up in log lines.
func sanitizeUsername(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > maxUsernameBytes {
		s = s[:maxUsernameBytes]
	}
	return s
}
