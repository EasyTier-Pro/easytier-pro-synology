package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The runtime archive cache keeps the last verified download on disk.
//
// Installing a runtime can fail after the archive has been downloaded and
// verified: the new core may refuse to start, extraction may time out, or the
// update transaction may not commit. Every retry after such a failure used to
// download the whole archive again, which on a slow or metered link costs far
// more than the failure itself. Keeping the verified archive turns the second
// attempt into a local read.
//
// An entry is named after the checksum the Console published for it, and every
// reuse is verified against that checksum before the archive is used, so a
// cached artifact is exactly as trustworthy as a fresh download. That is also
// why nothing is cached when the Console published no checksum: that case falls
// back to a weaker local check, and reusing such a file would quietly lower the
// bar for every later attempt.
//
// Only one archive is kept. That bounds the directory to a single download, and
// a retry only ever needs the artifact it is retrying.

// archiveCacheName is the cache file name of one artifact, or an empty string
// when the artifact cannot be cached because no checksum authenticated it.
func archiveCacheName(assetArch, version, checksum string) string {
	checksum = strings.ToLower(strings.TrimSpace(checksum))
	if checksum == "" {
		return ""
	}
	return fmt.Sprintf("easytier-linux-%s-%s.%s.zip", assetArch, version, checksum)
}

// cachedArchive returns the path of a usable cached archive for one artifact.
//
// An entry that no longer matches the published checksum, its declared size, or
// the download limit is discarded rather than reported, so a damaged or
// republished artifact costs a download instead of failing the update.
func (m *Manager) cachedArchive(assetArch, version, checksum string, limit, declaredSize int64) (string, bool) {
	name := archiveCacheName(assetArch, version, checksum)
	if name == "" {
		return "", false
	}
	path := filepath.Join(m.paths.ArchiveCacheDir(), name)
	size, err := fileSize(path)
	if err != nil || size <= 0 {
		return "", false
	}
	tooLarge := size > limit
	sizeMismatch := declaredSize > 0 && size != declaredSize
	if !tooLarge && !sizeMismatch {
		actual, hashErr := fileSHA256(path)
		if hashErr == nil && strings.EqualFold(actual, strings.TrimSpace(checksum)) {
			return path, true
		}
	}
	m.log.Printf("本机缓存的运行时归档已不可用，将重新下载")
	os.Remove(path)
	return "", false
}

// storeArchiveInCache keeps a verified archive for the next attempt.
//
// A cache write never fails the update: the archive in hand has already been
// verified, so losing the cache only costs a future download.
func (m *Manager) storeArchiveInCache(archive, assetArch, version, checksum string) {
	name := archiveCacheName(assetArch, version, checksum)
	if name == "" || filepath.Base(archive) == name {
		return
	}
	// copyFile writes through a temporary file and renames it, so a partial
	// write never looks like a cache entry.
	if err := copyFile(archive, filepath.Join(m.paths.ArchiveCacheDir(), name), 0o600); err != nil {
		m.log.Errorf("缓存运行时归档失败: %v", err)
		return
	}
	m.pruneArchiveCache(name)
}

// pruneArchiveCache keeps the named entry and removes everything else, so the
// directory stays the size of one download.
func (m *Manager) pruneArchiveCache(keep string) {
	entries, err := os.ReadDir(m.paths.ArchiveCacheDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() == keep {
			continue
		}
		os.Remove(filepath.Join(m.paths.ArchiveCacheDir(), entry.Name()))
	}
}
