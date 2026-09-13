package runtime

import (
	"encoding/binary"
	"strings"
	"testing"
)

// capabilityXattrValue builds a security.capability xattr value (struct vfs_cap_data:
// a u32 magic_etc followed by two {permitted, inheritable} u32 pairs). The
// effective flag is set, as `setcap ...+ep` does.
func capabilityXattrValue(permitted ...uint32) []byte {
	buffer := make([]byte, 20)
	buffer[0] = 0x02 // VFS_CAP_REVISION_2
	binary.LittleEndian.PutUint32(buffer, 0x02|capFlagsEffective)
	for word, bits := range permitted {
		binary.LittleEndian.PutUint32(buffer[4+word*8:], bits)
	}
	return buffer
}

// setcap without "e" grants the capability only to the permitted set, which a
// non-root exec cannot use, so it must not be reported as usable.
func TestParseFileCapabilityRequiresEffectiveFlag(t *testing.T) {
	data := capabilityXattrValue(1<<capNetAdmin, 0)
	binary.LittleEndian.PutUint32(data, 0x02) // clear VFS_CAP_FLAGS_EFFECTIVE
	if parseFileCapability(data, capNetAdmin) {
		t.Fatal("a capability without the effective flag was reported as usable")
	}
}

func TestParseFileCapabilityDetectsGrantedCapability(t *testing.T) {
	data := capabilityXattrValue(1<<capNetAdmin, 0)
	if !parseFileCapability(data, capNetAdmin) {
		t.Fatal("granted CAP_NET_ADMIN was not detected")
	}
	if parseFileCapability(data, 10) {
		t.Fatal("capability outside the permitted set was reported as granted")
	}
}

func TestParseFileCapabilityReadsSecondWord(t *testing.T) {
	if !parseFileCapability(capabilityXattrValue(0, 1<<4), 36) {
		t.Fatal("capability in the second permitted word was not detected")
	}
}

func TestParseFileCapabilityRejectsIncompleteData(t *testing.T) {
	if parseFileCapability(nil, capNetAdmin) {
		t.Fatal("empty xattr data was treated as capable")
	}
	if parseFileCapability(make([]byte, 8), capNetAdmin) {
		t.Fatal("truncated xattr data was treated as capable")
	}
	if parseFileCapability(capabilityXattrValue(1<<capNetAdmin), 36) {
		t.Fatal("capability beyond the provided words was treated as capable")
	}
}

// Relay mode is negotiated with the Console, never passed to the core: in
// secure mode the core runs no network of its own, so a --no-tun flag would be
// ignored. This guards against reintroducing one.
func TestCoreArgsNeverPassesNoTun(t *testing.T) {
	args := coreArgs()
	if len(args) != 1 || args[0] != "--secure-mode=true" {
		t.Fatalf("coreArgs() = %q, want [--secure-mode=true]", args)
	}
}

// The management endpoint listens on every address, so the client whitelist is
// the only thing keeping it private. These properties are what make that safe.
func TestRPCPortalStaysPrivate(t *testing.T) {
	if rpcPortalWhitelist == "" {
		t.Fatal("the RPC portal has no whitelist")
	}
	for _, forbidden := range []string{"0.0.0.0/0", "::/0", "0.0.0.0"} {
		if strings.Contains(rpcPortalWhitelist, forbidden) {
			t.Fatalf("whitelist %q admits non-loopback clients", rpcPortalWhitelist)
		}
	}
	if !strings.Contains(rpcPortalWhitelist, "127.") {
		t.Fatalf("whitelist %q does not admit the local client", rpcPortalWhitelist)
	}
	if !strings.HasPrefix(rpcPortalAddress, "127.") {
		t.Fatalf("clients would connect to %q instead of loopback", rpcPortalAddress)
	}
	// An unspecified address is required: a concrete one resolves to an
	// interface, and binding that interface needs CAP_NET_RAW.
	if !strings.HasPrefix(rpcPortalListen, "0.0.0.0:") && !strings.HasPrefix(rpcPortalListen, "[::]:") {
		t.Fatalf("listen address %q is not unspecified", rpcPortalListen)
	}
}
