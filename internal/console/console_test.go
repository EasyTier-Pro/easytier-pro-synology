package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *config.Store) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	paths := config.Paths{
		PkgDest: filepath.Join(t.TempDir(), "target"),
		PkgVar:  filepath.Join(t.TempDir(), "var"),
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	store := config.NewStore(paths)
	settings := config.DefaultSettings()
	settings.ConsoleURL = server.URL
	settings.AllowInsecureConsole = true
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	logger, err := config.NewLogger(paths.DaemonLogFile(), false)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(logger.Close)
	return New(store, logger), store
}

func writeSession(t *testing.T, store *config.Store, session Session) {
	t.Helper()
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := config.AtomicWrite(store.Paths().SessionFile(), data, 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
}

func TestRequestReplaysOnceAfterUnauthorized(t *testing.T) {
	var mu sync.Mutex
	meCalls := 0
	refreshes := 0
	var tokens []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/auth/me":
			meCalls++
			tokens = append(tokens, r.Header.Get("Authorization"))
			if meCalls == 1 {
				http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"user":{"id":"u1"},"tenants":[{"id":"ws-1"}]}`))
		case "/api/v1/auth/device/refresh":
			refreshes++
			if err := r.ParseForm(); err != nil {
				t.Errorf("refresh body: %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "refresh-old" {
				t.Errorf("refresh_token = %q, want refresh-old", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","token_type":"Bearer","expires_in":3600}`))
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	})

	client, store := newTestClient(t, handler)
	writeSession(t, store, Session{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Unix() + 3600,
	})

	account, aerr := client.AuthMe(context.Background())
	if aerr != nil {
		t.Fatalf("AuthMe: %v", aerr)
	}
	if len(account) == 0 {
		t.Fatal("empty account payload")
	}
	if meCalls != 2 {
		t.Fatalf("me calls = %d, want 2", meCalls)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
	if tokens[0] != "Bearer access-old" || tokens[1] != "Bearer access-new" {
		t.Fatalf("authorization headers = %v", tokens)
	}
	session, ok := client.session()
	if !ok {
		t.Fatal("session disappeared")
	}
	if session.AccessToken != "access-new" || session.RefreshToken != "refresh-new" {
		t.Fatalf("stored session = %+v", session)
	}
	if session.ExpiresAt <= time.Now().Unix()+3500 {
		t.Fatalf("expiry not refreshed: %d", session.ExpiresAt)
	}
}

func TestRefreshKeepsSessionOnServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	client, store := newTestClient(t, handler)
	writeSession(t, store, Session{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Unix() + 3600})

	if aerr := client.RefreshSession(context.Background(), true); aerr == nil {
		t.Fatal("expected refresh failure")
	}
	if _, err := os.Stat(store.Paths().SessionFile()); err != nil {
		t.Fatalf("session removed although the failure was not invalid_grant: %v", err)
	}
}

func TestRefreshClearsSessionOnlyOnInvalidGrant(t *testing.T) {
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	client, store := newTestClient(t, handler)
	writeSession(t, store, Session{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Unix() + 3600})

	aerr := client.RefreshSession(context.Background(), true)
	if aerr == nil || aerr.Code != apperr.CodeNotAuthenticated {
		t.Fatalf("error = %v, want not_authenticated", aerr)
	}
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
	}
	if _, err := os.Stat(store.Paths().SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("session was not cleared: %v", err)
	}
}

func TestAuthPollSlowDownIncreasesInterval(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"slow_down"}`))
	})
	client, _ := newTestClient(t, handler)
	state := DeviceAuth{DeviceCode: "device-code", ExpiresAt: time.Now().Unix() + 600, Interval: 5, NextPoll: 0}
	if err := client.saveDeviceAuth(state); err != nil {
		t.Fatalf("saveDeviceAuth: %v", err)
	}

	result, aerr := client.AuthPoll(context.Background())
	if aerr != nil {
		t.Fatalf("AuthPoll: %v", aerr)
	}
	if !result.Pending || result.RetryAfter != 10 {
		t.Fatalf("result = %+v, want pending with retry_after 10", result)
	}
	stored, ok := client.deviceAuth()
	if !ok {
		t.Fatal("device auth state disappeared")
	}
	if stored.Interval != 10 {
		t.Fatalf("stored interval = %d, want 10", stored.Interval)
	}
}

func TestAuthPollHonoursPendingInterval(t *testing.T) {
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	})
	client, _ := newTestClient(t, handler)
	state := DeviceAuth{DeviceCode: "device-code", ExpiresAt: time.Now().Unix() + 600, Interval: 5, NextPoll: time.Now().Unix() + 3}
	if err := client.saveDeviceAuth(state); err != nil {
		t.Fatalf("saveDeviceAuth: %v", err)
	}
	result, aerr := client.AuthPoll(context.Background())
	if aerr != nil {
		t.Fatalf("AuthPoll: %v", aerr)
	}
	if !result.Pending || result.RetryAfter <= 0 || result.RetryAfter > 3 {
		t.Fatalf("result = %+v, want pending with retry_after in (0,3]", result)
	}
	if requests != 0 {
		t.Fatalf("Console was polled %d times before retry_after elapsed", requests)
	}
}

func TestEnrollmentKeyUsability(t *testing.T) {
	expired := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	const id = "11111111-2222-3333-4444-555555555555"

	cases := []struct {
		name  string
		key   EnrollmentKey
		reuse bool
		want  bool
	}{
		{"plain key", EnrollmentKey{ID: id}, false, true},
		{"plain key for reuse", EnrollmentKey{ID: id}, true, false},
		{"reusable key", EnrollmentKey{ID: id, Reusable: true}, true, true},
		{"revoked key", EnrollmentKey{ID: id, Reusable: true, Revoked: true}, true, false},
		{"deleted lifecycle", EnrollmentKey{ID: id, Reusable: true, LifecycleState: "deleted"}, true, false},
		{"absent desired state", EnrollmentKey{ID: id, Reusable: true, DesiredState: "absent"}, true, false},
		{"expired key", EnrollmentKey{ID: id, Reusable: true, ExpiresAt: expired}, true, false},
		{"future expiry", EnrollmentKey{ID: id, Reusable: true, ExpiresAt: future}, true, true},
		{"unparsable expiry", EnrollmentKey{ID: id, Reusable: true, ExpiresAt: "not-a-date"}, true, false},
		{"invalid id", EnrollmentKey{ID: "nope", Reusable: true}, true, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := enrollmentKeyUsable(testCase.key, testCase.reuse); got != testCase.want {
				t.Fatalf("enrollmentKeyUsable = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestNetworkLeaveWhenNotEnrolled(t *testing.T) {
	const (
		workspace = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		network   = "11111111-2222-3333-4444-555555555555"
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/tenants/"+workspace+"/networks/"+network+"/nodes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"99999999-2222-3333-4444-555555555555","machine_id":"88888888-2222-3333-4444-555555555555"}]`))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	})
	client, store := newTestClient(t, handler)
	settings, _ := store.Settings()
	settings.ActiveWorkspaceID = workspace
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	writeSession(t, store, Session{AccessToken: "access", ExpiresAt: time.Now().Unix() + 3600})

	result, aerr := client.NetworkLeave(context.Background(), network)
	if aerr != nil {
		t.Fatalf("NetworkLeave: %v", aerr)
	}
	if !result.AlreadyOutside {
		t.Fatalf("result = %+v, want AlreadyOutside", result)
	}
}
