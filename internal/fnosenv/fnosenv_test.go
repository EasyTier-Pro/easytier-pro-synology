package fnosenv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
)

// adminRequest builds a gateway request carrying the given identity headers.
func adminRequest(isadmin, username string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	if isadmin != "" {
		r.Header.Set("X-Trim-Isadmin", isadmin)
	}
	if username != "" {
		r.Header.Set("X-Trim-Username", username)
	}
	return r
}

// The gateway did not authenticate the caller at all.
func TestRejectsMissingHeader(t *testing.T) {
	a := New(nil)
	_, aerr := a.Authenticate(t.Context(), adminRequest("", ""))
	if aerr == nil || aerr.Code != apperr.CodeFnOSAuthRequired {
		t.Fatalf("Authenticate() = %v, want code %q", aerr, apperr.CodeFnOSAuthRequired)
	}
}

// The caller is logged in but holds no administrator role.
func TestRejectsNonAdmin(t *testing.T) {
	a := New(nil)
	_, aerr := a.Authenticate(t.Context(), adminRequest("false", "bob"))
	if aerr == nil || aerr.Code != apperr.CodeFnOSAuthForbidden {
		t.Fatalf("Authenticate() = %v, want code %q", aerr, apperr.CodeFnOSAuthForbidden)
	}
}

// A gateway-authenticated administrator is accepted under its own name.
func TestAcceptsAdmin(t *testing.T) {
	a := New(nil)
	user, aerr := a.Authenticate(t.Context(), adminRequest("true", "bob"))
	if aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if user != "bob" {
		t.Fatalf("Authenticate() user = %q, want %q", user, "bob")
	}
}

// The gateway may omit the username header; the identity falls back to the
// administrator role name.
func TestUsernameFallback(t *testing.T) {
	a := New(nil)
	user, aerr := a.Authenticate(t.Context(), adminRequest("true", ""))
	if aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if user != "admin" {
		t.Fatalf("Authenticate() user = %q, want %q", user, "admin")
	}
}

// The identity goes into logs, so control characters are stripped and the
// value is capped at 64 bytes.
func TestUsernameSanitized(t *testing.T) {
	a := New(nil)
	user, aerr := a.Authenticate(t.Context(), adminRequest("true", "bo\x00b\n\x7f"))
	if aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if user != "bob" {
		t.Fatalf("Authenticate() user = %q, want %q", user, "bob")
	}
	long, aerr := a.Authenticate(t.Context(), adminRequest("true", strings.Repeat("x", 100)))
	if aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if len(long) != 64 {
		t.Fatalf("Authenticate() user length = %d, want 64", len(long))
	}
}

// The development bypass is honoured in dev mode with the explicit opt-in.
func TestDevBypass(t *testing.T) {
	t.Setenv("ETP_DEV_ROOT", t.TempDir())
	t.Setenv("ETP_DEV_NO_FNOS_AUTH", "1")
	a := New(nil)
	if !a.Bypassed() {
		t.Fatal("Bypassed() = false, want true in dev mode with the opt-in")
	}
	user, aerr := a.Authenticate(t.Context(), adminRequest("", ""))
	if aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if user != "dev" {
		t.Fatalf("Authenticate() user = %q, want %q", user, "dev")
	}
}

// The opt-in alone must never bypass authentication on a real appliance.
func TestNoBypassOutsideDev(t *testing.T) {
	t.Setenv("ETP_DEV_NO_FNOS_AUTH", "1")
	a := New(nil)
	if a.Bypassed() {
		t.Fatal("Bypassed() = true outside dev mode, want false")
	}
	if _, aerr := a.Authenticate(t.Context(), adminRequest("", "")); aerr == nil {
		t.Fatal("Authenticate() succeeded outside dev mode, want rejection")
	}
}
