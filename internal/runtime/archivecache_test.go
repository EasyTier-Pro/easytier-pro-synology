package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
)

// runtimeArchive builds a zip that passes the whole candidate pipeline: two
// members named the way the release uses, each reporting the version the daemon
// expects, so the shell stub stands in for a real runtime binary.
func runtimeArchive(t *testing.T, version string) string {
	t.Helper()
	return runtimeArchiveFor(t, "x86_64", version)
}

// runtimeArchiveFor builds the archive for one architecture, so a test can drive
// the fallback between two architectures.
func runtimeArchiveFor(t *testing.T, assetArch, version string) string {
	t.Helper()
	tag := strings.TrimPrefix(version, "v")
	archive := filepath.Join(t.TempDir(), "asset.zip")
	writeArchive(t, archive, map[string]string{
		"easytier-linux-" + assetArch + "/easytier-core": "#!/bin/sh\necho \"easytier-core " + tag + "\"\n",
		"easytier-linux-" + assetArch + "/easytier-cli":  "#!/bin/sh\necho \"easytier-cli " + tag + "\"\n",
	})
	return archive
}

// shaOf is the published-checksum stand-in for a test artifact.
func shaOf(t *testing.T, path string) string {
	t.Helper()
	sum, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	return sum
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// readLogFile returns the daemon log so a test can assert what an operator sees.
func readLogFile(t *testing.T, manager *Manager) string {
	t.Helper()
	data, err := os.ReadFile(manager.paths.DaemonLogFile())
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read daemon log: %v", err)
	}
	return string(data)
}

// A retained archive is what makes a retry cheap, so it has to be found again
// under the name of the artifact it belongs to.
func TestCachedArchiveIsReused(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}

	stored := manager.storeArchive(archive, "x86_64", "v2.6.4")
	if stored != filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("x86_64", "v2.6.4")) {
		t.Fatalf("stored archive path = %q", stored)
	}

	cached, ok := manager.cachedArchive("x86_64", "v2.6.4", size)
	if !ok {
		t.Fatal("the archive that was just retained was not found")
	}
	if cached != stored {
		t.Fatalf("cached path = %q, want %q", cached, stored)
	}
}

// The release metadata describes one artifact, so a retained file that does not
// match its size is not that artifact and must cost a download rather than be
// offered.
func TestCachedArchiveRejectsDeclaredSizeMismatch(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	manager.storeArchive(archive, "x86_64", "v2.6.4")

	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", size+1); ok {
		t.Fatal("an archive whose size disagrees with the release was offered")
	}
	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the mismatching entry was kept: %v", entryNames(entries))
	}
}

// A release that publishes no size still lets its archive be reused.
func TestCachedArchiveAcceptsUnknownSize(t *testing.T) {
	manager := newTestManager(t)
	manager.storeArchive(runtimeArchive(t, "v2.6.4"), "x86_64", "v2.6.4")

	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", 0); !ok {
		t.Fatal("a retained archive was refused when the release published no size")
	}
}

// The retained archive exists for the retry that may follow a failed install,
// and it is released once the runtime is in place.
func TestDiscardCachedArchiveReleasesTheEntry(t *testing.T) {
	manager := newTestManager(t)
	manager.storeArchive(runtimeArchive(t, "v2.6.4"), "x86_64", "v2.6.4")

	manager.discardCachedArchive("x86_64", "v2.6.4")

	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the installed archive was kept: %v", entryNames(entries))
	}
	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", 0); ok {
		t.Fatal("a discarded archive is still offered")
	}
}

// Only the artifact being installed is kept: leftovers of an interrupted
// attempt on another architecture must not accumulate.
func TestPruneArchiveCacheKeepsOnlyTheCurrentArtifact(t *testing.T) {
	manager := newTestManager(t)
	manager.storeArchive(runtimeArchiveFor(t, "armv7", "v2.6.4"), "armv7", "v2.6.4")
	current := manager.storeArchive(runtimeArchiveFor(t, "armv7hf", "v2.6.4"), "armv7hf", "v2.6.4")

	manager.pruneArchiveCache(filepath.Base(current))

	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(current) {
		t.Fatalf("cache holds %v, want only %q", entryNames(entries), filepath.Base(current))
	}
}

// The point of the whole feature: a retry of the same artifact must not touch
// the network. The download source is served by a handler that always fails and
// counts its requests, so an accepted candidate can only have come from the
// retained copy.
func TestPrepareCandidateUsesRetainedArchiveWithoutDownloading(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "the network must not be used", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	// A published checksum is what lets this source be plain HTTP; the cache
	// itself never needs one.
	checksum := shaOf(t, archive)
	source := downloadSource{name: "Gitee", url: server.URL + "/asset.zip"}

	// Without a retained copy the same source fails, which is what makes the
	// retained run below meaningful rather than a no-op.
	fresh := newTestManager(t)
	if _, err := fresh.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size); err == nil {
		t.Fatal("the failing source produced a candidate with nothing retained")
	}
	if atomic.LoadInt32(&requests) == 0 {
		t.Fatal("the control run never reached the network")
	}

	retained := newTestManager(t)
	retained.storeArchive(archive, "x86_64", version)
	before := atomic.LoadInt32(&requests)

	candidate, err := retained.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size)
	if err != nil {
		t.Fatalf("prepareCandidate from the retained copy: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != before {
		t.Fatalf("the retained run made %d network requests", got-before)
	}
	if !binaryMatchesVersion(context.Background(), candidate.core, version) {
		t.Fatal("the retained archive did not produce a usable core binary")
	}
	if !binaryMatchesVersion(context.Background(), candidate.cli, version) {
		t.Fatal("the retained archive did not produce a usable cli binary")
	}
}

// End to end through the real pipeline: the first attempt downloads and retains
// what it accepted, and the next attempt for the same artifact does not download
// it again. The saving is reported, because an operator would otherwise have no
// way to tell a fast retry apart from a download that silently did nothing.
func TestPrepareCandidateRetainsWhatItAcceptedAndReusesIt(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	checksum := shaOf(t, archive)

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	source := downloadSource{name: "Gitee", url: server.URL + "/asset.zip"}

	manager := newTestManager(t)
	if _, err := manager.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size); err != nil {
		t.Fatalf("first prepareCandidate: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("first attempt made %d downloads, want 1", got)
	}

	logBefore := readLogFile(t, manager)
	if _, err := manager.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size); err != nil {
		t.Fatalf("second prepareCandidate: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("the retry downloaded the archive again (%d downloads)", got)
	}
	if appended := strings.TrimPrefix(readLogFile(t, manager), logBefore); !strings.Contains(appended, "缓存") {
		t.Fatalf("the retry logged %q, which does not report the retained archive", appended)
	}
}

// A retained copy this host will not accept must cost a download, not fail the
// update forever: without dropping it, every later attempt would fail the same
// way.
func TestDamagedRetainedArchiveIsReplacedByADownload(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	manager := newTestManager(t)
	// A retained file of the expected size that is not a usable archive.
	damaged := filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("x86_64", version))
	if err := os.WriteFile(damaged, []byte(strings.Repeat("x", len(body))), 0o600); err != nil {
		t.Fatalf("write damaged entry: %v", err)
	}
	size, err := fileSize(damaged)
	if err != nil {
		t.Fatalf("size damaged entry: %v", err)
	}

	candidate, err := manager.prepareCandidate(context.Background(),
		downloadSource{name: "Gitee", url: server.URL + "/asset.zip"},
		t.TempDir(), "x86_64", version, shaOf(t, archive), size)
	if err != nil {
		t.Fatalf("a damaged retained copy was not replaced by a download: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("downloads = %d, want the damaged copy replaced exactly once", got)
	}
	if !binaryMatchesVersion(context.Background(), candidate.core, version) {
		t.Fatal("the replacement archive did not produce a usable core binary")
	}
	if log := readLogFile(t, manager); !strings.Contains(log, "不可用") {
		t.Fatalf("the replacement was not reported to the operator: %q", log)
	}
}

// A candidate this host turns out to reject must not reach the cache, or it
// would replace the archive that does work: armv7 devices prefer armv7hf, and
// that build cannot run on a soft-float host, so this is the normal arm case.
func TestRejectedCandidateDoesNotReplaceTheUsableArchive(t *testing.T) {
	manager := newTestManager(t)
	cached := manager.storeArchive(runtimeArchiveFor(t, "armv7", "v2.6.4"), "armv7", "v2.6.4")

	// The preferred archive downloads and unpacks, but its binaries report a
	// different version - how a build that cannot run on this host fails.
	rejectedBody, err := os.ReadFile(runtimeArchiveFor(t, "armv7hf", "v2.6.3"))
	if err != nil {
		t.Fatalf("read rejected archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(rejectedBody)
	}))
	t.Cleanup(server.Close)

	if _, err := manager.prepareCandidate(context.Background(),
		downloadSource{name: "Gitee", url: server.URL + "/asset.zip"},
		t.TempDir(), "armv7hf", "v2.6.4", sha256Hex(rejectedBody), int64(len(rejectedBody))); err == nil {
		t.Fatal("an archive whose binaries report the wrong version was accepted")
	}

	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("the usable retained archive was replaced by a rejected candidate: %v", err)
	}
}

// The cache is consulted before the free-space download limit. That limit only
// decides whether a download may start, so applying it first would let an
// archive too large for the current free space block the very update it was kept
// for - and the lookup that discards a bad entry is what would be skipped.
func TestResolveArchivePrefersTheRetainedCopyOverTheDownloadLimit(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}

	// A limit far below the archive size: the tight-volume case.
	const tightLimit = 1

	manager := newTestManager(t)
	cached := manager.storeArchive(archive, "x86_64", version)

	source := downloadSource{name: "Gitee", url: "https://invalid.example/asset.zip"}
	resolved, retained, err := manager.resolveArchive(context.Background(), source, t.TempDir(),
		"x86_64", version, size, tightLimit)
	if err != nil {
		t.Fatalf("a retained archive was refused because of the download limit: %v", err)
	}
	if !retained || resolved != cached {
		t.Fatalf("resolveArchive = %q, retained=%t; want the retained copy", resolved, retained)
	}

	// Without one, the same limit still refuses the download, so the check was
	// not simply removed.
	fresh := newTestManager(t)
	if _, _, err := fresh.resolveArchive(context.Background(), source, t.TempDir(),
		"x86_64", version, size, tightLimit); err == nil {
		t.Fatal("an oversized download was started despite the limit")
	}
}

// The accepted archive is moved into the cache, not copied, so peak disk usage
// stays at one archive: on a tight volume a second copy is what makes an
// otherwise viable download fail during extraction.
func TestStoreArchiveMovesInsteadOfCopying(t *testing.T) {
	manager := newTestManager(t)
	stage := t.TempDir()
	staged := filepath.Join(stage, "x86_64", "asset.zip")
	if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
		t.Fatalf("create staging dir: %v", err)
	}
	if err := os.Rename(runtimeArchive(t, "v2.6.4"), staged); err != nil {
		t.Fatalf("stage archive: %v", err)
	}

	stored := manager.storeArchive(staged, "x86_64", "v2.6.4")

	if stored == staged {
		t.Fatal("the archive was reported at its staging path, so it was not moved")
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the archive was left in the staging directory: %v", err)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("the archive is not in the cache: %v", err)
	}
}

// A cache that cannot be written must not fail the update: the archive in hand
// has already been accepted, so the cost is only a future download.
func TestStoreArchiveKeepsTheArchiveWhenItCannotCache(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	// Make the move impossible by replacing the cache directory with a file.
	if err := os.RemoveAll(manager.paths.ArchiveCacheDir()); err != nil {
		t.Fatalf("remove cache dir: %v", err)
	}
	if err := os.WriteFile(manager.paths.ArchiveCacheDir(), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block cache dir: %v", err)
	}

	if got := manager.storeArchive(archive, "x86_64", "v2.6.4"); got != archive {
		t.Fatalf("storeArchive = %q, want the original path so the update can continue", got)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the accepted archive was lost: %v", err)
	}
}

// Validation must stand on its own without a published checksum, because the
// Console does not publish one: a retained file that is not a usable archive is
// rejected by the member check before anything is extracted.
func TestBuildCandidateRejectsAnArchiveWithoutTheRuntimeMembers(t *testing.T) {
	manager := newTestManager(t)
	stage := t.TempDir()
	archive := filepath.Join(stage, "asset.zip")
	writeArchive(t, archive, map[string]string{"unrelated.txt": "not a runtime"})

	// An HTTPS source that is never contacted: this call reads only the archive.
	source := downloadSource{name: "Gitee", url: "https://invalid.example/asset.zip"}
	_, err := manager.buildCandidate(context.Background(), source, archive, stage,
		"x86_64", "v2.6.4", "", 0)
	if err == nil {
		t.Fatal("an archive without the runtime members was accepted")
	}
	if !strings.Contains(err.Error(), "easytier-core") {
		t.Fatalf("rejection reason = %v, want the missing members", err)
	}
}

// A retained copy that no longer matches the release size is not something to
// install, and the report has to say so rather than fail quietly.
func TestCachedArchiveReportsASizeMismatch(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	manager.storeArchive(archive, "x86_64", "v2.6.4")

	before := readLogFile(t, manager)
	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", size+1); ok {
		t.Fatal("the mismatching entry was offered")
	}
	if appended := strings.TrimPrefix(readLogFile(t, manager), before); !strings.Contains(appended, "不一致") {
		t.Fatalf("the mismatch was not reported: %q", appended)
	}
}

// The retained archive is released once the runtime is installed, so it holds
// disk space only for the window between a download and the update consuming it.
func TestSuccessfulInstallReleasesTheRetainedArchive(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	checksum := shaOf(t, archive)

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	manager := newTestManager(t)
	source := downloadSource{name: "Gitee", url: server.URL + "/asset.zip"}

	// Download and install the way downloadRun does.
	stage := filepath.Join(manager.paths.RuntimeDir(), ".stage.test")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatalf("create stage: %v", err)
	}
	defer os.RemoveAll(stage)
	candidate, err := manager.prepareCandidate(context.Background(), source, stage,
		"x86_64", version, checksum, size)
	if err != nil {
		t.Fatalf("prepareCandidate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("x86_64", version))); err != nil {
		t.Fatalf("the accepted archive was not retained for a retry: %v", err)
	}

	if !manager.installRuntime(context.Background(), candidate.core, candidate.cli, version, source) {
		t.Fatal("installRuntime did not report the runtime as installed")
	}
	manager.discardCachedArchive("x86_64", version)

	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the archive was kept after a successful install: %v", entryNames(entries))
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// A failure that says nothing about the retained file - here, extraction cannot
// even create its directory - must not send the daemon back to the network.
// Re-downloading the same bytes would cost the transfer this cache exists to
// save, and would fail in exactly the same way.
func TestEnvironmentalFailureKeepsTheRetainedArchive(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "must not be used", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	manager := newTestManager(t)
	cached := manager.storeArchive(archive, "x86_64", version)

	// The staging directory is usable, so a download would go through: only the
	// extraction target is blocked, by a regular file where its directory goes.
	stage := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, "x86_64"), 0o700); err != nil {
		t.Fatalf("create staging dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "x86_64", "extracted"), []byte("x"), 0o600); err != nil {
		t.Fatalf("block the extraction dir: %v", err)
	}

	if _, err := manager.prepareCandidate(context.Background(),
		downloadSource{name: "Gitee", url: server.URL + "/asset.zip"},
		stage, "x86_64", version, "", size); err == nil {
		t.Fatal("an impossible extraction directory produced a candidate")
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("an environmental failure triggered %d downloads", got)
	}
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("an environmental failure discarded the retained archive: %v", err)
	}
}

// A store that fails must not prune: the entry it failed to replace is the only
// copy that can be installed without the network. The target is blocked by a
// non-empty directory, which fails the rename while leaving the cache writable,
// so a prune would go through.
func TestFailedStoreKeepsThePreviousArchive(t *testing.T) {
	manager := newTestManager(t)
	previous := manager.storeArchive(runtimeArchiveFor(t, "armv7", "v2.6.4"), "armv7", "v2.6.4")

	blocked := filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("armv7hf", "v2.6.4"))
	if err := os.MkdirAll(filepath.Join(blocked, "inner"), 0o700); err != nil {
		t.Fatalf("create blocking directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "inner", "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("populate blocking directory: %v", err)
	}

	staged := filepath.Join(t.TempDir(), "asset.zip")
	if err := os.Rename(runtimeArchiveFor(t, "armv7hf", "v2.6.4"), staged); err != nil {
		t.Fatalf("stage archive: %v", err)
	}
	if got := manager.storeArchive(staged, "armv7hf", "v2.6.4"); got != staged {
		t.Fatalf("storeArchive = %q, want the staging path when the move fails", got)
	}

	if _, err := os.Stat(previous); err != nil {
		t.Fatalf("a failed store pruned the previous archive: %v", err)
	}
}

// An entry that is not a regular file is not the artifact. It must never be
// offered, because a directory cannot be removed and offering it again would
// make every later attempt fail in the same place, with no download at all.
func TestDirectoryEntryIsNeverOffered(t *testing.T) {
	manager := newTestManager(t)
	path := filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("x86_64", "v2.6.4"))
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o700); err != nil {
		t.Fatalf("create directory entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "inner", "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("populate directory entry: %v", err)
	}

	// Checked with no published size, so only the file kind can rule it out.
	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", 0); ok {
		t.Fatal("a directory was offered as the retained archive")
	}
}

// A directory in the way must not stop the update: it is passed over, the
// archive is downloaded, and the install proceeds.
func TestUnevictableEntryDoesNotBlockTheDownload(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	manager := newTestManager(t)
	blocked := filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("x86_64", version))
	if err := os.MkdirAll(filepath.Join(blocked, "inner"), 0o700); err != nil {
		t.Fatalf("create blocking directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "inner", "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("populate blocking directory: %v", err)
	}

	candidate, err := manager.prepareCandidate(context.Background(),
		downloadSource{name: "Gitee", url: server.URL + "/asset.zip"},
		t.TempDir(), "x86_64", version, shaOf(t, archive), size)
	if err != nil {
		t.Fatalf("an unevictable cache entry blocked the update: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("downloads = %d, want the archive downloaded once", got)
	}
	if !binaryMatchesVersion(context.Background(), candidate.core, version) {
		t.Fatal("the downloaded archive did not produce a usable core binary")
	}
	if log := readLogFile(t, manager); strings.Contains(log, "本机缓存的运行时归档不可用") {
		t.Fatalf("the directory was treated as a retained archive: %q", log)
	}
}

// A size published without a checksum still identifies the artifact, so it must
// reach the archive lookup rather than being dropped with the missing checksum.
func TestReleaseArtifactReadsTheSizeWithoutAChecksum(t *testing.T) {
	release := console.Release{Stable: console.VersionInfo{
		Version: "v2.6.4",
		Artifacts: map[string]console.Artifact{
			"linux-x86_64": {Size: json.Number("25506523")},
		},
	}}
	checksum, size, aerr := releaseArtifact(release, "x86_64")
	if aerr != nil {
		t.Fatalf("releaseArtifact: %v", aerr)
	}
	if checksum != "" {
		t.Fatalf("checksum = %q, want none", checksum)
	}
	if size != 25506523 {
		t.Fatalf("size = %d, want the published size", size)
	}

	// With neither published, nothing is claimed.
	empty := console.Release{Stable: console.VersionInfo{Version: "v2.6.4"}}
	if checksum, size, aerr := releaseArtifact(empty, "x86_64"); aerr != nil || checksum != "" || size != 0 {
		t.Fatalf("releaseArtifact without an artifact = %q, %d, %v", checksum, size, aerr)
	}
}
