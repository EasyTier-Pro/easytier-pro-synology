package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
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

func TestArchiveCacheNameNeedsAChecksum(t *testing.T) {
	if name := archiveCacheName("x86_64", "v2.6.4", ""); name != "" {
		t.Fatalf("an unauthenticated artifact was cacheable as %q", name)
	}
	if name := archiveCacheName("x86_64", "v2.6.4", "   "); name != "" {
		t.Fatalf("a blank checksum was cacheable as %q", name)
	}
	name := archiveCacheName("aarch64", "v2.6.4", "AB12")
	for _, part := range []string{"aarch64", "v2.6.4", "ab12"} {
		if !strings.Contains(name, part) {
			t.Fatalf("cache name %q is missing %q", name, part)
		}
	}
}

// A stored archive is what makes a retry cheap, so it has to be found again.
func TestCachedArchiveIsReused(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}

	manager.storeVerifiedArchive(archive, "x86_64", "v2.6.4", checksum)

	cached, ok := manager.cachedArchive("x86_64", "v2.6.4", checksum, size)
	if !ok {
		t.Fatal("the archive that was just cached was not found")
	}
	if got, err := fileSHA256(cached); err != nil || got != checksum {
		t.Fatalf("cached archive hash = %q, %v; want %q", got, err, checksum)
	}
}

// Reuse is only safe because the checksum is checked again. A damaged entry has
// to cost a download, not a failed update.
func TestCachedArchiveDiscardsDamage(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	manager.storeVerifiedArchive(archive, "x86_64", "v2.6.4", checksum)

	name := archiveCacheName("x86_64", "v2.6.4", checksum)
	path := filepath.Join(manager.paths.ArchiveCacheDir(), name)
	if err := os.WriteFile(path, []byte("not the archive"), 0o600); err != nil {
		t.Fatalf("damage cache entry: %v", err)
	}

	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", checksum, 0); ok {
		t.Fatal("a damaged cache entry was offered for reuse")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the damaged entry was kept: %v", err)
	}
	_ = size
}

// The published size is part of the artifact's identity, so an entry that
// disagrees with it is not the artifact being installed.
func TestCachedArchiveRejectsDeclaredSizeMismatch(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	manager.storeVerifiedArchive(archive, "x86_64", "v2.6.4", checksum)

	if _, ok := manager.cachedArchive("x86_64", "v2.6.4", checksum, size+1); ok {
		t.Fatal("an entry whose size disagrees with the release was offered for reuse")
	}
}

// The cache is bounded to one download: a retry only needs the artifact it is
// retrying, and the package volume may be small.
func TestStoreArchiveInCacheKeepsOneEntry(t *testing.T) {
	manager := newTestManager(t)
	first := runtimeArchive(t, "v2.6.4")
	second := runtimeArchive(t, "v2.6.5")
	firstSum, _ := fileSHA256(first)
	secondSum, _ := fileSHA256(second)

	manager.storeVerifiedArchive(first, "x86_64", "v2.6.4", firstSum)
	manager.storeVerifiedArchive(second, "x86_64", "v2.6.5", secondSum)

	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("cache holds %d entries %v, want 1", len(entries), names)
	}
	if entries[0].Name() != archiveCacheName("x86_64", "v2.6.5", secondSum) {
		t.Fatalf("cache kept %q instead of the newest entry", entries[0].Name())
	}
}

// The point of the whole feature: a retry of the same artifact must not touch
// the network. The download source is served by a handler that always fails and
// counts its requests, so a successful candidate can only have come from the
// cache.
func TestPrepareCandidateUsesCachedArchiveWithoutDownloading(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "the network must not be used", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}
	source := downloadSource{name: "Gitee", url: server.URL + "/asset.zip"}

	// Without the cache the same source fails, which is what makes the cached
	// run below meaningful rather than a no-op.
	fresh := newTestManager(t)
	if _, err := fresh.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size); err == nil {
		t.Fatal("the failing source produced a candidate without a cached archive")
	}
	if atomic.LoadInt32(&requests) == 0 {
		t.Fatal("the control run never reached the network")
	}

	cached := newTestManager(t)
	cached.storeVerifiedArchive(archive, "x86_64", version, checksum)
	before := atomic.LoadInt32(&requests)

	candidate, err := cached.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size)
	if err != nil {
		t.Fatalf("prepareCandidate from cache: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != before {
		t.Fatalf("the cached run made %d network requests", got-before)
	}
	if !binaryMatchesVersion(context.Background(), candidate.core, version) {
		t.Fatal("the cached archive did not produce a usable core binary")
	}
	if !binaryMatchesVersion(context.Background(), candidate.cli, version) {
		t.Fatal("the cached archive did not produce a usable cli binary")
	}
}

// Nothing may be cached for an artifact whose checksum the Console did not
// publish, because that path only has the weaker local check.
func TestStoreArchiveInCacheNeedsAChecksum(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")

	manager.storeVerifiedArchive(archive, "x86_64", "v2.6.4", "")

	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("an unauthenticated archive was cached as %v", entries[0].Name())
	}
}

// Without a checksum the client only accepts HTTPS, so an unaudited artifact
// cannot enter the cache through the download path either.
func TestPrepareCandidateRefusesPlainHTTPWithoutAChecksum(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	manager := newTestManager(t)
	_, err = manager.prepareCandidate(context.Background(),
		downloadSource{name: "Gitee", url: server.URL + "/asset.zip"},
		t.TempDir(), "x86_64", version, "", 0)
	if err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Fatalf("prepareCandidate error = %v, want the plain-HTTP refusal", err)
	}
	entries, readErr := os.ReadDir(manager.paths.ArchiveCacheDir())
	if readErr != nil {
		t.Fatalf("read cache dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("an unauthenticated archive was cached as %v", entries[0].Name())
	}
}

// The whole point, end to end through the real pipeline: the first attempt
// downloads and caches what it verified, and the next attempt for the same
// artifact does not download it again.
func TestPrepareCandidateCachesWhatItVerifiedAndReusesIt(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
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
	source := downloadSource{name: "Gitee", url: server.URL + "/asset.zip"}

	manager := newTestManager(t)
	if _, err := manager.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size); err != nil {
		t.Fatalf("first prepareCandidate: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("first attempt made %d downloads, want 1", got)
	}

	// A retry, exactly as a second attempt after a failed install would run it.
	logBefore := readLogFile(t, manager)
	if _, err := manager.prepareCandidate(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size); err != nil {
		t.Fatalf("second prepareCandidate: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("the retry downloaded the archive again (%d downloads)", got)
	}
	// The saving has to be visible: an operator who sees a fast retry otherwise
	// has no way to tell it apart from a download that silently did nothing.
	if appended := strings.TrimPrefix(readLogFile(t, manager), logBefore); !strings.Contains(appended, "缓存") {
		t.Fatalf("the retry logged %q, which does not report the cached archive", appended)
	}
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

// The cache is consulted before the free-space download limit. That limit only
// decides whether a download may start, so applying it first would let a cached
// archive that is now too large for the free space block the very update it was
// kept for - and the lookup that evicts a bad entry is exactly what would be
// skipped. The limit is passed in so the test can pin that ordering.
func TestResolveArchivePrefersTheCacheOverTheDownloadLimit(t *testing.T) {
	version := "v2.6.4"
	archive := runtimeArchive(t, version)
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	size, err := fileSize(archive)
	if err != nil {
		t.Fatalf("size archive: %v", err)
	}

	// A limit far below the archive size: exactly the tight-volume case.
	const tightLimit = 1

	manager := newTestManager(t)
	manager.storeVerifiedArchive(archive, "x86_64", version, checksum)

	source := downloadSource{name: "Gitee", url: "https://invalid.example/asset.zip"}
	resolved, err := manager.resolveArchive(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size, tightLimit)
	if err != nil {
		t.Fatalf("a cached archive was refused because of the download limit: %v", err)
	}
	if got, hashErr := fileSHA256(resolved); hashErr != nil || got != checksum {
		t.Fatalf("resolved archive hash = %q, %v; want the cached archive", got, hashErr)
	}

	// Without a cache the same limit still refuses the download, so the check
	// was not simply removed.
	fresh := newTestManager(t)
	if _, err := fresh.resolveArchive(context.Background(), source, t.TempDir(),
		"x86_64", version, checksum, size, tightLimit); err == nil {
		t.Fatal("an oversized download was started despite the limit")
	}
}

// The verified archive is moved into the cache, not copied, so peak disk usage
// stays at one archive: on a tight volume a second copy is what makes an
// otherwise viable download fail during extraction.
func TestStoreVerifiedArchiveMovesInsteadOfCopying(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	stage := t.TempDir()
	staged := filepath.Join(stage, "asset.zip")
	if err := os.Rename(archive, staged); err != nil {
		t.Fatalf("stage archive: %v", err)
	}

	stored := manager.storeVerifiedArchive(staged, "x86_64", "v2.6.4", checksum)

	if stored != filepath.Join(manager.paths.ArchiveCacheDir(), archiveCacheName("x86_64", "v2.6.4", checksum)) {
		t.Fatalf("stored archive path = %q", stored)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the archive was left in the staging directory: %v", err)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("the archive is not in the cache: %v", err)
	}
}

// A cache that cannot be written must not fail the update: the archive in hand
// has already been verified, so the cost is only a future download.
func TestStoreVerifiedArchiveKeepsTheArchiveWhenItCannotCache(t *testing.T) {
	manager := newTestManager(t)
	archive := runtimeArchive(t, "v2.6.4")
	checksum, err := fileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	// Make the move impossible by replacing the cache directory with a file.
	if err := os.RemoveAll(manager.paths.ArchiveCacheDir()); err != nil {
		t.Fatalf("remove cache dir: %v", err)
	}
	if err := os.WriteFile(manager.paths.ArchiveCacheDir(), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block cache dir: %v", err)
	}

	if got := manager.storeVerifiedArchive(archive, "x86_64", "v2.6.4", checksum); got != archive {
		t.Fatalf("storeVerifiedArchive = %q, want the original path so the update can continue", got)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the verified archive was lost: %v", err)
	}
}

// A candidate this host turns out to reject must not reach the cache. The cache
// holds one entry, so storing a rejected archive would evict the archive that
// does work: armv7 devices prefer armv7hf, and that build cannot run on a
// soft-float host, so this is the normal arm case rather than an edge case.
func TestRejectedCandidateDoesNotEvictTheUsableCachedArchive(t *testing.T) {
	// The usable archive is already cached from an earlier failed run.
	usable := runtimeArchiveFor(t, "armv7", "v2.6.4")
	usableSum, err := fileSHA256(usable)
	if err != nil {
		t.Fatalf("hash usable archive: %v", err)
	}
	manager := newTestManager(t)
	cached := manager.storeVerifiedArchive(usable, "armv7", "v2.6.4", usableSum)

	// The preferred archive downloads and verifies, but its binaries report a
	// different version - how a build that cannot run on this host fails.
	rejected := runtimeArchiveFor(t, "armv7hf", "v2.6.3")
	rejectedBody, err := os.ReadFile(rejected)
	if err != nil {
		t.Fatalf("read rejected archive: %v", err)
	}
	rejectedSum, err := fileSHA256(rejected)
	if err != nil {
		t.Fatalf("hash rejected archive: %v", err)
	}
	rejectedSize, err := fileSize(rejected)
	if err != nil {
		t.Fatalf("size rejected archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(rejectedBody)
	}))
	t.Cleanup(server.Close)

	if _, err := manager.prepareCandidate(context.Background(),
		downloadSource{name: "Gitee", url: server.URL + "/asset.zip"},
		t.TempDir(), "armv7hf", "v2.6.4", rejectedSum, rejectedSize); err == nil {
		t.Fatal("an archive whose binaries report the wrong version was accepted")
	}

	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("the usable cached archive was evicted by a rejected candidate: %v", err)
	}
	entries, err := os.ReadDir(manager.paths.ArchiveCacheDir())
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(cached) {
		t.Fatalf("cache holds %d entries after a rejected candidate, want only %q", len(entries), filepath.Base(cached))
	}
}
