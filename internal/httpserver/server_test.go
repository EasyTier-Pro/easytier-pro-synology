package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// nginx forwards "Host $host" (no port) while a browser sends an Origin that
// includes the port, so the comparison has to ignore the port.
func TestCheckSameOriginAcceptsBrowserOriginWithPort(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "10.147.223.115"
	request.Header.Set(requestMarkerHeader, "1")
	request.Header.Set("Origin", "https://10.147.223.115:5001")
	if aerr := checkSameOrigin(request); aerr != nil {
		t.Fatalf("checkSameOrigin() rejected a same-origin request: %v", aerr)
	}
}

func TestCheckSameOriginRejectsForeignOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "10.147.223.115"
	request.Header.Set(requestMarkerHeader, "1")
	request.Header.Set("Origin", "https://evil.example:5001")
	if aerr := checkSameOrigin(request); aerr == nil {
		t.Fatal("checkSameOrigin() accepted a foreign origin")
	}
}

func TestCheckSameOriginRequiresMarkerOnWrites(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	request.Host = "nas"
	if aerr := checkSameOrigin(request); aerr == nil {
		t.Fatal("checkSameOrigin() accepted a write without the request marker")
	}
}

func TestCheckSameOriginSkipsReads(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	if aerr := checkSameOrigin(request); aerr != nil {
		t.Fatalf("checkSameOrigin() rejected a read: %v", aerr)
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
