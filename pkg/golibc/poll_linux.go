package golibc

import (
	"strconv"
	"syscall"
	"unsafe"
)

// Waiting for descriptors: poll (poll.h), epoll (sys/epoll.h), select (sys/select.h).
//
// arm64 has none of the older poll, epoll_wait and select system calls, so
// these use ppoll, epoll_pwait and pselect6 on every architecture.
//
// Unlike C, these calls never return EINTR. The Go runtime signals its own
// threads (for goroutine preemption), and poll, epoll and select are never
// restarted by the kernel, so a C-style EINTR would appear spuriously in
// programs that never installed a signal handler. Interrupted waits resume
// with the remaining time instead, as the standard library does.

// monotonicNow returns CLOCK_MONOTONIC in nanoseconds.
func monotonicNow() int64 {
	var ts Timespec
	ClockGettime(CLOCK_MONOTONIC, &ts)
	return ts.Sec*1_000_000_000 + ts.Nsec
}

// remainingMs converts the time left until deadline (a monotonicNow value) to
// whole milliseconds, rounding up and never going below 0.
func remainingMs(deadline int64) int {
	left := deadline - monotonicNow()
	if left <= 0 {
		return 0
	}
	return int((left + 999_999) / 1_000_000)
}

const (
	POLLIN   = 0x001
	POLLPRI  = 0x002
	POLLOUT  = 0x004
	POLLERR  = 0x008
	POLLHUP  = 0x010
	POLLNVAL = 0x020
)

// Pollfd is one descriptor to wait on; the kernel fills Revents.
type Pollfd struct {
	Fd      int32
	Events  int16
	Revents int16
}

// Poll waits up to timeoutMs milliseconds (negative: forever, 0: don't wait)
// for events on fds. It returns the number of descriptors with events, 0 on
// timeout, or -1 on error.
func Poll(fds []Pollfd, timeoutMs int) int {
	var p unsafe.Pointer
	if len(fds) > 0 {
		p = unsafe.Pointer(&fds[0])
	}
	var deadline int64
	if timeoutMs > 0 {
		deadline = monotonicNow() + int64(timeoutMs)*1_000_000
	}
	for {
		ms := timeoutMs
		if timeoutMs > 0 {
			ms = remainingMs(deadline)
		}
		var ts *Timespec
		if ms >= 0 {
			ts = &Timespec{Sec: int64(ms / 1000), Nsec: int64(ms%1000) * 1_000_000}
		}
		r, _, e := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(p), uintptr(len(fds)), uintptr(unsafe.Pointer(ts)), 0, 0, 0)
		if e == EINTR {
			continue
		}
		return ret(int(r), e)
	}
}

const (
	EPOLLIN       = 0x001
	EPOLLPRI      = 0x002
	EPOLLOUT      = 0x004
	EPOLLERR      = 0x008
	EPOLLHUP      = 0x010
	EPOLLRDHUP    = 0x2000
	EPOLLONESHOT  = 1 << 30
	EPOLLET       = 1 << 31
	EPOLL_CTL_ADD = 1
	EPOLL_CTL_DEL = 2
	EPOLL_CTL_MOD = 3
	EPOLL_CLOEXEC = syscall.EPOLL_CLOEXEC
)

// EpollEvent is one readiness event: Events is a mask of EPOLL* flags and Fd
// is the descriptor it was registered with. (The layout differs between
// amd64, where the kernel packs it, and arm64; the standard library's type
// is correct for each.)
type EpollEvent = syscall.EpollEvent

// EpollCreate1 creates an epoll instance; flags may be EPOLL_CLOEXEC.
func EpollCreate1(flags int) int {
	r, _, e := syscall.Syscall(syscall.SYS_EPOLL_CREATE1, uintptr(flags), 0, 0)
	return ret(int(r), e)
}

// EpollCreate creates an epoll instance. The size hint is ignored, as in modern C libraries.
func EpollCreate(size int) int {
	if size <= 0 {
		SetErrno(EINVAL)
		return -1
	}
	return EpollCreate1(0)
}

// EpollCtl adds, modifies or removes fd on epfd. ev is ignored for EPOLL_CTL_DEL.
func EpollCtl(epfd, op, fd int, ev *EpollEvent) int {
	r, _, e := syscall.Syscall6(syscall.SYS_EPOLL_CTL, uintptr(epfd), uintptr(op), uintptr(fd), uintptr(unsafe.Pointer(ev)), 0, 0)
	return ret(int(r), e)
}

// EpollWait waits up to timeoutMs milliseconds (negative: forever) and fills
// events. It returns the number of events, 0 on timeout, or -1 on error.
func EpollWait(epfd int, events []EpollEvent, timeoutMs int) int {
	if len(events) == 0 {
		SetErrno(EINVAL)
		return -1
	}
	var deadline int64
	if timeoutMs > 0 {
		deadline = monotonicNow() + int64(timeoutMs)*1_000_000
	}
	for {
		ms := timeoutMs
		if timeoutMs > 0 {
			ms = remainingMs(deadline)
		}
		r, _, e := syscall.Syscall6(syscall.SYS_EPOLL_PWAIT, uintptr(epfd), uintptr(unsafe.Pointer(&events[0])),
			uintptr(len(events)), uintptr(ms), 0, 0)
		if e == EINTR {
			continue
		}
		return ret(int(r), e)
	}
}

// FD_SETSIZE is the number of descriptors an Fdset can hold.
const FD_SETSIZE = 1024

// Fdset is a set of descriptors below FD_SETSIZE for Select.
type Fdset struct{ Bits [FD_SETSIZE / 64]uint64 }

func FD_ZERO(s *Fdset) { *s = Fdset{} }

// fdIndex checks fd against FD_SETSIZE. In C an out-of-range FD_SET silently
// corrupts memory; here it panics with a message. Use Poll or Epoll for
// descriptor numbers of 1024 and above.
func fdIndex(op string, fd int) int {
	if fd < 0 || fd >= FD_SETSIZE {
		panic("golibc: " + op + ": descriptor " + strconv.Itoa(fd) + " is outside 0..FD_SETSIZE-1 (use Poll or Epoll)")
	}
	return fd / 64
}

func FD_SET(fd int, s *Fdset) { s.Bits[fdIndex("FD_SET", fd)] |= 1 << (uint(fd) % 64) }

func FD_CLR(fd int, s *Fdset) { s.Bits[fdIndex("FD_CLR", fd)] &^= 1 << (uint(fd) % 64) }

// FD_ISSET reports whether fd is in the set; a descriptor outside the range is never in it.
func FD_ISSET(fd int, s *Fdset) bool {
	return fd >= 0 && fd < FD_SETSIZE && s.Bits[fd/64]&(1<<(uint(fd)%64)) != 0
}

// Select waits for the descriptors in the three sets (any may be nil) to
// become ready. nfds is one more than the highest descriptor. A nil timeout
// waits forever. The sets are updated to the ready descriptors; unlike some C
// libraries, timeout is not updated. It returns the number of ready
// descriptors, 0 on timeout, or -1 on error.
func Select(nfds int, r, w, x *Fdset, timeout *Timeval) int {
	if nfds > FD_SETSIZE {
		SetErrno(EINVAL)
		return -1
	}
	var deadline int64
	if timeout != nil {
		deadline = monotonicNow() + timeout.Sec*1_000_000_000 + timeout.Usec*1000
	}
	for {
		var ts *Timespec
		if timeout != nil {
			left := max(deadline-monotonicNow(), 0)
			ts = &Timespec{Sec: left / 1_000_000_000, Nsec: left % 1_000_000_000}
		}
		n, _, e := syscall.Syscall6(syscall.SYS_PSELECT6, uintptr(nfds), uintptr(unsafe.Pointer(r)),
			uintptr(unsafe.Pointer(w)), uintptr(unsafe.Pointer(x)), uintptr(unsafe.Pointer(ts)), 0)
		if e == EINTR {
			continue // pselect6 leaves the sets unmodified when interrupted
		}
		return ret(int(n), e)
	}
}
