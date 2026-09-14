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

// DSM rejects a valid session that arrives without its token, so the token has
// to reach the probe.
func TestAuthenticateForwardsTheSessionToken(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Syno-Token")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)
	parsed, _ := url.Parse(server.URL)
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("X-DSM-Scheme", parsed.Scheme)
	request.Header.Set("X-DSM-Port", parsed.Port())
	request.Header.Set("Cookie", "id=session")
	request.Header.Set("X-Syno-Token", "token-value")
	auth := New(nil)
	if _, aerr := auth.Authenticate(request.Context(), request); aerr != nil {
		t.Fatalf("Authenticate() = %v, want success", aerr)
	}
	if seen != "token-value" {
		t.Fatalf("the probe sent X-Syno-Token %q, want %q", seen, "token-value")
	}
}

// The token is part of the credential, so it must take part in the cache key.
func TestAuthenticateDoesNotShareCacheAcrossTokens(t *testing.T) {
	auth, request, calls := newTestAuthenticator(t, `{"error":{"code":119},"success":false}`, http.StatusOK)
	if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil {
		t.Fatal("Authenticate() = nil, want a rejection")
	}
	request.Header.Set("X-Syno-Token", "another-token")
	_, _ = auth.Authenticate(request.Context(), request)
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("DSM was asked %d times, want 2 (one per token)", got)
	}
}

// DSM binds browser sessions to their source IP. A loopback probe must retain
// the address supplied by our nginx location, not a caller's forwarded chain.
func TestAuthenticatePreservesClientIP(t *testing.T) {
	for _, clientIP := range []string{"192.0.2.10", "2001:db8::10"} {
		t.Run(clientIP, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Forwarded-For") != clientIP {
					_, _ = w.Write([]byte(`{"success":false,"error":{"code":150}}`))
					return
				}
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			t.Cleanup(server.Close)
			auth := New(nil)
			auth.endpoint = server.URL
			request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			request.Header.Set("Cookie", "id=session")
			request.Header.Set("X-Real-IP", clientIP)
			request.Header.Set("X-Forwarded-For", "198.51.100.1, 198.51.100.2")
			if _, aerr := auth.Authenticate(request.Context(), request); aerr != nil {
				t.Fatalf("Authenticate() lost the browser's source IP: %v", aerr)
			}
		})
	}
}

func TestAuthenticateDoesNotShareCacheAcrossClientIPs(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("X-Forwarded-For") != "192.0.2.10" {
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":150}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)
	auth := New(nil)
	auth.endpoint = server.URL
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("Cookie", "id=session")
	request.Header.Set("X-Syno-Token", "token")
	request.Header.Set("X-Real-IP", "192.0.2.10")
	for range 2 {
		if _, aerr := auth.Authenticate(request.Context(), request); aerr != nil {
			t.Fatalf("Authenticate() = %v, want success", aerr)
		}
	}
	request.Header.Set("X-Real-IP", "192.0.2.11")
	if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil || aerr.Code != "dsm_auth_required" {
		t.Fatalf("Authenticate() = %v, want dsm_auth_required for another IP", aerr)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("DSM was asked %d times, want 2 (one per source IP)", got)
	}
}

func TestAuthenticateRejectsMalformedClientIP(t *testing.T) {
	for _, clientIP := range []string{"not-an-ip", "192.0.2.10, 192.0.2.11", "192.0.2.10:5000"} {
		t.Run(clientIP, func(t *testing.T) {
			auth, request, calls := newTestAuthenticator(t, `{"success":true}`, http.StatusOK)
			request.Header.Set("X-Real-IP", clientIP)
			if _, aerr := auth.Authenticate(request.Context(), request); aerr == nil || aerr.Code != "dsm_auth_required" {
				t.Fatalf("Authenticate() = %v, want dsm_auth_required", aerr)
			}
			if got := atomic.LoadInt32(calls); got != 0 {
				t.Fatalf("a malformed client IP reached DSM %d times", got)
			}
		})
	}
}
