package runtime

import (
	"encoding/binary"
	"os"
	"syscall"
)

// Capabilities the core needs, and what each one buys:
//
//   - CAP_NET_ADMIN (12) creates the virtual interface, so the host itself can
//     originate traffic into the network. Without it the core runs in no-TUN
//     mode.
//   - CAP_NET_RAW (13) binds sockets to an interface. The core does this for
//     every peer connection unless the pushed configuration disables
//     bind_device, which the Console does not expose (see bindCapable).
const (
	capNetAdmin = 12
	capNetRaw   = 13
)

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

// bindCapable reports whether the core may bind a socket to an interface.
//
// This decides whether the node can reach any peer at all. The instances the
// Console pushes leave the core's bind_device flag at its default of true, so
// the core binds every outgoing socket to the local address and to the
// interface carrying it. Linux permits that only with CAP_NET_RAW, and without
// it each connection fails with EPERM: the node registers and looks online, but
// its peer list stays empty.
func bindCapable(corePath string) bool {
	return fileCapability(corePath, capNetRaw)
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
