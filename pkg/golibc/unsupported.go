//go:build !linux

package golibc

// golibc talks to the Linux kernel directly (no libc, no cgo), so it only
// builds for GOOS=linux. Cross-compile with GOOS=linux, or set the OS to
// linux in your IDE's build settings. This declaration exists so that other
// platforms get one clear error instead of dozens of "undefined" ones.
var _ = golibc_supports_only_linux__set_GOOS_to_linux
