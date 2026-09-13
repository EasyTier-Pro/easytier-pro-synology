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
