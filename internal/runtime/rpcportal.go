package runtime

// The RPC portal is the management endpoint of easytier-core, used by
// easytier-cli and by this daemon's health checks.
//
// It listens on every address but only answers loopback clients: the core
// checks each client against rpcPortalWhitelist before serving it, and rejects
// anything else. That combination is deliberate. The core binds its management
// socket to the interface that carries the portal address, and Linux only
// allows that with CAP_NET_RAW, which this package never has; an unspecified
// address resolves to no interface, so the core skips the bind. A concrete
// loopback address would resolve to "lo" and the core would refuse to start.
// The whitelist, not the listening address, is what keeps the endpoint private.
const (
	// rpcPortalListen accepts connections on every local address.
	rpcPortalListen = "0.0.0.0:15888"
	// rpcPortalAddress is what local clients connect to.
	rpcPortalAddress = "127.0.0.1:15888"
	// rpcPortalWhitelist is the set of clients the core serves, and is the
	// security boundary of this endpoint. It must never be widened to a range
	// that includes non-loopback addresses.
	rpcPortalWhitelist = "127.0.0.0/8,::1/128"
)
