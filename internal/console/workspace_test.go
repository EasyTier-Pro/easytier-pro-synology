package console

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	tenantFirst  = "11111111-1111-1111-1111-111111111111"
	tenantSecond = "22222222-2222-2222-2222-222222222222"
)

// selectWorkspace stores an explicit workspace choice, the way activation does.
func selectWorkspace(t *testing.T, client *Client, workspaceID string) {
	t.Helper()
	settings, err := client.store.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	settings.ActiveWorkspaceID = workspaceID
	if err := client.store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}

// A device connected with a token alone never chose a workspace, so the
// workspace has to be found from the account before any tenant-scoped call can
// be made.
func TestWorkspaceIDDiscoversTheOwningTenant(t *testing.T) {
	var machineLookups int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":"u1"},"tenants":[{"id":"` + tenantFirst + `"},{"id":"` + tenantSecond + `"}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/tenants/"+tenantFirst+"/machines/"):
			atomic.AddInt32(&machineLookups, 1)
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/api/v1/tenants/"+tenantSecond+"/machines/"):
			atomic.AddInt32(&machineLookups, 1)
			_, _ = w.Write([]byte(`{"device":{"id":"d1"}}`))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	})

	client, store := newTestClient(t, handler)
	writeSession(t, store, Session{AccessToken: "access", ExpiresAt: time.Now().Unix() + 3600})

	got, aerr := client.workspaceID(context.Background())
	if aerr != nil {
		t.Fatalf("workspaceID() error = %v", aerr)
	}
	if got != tenantSecond {
		t.Fatalf("workspaceID() = %q, want the tenant that knows this machine", got)
	}
	if n := atomic.LoadInt32(&machineLookups); n != 2 {
		t.Fatalf("machine lookups = %d, want 2 (stop at the match)", n)
	}
}

// The answer is remembered, so later calls do not ask again and the operator
// can see which workspace the device settled on.
func TestWorkspaceIDPersistsTheDiscoveredTenant(t *testing.T) {
	var meCalls int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/me" {
			atomic.AddInt32(&meCalls, 1)
			_, _ = w.Write([]byte(`{"tenants":[{"id":"` + tenantFirst + `"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"device":{"id":"d1"}}`))
	})
	client, store := newTestClient(t, handler)
	writeSession(t, store, Session{AccessToken: "access", ExpiresAt: time.Now().Unix() + 3600})

	for range 3 {
		if _, aerr := client.workspaceID(context.Background()); aerr != nil {
			t.Fatalf("workspaceID() error = %v", aerr)
		}
	}
	settings, err := store.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if settings.ActiveWorkspaceID != tenantFirst {
		t.Fatalf("ActiveWorkspaceID = %q, want it stored", settings.ActiveWorkspaceID)
	}
	if n := atomic.LoadInt32(&meCalls); n != 1 {
		t.Fatalf("auth/me calls = %d, want 1: the discovered workspace must be reused", n)
	}
}

// An explicitly chosen workspace must never be overridden, and must not cost a
// Console round trip.
func TestWorkspaceIDKeepsAStoredChoice(t *testing.T) {
	var calls int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "must not be called", http.StatusInternalServerError)
	})
	client, _ := newTestClient(t, handler)
	selectWorkspace(t, client, tenantSecond)

	got, aerr := client.workspaceID(context.Background())
	if aerr != nil {
		t.Fatalf("workspaceID() error = %v", aerr)
	}
	if got != tenantSecond {
		t.Fatalf("workspaceID() = %q, want the stored workspace", got)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("the Console was called %d times for a stored workspace", n)
	}
}

// Without a Console session there is nothing to ask. Reporting that is what
// lets the caller explain why the node cannot be configured.
func TestWorkspaceIDWithoutASession(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected call to %s", r.URL.Path)
	}))
	if _, aerr := client.workspaceID(context.Background()); aerr == nil || aerr.Code != "not_authenticated" {
		t.Fatalf("workspaceID() = %v, want not_authenticated", aerr)
	}
}

// A token-only device that has not registered yet cannot be located; the caller
// retries once the core has registered it.
func TestWorkspaceIDWhenTheDeviceIsUnknown(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/me" {
			_, _ = w.Write([]byte(`{"tenants":[{"id":"` + tenantFirst + `"},{"id":"` + tenantSecond + `"}]}`))
			return
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})
	client, store := newTestClient(t, handler)
	writeSession(t, store, Session{AccessToken: "access", ExpiresAt: time.Now().Unix() + 3600})

	if _, aerr := client.workspaceID(context.Background()); aerr == nil || aerr.Code != "no_workspace" {
		t.Fatalf("workspaceID() = %v, want no_workspace", aerr)
	}
	settings, err := store.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if settings.ActiveWorkspaceID != "" {
		t.Fatalf("a failed discovery stored %q", settings.ActiveWorkspaceID)
	}
}
