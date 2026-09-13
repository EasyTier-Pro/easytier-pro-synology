package runtime

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{PkgDest: filepath.Join(root, "target"), PkgVar: filepath.Join(root, "var")}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	logger, err := config.NewLogger(paths.DaemonLogFile(), false)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(logger.Close)
	store := config.NewStore(paths)
	manager := NewManager(paths, store, console.New(store, logger), logger)
	// The supervisor is not started: stopping a core that never ran is a no-op.
	manager.core = newCoreSupervisor(manager, context.Background())
	return manager
}

func TestArchAssets(t *testing.T) {
	cases := []struct {
		goarch string
		want   []string
	}{
		{"amd64", []string{"x86_64"}},
		{"arm64", []string{"aarch64"}},
		{"arm", []string{"armv7hf", "armv7"}},
		{"riscv64", nil},
	}
	for _, testCase := range cases {
		got := archAssets(testCase.goarch)
		if len(got) != len(testCase.want) {
			t.Fatalf("archAssets(%q) = %v, want %v", testCase.goarch, got, testCase.want)
		}
		for index := range got {
			if got[index] != testCase.want[index] {
				t.Fatalf("archAssets(%q) = %v, want %v", testCase.goarch, got, testCase.want)
			}
		}
	}
}

func TestReleaseSourceURLsAndVersionNormalization(t *testing.T) {
	sources := releaseSources("v2.6.4", "aarch64")
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(sources))
	}
	if sources[0].name != "Gitee" || sources[1].name != "GitHub" {
		t.Fatalf("unexpected source order: %v", sources)
	}
	wantURL := "https://gitee.com/EasyTier/EasyTier/releases/download/v2.6.4/easytier-linux-aarch64-v2.6.4.zip"
	if sources[0].url != wantURL {
		t.Fatalf("gitee url = %q, want %q", sources[0].url, wantURL)
	}
	if !strings.HasPrefix(sources[1].url, "https://github.com/EasyTier/EasyTier/releases/download/v2.6.4/") {
		t.Fatalf("github url = %q", sources[1].url)
	}
	if sources[0].downloadPercent != 12 || sources[1].downloadPercent != 45 {
		t.Fatalf("download percentages = %d/%d, want 12/45", sources[0].downloadPercent, sources[1].downloadPercent)
	}

	valid := map[string]string{"v2.6.4": "v2.6.4", "2.6.4": "v2.6.4", " v2.6.4-rc1 ": "v2.6.4-rc1"}
	for input, want := range valid {
		got, ok := normalizeVersion(input)
		if !ok || got != want {
			t.Fatalf("normalizeVersion(%q) = %q/%v, want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "latest", "v", "vx.y.z", "v2.6.4; rm -rf /", strings.Repeat("v1", 40)} {
		if got, ok := normalizeVersion(input); ok {
			t.Fatalf("normalizeVersion(%q) = %q, want rejection", input, got)
		}
	}
}

func writeArchive(t *testing.T, path string, members map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for name, content := range members {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create member %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write member %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

func TestArchiveMemberWhitelistAllowsExtraFiles(t *testing.T) {
	manager := newTestManager(t)
	archive := filepath.Join(t.TempDir(), "asset.zip")
	writeArchive(t, archive, map[string]string{
		"easytier-linux-x86_64/":              "",
		"easytier-linux-x86_64/easytier-core": "core-binary",
		"easytier-linux-x86_64/easytier-cli":  "cli-binary",
		"easytier-linux-x86_64/easytier-web":  "web-binary",
		"easytier-core":                       "unprefixed",
	})
	names, err := manager.archiveMembers(archive)
	if err != nil {
		t.Fatalf("archiveMembers: %v", err)
	}
	core, cli := runtimeMembers(names, "x86_64")
	if core == "" || cli == "" {
		t.Fatalf("members rejected: core=%q cli=%q names=%v", core, cli, names)
	}
	target := filepath.Join(t.TempDir(), "easytier-core")
	if err := manager.extractMember(archive, core, target); err != nil {
		t.Fatalf("extractMember: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read extracted member: %v", err)
	}
	if string(content) != "core-binary" {
		t.Fatalf("extracted content = %q", content)
	}
}

func TestArchiveMemberWhitelistRejectsIncompleteArchive(t *testing.T) {
	manager := newTestManager(t)
	archive := filepath.Join(t.TempDir(), "asset.zip")
	writeArchive(t, archive, map[string]string{
		"easytier-linux-x86_64/easytier-core": "core-binary",
	})
	names, err := manager.archiveMembers(archive)
	if err != nil {
		t.Fatalf("archiveMembers: %v", err)
	}
	if core, cli := runtimeMembers(names, "x86_64"); core != "" || cli != "" {
		t.Fatalf("archive without easytier-cli was accepted: core=%q cli=%q", core, cli)
	}

	// Only the exact member names count; nested paths are different members.
	nested := filepath.Join(t.TempDir(), "nested.zip")
	writeArchive(t, nested, map[string]string{
		"easytier-linux-x86_64/easytier-core":        "one",
		"easytier-linux-x86_64/easytier-cli":         "two",
		"nested/easytier-linux-x86_64/easytier-core": "three",
	})
	names, err = manager.archiveMembers(nested)
	if err != nil {
		t.Fatalf("archiveMembers: %v", err)
	}
	core, cli := runtimeMembers(names, "x86_64")
	if core != "easytier-linux-x86_64/easytier-core" || cli != "easytier-linux-x86_64/easytier-cli" {
		t.Fatalf("nested member changed the match: core=%q cli=%q", core, cli)
	}

	// A member listed twice is ambiguous and must be rejected.
	if core, cli := runtimeMembers([]string{
		"easytier-linux-x86_64/easytier-core",
		"easytier-linux-x86_64/easytier-core",
		"easytier-linux-x86_64/easytier-cli",
	}, "x86_64"); core != "" || cli != "" {
		t.Fatalf("duplicate members accepted: core=%q cli=%q", core, cli)
	}
}

func TestInstallFailureRestoresPreviousRuntime(t *testing.T) {
	manager := newTestManager(t)
	if err := os.WriteFile(manager.paths.CoreBinary(), []byte("old-core"), 0o755); err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := os.WriteFile(manager.paths.CLIbinary(), []byte("old-cli"), 0o755); err != nil {
		t.Fatalf("seed cli: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	manager.installRuntime(context.Background(), filepath.Join(missing, "core"), filepath.Join(missing, "cli"),
		"v2.6.4", downloadSource{name: "Gitee"})

	core, err := os.ReadFile(manager.paths.CoreBinary())
	if err != nil {
		t.Fatalf("core binary is gone: %v", err)
	}
	if string(core) != "old-core" {
		t.Fatalf("core binary = %q, want the previous binary", core)
	}
	cli, err := os.ReadFile(manager.paths.CLIbinary())
	if err != nil {
		t.Fatalf("cli binary is gone: %v", err)
	}
	if string(cli) != "old-cli" {
		t.Fatalf("cli binary = %q, want the previous binary", cli)
	}
	if _, err := os.Stat(manager.paths.UpdateTransactionFile()); !os.IsNotExist(err) {
		t.Fatalf("update transaction was not cleared: %v", err)
	}
	entries, err := os.ReadDir(manager.paths.RuntimeDir())
	if err != nil {
		t.Fatalf("read runtime dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".easytier-core.previous.") ||
			strings.HasPrefix(entry.Name(), ".easytier-cli.previous.") {
			t.Fatalf("backup file left behind: %s", entry.Name())
		}
	}
	status := manager.DownloadStatus()
	if status.State != StateFailed || status.Phase != PhaseInstall {
		t.Fatalf("download status = %+v, want failed install", status)
	}
}

func TestRecoverUpdateFilesWithoutPreviousRuntime(t *testing.T) {
	manager := newTestManager(t)
	if err := os.WriteFile(manager.paths.CoreBinary(), []byte("new-core"), 0o755); err != nil {
		t.Fatalf("seed core: %v", err)
	}
	transaction := updateTransaction{
		InstallDir: manager.paths.RuntimeDir(),
		BackupCore: filepath.Join(manager.paths.RuntimeDir(), ".easytier-core.previous.1"),
		BackupCLI:  filepath.Join(manager.paths.RuntimeDir(), ".easytier-cli.previous.1"),
	}
	if err := manager.writeUpdateTransaction(transaction); err != nil {
		t.Fatalf("writeUpdateTransaction: %v", err)
	}
	if err := manager.recoverUpdateFiles(context.Background(), true); err != nil {
		t.Fatalf("recoverUpdateFiles: %v", err)
	}
	if _, err := os.Stat(manager.paths.CoreBinary()); !os.IsNotExist(err) {
		t.Fatalf("freshly installed core was kept without a previous version: %v", err)
	}
	if _, err := os.Stat(manager.paths.UpdateTransactionFile()); !os.IsNotExist(err) {
		t.Fatalf("update transaction was not cleared: %v", err)
	}
}

func TestCleanupOrphanRuntimeFilesKeepsTrackedUpdate(t *testing.T) {
	manager := newTestManager(t)
	stale := filepath.Join(manager.paths.RuntimeDir(), ".stage.999")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	backup := filepath.Join(manager.paths.RuntimeDir(), ".easytier-core.previous.999")
	if err := os.WriteFile(backup, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}
	if err := manager.cleanupOrphanRuntimeFiles(); err != nil {
		t.Fatalf("cleanupOrphanRuntimeFiles: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("staging directory was kept: %v", err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("orphan backup was kept: %v", err)
	}

	if err := manager.writeUpdateTransaction(updateTransaction{
		InstallDir: manager.paths.RuntimeDir(),
		BackupCore: filepath.Join(manager.paths.RuntimeDir(), ".easytier-core.previous.1"),
		BackupCLI:  filepath.Join(manager.paths.RuntimeDir(), ".easytier-cli.previous.1"),
	}); err != nil {
		t.Fatalf("writeUpdateTransaction: %v", err)
	}
	if err := manager.cleanupOrphanRuntimeFiles(); err == nil {
		t.Fatal("cleanup ran while an update transaction was pending")
	}
}

func TestRedactRemovesSecrets(t *testing.T) {
	input := strings.Join([]string{
		"ET_CONFIG_SERVER=tcp://config.example.com:22020/etk_abc123.def-456",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9",
		"token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
	}, "\n")
	redacted := Redact(input)
	for _, secret := range []string{"etk_abc123.def-456", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q survived redaction: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "[ENROLLMENT TOKEN REDACTED]") {
		t.Fatalf("enrollment token marker missing: %s", redacted)
	}
	if !strings.Contains(redacted, "Bearer [REDACTED]") {
		t.Fatalf("bearer marker missing: %s", redacted)
	}
	if !strings.Contains(redacted, "[JWT REDACTED]") {
		t.Fatalf("jwt marker missing: %s", redacted)
	}
}

func TestRedactKeepsOrdinaryLogLines(t *testing.T) {
	input := "2026-09-10 10:00:00 info EasyTier core 已启动，PID 4242"
	if got := Redact(input); got != input {
		t.Fatalf("Redact changed an ordinary line: %q", got)
	}
}

func TestDownloadStatusRecoversInterruptedRun(t *testing.T) {
	manager := newTestManager(t)
	manager.dl.write(DownloadStatus{State: StateRunning, Phase: PhaseDownload, Percent: 12, Version: "v2.6.4"})
	status := manager.DownloadStatus()
	if status.State != StateFailed || status.Phase != PhaseInterrupt {
		t.Fatalf("status = %+v, want failed interrupted", status)
	}
	if status.Version != "v2.6.4" {
		t.Fatalf("version = %q, want the interrupted version", status.Version)
	}
}
