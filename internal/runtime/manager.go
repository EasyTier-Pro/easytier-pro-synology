// Package runtime runs and supervises the local EasyTier core, installs and
// updates the runtime binaries, and owns the persistent connection state.
package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
)

// healthAttempts is how long the runtime waits for a starting core to answer
// the management RPC, in seconds.
const healthAttempts = 90

// closeTimeout bounds how long a stopping core may take before it is killed.
const closeTimeout = 5 * time.Second

// Manager owns every local change: the core process, the runtime binaries and
// the connection settings.
type Manager struct {
	paths config.Paths
	store *config.Store
	cli   *console.Client
	log   *config.Logger

	// mu serializes runtime mutations: service actions, connection changes,
	// settings changes and runtime installs.
	mu   sync.Mutex
	core *coreSupervisor
	dl   *downloadState
	ops  *operationSet

	// syncState remembers the outcome of the mode negotiation. It has its own
	// lock because the watch runs outside the mutation lock.
	syncState struct {
		mu sync.Mutex
		// lastError is the most recent failure, so the watch reports a
		// persisting problem once instead of at every interval.
		lastError string
		// applied is the mode the Console was last told about, and synced
		// whether that write succeeded.
		applied console.NodeMode
		synced  bool
		// declarationReported records that this Console was found to have no
		// device-level declaration, so that is said once and not every watch.
		declarationReported bool
	}

	// releaseState remembers the release the Console last offered. It has its
	// own lock because the watch runs outside the mutation lock.
	releaseState struct {
		mu sync.Mutex
		// latest is the release tag from the last successful lookup, kept
		// across a failed one so the interface's notice does not flicker.
		latest string
		// lastError is the most recent failure code, so a Console that cannot
		// be reached is reported once rather than at every interval.
		lastError string
	}
}

// NewManager wires a manager to the package roots, state store and Console
// client.
func NewManager(paths config.Paths, store *config.Store, cli *console.Client, log *config.Logger) *Manager {
	manager := &Manager{paths: paths, store: store, cli: cli, log: log}
	manager.dl = newDownloadState(manager)
	manager.ops = newOperationSet(manager)
	return manager
}

// Start recovers interrupted work and launches the core supervisor.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.paths.EnsureDirs(); err != nil {
		return err
	}
	// The supervisor must exist before recovery: rolling a transaction back
	// stops the core. A core left over from a killed daemon is stopped first,
	// so recovery never mutates state under a running tunnel.
	m.core = newCoreSupervisor(m, ctx)
	// The loop idles until something asks for the core, so recovery can use
	// EnsureHealthy while remaining the only writer of the restored state.
	m.core.Start()
	m.stopStaleCore()
	if err := m.recoverUpdateFiles(ctx, true); err != nil {
		m.log.Errorf("恢复运行时更新失败: %v", err)
	}
	if err := m.recoverConnectionState(ctx, true); err != nil {
		m.log.Errorf("恢复连接状态失败: %v", err)
	}
	if err := m.cleanupOrphanRuntimeFiles(); err != nil {
		m.log.Errorf("清理残留运行时文件失败: %v", err)
	}
	m.dl.recoverStatus()
	m.log.Printf("EasyTier Pro 已启动，运行目录 %s", m.paths.RuntimeDir())

	m.ops.Start(ctx)
	m.refreshDesired()
	m.startRelayModeSync(ctx)
	m.startInstanceWatch(ctx)
	m.startReleaseWatch(ctx)
	return nil
}

// Shutdown stops the supervised core.
func (m *Manager) Shutdown() {
	if m.core == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m.core.StopAndWait(ctx)
}

// refreshDesired tells the supervisor whether the core should run right now.
func (m *Manager) refreshDesired() {
	if m.core == nil {
		return
	}
	if err := m.coreStartBlocker(); err != nil {
		m.core.SetDesired(false)
		return
	}
	m.core.SetDesired(true)
}

// coreStartBlocker reports why the core must not run, or nil when it may.
func (m *Manager) coreStartBlocker() error {
	settings, err := m.store.Settings()
	if err != nil {
		return fmt.Errorf("读取设置失败: %w", err)
	}
	if !settings.Enabled {
		return errDisabled
	}
	if !m.store.HasBootstrapToken() {
		return errNoToken
	}
	if !config.ValidConfigServer(settings.ConfigServer) {
		return errConfigServer
	}
	if !isExecutable(m.paths.CoreBinary()) {
		return errCoreMissing
	}
	return nil
}

var (
	errDisabled     = fmt.Errorf("连接已关闭")
	errNoToken      = fmt.Errorf("尚未配置设备注册令牌")
	errConfigServer = fmt.Errorf("配置服务器地址无效")
	errCoreMissing  = fmt.Errorf("尚未安装 EasyTier 运行时")
)

// coreEnv builds the environment easytier-core expects.
func (m *Manager) coreEnv(settings config.Settings, token string) ([]string, error) {
	machineID, err := m.store.MachineID()
	if err != nil {
		return nil, err
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "Synology NAS"
	}
	env := append(os.Environ(),
		"ET_CONFIG_SERVER="+config.NormalizeConfigServer(settings.ConfigServer)+"/"+token,
		"ET_MACHINE_ID="+machineID,
		"ET_HOSTNAME="+hostname,
		"ET_RPC_PORTAL="+rpcPortalListen,
		"ET_RPC_PORTAL_WHITELIST="+rpcPortalWhitelist,
		"ET_CONSOLE_LOG_LEVEL=off",
	)
	return env, nil
}

// isExecutable reports whether path is a regular executable file.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// commandOutput runs a short-lived helper binary and returns trimmed stdout.
func commandOutput(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimRight(string(output), "\r\n"), err
}

// combinedCommandOutput merges stdout and stderr, which is how the management
// API reports "no running instances found" for a core that is up but has not
// joined a network yet.
func combinedCommandOutput(parent context.Context, timeout time.Duration, limit int64, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	buffer := &limitedBuffer{limit: limit}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = buffer
	command.Stderr = buffer
	err := command.Run()
	return strings.TrimRight(buffer.String(), "\r\n"), err
}
