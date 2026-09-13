package runtime

import (
	"encoding/binary"
	"os"
	"syscall"
)

// capNetAdmin is CAP_NET_ADMIN (12), the capability a process needs to create
// and configure a TUN device.
const capNetAdmin = 12

const capabilityXattr = "security.capability"

// tunCapable reports whether the core binary may create a TUN device.
//
// DSM 7 never runs a third-party package as root, so the only way the
// downloaded core can obtain CAP_NET_ADMIN is a file capability granted by the
// administrator (setcap on the runtime binary). Outside DSM the daemon may run
// as root, which is enough on its own.
func tunCapable(corePath string) bool {
	return fileCapability(corePath, capNetAdmin)
}

// fileCapability reports whether a non-root exec of path obtains one capability.
func fileCapability(path string, capability int) bool {
	if os.Geteuid() == 0 {
		return true
	}
	buffer := make([]byte, 24)
	size, err := syscall.Getxattr(path, capabilityXattr, buffer)
	if err != nil {
		return false
	}
	return parseFileCapability(buffer[:size], capability)
}

// capFlagsEffective is VFS_CAP_FLAGS_EFFECTIVE in the magic_etc word of a
// security.capability xattr. Without it the permitted set of the file does not
// become effective when a non-root process executes it, so the capability
// cannot actually be used.
const capFlagsEffective = 0x000001

// parseFileCapability reports whether one capability is part of the permitted
// set of a security.capability xattr (struct vfs_cap_data: a u32 magic_etc
// followed by up to two {permitted, inheritable} u32 pairs).
//
// The effective flag is required as well: `setcap cap_net_admin+p` (without
// `e`) grants the capability only to the permitted set, which a non-root exec
// cannot use, and reporting that as capable would leave the core failing to
// create its TUN device.
func parseFileCapability(data []byte, capability int) bool {
	word := capability / 32
	if word > 1 || len(data) < 12+word*8 {
		return false
	}
	if binary.LittleEndian.Uint32(data[:4])&capFlagsEffective == 0 {
		return false
	}
	permitted := binary.LittleEndian.Uint32(data[4+word*8:])
	return permitted&(1<<uint(capability%32)) != 0
}
