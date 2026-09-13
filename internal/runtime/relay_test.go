package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
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

// ModeSynced decides whether the interface may claim the Console holds this
// device's settings, so it has to follow what was actually applied.
func TestModeSyncedTracksTheAppliedMode(t *testing.T) {
	manager := &Manager{}
	needed := manager.NodeMode()

	// A mode different from what the device needs must never count as synced.
	other := console.NodeMode{NoTun: !needed.NoTun, DisableBindDevice: needed.DisableBindDevice}
	manager.recordModeSync(other, true)
	if manager.ModeSynced() {
		t.Fatal("settings that do not match the device were reported as synced")
	}

	if !needed.NoTun && !needed.DisableBindDevice {
		// This host grants the capabilities, so there is nothing to write and
		// the device is in sync no matter what was recorded.
		if !manager.ModeSynced() {
			t.Fatal("a device that needs nothing was reported as out of sync")
		}
		return
	}

	if manager.ModeSynced() {
		t.Fatal("a device whose settings were never written reported itself as synced")
	}
	manager.recordModeSync(needed, true)
	if !manager.ModeSynced() {
		t.Fatal("a completed sync was not reported as synced")
	}
	// A failed write must not be remembered as success.
	manager.recordModeSync(needed, false)
	if manager.ModeSynced() {
		t.Fatal("a failed sync was reported as synced")
	}
	// Losing a capability changes what the device needs, and the recorded
	// success for the old mode must not cover it.
	manager.recordModeSync(other, true)
	if manager.ModeSynced() {
		t.Fatal("a stale sync was reported as covering the current mode")
	}
}
