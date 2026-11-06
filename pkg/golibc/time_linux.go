package golibc

import (
	"syscall"
	"unsafe"
)

type Timespec = syscall.Timespec

const (
	CLOCK_REALTIME  = 0
	CLOCK_MONOTONIC = 1
)

// ClockGettime fills ts from the given clock.
func ClockGettime(clock int, ts *Timespec) int {
	r, _, e := syscall.RawSyscall(syscall.SYS_CLOCK_GETTIME, uintptr(clock), uintptr(unsafe.Pointer(ts)), 0)
	return ret(r, e)
}

// Time returns seconds since the Unix epoch.
func Time() int64 {
	var ts Timespec
	if ClockGettime(CLOCK_REALTIME, &ts) != 0 {
		return -1
	}
	return int64(ts.Sec)
}

// Nanosleep sleeps for req; if interrupted and rem is non-nil it receives the time left.
func Nanosleep(req, rem *Timespec) int {
	r, _, e := syscall.Syscall(syscall.SYS_NANOSLEEP, uintptr(unsafe.Pointer(req)), uintptr(unsafe.Pointer(rem)), 0)
	return ret(r, e)
}

// Usleep sleeps for us microseconds.
func Usleep(us int) int {
	ts := Timespec{Sec: int64(us / 1_000_000), Nsec: int64(us%1_000_000) * 1000}
	return Nanosleep(&ts, nil)
}
