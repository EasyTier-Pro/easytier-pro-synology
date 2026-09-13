package runtime

import (
	"context"
	"time"
)

// How often the Console is asked which release it currently offers. A release
// does not change often, and the answer is a small public request, so this is
// about noticing within the hour rather than about freshness.
const releaseCheckInterval = 30 * time.Minute

// startReleaseWatch follows the release the Console offers.
//
// The answer is what lets the interface say an upgrade exists. Nothing is
// installed from here: an upgrade restarts the core and briefly interrupts the
// tunnel, which is the operator's decision to make, not a background task's.
func (m *Manager) startReleaseWatch(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(releaseCheckInterval)
		defer ticker.Stop()
		// Asked once at startup so a device that has been off does not wait a
		// full interval to learn about a release.
		m.refreshLatestRelease(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			m.refreshLatestRelease(ctx)
		}
	}()
}

// refreshLatestRelease records the release the Console currently offers.
//
// A failed lookup keeps the value from the last successful one. The Console is
// reached through the network, and forgetting what it last said would make the
// interface's notice appear and disappear with every hiccup. The failure is
// reported once per run of failures and not at every interval.
func (m *Manager) refreshLatestRelease(ctx context.Context) {
	release, aerr := m.cli.LatestRelease(ctx)
	if aerr != nil {
		m.releaseState.mu.Lock()
		firstFailure := m.releaseState.lastError == ""
		m.releaseState.lastError = aerr.Code
		m.releaseState.mu.Unlock()
		if firstFailure {
			m.log.Errorf("读取 EasyTier 稳定版本失败: %s", aerr.Code)
		}
		return
	}
	version, ok := normalizeVersion(release.Stable.Version)
	if !ok {
		// The Console answered without a usable release, which is its own
		// problem and not something to report as the device's.
		return
	}

	m.releaseState.mu.Lock()
	defer m.releaseState.mu.Unlock()
	previous := m.releaseState.latest
	m.releaseState.latest = version
	m.releaseState.lastError = ""
	if previous != "" && previous != version {
		m.log.Printf("EasyTier 稳定版本已更新：%s → %s", previous, version)
	}
}

// latestRelease returns the release the Console last reported, if any.
func (m *Manager) latestRelease() string {
	m.releaseState.mu.Lock()
	defer m.releaseState.mu.Unlock()
	return m.releaseState.latest
}

// coreUpdateAvailable reports whether the installed core is older than the
// release the Console offers.
//
// It is answered here rather than by the interface because it is the same
// question the installer asks of a downloaded binary, and the two must agree:
// the release tag in a runtime's version output carries the build's commit, so
// a comparison made twice is a comparison that will differ once.
func (m *Manager) coreUpdateAvailable(installed string) bool {
	latest := m.latestRelease()
	if latest == "" || installed == "" {
		return false
	}
	return !versionReports(installed, latest)
}
