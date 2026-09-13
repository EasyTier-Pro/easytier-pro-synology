package runtime

import (
	"context"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
)

// relaySyncTimeout bounds the Console round trips of one relay-mode sync. It is
// background housekeeping, never a user-visible action.
const relaySyncTimeout = 30 * time.Second

// Relay-mode sync retry policy, used right after a node is created.
const (
	relaySyncAttempts   = 3
	relaySyncRetryDelay = 2 * time.Second
)

// RelayMode reports whether the local core has to run without a TUN device.
//
// DSM never runs a package as root, so the downloaded core only obtains
// CAP_NET_ADMIN when an administrator grants it as a file capability. Without
// it the core cannot create a tun interface, and the instance configuration the
// Console pushes has to say so.
func (m *Manager) RelayMode() bool {
	return !tunCapable(m.paths.CoreBinary())
}

// SyncRelayMode tells the Console whether this device must run in relay mode,
// so the runtime configuration it pushes matches what the local core can do.
//
// The command line cannot express this: in secure mode the core is started with
// --secure-mode and no network of its own, and the instances it runs come from
// the Console. `--no-tun` on the command line is therefore ignored, which is
// why the mode is negotiated here instead.
//
// It is best effort: failures are reported to the caller, which logs them, and
// never block starting, joining or leaving a network.
func (m *Manager) SyncRelayMode(ctx context.Context) *apperr.Error {
	if m.cli == nil || !m.cli.LoggedIn() {
		return nil
	}
	if !m.store.HasBootstrapToken() {
		return nil
	}
	relay := m.RelayMode()
	networks, aerr := m.cli.EnrolledNetworkIDs(ctx)
	if aerr != nil {
		return aerr
	}
	// Every network is attempted even after a failure: stopping at the first
	// error would leave the rest on a stale mode and, since the networks are
	// visited in the same order every time, could keep them there forever.
	var firstError *apperr.Error
	for _, networkID := range networks {
		changed, aerr := m.cli.SetNodeNoTun(ctx, networkID, relay)
		if aerr != nil {
			m.log.Errorf("同步网络 %s 的运行模式失败: %s", networkID, aerr.Message)
			if firstError == nil {
				firstError = aerr
			}
			continue
		}
		if changed {
			if relay {
				m.log.Printf("本机没有创建虚拟网卡的权限，已把网络 %s 上的本机节点设为无 TUN 模式", networkID)
			} else {
				m.log.Printf("本机已获得创建虚拟网卡的权限，已取消网络 %s 上本机节点的无 TUN 模式", networkID)
			}
		}
	}
	return firstError
}

// syncRelayModeQuietly runs SyncRelayMode for callers that must not fail.
func (m *Manager) syncRelayModeQuietly(ctx context.Context) {
	if err := m.SyncRelayMode(ctx); err != nil {
		m.log.Errorf("同步本机运行模式到 Console 失败: %s", err.Message)
	}
}

// syncRelayModeRetrying retries the sync a few times. It exists for the moment
// right after a node is created, when the Console may not list the new node yet.
func (m *Manager) syncRelayModeRetrying(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		aerr := m.SyncRelayMode(ctx)
		if aerr == nil {
			return
		}
		if attempt >= relaySyncAttempts-1 || ctx.Err() != nil {
			m.log.Errorf("同步本机运行模式到 Console 失败: %s", aerr.Message)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(relaySyncRetryDelay):
		}
	}
}

// startRelayModeSync keeps the Console setting aligned in the background, so
// startup never waits on the Console.
func (m *Manager) startRelayModeSync(ctx context.Context) {
	go func() {
		syncCtx, cancel := context.WithTimeout(ctx, relaySyncTimeout)
		defer cancel()
		m.syncRelayModeQuietly(syncCtx)
	}()
}
