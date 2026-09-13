package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// installFakeCLI replaces easytier-cli with a script that prints output and
// exits with code, so the probe can be driven through its real execution path.
func installFakeCLI(t *testing.T, manager *Manager, output string, code int) {
	t.Helper()
	path := manager.paths.CLIbinary()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	script := fmt.Sprintf("#!/bin/sh\ncat <<'CLI_OUTPUT'\n%s\nCLI_OUTPUT\nexit %d\n", output, code)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// The probe has to separate an instance the core cannot serve from the ordinary
// state of a machine that is in no network, because only the first is worth
// asking the Console to correct.
func TestCoreInstancesUsable(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		code    int
		want    bool
		comment string
	}{
		{
			name:    "healthy instance",
			output:  `{"inst_id":"9f0b23cd-8194-4876-ac06-8985fc8ac283","ipv4_addr":"10.99.0.3"}`,
			code:    0,
			want:    true,
			comment: "a core serving its instance is usable",
		},
		{
			name:    "no instances yet",
			output:  "Error: no running instances found",
			code:    1,
			want:    true,
			comment: "a machine that has joined no network is not failing",
		},
		{
			name: "instance the core cannot serve",
			output: "Error: instance nt_d2fdd02c9843_16c5c5de872d (9f0b23cd-8194-4876-ac06-8985fc8ac283)\n\n" +
				"Caused by:\n    0: Rust error: Instance not found or API service not available",
			code:    1,
			want:    false,
			comment: "this is the unusable instance the Console must be told about",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager := newTestManager(t)
			installFakeCLI(t, manager, tc.output, tc.code)
			if got := manager.coreInstancesUsable(context.Background()); got != tc.want {
				t.Fatalf("coreInstancesUsable() = %v, want %v (%s)", got, tc.want, tc.comment)
			}
		})
	}
}

// A missing CLI must not be read as a broken instance: the package would then
// ask the Console to fix a configuration it never inspected.
func TestCoreInstancesUsableWithoutACLI(t *testing.T) {
	manager := newTestManager(t)
	if got := manager.coreInstancesUsable(context.Background()); !got {
		t.Fatal("a missing easytier-cli was reported as an unusable instance")
	}
}

// One failed probe is not enough to act on, because the Console deletes and
// recreates the instance on every legitimate change and a probe can land in that
// window.
func TestInstanceWatchNeedsTwoConsecutiveFailures(t *testing.T) {
	var state instanceWatchState
	now := time.Now()

	if state.failedProbe(now) {
		t.Fatal("a single failed probe triggered a corrective sync")
	}
	if !state.failedProbe(now.Add(instanceProbeInterval)) {
		t.Fatal("two consecutive failed probes did not trigger a corrective sync")
	}
}

// A healthy probe in between must reset the run, so transient failures during a
// normal recreate never accumulate into a correction.
func TestInstanceWatchResetsOnAHealthyProbe(t *testing.T) {
	var state instanceWatchState
	now := time.Now()

	if state.failedProbe(now) {
		t.Fatal("a single failed probe triggered a corrective sync")
	}
	state.usableProbe()
	if state.failedProbe(now.Add(instanceProbeInterval)) {
		t.Fatal("failures either side of a healthy probe were counted as consecutive")
	}
	if !state.failedProbe(now.Add(2 * instanceProbeInterval)) {
		t.Fatal("the next run of failures did not trigger a corrective sync")
	}
}

// A cause the mode cannot fix must not become a Console request every probe.
func TestInstanceWatchRateLimitsCorrections(t *testing.T) {
	var state instanceWatchState
	start := time.Now()

	state.failedProbe(start)
	if !state.failedProbe(start.Add(instanceProbeInterval)) {
		t.Fatal("the first correction did not fire")
	}
	// The instance is still broken, so failures keep arriving.
	push := func(at time.Time) bool {
		state.failedProbe(at)
		return state.failedProbe(at.Add(instanceProbeInterval))
	}
	if push(start.Add(2 * instanceProbeInterval)) {
		t.Fatal("a correction fired again inside the rate limit")
	}
	if push(start.Add(4 * instanceProbeInterval)) {
		t.Fatal("a correction fired again inside the rate limit")
	}
	if !push(start.Add(correctiveSyncInterval)) {
		t.Fatal("a persisting failure stopped being corrected after the rate limit")
	}
}

// The probe must ask the core's management portal the same way the rest of the
// daemon does.
func TestCoreInstancesUsableQueriesThePortal(t *testing.T) {
	manager := newTestManager(t)
	log := filepath.Join(t.TempDir(), "args")
	path := manager.paths.CLIbinary()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >>%s\nexit 0\n", log)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !manager.coreInstancesUsable(context.Background()) {
		t.Fatal("a successful probe was reported as unusable")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "-p\n" + rpcPortalAddress + "\n-o\njson\nnode\ninfo\n"
	if string(data) != want {
		t.Fatalf("probe args = %q, want %q", string(data), want)
	}
}

// A correction that worked must not delay the next one. The rate limit is there
// for a failure the mode cannot fix, not for a machine that recovers and then
// breaks again.
func TestInstanceWatchLiftsTheLimitAfterRecovery(t *testing.T) {
	var state instanceWatchState
	start := time.Now()

	state.failedProbe(start)
	if !state.failedProbe(start.Add(instanceProbeInterval)) {
		t.Fatal("the first correction did not fire")
	}
	// The instance recovered, so the machine is healthy again...
	state.usableProbe()
	// ...and breaks again well inside the rate-limit window.
	soon := start.Add(3 * instanceProbeInterval)
	state.failedProbe(soon)
	if !state.failedProbe(soon.Add(instanceProbeInterval)) {
		t.Fatal("a failure after a recovery waited for the rate limit")
	}
}
