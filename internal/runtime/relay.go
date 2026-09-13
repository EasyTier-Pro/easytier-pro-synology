package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
)

// relaySyncTimeout bounds the Console round trips of one relay-mode sync. It is
// background housekeeping, never a user-visible action.
const relaySyncTimeout = 30 * time.Second

// Relay-mode sync retry policy, used right after a node is created.
const (
	relaySyncAttempts   = 3
	relaySyncRetryDelay = 2 * time.Second
)

// relayWatchInterval is how often the Console configuration is re-checked.
//
// The mode can be changed from the Console at any time - attaching this machine
// to a network there creates a node with TUN enabled by default, which this
// host cannot honour, and that used to leave the instance broken with nothing
// to correct it. Re-checking on a timer converges on the right configuration
// without depending on which side made the change.
const relayWatchInterval = 90 * time.Second

// NodeMode reports the configuration this device needs on the Console.
//
// DSM never runs a package as root, so the downloaded core only gets a
// capability when an administrator grants it to the binary as a file
// capability. Each capability the core lacks has to be reflected in the
// instance configuration the Console pushes, because none of it can be
// expressed on the core's command line in secure mode.
func (m *Manager) NodeMode() console.NodeMode {
	corePath := m.paths.CoreBinary()
	return console.NodeMode{
		// Without CAP_NET_ADMIN the core cannot create a virtual interface.
		NoTun: !tunCapable(corePath),
		// Without CAP_NET_RAW the core cannot bind its sockets to an
		// interface, and every peer connection then fails.
		DisableBindDevice: !bindCapable(corePath),
	}
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
	if m.cli == nil {
		return nil
	}
	if !m.store.HasBootstrapToken() {
		return nil
	}
	mode := m.NodeMode()
	if !m.cli.LoggedIn() {
		if !mode.NoTun && !mode.DisableBindDevice {
			// This host can do everything the core asks of it and there is
			// nothing inherited to correct, so there is nothing to say - and a
			// device token alone could not say it anyway.
			m.recordModeSync(mode, true)
			return nil
		}
		// The settings still have to be written, but a device token alone
		// cannot reach the Console API. Say so rather than skipping silently:
		// the node will not start until the Console is told, and the operator
		// needs to know why.
		return apperr.New(apperr.CodeNotAuthenticated)
	}
	state, aerr := m.cli.MachineState(ctx)
	if aerr != nil {
		return aerr
	}

	// The device-level declaration is what makes a node correct from its first
	// push, so it is written before anything is said about individual nodes.
	// A Console that does not know the endpoint reports that, and the node
	// overrides below remain the whole mechanism there.
	declared, aerr := m.cli.DeclareDeviceDefaults(ctx, state.DeviceID, mode)
	if aerr != nil {
		m.log.Errorf("向 Console 声明本机所需配置失败: %s", aerr.Message)
	} else if !declared {
		// Said once: an older Console keeps working through the node overrides
		// below, and repeating this on every watch would only add noise.
		m.reportDeclarationUnsupported()
	}

	// Every network is attempted even after a failure: stopping at the first
	// error would leave the rest on a stale mode and, since the networks are
	// visited in the same order every time, could keep them there forever.
	var firstError *apperr.Error
	if aerr != nil {
		firstError = aerr
	}
	for _, networkID := range state.NetworkIDs {
		changed, aerr := m.cli.SetNodeMode(ctx, networkID, mode)
		if aerr != nil {
			m.log.Errorf("同步网络 %s 的运行模式失败: %s", networkID, aerr.Message)
			if firstError == nil {
				firstError = aerr
			}
			continue
		}
		if changed {
			m.log.Printf("已按本机能力更新网络 %s 上的节点配置：%s", networkID, describeNodeMode(mode))
		}
	}
	if declared {
		m.log.Printf("已在 Console 上为本机声明默认配置：%s", describeNodeMode(mode))
	}
	m.recordModeSync(mode, firstError == nil)
	return firstError
}

// reportDeclarationUnsupported says once that this Console has no device-level
// declaration, so the mode is still being expressed per node.
func (m *Manager) reportDeclarationUnsupported() {
	m.syncState.mu.Lock()
	reported := m.syncState.declarationReported
	m.syncState.declarationReported = true
	m.syncState.mu.Unlock()
	if !reported {
		m.log.Printf("当前 Console 不支持设备级默认配置，仍按网络逐个下发本机所需配置")
	}
}

// recordModeSync remembers what the Console was told, so the interface can tell
// whether the settings it describes are actually in place.
func (m *Manager) recordModeSync(mode console.NodeMode, ok bool) {
	m.syncState.mu.Lock()
	defer m.syncState.mu.Unlock()
	if !ok {
		m.syncState.synced = false
		return
	}
	m.syncState.applied = mode
	m.syncState.synced = true
}

// ModeSynced reports whether the Console has been told the mode this device
// needs. A device that needs nothing is always in sync.
func (m *Manager) ModeSynced() bool {
	needed := m.NodeMode()
	if !needed.NoTun && !needed.DisableBindDevice {
		return true
	}
	m.syncState.mu.Lock()
	defer m.syncState.mu.Unlock()
	return m.syncState.synced && m.syncState.applied == needed
}

// describeNodeMode renders the mode for the log.
func describeNodeMode(mode console.NodeMode) string {
	parts := make([]string, 0, 2)
	if mode.NoTun {
		parts = append(parts, "无 TUN（无 CAP_NET_ADMIN）")
	}
	if mode.DisableBindDevice {
		parts = append(parts, "不绑定网卡（无 CAP_NET_RAW）")
	}
	if len(parts) == 0 {
		return "完整模式"
	}
	return strings.Join(parts, "，")
}

// syncRelayModeQuietly runs SyncRelayMode for callers that must not fail.
//
// The watch repeats this check, so an unchanged failure is reported once
// instead of filling the log at every interval.
func (m *Manager) syncRelayModeQuietly(ctx context.Context) {
	err := m.SyncRelayMode(ctx)
	if err == nil {
		m.syncState.mu.Lock()
		m.syncState.lastError = ""
		m.syncState.mu.Unlock()
		return
	}
	m.syncState.mu.Lock()
	repeated := m.syncState.lastError == err.Message
	m.syncState.lastError = err.Message
	m.syncState.mu.Unlock()
	if !repeated {
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
// startup never waits on the Console, and keeps re-checking it afterwards.
func (m *Manager) startRelayModeSync(ctx context.Context) {
	go func() {
		syncCtx, cancel := context.WithTimeout(ctx, relaySyncTimeout)
		m.syncRelayModeQuietly(syncCtx)
		cancel()

		ticker := time.NewTicker(relayWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				watchCtx, cancelWatch := context.WithTimeout(ctx, relaySyncTimeout)
				m.syncRelayModeQuietly(watchCtx)
				cancelWatch()
			}
		}
	}()
}
