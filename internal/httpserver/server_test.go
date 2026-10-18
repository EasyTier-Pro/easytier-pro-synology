package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// A NAS gateway may strip the port used by the browser.
func TestCheckRequestMarkerAcceptsBrowserOriginWithPort(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "10.147.223.115"
	request.Header.Set(requestMarkerHeader, "1")
	request.Header.Set("Origin", "https://10.147.223.115:5001")
	if aerr := checkRequestMarker(request); aerr != nil {
		t.Fatalf("checkRequestMarker() rejected a same-origin request: %v", aerr)
	}
}

func TestCheckRequestMarkerAcceptsProxyHostRewrite(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "10.147.223.115"
	request.Header.Set(requestMarkerHeader, "1")
	request.Header.Set("Origin", "https://nas.example.com:5001")
	if aerr := checkRequestMarker(request); aerr != nil {
		t.Fatalf("checkRequestMarker() rejected a proxy request: %v", aerr)
	}
}

func TestCheckRequestMarkerRequiresMarkerOnWrites(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "nas"
	if aerr := checkRequestMarker(request); aerr == nil {
		t.Fatal("checkRequestMarker() accepted a write without the request marker")
	}
}

func TestCheckRequestMarkerSkipsReads(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	if aerr := checkRequestMarker(request); aerr != nil {
		t.Fatalf("checkRequestMarker() rejected a read: %v", aerr)
	}
}

type fakeAuth struct {
	identity string
	err      *apperr.Error
}

func (f fakeAuth) Authenticate(ctx context.Context, r *http.Request) (string, *apperr.Error) {
	return f.identity, f.err
}
func (f fakeAuth) Bypassed() bool { return false }

// requireSession maps the platform authenticator's required code to 401 and its
// forbidden code to 403, for both the DSM and fnOS code pairs.
func TestRequireSessionStatusMapping(t *testing.T) {
	log, err := config.NewLogger(t.TempDir()+"/daemon.log", false)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	cases := []struct {
		name          string
		requiredCode  string
		forbiddenCode string
		authErr       *apperr.Error
		wantStatus    int
	}{
		{"dsm missing session", apperr.CodeDSMAuthRequired, apperr.CodeDSMAuthForbidden, apperr.New(apperr.CodeDSMAuthRequired), http.StatusUnauthorized},
		{"dsm non-admin", apperr.CodeDSMAuthRequired, apperr.CodeDSMAuthForbidden, apperr.New(apperr.CodeDSMAuthForbidden), http.StatusForbidden},
		{"fnos missing session", apperr.CodeFnOSAuthRequired, apperr.CodeFnOSAuthForbidden, apperr.New(apperr.CodeFnOSAuthRequired), http.StatusUnauthorized},
		{"fnos non-admin", apperr.CodeFnOSAuthRequired, apperr.CodeFnOSAuthForbidden, apperr.New(apperr.CodeFnOSAuthForbidden), http.StatusForbidden},
		{"ok", apperr.CodeFnOSAuthRequired, apperr.CodeFnOSAuthForbidden, nil, http.StatusTeapot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				auth:              fakeAuth{identity: "u", err: tc.authErr},
				log:               log,
				authRequiredCode:  tc.requiredCode,
				authForbiddenCode: tc.forbiddenCode,
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			s.requireSession(next).ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("requireSession status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRequireSessionProtectsProxiedWrites(t *testing.T) {
	log, err := config.NewLogger(t.TempDir()+"/daemon.log", false)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	for _, tc := range []struct {
		name       string
		marker     string
		authErr    *apperr.Error
		wantStatus int
	}{
		{"administrator", "1", nil, http.StatusNoContent},
		{"cross-site form", "", nil, http.StatusForbidden},
		{"invalid marker", "0", nil, http.StatusForbidden},
		{"non-admin", "1", apperr.New(apperr.CodeFnOSAuthForbidden), http.StatusForbidden},
		{"missing session", "1", apperr.New(apperr.CodeFnOSAuthRequired), http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				auth:              fakeAuth{identity: "admin", err: tc.authErr},
				log:               log,
				authForbiddenCode: apperr.CodeFnOSAuthForbidden,
			}
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, "http://nas/api/disconnect", nil)
			req.Header.Set("Origin", "https://nas.example.com")
			req.Header.Set(requestMarkerHeader, tc.marker)
			rec := httptest.NewRecorder()
			s.requireSession(next).ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus || called != (tc.wantStatus == http.StatusNoContent) {
				t.Fatalf("status = %d, called = %v, want status %d", rec.Code, called, tc.wantStatus)
			}
		})
	}
}

func TestHandlerDoesNotAuthorizeCrossOriginPreflight(t *testing.T) {
	log, err := config.NewLogger(t.TempDir()+"/daemon.log", false)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	s := &Server{auth: fakeAuth{identity: "admin"}, log: log}
	req := httptest.NewRequest(http.MethodOptions, "/api/disconnect", nil)
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", requestMarkerHeader)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Headers", "Access-Control-Allow-Credentials"} {
		if value := rec.Header().Get(header); value != "" {
			t.Errorf("preflight authorizes cross-origin access: %s = %q", header, value)
		}
	}
}
