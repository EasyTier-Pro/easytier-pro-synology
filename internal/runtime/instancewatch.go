package runtime

import (
	"context"
	"strings"
	"time"
)

// The instance watch detects a network instance the core cannot run and asks the
// Console to correct it.
//
// The Console pushes a node's configuration and the core then builds its
// instance from it. A configuration this host cannot honour - TUN without
// CAP_NET_ADMIN, interface binding without CAP_NET_RAW - leaves the instance
// registered but unusable, and every peer connection fails. The Console cannot
// see that by itself, so this device has to say so, and the sooner it does the
// shorter the outage.
//
// The probe is local and cheap: easytier-cli asks the core's management portal
// about its own instances over loopback. It never contacts the Console, which is
// why it can run far more often than the Console reconciliation. The Console
// publishes a corrected configuration within seconds of being told, so the probe
// interval is what the outage mostly consists of.
const (
	// instanceProbeInterval is how often the local instance state is read.
	instanceProbeInterval = 10 * time.Second
	// instanceProbeTimeout bounds one probe, a loopback query to a local socket.
	instanceProbeTimeout = 5 * time.Second
	// instanceProbeFailures is how many consecutive probes must fail before the
	// Console is asked to correct the configuration. The Console deletes and
	// recreates the instance on every legitimate change, and a probe can land in
	// that window, so a single failure is not enough to act on.
	instanceProbeFailures = 2
	// correctiveSyncInterval is the shortest gap between two corrective syncs.
	// A probe failure can have a cause the mode cannot fix - an unreachable
	// configuration server, for instance - and this keeps that from becoming a
	// Console request every few seconds.
	correctiveSyncInterval = 60 * time.Second
)

// coreInstancesUsable reports whether the core has instances it can actually run.
//
// A core that reports "no running instances found" is not failing: that is what
// it says before this machine joins a network and after it leaves them all. Only
// an instance the core knows about but cannot serve counts as unusable, which is
// what the management API reports when its instance never finished starting.
func (m *Manager) coreInstancesUsable(ctx context.Context) bool {
	if !isExecutable(m.paths.CLIbinary()) {
		// Nothing to ask, so nothing to conclude; the Console still reconciles
		// on its own schedule.
		return true
	}
	output, err := combinedCommandOutput(ctx, instanceProbeTimeout, 8<<10,
		m.paths.CLIbinary(), "-p", rpcPortalAddress, "-o", "json", "node", "info")
	if err == nil {
		return true
	}
	return strings.Contains(output, "no running instances found")
}

// instanceWatchState decides when a run of failed probes warrants asking the
// Console to correct the configuration.
type instanceWatchState struct {
	failures       int
	lastCorrection time.Time
}

// failedProbe records a failed probe and reports whether this one should trigger
// a corrective sync.
func (s *instanceWatchState) failedProbe(now time.Time) bool {
	s.failures++
	if s.failures < instanceProbeFailures {
		return false
	}
	if !s.lastCorrection.IsZero() && now.Sub(s.lastCorrection) < correctiveSyncInterval {
		return false
	}
	s.failures = 0
	s.lastCorrection = now
	return true
}

// usableProbe records a healthy probe.
//
// It also lifts the rate limit: that limit exists to stop a failure the mode
// cannot fix from becoming a Console request every few seconds, and once the
// instance has recovered the previous correction has clearly done its job, so a
// later failure deserves an immediate one.
func (s *instanceWatchState) usableProbe() {
	s.failures = 0
	s.lastCorrection = time.Time{}
}

// startInstanceWatch asks the Console to correct the node configuration as soon
// as the local core reports an instance it cannot run, instead of waiting for
// the next periodic reconciliation.
func (m *Manager) startInstanceWatch(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(instanceProbeInterval)
		defer ticker.Stop()
		var state instanceWatchState
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !m.store.HasBootstrapToken() {
				// Not connected to a Console, so there is no configuration to
				// correct and nothing worth probing.
				state.usableProbe()
				continue
			}
			if m.coreInstancesUsable(ctx) {
				state.usableProbe()
				continue
			}
			if !state.failedProbe(time.Now()) {
				continue
			}
			m.log.Errorf("本机实例无法运行，重新向 Console 声明本机所需配置")
			correctCtx, cancel := context.WithTimeout(ctx, relaySyncTimeout)
			m.syncRelayModeQuietly(correctCtx)
			cancel()
		}
	}()
}
