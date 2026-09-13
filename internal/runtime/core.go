package runtime

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Respawn behaviour of the local EasyTier core.
const (
	restartBackoffMin = 1 * time.Second
	restartBackoffMax = 60 * time.Second
	// restartHealthyRun is how long a core must stay up before its next exit
	// is treated as a fresh failure rather than a flapping restart.
	restartHealthyRun = 60 * time.Second
	// unstartableRetry is how long the supervisor waits before re-testing a
	// configuration that cannot start at all.
	unstartableRetry = 30 * time.Second
	// staleCoreGrace is how long a leftover core from a previous run may take
	// to exit before it is killed.
	staleCoreGrace = 5 * time.Second
)

// coreSupervisor keeps easytier-core running for as long as the local
// configuration asks for it, restarting it with a bounded backoff.
type coreSupervisor struct {
	m   *Manager
	ctx context.Context

	wake chan struct{}

	mu      sync.Mutex
	desired bool
	proc    *os.Process
	exited  chan struct{}
	backoff time.Duration
}

func newCoreSupervisor(m *Manager, ctx context.Context) *coreSupervisor {
	return &coreSupervisor{m: m, ctx: ctx, wake: make(chan struct{}, 1), backoff: restartBackoffMin}
}

// Start launches the supervision loop.
func (s *coreSupervisor) Start() {
	go s.run()
}

// SetDesired declares whether the core should be running.
func (s *coreSupervisor) SetDesired(desired bool) {
	s.mu.Lock()
	changed := s.desired != desired
	s.desired = desired
	s.mu.Unlock()
	if changed {
		s.signal()
	}
}

// pid returns the supervised process id, or 0 when no core runs.
func (s *coreSupervisor) pid() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return 0
	}
	return s.proc.Pid
}

// Running reports whether a core process is currently supervised.
func (s *coreSupervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc != nil
}

func (s *coreSupervisor) isDesired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desired
}

func (s *coreSupervisor) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *coreSupervisor) setProcess(proc *os.Process, exited chan struct{}) {
	s.mu.Lock()
	s.proc = proc
	s.exited = exited
	s.mu.Unlock()
}

func (s *coreSupervisor) clearProcess() {
	s.mu.Lock()
	s.proc = nil
	s.exited = nil
	s.mu.Unlock()
}

func (s *coreSupervisor) run() {
	for {
		if s.ctx.Err() != nil {
			return
		}
		if !s.isDesired() {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			}
			continue
		}
		cmd, err := s.startProcess()
		if err != nil {
			s.m.log.Errorf("EasyTier core 未启动: %v", err)
			s.sleep(unstartableRetry)
			continue
		}
		s.m.log.Printf("EasyTier core 已启动，PID %d", cmd.Process.Pid)
		exited := make(chan struct{})
		waited := make(chan error, 1)
		go func() {
			waited <- cmd.Wait()
			close(exited)
		}()
		s.setProcess(cmd.Process, exited)
		startedAt := time.Now()
		if !s.isDesired() {
			// A stop ran while the child was being created.
			s.terminate(cmd.Process)
		} else if !s.awaitHealthy(exited) {
			// The core is alive but its management API never answered;
			// replace it instead of holding it forever.
			s.m.log.Errorf("EasyTier core 未在预期时间内就绪，重新启动")
			s.terminate(cmd.Process)
		}
		waitErr := <-waited
		s.clearProcess()
		s.m.removeCorePID()

		if s.ctx.Err() != nil {
			return
		}
		if !s.isDesired() {
			s.m.log.Printf("EasyTier core 已停止")
			continue
		}
		s.m.log.Errorf("EasyTier core 已退出: %v", waitErr)
		delay := s.nextBackoff(time.Since(startedAt))
		s.m.log.Printf("%s 后重新启动 EasyTier core", delay)
		s.sleep(delay)
	}
}

// startProcess spawns the core with the environment the Console contract
// requires. Its output is discarded: the configuration URL carries the
// enrollment token.
func (s *coreSupervisor) startProcess() (*exec.Cmd, error) {
	if err := s.m.coreStartBlocker(); err != nil {
		return nil, err
	}
	settings, err := s.m.store.Settings()
	if err != nil {
		return nil, err
	}
	token, err := s.m.store.ReadBootstrapToken()
	if err != nil {
		return nil, err
	}
	env, err := s.m.coreEnv(settings, token)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(s.m.paths.CoreBinary(), coreArgs()...)
	cmd.Env = env
	cmd.Dir = s.m.paths.RuntimeDir()
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s.m.writeCorePID(cmd.Process.Pid)
	return cmd, nil
}

// coreArgs builds the command line of the core.
//
// Relay mode is deliberately absent: in secure mode the core runs no network of
// its own and every instance comes from the Console, so a --no-tun flag here
// would be ignored. The mode is negotiated with the Console instead (see
// Manager.SyncRelayMode).
func coreArgs() []string {
	return []string{"--secure-mode=true"}
}

// awaitHealthy reports whether the management API became reachable before the
// process exited or the attempt budget ran out.
func (s *coreSupervisor) awaitHealthy(exited <-chan struct{}) bool {
	for attempt := 0; attempt < healthAttempts; attempt++ {
		if s.m.apiHealthy(s.ctx) {
			return true
		}
		select {
		case <-exited:
			return false
		case <-s.ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// terminate stops one child, escalating to SIGKILL.
func (s *coreSupervisor) terminate(proc *os.Process) {
	if !processAlive(proc) {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(closeTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(proc) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = proc.Kill()
}

// EnsureHealthy asks the supervisor to run the core and waits until the
// management API answers.
func (s *coreSupervisor) EnsureHealthy(ctx context.Context) bool {
	s.SetDesired(true)
	for attempt := 0; attempt < healthAttempts; attempt++ {
		if ctx.Err() != nil {
			return false
		}
		if s.Running() && s.m.apiHealthy(ctx) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// StopAndWait stops the core, escalating to SIGKILL when it does not exit.
func (s *coreSupervisor) StopAndWait(ctx context.Context) {
	s.mu.Lock()
	s.desired = false
	proc := s.proc
	exited := s.exited
	s.mu.Unlock()
	s.signal()
	if proc == nil || exited == nil {
		// The supervisor may be publishing a child right now; wait for its
		// loop to notice that the core is no longer desired.
		deadline := time.Now().Add(closeTimeout)
		for time.Now().Before(deadline) && s.Running() {
			time.Sleep(100 * time.Millisecond)
		}
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	select {
	case <-exited:
		return
	case <-time.After(closeTimeout):
	case <-ctx.Done():
	}
	_ = proc.Kill()
	select {
	case <-exited:
	case <-time.After(closeTimeout):
	case <-ctx.Done():
	}
}

// ResetBackoff forgets accumulated restart delays after a deliberate change.
func (s *coreSupervisor) ResetBackoff() {
	s.mu.Lock()
	s.backoff = restartBackoffMin
	s.mu.Unlock()
}

func (s *coreSupervisor) nextBackoff(runFor time.Duration) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runFor >= restartHealthyRun {
		s.backoff = restartBackoffMin
	}
	delay := s.backoff
	if s.backoff < restartBackoffMax {
		s.backoff *= 2
		if s.backoff > restartBackoffMax {
			s.backoff = restartBackoffMax
		}
	}
	return delay
}

func (s *coreSupervisor) sleep(duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
	case <-s.wake:
	case <-timer.C:
	}
}

// processAlive reports whether the process still runs. A child that exited but
// has not been reaped yet is a zombie and does not count as running.
func processAlive(proc *os.Process) bool {
	if proc == nil {
		return false
	}
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(proc.Pid) + "/stat"); err == nil {
		text := string(data)
		if index := strings.LastIndex(text, ")"); index >= 0 {
			if fields := strings.Fields(text[index+1:]); len(fields) > 0 && fields[0] == "Z" {
				return false
			}
		}
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// apiHealthy reports whether the core is up and its management portal is
// accepting connections.
//
// It deliberately says nothing about the network instances the core runs. The
// previous implementation asked easytier-cli for node information and treated
// anything but "no running instances found" as a failure, so one instance whose
// configuration the device cannot honour - an instance created with TUN enabled
// on a host that may not create it - made the whole core look dead. That turned
// into failed runtime installs and rolled back connection changes, even though
// the core process was running and serving. Instance problems are diagnosed and
// repaired through the Console instead (see Manager.SyncRelayMode).
func (m *Manager) apiHealthy(ctx context.Context) bool {
	dialCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", rpcPortalAddress)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *Manager) writeCorePID(pid int) {
	if err := os.WriteFile(m.paths.CorePIDFile(), []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		m.log.Errorf("记录 EasyTier core PID 失败: %v", err)
	}
}

func (m *Manager) removeCorePID() {
	os.Remove(m.paths.CorePIDFile())
}

// stopStaleCore kills a core that outlived a previous daemon run.
func (m *Manager) stopStaleCore() {
	data, err := os.ReadFile(m.paths.CorePIDFile())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		os.Remove(m.paths.CorePIDFile())
		return
	}
	if !m.coreProcessMatches(pid) {
		os.Remove(m.paths.CorePIDFile())
		return
	}
	m.log.Printf("停止上次残留的 EasyTier core（PID %d）", pid)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(staleCoreGrace)
	for time.Now().Before(deadline) {
		if !m.coreProcessMatches(pid) {
			os.Remove(m.paths.CorePIDFile())
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	os.Remove(m.paths.CorePIDFile())
}

// coreProcessMatches reports whether pid is still our easytier-core.
func (m *Manager) coreProcessMatches(pid int) bool {
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), m.paths.CoreBinary())
}
