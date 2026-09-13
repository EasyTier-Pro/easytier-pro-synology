package runtime

import (
	"context"
	"testing"
	"time"
)

// The watch must keep re-checking after the first sync: the Console can attach
// this machine to a network at any time, and a node created there defaults to
// TUN enabled, which this host cannot honour.
func TestRelayWatchRepeats(t *testing.T) {
	if relayWatchInterval <= 0 {
		t.Fatal("the relay watch interval must be positive")
	}
	if relayWatchInterval > 5*time.Minute {
		t.Fatalf("relayWatchInterval = %v, too slow to repair a Console-side change", relayWatchInterval)
	}
	// A cancelled context has to stop the watch, or the daemon leaks a
	// goroutine once per start.
	ctx, cancel := context.WithCancel(context.Background())
	// A manager without a Console client short-circuits the sync, which is all
	// this test needs: the watch loop itself must still exit.
	manager := &Manager{}
	done := make(chan struct{})
	go func() {
		manager.startRelayModeSync(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("startRelayModeSync did not stop when its context was cancelled")
	}
}
