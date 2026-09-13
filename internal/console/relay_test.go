package console

import "testing"

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
