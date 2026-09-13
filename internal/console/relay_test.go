package console

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Console replays a write when it sees an idempotency key it already
// recorded, so two logical writes must never share one.
func TestNewIdempotencyKeyIsUniquePerAttempt(t *testing.T) {
	first := newIdempotencyKey("relay", "machine", "network")
	second := newIdempotencyKey("relay", "machine", "network")
	if first == second {
		t.Fatalf("two attempts produced the same key %q", first)
	}
	if len(first) <= len("join-machine-network-") {
		t.Fatalf("key %q carries no unique suffix", first)
	}
}

func TestNewIdempotencyKeyIncludesItsParts(t *testing.T) {
	key := newIdempotencyKey("relay", "machine-1", "network-1")
	for _, part := range []string{"relay", "machine-1", "network-1"} {
		if !contains(key, part) {
			t.Fatalf("key %q is missing %q", key, part)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The override is replaced wholesale on write, so the merge must keep every
// other setting and must express each one the way its own default requires.
func TestSetOverrideBoolRespectsEachDefault(t *testing.T) {
	override := map[string]any{"ipv4": "10.0.0.5"}

	// no_tun defaults to off: asking for it writes an explicit true.
	if !setOverrideBool(override, "no_tun", true, false) {
		t.Fatal("enabling no_tun was not reported as a change")
	}
	if override["no_tun"] != true {
		t.Fatalf("no_tun = %#v, want true", override["no_tun"])
	}

	// bind_device defaults to on: turning it off has to write an explicit
	// false, because removing the key would mean "bind anyway".
	if !setOverrideBool(override, "bind_device", false, true) {
		t.Fatal("disabling bind_device was not reported as a change")
	}
	if value, present := override["bind_device"]; !present || value != false {
		t.Fatalf("bind_device = %#v (present %v), want an explicit false", value, present)
	}

	if override["ipv4"] != "10.0.0.5" {
		t.Fatalf("an unrelated setting was dropped: %#v", override)
	}
}

// Reverting to the default removes the key again, and repeating a request that
// already holds must not rewrite the Console configuration.
func TestSetOverrideBoolRevertsAndIsIdempotent(t *testing.T) {
	override := map[string]any{"no_tun": true, "bind_device": false}

	if !setOverrideBool(override, "no_tun", false, false) {
		t.Fatal("disabling no_tun was not reported as a change")
	}
	if _, present := override["no_tun"]; present {
		t.Fatal("no_tun should be removed once it matches the core default")
	}
	if !setOverrideBool(override, "bind_device", true, true) {
		t.Fatal("re-enabling bind_device was not reported as a change")
	}
	if _, present := override["bind_device"]; present {
		t.Fatal("bind_device should be removed once it matches the core default")
	}

	empty := map[string]any{}
	if setOverrideBool(empty, "no_tun", false, false) {
		t.Fatal("a setting already at its default was reported as a change")
	}
	if setOverrideBool(empty, "bind_device", true, true) {
		t.Fatal("a setting already at its default was reported as a change")
	}
	if len(empty) != 0 {
		t.Fatalf("no keys should have been written: %#v", empty)
	}
}

// deviceDefaultHandler serves the endpoints the device-declaration flow uses,
// and records what was written.
type deviceDefaultHandler struct {
	// supportsDeclaration=false stands in for a Console that predates the
	// endpoint.
	supportsDeclaration bool
	stored              map[string]any
	puts                int
}

func (h *deviceDefaultHandler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/v1/auth/me":
		_, _ = w.Write([]byte(`{"tenants":[{"id":"` + relayTenantID + `"}]}`))
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/machines/"):
		_, _ = w.Write([]byte(`{"device":{"id":"` + relayDeviceID + `"},"networks":[{"id":"` + relayNetworkID + `"}]}`))
	case strings.HasSuffix(r.URL.Path, "/devices/"+relayDeviceID+"/default-config"):
		if !h.supportsDeclaration {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			body, _ := json.Marshal(h.stored)
			_, _ = w.Write(body)
			return
		}
		h.puts++
		body, _ := io.ReadAll(r.Body)
		h.stored = map[string]any{}
		_ = json.Unmarshal(body, &h.stored)
		_, _ = w.Write(body)
	default:
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}
}

const (
	relayTenantID  = "b1f0a1c2-0000-4000-8000-000000000001"
	relayDeviceID  = "b1f0a1c2-0000-4000-8000-000000000002"
	relayNetworkID = "b1f0a1c2-0000-4000-8000-000000000003"
)

func newRelayClient(t *testing.T, handler *deviceDefaultHandler) *Client {
	t.Helper()
	client, store := newTestClient(t, http.HandlerFunc(handler.serve))
	writeSession(t, store, Session{AccessToken: "access", ExpiresAt: time.Now().Unix() + 3600})
	selectWorkspace(t, client, relayTenantID)
	return client
}

// A node cannot be corrected before it exists, so the declaration that keeps a
// new node correct has to be written against the device.
func TestDeclareDeviceDefaultsWritesWhatTheDeviceNeeds(t *testing.T) {
	handler := &deviceDefaultHandler{supportsDeclaration: true}
	client := newRelayClient(t, handler)

	supported, aerr := client.DeclareDeviceDefaults(t.Context(), relayDeviceID,
		NodeMode{NoTun: true, DisableBindDevice: true})
	if aerr != nil {
		t.Fatalf("declare: %v", aerr)
	}
	if !supported {
		t.Fatal("a Console with the endpoint was reported as unsupported")
	}
	if handler.stored["no_tun"] != true {
		t.Fatalf("no_tun = %#v, want true", handler.stored["no_tun"])
	}
	if handler.stored["bind_device"] != false {
		t.Fatalf("bind_device = %#v, want false", handler.stored["bind_device"])
	}
	if len(handler.stored) != 2 {
		t.Fatalf("declaration carried unexpected keys: %#v", handler.stored)
	}
}

// The declaration is written only when it would change, so a device that is
// already configured does not rewrite the Console on every watch.
func TestDeclareDeviceDefaultsIsIdempotent(t *testing.T) {
	handler := &deviceDefaultHandler{supportsDeclaration: true}
	client := newRelayClient(t, handler)
	mode := NodeMode{NoTun: true, DisableBindDevice: false}

	for range 3 {
		if _, aerr := client.DeclareDeviceDefaults(t.Context(), relayDeviceID, mode); aerr != nil {
			t.Fatalf("declare: %v", aerr)
		}
	}
	if handler.puts != 1 {
		t.Fatalf("declaration written %d times, want 1", handler.puts)
	}
}

// A device that can do everything again has to clear its declaration, or it
// would stay on a restricted mode it no longer needs.
func TestDeclareDeviceDefaultsClearsWhenNothingIsNeeded(t *testing.T) {
	handler := &deviceDefaultHandler{supportsDeclaration: true, stored: map[string]any{"no_tun": true}}
	client := newRelayClient(t, handler)

	if _, aerr := client.DeclareDeviceDefaults(t.Context(), relayDeviceID, NodeMode{}); aerr != nil {
		t.Fatalf("declare: %v", aerr)
	}
	if len(handler.stored) != 0 {
		t.Fatalf("declaration was not cleared: %#v", handler.stored)
	}
}

// A Console that predates the endpoint must be reported, not failed: the node
// overrides still cover it, and a device must not refuse to work over a missing
// improvement.
func TestDeclareDeviceDefaultsReportsAnOlderConsole(t *testing.T) {
	handler := &deviceDefaultHandler{supportsDeclaration: false}
	client := newRelayClient(t, handler)

	supported, aerr := client.DeclareDeviceDefaults(t.Context(), relayDeviceID,
		NodeMode{NoTun: true, DisableBindDevice: true})
	if aerr != nil {
		t.Fatalf("an older Console was reported as an error: %v", aerr)
	}
	if supported {
		t.Fatal("an older Console was reported as supporting the declaration")
	}
}

// The device id and the memberships come from the machine payload, which is
// what the declaration is written against.
func TestMachineStateReadsTheDeviceAndItsNetworks(t *testing.T) {
	handler := &deviceDefaultHandler{supportsDeclaration: true}
	client := newRelayClient(t, handler)

	state, aerr := client.MachineState(t.Context())
	if aerr != nil {
		t.Fatalf("machine state: %v", aerr)
	}
	if state.DeviceID != relayDeviceID {
		t.Fatalf("device id = %q, want %q", state.DeviceID, relayDeviceID)
	}
	if len(state.NetworkIDs) != 1 || state.NetworkIDs[0] != relayNetworkID {
		t.Fatalf("networks = %v, want [%s]", state.NetworkIDs, relayNetworkID)
	}
}
