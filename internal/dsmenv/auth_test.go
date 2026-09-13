package dsmenv

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// newTestAuthenticator points an authenticator at a fake DSM listener and
// reports how many times that listener was called.
func newTestAuthenticator(t *testing.T, body string, status int) (*Authenticator, *http.Request, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("cannot parse the test server address: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("X-DSM-Scheme", parsed.Scheme)
	request.Header.Set("X-DSM-Port", parsed.Port())
	request.Header.Set("Cookie", "id=session")
	return New(nil), request, &calls
}

func TestAuthenticateAcceptsAdministratorSession(t *testing.T) {
	auth, request, _ := newTestAuthenticator(t, `{"data":{"users":[]},"success":true}`, http.StatusOK)
	user, aerr := auth.Authenticate(request.Context(), request)
	if aerr != nil {
		t.Fatalf("Authenticate() rejected an administrator session: %v", aerr)
	}
	if user == "" {
		t.Fatal("Authenticate() returned an empty identity")
	}
}

// A valid session whose account may not call an administrative API must be
// reported as forbidden, not as unauthenticated.
func TestAuthenticateRejectsNonAdministrator(t *testing.T) {
	auth, request, _ := newTestAuthenticator(t, `{"error":{"code":105},"success":false}`, http.StatusOK)
	if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil || aerr.Code != "dsm_auth_forbidden" {
		t.Fatalf("Authenticate() = %v, want dsm_auth_forbidden", aerr)
	}
}

func TestAuthenticateRejectsUnknownSession(t *testing.T) {
	auth, request, _ := newTestAuthenticator(t, `{"error":{"code":119},"success":false}`, http.StatusOK)
	if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil || aerr.Code != "dsm_auth_required" {
		t.Fatalf("Authenticate() = %v, want dsm_auth_required", aerr)
	}
}

// A listener that is not the DSM Web API must not be treated as an authority.
func TestAuthenticateRejectsNonDSMAnswer(t *testing.T) {
	auth, request, _ := newTestAuthenticator(t, "<html>not dsm</html>", http.StatusOK)
	if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil || aerr.Code != "dsm_auth_required" {
		t.Fatalf("Authenticate() = %v, want dsm_auth_required", aerr)
	}
}

func TestAuthenticateRequiresACookie(t *testing.T) {
	auth, request, calls := newTestAuthenticator(t, `{"success":true}`, http.StatusOK)
	request.Header.Del("Cookie")
	if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil || aerr.Code != "dsm_auth_required" {
		t.Fatalf("Authenticate() = %v, want dsm_auth_required", aerr)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("a request without a cookie reached DSM %d times", got)
	}
}

// The answer is cached per session cookie, so a burst of requests does not
// cause one DSM call each.
func TestAuthenticateCachesPerCookie(t *testing.T) {
	auth, request, calls := newTestAuthenticator(t, `{"success":true}`, http.StatusOK)
	for range 3 {
		if _, aerr := auth.Authenticate(request.Context(), request); aerr != nil {
			t.Fatalf("Authenticate() = %v, want success", aerr)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("DSM was asked %d times, want 1", got)
	}
}

func TestEndpointsPrefersTheProxiedAddress(t *testing.T) {
	auth := New(nil)
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("X-DSM-Scheme", "https")
	request.Header.Set("X-DSM-Port", "5001")
	got := auth.endpoints(request)
	if len(got) == 0 || got[0] != "https://127.0.0.1:5001" {
		t.Fatalf("endpoints() = %q, want the reported address first", got)
	}
}

func TestEndpointsIgnoresAMalformedPort(t *testing.T) {
	auth := New(nil)
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("X-DSM-Scheme", "https")
	request.Header.Set("X-DSM-Port", "not-a-port")
	for _, endpoint := range auth.endpoints(request) {
		if endpoint == "https://127.0.0.1:not-a-port" {
			t.Fatalf("endpoints() used a malformed port: %q", endpoint)
		}
	}
}

func TestDevelopmentBypassSkipsDSM(t *testing.T) {
	auth := &Authenticator{bypass: true, cache: map[string]entry{}}
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	user, aerr := auth.Authenticate(request.Context(), request)
	if aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if user != devUser {
		t.Fatalf("Authenticate() = %q, want %q", user, devUser)
	}
}
