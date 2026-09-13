package runtime

import (
	"context"
	"testing"
	"time"
)

// The strings here are the ones this package really sees. A runtime reports the
// release tag with its build's commit appended, while the Console reports the
// bare tag, so a comparison that treated them as equal strings would call an
// installed release an available update forever.
func TestVersionReportsMatchesTheReleaseTag(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		version string
		want    bool
	}{
		{"the real core against the real release", "easytier-core 2.6.4-8428a89d", "v2.6.4", true},
		{"the real cli against the real release", "easytier-cli 2.6.4-8428a89d", "v2.6.4", true},
		{"an older core against a newer release", "easytier-core 2.6.3-8428a89d", "v2.6.4", false},
		{"a newer core against an older release", "easytier-core 2.6.5-8428a89d", "v2.6.4", false},
		{"a release without its v", "easytier-core 2.6.4-8428a89d", "2.6.4", true},
		{"a patch that starts with the same digits", "easytier-core 2.6.40-abc", "v2.6.4", false},
		{"a multi-line answer uses its first line", "easytier-core 2.6.4-abc\nbuild 12", "v2.6.4", true},
		{"an empty answer matches nothing", "", "v2.6.4", false},
		{"an empty release matches nothing", "easytier-core 2.6.4-abc", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := versionReports(testCase.output, testCase.version); got != testCase.want {
				t.Fatalf("versionReports(%q, %q) = %t, want %t",
					testCase.output, testCase.version, got, testCase.want)
			}
		})
	}
}

// offlineManager returns a manager whose Console cannot be reached, so a lookup
// fails at once instead of reaching the public one. No test may depend on the
// internet.
func offlineManager(t *testing.T) *Manager {
	t.Helper()
	manager := newTestManager(t)
	settings, err := manager.store.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	// Port 1 is reserved and nothing listens on it.
	settings.ConsoleURL = "http://127.0.0.1:1"
	settings.AllowInsecureConsole = true
	if err := manager.store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	return manager
}

// setRelease records a release the way a successful lookup would.
func setRelease(m *Manager, version string) {
	m.releaseState.mu.Lock()
	defer m.releaseState.mu.Unlock()
	m.releaseState.latest = version
	m.releaseState.lastError = ""
}

func TestCoreUpdateAvailable(t *testing.T) {
	manager := newTestManager(t)

	// Nothing known yet, so nothing is claimed.
	if manager.coreUpdateAvailable("easytier-core 2.6.4-8428a89d") {
		t.Fatal("an update was claimed before the Console was ever asked")
	}

	setRelease(manager, "v2.6.4")
	if manager.coreUpdateAvailable("easytier-core 2.6.4-8428a89d") {
		t.Fatal("the installed release was reported as an available update")
	}

	setRelease(manager, "v2.6.5")
	if !manager.coreUpdateAvailable("easytier-core 2.6.4-8428a89d") {
		t.Fatal("a newer release was not reported as an available update")
	}

	// A device with no core installed has nothing to upgrade.
	if manager.coreUpdateAvailable("") {
		t.Fatal("an update was claimed with no core installed")
	}
}

// A failed lookup must keep what the Console last said: the notice appearing and
// disappearing with every hiccup would be worse than a stale answer.
func TestLatestReleaseSurvivesAFailedLookup(t *testing.T) {
	manager := offlineManager(t)
	setRelease(manager, "v2.6.4")

	manager.refreshLatestRelease(context.Background())

	if got := manager.latestRelease(); got != "v2.6.4" {
		t.Fatalf("latest release = %q after a failed lookup, want the previous value", got)
	}

	// The failure is recorded once, so the next interval does not report it again.
	manager.releaseState.mu.Lock()
	reported := manager.releaseState.lastError
	manager.releaseState.mu.Unlock()
	if reported == "" {
		t.Fatal("the failure was not recorded")
	}
}

// The watch asks immediately, so a device that has been off does not wait a full
// interval to learn about a release.
func TestReleaseWatchAsksAtStartup(t *testing.T) {
	manager := offlineManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.startReleaseWatch(ctx)
	// The first lookup happens before the first interval, so an unreachable
	// Console is recorded straight away rather than half an hour later.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		manager.releaseState.mu.Lock()
		asked := manager.releaseState.lastError != ""
		manager.releaseState.mu.Unlock()
		if asked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the watch never asked the Console")
}

// The watch must stop with the daemon, not outlive it.
func TestReleaseWatchStopsWithItsContext(t *testing.T) {
	manager := offlineManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	manager.startReleaseWatch(ctx)
	cancel()

	// A cancelled context leaves the immediate lookup to fail and the goroutine
	// to return; nothing hangs or panics on the way out.
	time.Sleep(200 * time.Millisecond)
	manager.refreshLatestRelease(context.Background())
	if manager.latestRelease() != "" {
		t.Fatal("a release was recorded although every lookup failed")
	}
}
