package config

import (
	"regexp"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// ValidUUID reports whether value is a canonical UUID.
func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

// ValidSecret reports whether value is usable as a secret: non-empty and free
// of control characters.
func ValidSecret(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func hasBlankOrControl(value string) bool {
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ValidConsoleURL reports whether value is an acceptable Console API base URL.
// Plain HTTP is only accepted when the caller opted in.
func ValidConsoleURL(value string, allowInsecure bool) bool {
	if hasBlankOrControl(value) {
		return false
	}
	rest, ok := strings.CutPrefix(value, "https://")
	if !ok {
		rest, ok = strings.CutPrefix(value, "http://")
		if !ok || !allowInsecure {
			return false
		}
	}
	host := rest
	if idx := strings.IndexAny(host, "/?#"); idx >= 0 {
		host = host[:idx]
	}
	return host != ""
}

// configServerSchemes are the transports EasyTier accepts for its
// configuration server.
var configServerSchemes = []string{"tcp", "udp", "quic", "http", "https", "ws", "wss"}

// ValidConfigServer reports whether value is a usable EasyTier configuration
// server URL.
func ValidConfigServer(value string) bool {
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok {
		return false
	}
	known := false
	for _, candidate := range configServerSchemes {
		if scheme == candidate {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	if rest == "" || rest == "/" {
		return false
	}
	return !hasBlankOrControl(value)
}

// NormalizeConsoleURL trims a trailing slash as the OpenWrt client does.
func NormalizeConsoleURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

// NormalizeConfigServer trims a trailing slash as the OpenWrt client does.
func NormalizeConfigServer(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}
