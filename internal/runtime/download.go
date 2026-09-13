package runtime

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
)

// Limits ported from the OpenWrt client's download.sh.
const (
	downloadReserveBytes    = 4096 * 1024
	downloadMaxArchiveBytes = 262144 * 1024
	downloadMaxBinaryBytes  = 131072 * 1024
	downloadMaxListBytes    = 1024 * 1024
	downloadMaxVersionBytes = 64 * 1024
	downloadTransferTimeout = 930 * time.Second
	downloadExtractTimeout  = 120 * time.Second
	downloadListTimeout     = 30 * time.Second
	downloadVersionTimeout  = 15 * time.Second
	downloadRunTimeout      = 15 * time.Minute
	// downloadStableChecks is how many consecutive healthy polls a restarted
	// runtime must reach before an update is committed.
	downloadStableChecks = 5
	downloadStableWindow = 20
)

// Download phases, mirroring the OpenWrt client's progress reporting.
const (
	PhaseQueued    = "queued"
	PhaseRelease   = "release"
	PhaseDownload  = "download"
	PhaseVerify    = "verify"
	PhaseValidate  = "validate"
	PhaseInstall   = "install"
	PhaseRestart   = "restart"
	PhaseDone      = "done"
	PhaseFailed    = "failed"
	PhaseInterrupt = "interrupted"
)

// DownloadStatus is the persisted progress of a runtime update.
type DownloadStatus struct {
	State     string `json:"state"`
	Phase     string `json:"phase"`
	Percent   int    `json:"percent"`
	Message   string `json:"message,omitempty"`
	Version   string `json:"version,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

// updateTransaction records the binaries replaced by an update.
type updateTransaction struct {
	InstallDir string `json:"install_dir"`
	BackupCore string `json:"backup_core"`
	BackupCLI  string `json:"backup_cli"`
	HadCore    bool   `json:"had_core"`
	HadCLI     bool   `json:"had_cli"`
}

// downloadSource is one release mirror.
type downloadSource struct {
	name            string
	url             string
	downloadPercent int
	verifyPercent   int
}

var versionPattern = regexp.MustCompile(`^v[0-9][A-Za-z0-9._+-]{0,63}$`)

// downloadState serializes runtime updates.
type downloadState struct {
	m *Manager

	mu     sync.Mutex
	active bool
}

func newDownloadState(m *Manager) *downloadState {
	return &downloadState{m: m}
}

func (d *downloadState) begin() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active {
		return false
	}
	d.active = true
	return true
}

func (d *downloadState) finish() {
	d.mu.Lock()
	d.active = false
	d.mu.Unlock()
}

func (d *downloadState) running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active
}

// recoverStatus marks a status left behind by a previous daemon run as
// interrupted.
func (d *downloadState) recoverStatus() {
	status, err := d.read()
	if err != nil {
		return
	}
	if status.State != StateQueued && status.State != StateRunning {
		return
	}
	d.write(DownloadStatus{
		State:   StateFailed,
		Phase:   PhaseInterrupt,
		Message: "上次运行时更新被中断，可以重新尝试。",
		Version: status.Version,
	})
}

func (d *downloadState) read() (DownloadStatus, error) {
	data, err := os.ReadFile(d.m.paths.DownloadStatusFile())
	if err != nil {
		return DownloadStatus{}, err
	}
	var status DownloadStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return DownloadStatus{}, err
	}
	return status, nil
}

func (d *downloadState) write(status DownloadStatus) {
	status.UpdatedAt = time.Now().Unix()
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	if err := config.AtomicWrite(d.m.paths.DownloadStatusFile(), data, 0o600); err != nil {
		d.m.log.Errorf("写入更新状态失败: %v", err)
	}
}

// DownloadStart launches a runtime update in the background.
func (m *Manager) DownloadStart(requestedVersion string) (DownloadStatus, *apperr.Error) {
	if !m.dl.begin() {
		return DownloadStatus{}, apperr.New(apperr.CodeDownloadBusy)
	}
	status := DownloadStatus{State: StateRunning, Phase: PhaseQueued, Percent: 0, Message: "运行时更新已排队。"}
	m.dl.write(status)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), downloadRunTimeout)
		defer cancel()
		m.downloadRun(ctx, requestedVersion)
	}()
	return status, nil
}

// DownloadStatus reports the current update progress.
func (m *Manager) DownloadStatus() DownloadStatus {
	status, err := m.dl.read()
	if err != nil {
		return DownloadStatus{State: "idle", Phase: "idle"}
	}
	if (status.State == StateQueued || status.State == StateRunning) && !m.dl.running() {
		m.dl.write(DownloadStatus{
			State:   StateFailed,
			Phase:   PhaseInterrupt,
			Message: "上次运行时更新被中断，可以重新尝试。",
			Version: status.Version,
		})
		return m.mustReadDownloadStatus()
	}
	return status
}

func (m *Manager) mustReadDownloadStatus() DownloadStatus {
	status, err := m.dl.read()
	if err != nil {
		return DownloadStatus{State: "idle", Phase: "idle"}
	}
	return status
}

func (m *Manager) writeDownloadStatus(state, phase string, percent int, message, version string) {
	m.dl.write(DownloadStatus{State: state, Phase: phase, Percent: percent, Message: message, Version: version})
}

func (m *Manager) failDownload(phase string, percent int, message, version string) {
	m.writeDownloadStatus(StateFailed, phase, percent, message, version)
	m.log.Errorf("运行时更新失败: %s", message)
}

// archAssets maps the compiled architecture to the release asset names, most
// preferred first.
func archAssets(goarch string) []string {
	switch goarch {
	case "amd64":
		return []string{"x86_64"}
	case "arm64":
		return []string{"aarch64"}
	case "arm":
		return []string{"armv7hf", "armv7"}
	default:
		return nil
	}
}

// normalizeVersion accepts both v2.6.4 and 2.6.4 and rejects anything else.
func normalizeVersion(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value, versionPattern.MatchString(value)
}

// releaseSources lists the mirrors in the order the client tries them.
func releaseSources(version, asset string) []downloadSource {
	assetName := fmt.Sprintf("easytier-linux-%s-%s.zip", asset, version)
	return []downloadSource{
		{
			name:            "Gitee",
			url:             "https://gitee.com/EasyTier/EasyTier/releases/download/" + version + "/" + assetName,
			downloadPercent: 12,
			verifyPercent:   35,
		},
		{
			name:            "GitHub",
			url:             "https://github.com/EasyTier/EasyTier/releases/download/" + version + "/" + assetName,
			downloadPercent: 45,
			verifyPercent:   70,
		},
	}
}

// downloadRun performs one runtime update: resolve the release, download and
// validate the matching archive, then install it transactionally.
func (m *Manager) downloadRun(ctx context.Context, requested string) {
	defer m.dl.finish()

	m.writeDownloadStatus(StateRunning, PhaseRelease, 5, "正在读取稳定版本信息。", requested)
	release, aerr := m.cli.LatestRelease(ctx)
	if aerr != nil {
		m.failDownload(PhaseRelease, 5, "无法读取 EasyTier 稳定版本。", requested)
		return
	}
	version, ok := normalizeVersion(firstNonEmpty(requested, release.Stable.Version))
	if !ok {
		m.failDownload(PhaseRelease, 5, "EasyTier 版本号无效。", requested)
		return
	}
	arches := archAssets(runtime.GOARCH)
	if len(arches) == 0 {
		m.failDownload(PhaseRelease, 5, "本机 CPU 架构暂不受支持。", version)
		return
	}
	if err := m.paths.EnsureDirs(); err != nil {
		m.failDownload(PhaseInstall, 15, "无法创建运行目录。", version)
		return
	}
	stage := filepath.Join(m.paths.RuntimeDir(), fmt.Sprintf(".stage.%d", os.Getpid()))
	if err := os.RemoveAll(stage); err != nil {
		m.failDownload(PhaseInstall, 15, "无法清理临时目录。", version)
		return
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		m.failDownload(PhaseInstall, 15, "无法创建临时目录。", version)
		return
	}
	defer os.RemoveAll(stage)

	for _, assetArch := range arches {
		checksum, declaredSize, aerr := releaseArtifact(release, assetArch)
		if aerr != nil {
			m.failDownload(PhaseRelease, 5, aerr.Message, version)
			return
		}
		for _, source := range releaseSources(version, assetArch) {
			m.writeDownloadStatus(StateRunning, PhaseDownload, source.downloadPercent,
				fmt.Sprintf("正在从 %s 下载运行时。", source.name), version)
			candidate, err := m.prepareCandidate(ctx, source, stage, assetArch, version, checksum, declaredSize)
			if err != nil {
				m.log.Errorf("%s 下载或校验失败: %v", source.name, err)
				continue
			}
			if m.installRuntime(ctx, candidate.core, candidate.cli, version, source) {
				// The archive has been installed, so the retained copy has
				// served its purpose: keeping it would hold disk space for a
				// runtime that is already in place.
				m.discardCachedArchive(assetArch, version)
			}
			return
		}
	}
	m.failDownload(PhaseValidate, 78, "Gitee 与 GitHub 均下载或校验失败。", version)
}

// resolveArchive produces the archive to check: the retained copy of this
// artifact when one exists, and a fresh download otherwise. It reports whether
// the archive came from the cache.
//
// The cache is consulted before the download limit on purpose. That limit only
// decides whether a download may start, and a retained archive needs none, so
// asking it first would let an archive too large for the current free space
// block the very update it was kept for - and the lookup that discards a bad
// entry is exactly what applying the limit first would skip.
func (m *Manager) resolveArchive(ctx context.Context, source downloadSource, stage, assetArch, version string, declaredSize, limit int64) (string, bool, error) {
	if cached, ok := m.cachedArchive(assetArch, version, declaredSize); ok {
		m.log.Printf("使用本机缓存的运行时归档，跳过下载")
		m.writeDownloadStatus(StateRunning, PhaseDownload, source.downloadPercent,
			"正在使用本机缓存的运行时归档。", version)
		return cached, true, nil
	}
	archive, err := m.downloadArchive(ctx, source, stage, assetArch, version, declaredSize, limit)
	return archive, false, err
}

// downloadArchive fetches one archive into the staging directory, which is left
// to the caller to remove.
func (m *Manager) downloadArchive(ctx context.Context, source downloadSource, stage, assetArch, version string, declaredSize, limit int64) (string, error) {
	archive := filepath.Join(stage, assetArch, archiveCacheName(assetArch, version))
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		return "", err
	}
	if declaredSize > 0 && declaredSize > limit {
		return "", errors.New("the runtime archive exceeds the safe download limit")
	}
	if err := m.fetchArchive(ctx, source.url, archive, limit); err != nil {
		return "", err
	}
	return archive, nil
}

// candidate is a validated, extracted runtime pair.
type candidate struct {
	core string
	cli  string
}

// releaseArtifact reads the optional checksum and size for one asset.
//
// The two are read independently: a Console that publishes a size without a
// checksum still describes the artifact, and that size is the only identity
// check available to a retained archive, so losing it would leave the archive
// unverified in every way.
func releaseArtifact(release console.Release, assetArch string) (string, int64, *apperr.Error) {
	artifact, ok := release.Stable.Artifacts["linux-"+assetArch]
	if !ok {
		return "", 0, nil
	}
	checksum := strings.ToLower(strings.TrimSpace(artifact.SHA256))
	if checksum != "" {
		if len(checksum) != 64 {
			return "", 0, apperr.New(apperr.CodeInvalidConsoleResponse)
		}
		if _, err := hex.DecodeString(checksum); err != nil {
			return "", 0, apperr.New(apperr.CodeInvalidConsoleResponse)
		}
	}
	size, err := artifact.Size.Int64()
	if err != nil || size <= 0 {
		if checksum == "" {
			// Nothing usable was published for this asset, which is the normal
			// case for a Console without artifacts.
			return "", 0, nil
		}
		return "", 0, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	return checksum, size, nil
}

// prepareCandidate produces a runtime pair this host accepts, from the retained
// archive when there is one and from a fresh download otherwise.
//
// A retained file that no longer matches what was published is replaced by a
// download in the same call, because it would otherwise fail every later
// attempt in exactly the same way. Only that kind of failure is retried: the
// rest - no free space, an unwritable directory, a binary that will not run -
// say nothing about the file, and downloading the same bytes again would cost
// the transfer this cache exists to save. The fresh copy is fetched before the
// retained one is touched, so a download that fails leaves the cache as it was
// rather than emptying it.
func (m *Manager) prepareCandidate(ctx context.Context, source downloadSource, stage, assetArch, version, checksum string, declaredSize int64) (candidate, error) {
	limit, err := m.archiveLimit()
	if err != nil {
		return candidate{}, err
	}
	archive, retained, err := m.resolveArchive(ctx, source, stage, assetArch, version, declaredSize, limit)
	if err != nil {
		return candidate{}, err
	}
	result, err := m.buildCandidate(ctx, source, archive, stage, assetArch, version, checksum, declaredSize)
	if err == nil || !retained || !errors.Is(err, errArchiveUnusable) {
		return result, err
	}
	m.log.Errorf("本机缓存的运行时归档不可用，将重新下载: %v", err)
	fresh, err := m.downloadArchive(ctx, source, stage, assetArch, version, declaredSize, limit)
	if err != nil {
		return candidate{}, err
	}
	return m.buildCandidate(ctx, source, fresh, stage, assetArch, version, checksum, declaredSize)
}

// errArchiveUnusable marks a failure that says the file itself does not match
// what was published, and which a fresh download may therefore fix. Failures of
// the environment around the archive are deliberately not marked.
var errArchiveUnusable = errors.New("the runtime archive is unusable")

// buildCandidate checks one archive and extracts the runtime pair from it.
func (m *Manager) buildCandidate(ctx context.Context, source downloadSource, archive, stage, assetArch, version, checksum string, declaredSize int64) (candidate, error) {
	size, err := fileSize(archive)
	if err != nil {
		return candidate{}, err
	}
	if size <= 0 {
		return candidate{}, fmt.Errorf("%w: empty archive", errArchiveUnusable)
	}
	if declaredSize > 0 && size != declaredSize {
		return candidate{}, fmt.Errorf("%w: size does not match the release metadata", errArchiveUnusable)
	}
	if err := m.requireFreeSpace(size + 2*1024*1024); err != nil {
		return candidate{}, err
	}

	m.writeDownloadStatus(StateRunning, PhaseVerify, source.verifyPercent,
		fmt.Sprintf("正在校验来自 %s 的下载。", source.name), version)
	if checksum != "" {
		actual, err := fileSHA256(archive)
		if err != nil {
			return candidate{}, err
		}
		if actual != checksum {
			return candidate{}, fmt.Errorf("%w: checksum mismatch", errArchiveUnusable)
		}
	} else {
		if !strings.HasPrefix(source.url, "https://") {
			return candidate{}, errors.New("refusing an unverified download over plain HTTP")
		}
		m.log.Printf("Console 未提供 %s 的校验和，改用本地校验", version)
	}
	// The archive is authentic, so it is worth keeping for a retry. Only an
	// archive whose checksum was published can be cached.
	members, err := m.archiveMembers(archive)
	if err != nil {
		return candidate{}, fmt.Errorf("%w: %v", errArchiveUnusable, err)
	}
	core, cli := runtimeMembers(members, assetArch)
	if core == "" || cli == "" {
		return candidate{}, fmt.Errorf("%w: it does not contain exactly one easytier-core and easytier-cli", errArchiveUnusable)
	}
	extracted := filepath.Join(stage, assetArch, "extracted")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		return candidate{}, err
	}
	corePath := filepath.Join(extracted, "easytier-core")
	cliPath := filepath.Join(extracted, "easytier-cli")
	if err := m.extractMember(archive, core, corePath); err != nil {
		return candidate{}, err
	}
	if err := m.extractMember(archive, cli, cliPath); err != nil {
		return candidate{}, err
	}
	if err := os.Chmod(corePath, 0o755); err != nil {
		return candidate{}, err
	}
	if err := os.Chmod(cliPath, 0o755); err != nil {
		return candidate{}, err
	}
	if !binaryMatchesVersion(ctx, corePath, version) {
		return candidate{}, errors.New("easytier-core does not report the expected version")
	}
	if !binaryMatchesVersion(ctx, cliPath, version) {
		return candidate{}, errors.New("easytier-cli does not report the expected version")
	}
	// Kept only now that this host accepted the archive, so a build this device
	// rejects - the armv7hf one on a soft-float host, for instance - never
	// replaces the archive that does work.
	m.storeArchive(archive, assetArch, version)
	return candidate{core: corePath, cli: cliPath}, nil
}

// installRuntime swaps the validated binaries into place with rollback.
// installRuntime swaps the validated binaries into place with rollback. It
// reports whether the runtime is now installed.
func (m *Manager) installRuntime(ctx context.Context, corePath, cliPath, version string, source downloadSource) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.writeDownloadStatus(StateRunning, PhaseInstall, 84, "正在安装已校验的运行时。", version)
	if err := m.paths.EnsureDirs(); err != nil {
		m.failDownload(PhaseInstall, 84, "无法创建运行目录。", version)
		return false
	}
	transaction := updateTransaction{
		InstallDir: m.paths.RuntimeDir(),
		BackupCore: filepath.Join(m.paths.RuntimeDir(), fmt.Sprintf(".easytier-core.previous.%d", os.Getpid())),
		BackupCLI:  filepath.Join(m.paths.RuntimeDir(), fmt.Sprintf(".easytier-cli.previous.%d", os.Getpid())),
		HadCore:    fileExists(m.paths.CoreBinary()),
		HadCLI:     fileExists(m.paths.CLIbinary()),
	}
	if err := m.writeUpdateTransaction(transaction); err != nil {
		m.failDownload(PhaseInstall, 84, "无法开始运行时更新事务。", version)
		return false
	}
	rollback := func(reason string) {
		if err := m.recoverUpdateFilesLocked(ctx); err != nil {
			m.log.Errorf("运行时回滚失败: %v", err)
		}
		m.failDownload(PhaseInstall, 84, reason, version)
	}
	m.core.StopAndWait(ctx)
	if transaction.HadCore {
		if err := os.Rename(m.paths.CoreBinary(), transaction.BackupCore); err != nil {
			rollback("运行时安装失败，已恢复旧版本。")
			return false
		}
	}
	if transaction.HadCLI {
		if err := os.Rename(m.paths.CLIbinary(), transaction.BackupCLI); err != nil {
			rollback("运行时安装失败，已恢复旧版本。")
			return false
		}
	}
	if err := os.Rename(corePath, m.paths.CoreBinary()); err != nil {
		rollback("运行时安装失败，已恢复旧版本。")
		return false
	}
	if err := os.Rename(cliPath, m.paths.CLIbinary()); err != nil {
		rollback("运行时安装失败，已恢复旧版本。")
		return false
	}
	if err := os.Chmod(m.paths.CoreBinary(), 0o755); err != nil {
		rollback("运行时权限设置失败，已恢复旧版本。")
		return false
	}
	if err := os.Chmod(m.paths.CLIbinary(), 0o755); err != nil {
		rollback("运行时权限设置失败，已恢复旧版本。")
		return false
	}

	settings, err := m.store.Settings()
	if err != nil {
		rollback("运行时安装失败，已恢复旧版本。")
		return false
	}
	if settings.Enabled && m.store.HasBootstrapToken() {
		// The replaced binary does not inherit a file capability granted to the
		// old one, so what this device is able to do may have just changed.
		// Negotiate the mode before the core restarts, otherwise the Console
		// keeps pushing a configuration the new binary cannot honour.
		syncCtx, cancelSync := context.WithTimeout(ctx, relaySyncTimeout)
		m.syncRelayModeQuietly(syncCtx)
		cancelSync()
		m.writeDownloadStatus(StateRunning, PhaseRestart, 92, "正在重启 EasyTier Pro。", version)
		m.core.ResetBackoff()
		if !m.waitForStableCore(ctx) {
			rollback("新运行时未能启动，已恢复旧版本。")
			return false
		}
	}
	if err := m.commitUpdateTransaction(transaction); err != nil {
		rollback("无法提交运行时更新事务，已恢复旧版本。")
		return false
	}
	m.writeDownloadStatus(StateCompleted, PhaseDone, 100, "EasyTier 运行时安装完成。", version)
	m.log.Printf("EasyTier 运行时应更新到 %s（%s）", version, source.name)
	return true
}

// waitForStableCore waits until the management API answers repeatedly.
func (m *Manager) waitForStableCore(ctx context.Context) bool {
	m.core.SetDesired(true)
	stable := 0
	lastPID := 0
	attempts := 0
	healthyChecks := 0
	for attempt := 0; attempt < downloadStableWindow; attempt++ {
		attempts = attempt + 1
		if ctx.Err() != nil {
			break
		}
		pid := m.core.pid()
		if pid > 0 && m.apiHealthy(ctx) {
			healthyChecks++
			if pid == lastPID {
				stable++
			} else {
				lastPID = pid
				stable = 1
			}
			if stable >= downloadStableChecks {
				return true
			}
		} else {
			stable = 0
			lastPID = 0
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	m.log.Errorf("等待新运行时稳定失败：尝试 %d 次，健康检查通过 %d 次，最后 PID %d", attempts, healthyChecks, lastPID)
	return false
}

func (m *Manager) archiveLimit() (int64, error) {
	available, err := m.freeSpace()
	if err != nil {
		return 0, err
	}
	if available <= downloadReserveBytes {
		return 0, errors.New("not enough free space for a runtime update")
	}
	limit := (available - downloadReserveBytes) / 2
	if limit > downloadMaxArchiveBytes {
		limit = downloadMaxArchiveBytes
	}
	if limit <= 0 {
		return 0, errors.New("not enough free space for a runtime update")
	}
	return limit, nil
}

func (m *Manager) freeSpace() (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(m.paths.PkgVar, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func (m *Manager) requireFreeSpace(required int64) error {
	available, err := m.freeSpace()
	if err != nil {
		return err
	}
	if available < required {
		return errors.New("not enough free space for a runtime update")
	}
	return nil
}

// fetchArchive downloads one release asset with a hard size limit.
func (m *Manager) fetchArchive(ctx context.Context, url, target string, maxBytes int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := downloadHTTPClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", response.StatusCode)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return errors.New("the runtime archive exceeds the safe download limit")
	}
	return file.Sync()
}

func downloadHTTPClient() *http.Client {
	return &http.Client{
		Timeout: downloadTransferTimeout,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if next.URL.Scheme != "https" {
				return errors.New("refusing to follow a redirect to an insecure scheme")
			}
			return nil
		},
	}
}

// archiveMembers lists the archive members with a size cap.
func (m *Manager) archiveMembers(path string) ([]string, error) {
	names := []string{}
	var total int64
	err := m.withZip(path, func(reader *zip.ReadCloser) error {
		for _, file := range reader.File {
			name := file.Name
			total += int64(len(name)) + 1
			if total > downloadMaxListBytes {
				return errors.New("the archive member list is too large")
			}
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("empty archive member list")
	}
	return names, nil
}

// runtimeMembers returns the core and cli members, requiring exactly one each.
func runtimeMembers(names []string, assetArch string) (string, string) {
	core := ""
	cli := ""
	coreCount := 0
	cliCount := 0
	for _, name := range names {
		switch name {
		case fmt.Sprintf("easytier-linux-%s/easytier-core", assetArch):
			core = name
			coreCount++
		case fmt.Sprintf("easytier-linux-%s/easytier-cli", assetArch):
			cli = name
			cliCount++
		}
	}
	if coreCount != 1 || cliCount != 1 {
		return "", ""
	}
	return core, cli
}

// extractMember writes one archive member to target with a size cap.
func (m *Manager) extractMember(archive, member, target string) error {
	available, err := m.freeSpace()
	if err != nil {
		return err
	}
	if available <= downloadReserveBytes {
		return errors.New("not enough free space to extract the runtime")
	}
	limit := available - downloadReserveBytes
	if limit > downloadMaxBinaryBytes {
		limit = downloadMaxBinaryBytes
	}
	return m.withZip(archive, func(reader *zip.ReadCloser) error {
		for _, file := range reader.File {
			if file.Name != member {
				continue
			}
			if file.UncompressedSize64 > uint64(limit) {
				return errors.New("the runtime binary exceeds the safe size limit")
			}
			source, err := file.Open()
			if err != nil {
				return err
			}
			defer source.Close()
			output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			defer output.Close()
			written, err := io.Copy(output, io.LimitReader(source, limit+1))
			if err != nil {
				return err
			}
			if written > limit {
				return errors.New("the runtime binary exceeds the safe size limit")
			}
			if written == 0 {
				return errors.New("the runtime binary is empty")
			}
			return output.Sync()
		}
		return fmt.Errorf("member %s is missing", member)
	})
}

func (m *Manager) withZip(path string, fn func(*zip.ReadCloser) error) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	return fn(reader)
}

// binaryMatchesVersion reports whether a runtime binary reports version.
func binaryMatchesVersion(ctx context.Context, binary, version string) bool {
	output, err := limitedCommandOutput(ctx, downloadVersionTimeout, downloadMaxVersionBytes, binary, "--version")
	if err != nil && output == "" {
		return false
	}
	return versionReports(output, version)
}

// versionReports reports whether a runtime's version output is the release the
// caller expected.
//
// A runtime reports more than the release tag - the real answer is
// "easytier-core 2.6.4-8428a89d" for the release "v2.6.4", with the build's
// commit appended - so the tag is matched as a whole rather than compared as a
// string. What follows it has to be the end of the answer or a separator, which
// is what keeps "2.6.40" from passing for "2.6.4": without that, a download of
// the wrong release would be accepted, and an installed release would be called
// up to date when the Console offers a different one.
//
// Every question of the form "is this runtime that release?" goes through here.
// Two definitions of it would eventually disagree, and the disagreement would be
// a device that refuses a good download or never offers an update it needs.
func versionReports(output, version string) bool {
	expected := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if expected == "" {
		return false
	}
	first := output
	if index := strings.IndexByte(first, '\n'); index >= 0 {
		first = first[:index]
	}
	index := strings.Index(first, " "+expected)
	if index < 0 {
		return false
	}
	rest := first[index+1+len(expected):]
	return rest == "" || rest[0] == '-' || rest[0] == '+' || rest[0] == ' '
}

// limitedBuffer caps how much command output is kept in memory.
type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int64
	truncated bool
}

func (l *limitedBuffer) Write(data []byte) (int, error) {
	remaining := l.limit - int64(l.buffer.Len())
	if remaining <= 0 {
		l.truncated = true
		return len(data), nil
	}
	if int64(len(data)) > remaining {
		l.buffer.Write(data[:remaining])
		l.truncated = true
		return len(data), nil
	}
	l.buffer.Write(data)
	return len(data), nil
}

func (l *limitedBuffer) String() string { return l.buffer.String() }

func limitedCommandOutput(ctx context.Context, timeout time.Duration, limit int64, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buffer := &limitedBuffer{limit: limit}
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Stdout = buffer
	cmd.Stderr = nil
	err := cmd.Run()
	return strings.TrimRight(buffer.String(), "\r\n"), err
}

// writeUpdateTransaction records the files an update is replacing.
func (m *Manager) writeUpdateTransaction(transaction updateTransaction) error {
	data, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	return config.AtomicWrite(m.paths.UpdateTransactionFile(), data, 0o600)
}

// commitUpdateTransaction drops the rollback data of a finished update. The
// runtime directory and the transaction removal are flushed first, so a power
// loss cannot resurrect a transaction whose backups are already gone.
func (m *Manager) commitUpdateTransaction(transaction updateTransaction) error {
	config.SyncDir(m.paths.RuntimeDir())
	if err := os.Remove(m.paths.UpdateTransactionFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config.SyncDir(m.paths.StateDir())
	os.Remove(transaction.BackupCore)
	os.Remove(transaction.BackupCLI)
	return nil
}

// recoverUpdateFiles restores the binaries an interrupted update replaced.
func (m *Manager) recoverUpdateFiles(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recoverUpdateFilesLocked(ctx)
}

func (m *Manager) recoverUpdateFilesLocked(ctx context.Context) error {
	path := m.paths.UpdateTransactionFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var transaction updateTransaction
	if err := json.Unmarshal(data, &transaction); err != nil {
		return err
	}
	if transaction.InstallDir != m.paths.RuntimeDir() ||
		!validBackupName(transaction.BackupCore, ".easytier-core.previous.") ||
		!validBackupName(transaction.BackupCLI, ".easytier-cli.previous.") {
		return errors.New("update transaction has unsafe paths")
	}
	m.log.Printf("回滚未完成的运行时更新")
	m.core.StopAndWait(ctx)
	if err := restoreBinary(m.paths.CoreBinary(), transaction.BackupCore, transaction.HadCore); err != nil {
		return err
	}
	if err := restoreBinary(m.paths.CLIbinary(), transaction.BackupCLI, transaction.HadCLI); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	os.Remove(transaction.BackupCore)
	os.Remove(transaction.BackupCLI)
	settings, err := m.store.Settings()
	if err != nil {
		return err
	}
	if transaction.HadCore && settings.Enabled && m.store.HasBootstrapToken() {
		m.core.ResetBackoff()
		m.core.EnsureHealthy(ctx)
	}
	return nil
}

func restoreBinary(current, backup string, hadPrevious bool) error {
	if hadPrevious {
		if fileExists(backup) {
			if err := os.Remove(current); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return os.Rename(backup, current)
		}
		if !fileExists(current) {
			return errors.New("the previous runtime binary is missing")
		}
		return nil
	}
	os.Remove(current)
	os.Remove(backup)
	return nil
}

// validBackupName confines update backups to the runtime directory.
func validBackupName(path, prefix string) bool {
	if path == "" {
		return false
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(base, prefix)
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// cleanupOrphanRuntimeFiles removes staging and backup leftovers of updates
// that are no longer tracked by a transaction.
func (m *Manager) cleanupOrphanRuntimeFiles() error {
	if fileExists(m.paths.UpdateTransactionFile()) {
		return errors.New("an update transaction is still pending")
	}
	entries, err := os.ReadDir(m.paths.RuntimeDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, ".stage."):
			os.RemoveAll(filepath.Join(m.paths.RuntimeDir(), name))
		case strings.HasPrefix(name, ".easytier-core.previous."), strings.HasPrefix(name, ".easytier-cli.previous."):
			os.Remove(filepath.Join(m.paths.RuntimeDir(), name))
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
