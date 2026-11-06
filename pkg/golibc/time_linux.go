package golibc

import (
	"syscall"
	"unsafe"
)

type (
	Timespec = syscall.Timespec
	Timeval  = syscall.Timeval
)

const (
	CLOCK_REALTIME  = 0
	CLOCK_MONOTONIC = 1
)

// ClockGettime fills ts from the given clock.
func ClockGettime(clock int, ts *Timespec) int {
	r, _, e := syscall.RawSyscall(syscall.SYS_CLOCK_GETTIME, uintptr(clock), uintptr(unsafe.Pointer(ts)), 0)
	return ret(int(r), e)
}

// Time returns seconds since the Unix epoch.
func Time() int64 {
	var ts Timespec
	if ClockGettime(CLOCK_REALTIME, &ts) != 0 {
		return -1
	}
	return int64(ts.Sec)
}

// Nanosleep sleeps for req. Unlike C it is not cut short by signals: the Go
// runtime signals its own threads, so an interrupted sleep resumes with the
// time remaining. rem, if non-nil, is set to zero on completion.
func Nanosleep(req, rem *Timespec) int {
	cur := *req
	for {
		var left Timespec
		_, _, e := syscall.Syscall(syscall.SYS_NANOSLEEP, uintptr(unsafe.Pointer(&cur)), uintptr(unsafe.Pointer(&left)), 0)
		switch e {
		case 0:
			if rem != nil {
				*rem = Timespec{}
			}
			return 0
		case EINTR:
			cur = left
		default:
			SetErrno(e)
			return -1
		}
	}
}

// Usleep sleeps for us microseconds.
func Usleep(us int) int {
	ts := Timespec{Sec: int64(us / 1_000_000), Nsec: int64(us%1_000_000) * 1000}
	return Nanosleep(&ts, nil)
}

// Gettimeofday fills tv with the current time. (The obsolete timezone argument is not supported.)
func Gettimeofday(tv *Timeval) int {
	var ts Timespec
	if ClockGettime(CLOCK_REALTIME, &ts) != 0 {
		return -1
	}
	tv.Sec, tv.Usec = ts.Sec, ts.Nsec/1000
	return 0
}
