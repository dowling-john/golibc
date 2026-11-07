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
	CLOCK_REALTIME           = 0
	CLOCK_MONOTONIC          = 1
	CLOCK_PROCESS_CPUTIME_ID = 2
	CLOCK_THREAD_CPUTIME_ID  = 3 // per OS thread, and a goroutine moves between threads
	CLOCK_MONOTONIC_RAW      = 4
	CLOCK_REALTIME_COARSE    = 5
	CLOCK_MONOTONIC_COARSE   = 6
	CLOCK_BOOTTIME           = 7
	CLOCK_TAI                = 11

	TIMER_ABSTIME = 1 // ClockNanosleep flag: req is an absolute time on the clock

	CLOCKS_PER_SEC = 1_000_000
)

// ClockGettime fills ts from the given clock.
func ClockGettime(clock int, ts *Timespec) int {
	r, _, e := syscall.RawSyscall(syscall.SYS_CLOCK_GETTIME, uintptr(clock), uintptr(unsafe.Pointer(ts)), 0)
	return ret(int(r), e)
}

// ClockGetres stores the resolution of the given clock in res (if non-nil).
func ClockGetres(clock int, res *Timespec) int {
	r, _, e := syscall.RawSyscall(syscall.SYS_CLOCK_GETRES, uintptr(clock), uintptr(unsafe.Pointer(res)), 0)
	return ret(int(r), e)
}

// Clock returns the processor time the program has used, in units of
// 1/CLOCKS_PER_SEC seconds, or -1 if it is not available. The time is
// summed over every thread of the process, including the ones the Go runtime
// uses for garbage collection.
func Clock() int64 {
	var ts Timespec
	if ClockGettime(CLOCK_PROCESS_CPUTIME_ID, &ts) != 0 {
		return -1
	}
	return int64(ts.Sec)*CLOCKS_PER_SEC + int64(ts.Nsec)/(1_000_000_000/CLOCKS_PER_SEC)
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

// ClockNanosleep sleeps on the given clock until req has elapsed, or, with
// TIMER_ABSTIME in flags, until the clock reaches req. As in C it returns 0
// or the error number itself (for example EINVAL for a clock that cannot be
// slept on) and leaves errno alone. Like Nanosleep it is never cut short by
// signals: a relative sleep resumes with the time remaining and an absolute
// one resumes toward the same deadline. rem, if non-nil, is set to zero on
// completion.
func ClockNanosleep(clock, flags int, req, rem *Timespec) int {
	cur := *req
	for {
		var left Timespec
		_, _, e := syscall.Syscall6(syscall.SYS_CLOCK_NANOSLEEP, uintptr(clock), uintptr(flags),
			uintptr(unsafe.Pointer(&cur)), uintptr(unsafe.Pointer(&left)), 0, 0)
		switch e {
		case 0:
			if rem != nil {
				*rem = Timespec{}
			}
			return 0
		case EINTR:
			if flags&TIMER_ABSTIME == 0 {
				cur = left
			}
		default:
			return int(e)
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
