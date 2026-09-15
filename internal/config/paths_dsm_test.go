//go:build !fnos

package config

// daemonBinaryForTest pins the daemon binary name this platform's packages
// install; config_test.go asserts Paths resolves it.
const daemonBinaryForTest = "easytier-pro-dsm"
