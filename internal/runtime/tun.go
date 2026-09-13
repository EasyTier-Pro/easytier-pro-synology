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
	if os.Geteuid() == 0 {
		return true
	}
	buffer := make([]byte, 24)
	size, err := syscall.Getxattr(corePath, capabilityXattr, buffer)
	if err != nil {
		return false
	}
	return parseFileCapability(buffer[:size], capNetAdmin)
}

// parseFileCapability reports whether one capability is part of the permitted
// set of a security.capability xattr (struct vfs_cap_data: a u32 magic_etc
// followed by up to two {permitted, inheritable} u32 pairs).
func parseFileCapability(data []byte, capability int) bool {
	word := capability / 32
	if word > 1 || len(data) < 12+word*8 {
		return false
	}
	permitted := binary.LittleEndian.Uint32(data[4+word*8:])
	return permitted&(1<<uint(capability%32)) != 0
}
