package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The runtime archive cache keeps the last downloaded runtime archive.
//
// Installing a runtime can fail after the archive has been downloaded: the new
// core may refuse to start, extraction may time out, or the update transaction
// may not commit. Every retry after such a failure used to download the whole
// archive again - about 25 MB - for a failure that had nothing to do with the
// download. Keeping the archive turns the second attempt into a local read.
//
// An entry is named after the architecture and version it was downloaded for,
// and it is removed once that runtime is installed, so it lives only for the
// window between a download and the update consuming it. A release is never
// republished under the same version, so that name identifies the artifact.
//
// The Console publishes no checksum for these archives, so a retained copy
// cannot be authenticated the way a download over TLS is. It still passes the
// member check and the binary version check on the way in, and a retained copy
// that fails them is discarded so the next attempt downloads again. The cache
// lives with the binaries it installs and is writable only by the package user,
// so retaining it grants no access that the runtime directory does not already.

// archiveCacheName is the cache file name of one artifact.
func archiveCacheName(assetArch, version string) string {
	return fmt.Sprintf("easytier-linux-%s-%s.zip", assetArch, version)
}

// cachedArchive returns the path of a retained archive for one artifact.
//
// Only its size is compared against the release metadata; the contents are
// judged by the same checks as a fresh download, in the caller. An entry whose
// size contradicts the published artifact is not the file this release
// describes, so it is discarded rather than offered.
func (m *Manager) cachedArchive(assetArch, version string, declaredSize int64) (string, bool) {
	path := filepath.Join(m.paths.ArchiveCacheDir(), archiveCacheName(assetArch, version))
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		// Anything else - a directory, a device, an unreadable path - is not the
		// artifact. It is left alone rather than removed, because a store onto
		// its name replaces it and an archive that cannot be removed must not
		// stop the update from being downloaded.
		return "", false
	}
	size := info.Size()
	if declaredSize > 0 && size != declaredSize {
		m.log.Printf("本机缓存的运行时归档与发布信息不一致，将重新下载")
		os.Remove(path)
		return "", false
	}
	return path, true
}

// storeArchive keeps an accepted archive for the retry that may follow and
// returns the path it now lives at, so the work that follows reads it there.
//
// The archive is moved rather than copied: the staging directory shares the
// package volume with the cache, so a move costs no space and keeps the peak at
// one archive instead of two - which matters precisely when the volume is tight
// enough for the size limits to matter. Nothing here can fail the update: the
// archive in hand has already been checked, so a cache that cannot be written
// only costs a future download.
func (m *Manager) storeArchive(archive, assetArch, version string) string {
	target := filepath.Join(m.paths.ArchiveCacheDir(), archiveCacheName(assetArch, version))
	if archive == target {
		// Already the retained copy, which is the case on a cache hit.
		return archive
	}
	if err := os.Rename(archive, target); err != nil {
		m.log.Errorf("缓存运行时归档失败: %v", err)
		return archive
	}
	// Only a store that succeeded may prune: pruning after a failed one would
	// delete the entry it failed to replace, leaving nothing to retry from.
	m.pruneArchiveCache(filepath.Base(target))
	return target
}

// discardCachedArchive drops the retained archive of one artifact. The update
// that downloaded it has installed it, so the copy has served its purpose and
// its space is given back.
func (m *Manager) discardCachedArchive(assetArch, version string) {
	os.Remove(filepath.Join(m.paths.ArchiveCacheDir(), archiveCacheName(assetArch, version)))
}

// pruneArchiveCache removes every retained archive that does not belong to the
// given artifact. Entries are normally removed as soon as their update
// finishes, so this only clears the leftovers of an interrupted attempt.
func (m *Manager) pruneArchiveCache(keep string) {
	entries, err := os.ReadDir(m.paths.ArchiveCacheDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() == keep || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		os.Remove(filepath.Join(m.paths.ArchiveCacheDir(), entry.Name()))
	}
}
