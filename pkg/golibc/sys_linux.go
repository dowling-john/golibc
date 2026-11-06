package golibc

import (
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Linux syscall layer built on the standard library's syscall package.
//
// Each system call exists twice. The unexported raw* form returns
// (result, errno) and is what the library itself uses, so its behavior never
// depends on shared state. The exported C-style form returns -1 (or nil) on
// failure and records the reason for GetErrno.

// Errno is a Linux error number.
type Errno = syscall.Errno

// errno holds the error from the most recent failed C-style call.
//
// C's errno is thread-local. Go has no goroutine-local storage, and goroutines
// move between OS threads, so this is one process-wide value: updated
// atomically (no data race), but a concurrent failure in another goroutine
// can replace it before you read it. Code that must be exact under
// concurrency should check return values, which the library does internally.
var errno atomic.Uintptr

// GetErrno returns the error number set by the most recent failing call.
func GetErrno() Errno { return Errno(errno.Load()) }

// SetErrno sets the value returned by GetErrno.
func SetErrno(e Errno) { errno.Store(uintptr(e)) }

const (
	EPERM         = syscall.EPERM
	ENOENT        = syscall.ENOENT
	EINTR         = syscall.EINTR
	EIO           = syscall.EIO
	EBADF         = syscall.EBADF
	ECHILD        = syscall.ECHILD
	ENOMEM        = syscall.ENOMEM
	EACCES        = syscall.EACCES
	EEXIST        = syscall.EEXIST
	ENOTDIR       = syscall.ENOTDIR
	EISDIR        = syscall.EISDIR
	EINVAL        = syscall.EINVAL
	ERANGE        = syscall.ERANGE
	EAGAIN        = syscall.EAGAIN
	ENOTTY        = syscall.ENOTTY
	ENOSPC        = syscall.ENOSPC
	EPIPE         = syscall.EPIPE
	ECONNREFUSED  = syscall.ECONNREFUSED
	ECONNRESET    = syscall.ECONNRESET
	EADDRINUSE    = syscall.EADDRINUSE
	EADDRNOTAVAIL = syscall.EADDRNOTAVAIL
	EAFNOSUPPORT  = syscall.EAFNOSUPPORT
	ENOTSOCK      = syscall.ENOTSOCK
	EINPROGRESS   = syscall.EINPROGRESS
	ETIMEDOUT     = syscall.ETIMEDOUT
	ENOTEMPTY     = syscall.ENOTEMPTY
)

const (
	O_RDONLY    = syscall.O_RDONLY
	O_WRONLY    = syscall.O_WRONLY
	O_RDWR      = syscall.O_RDWR
	O_CREAT     = syscall.O_CREAT
	O_TRUNC     = syscall.O_TRUNC
	O_APPEND    = syscall.O_APPEND
	O_EXCL      = syscall.O_EXCL
	O_DIRECTORY = syscall.O_DIRECTORY
)

// ret converts a raw result to the C convention: -1 and errno on failure.
func ret(r int, e Errno) int {
	if e != 0 {
		SetErrno(e)
		return -1
	}
	return r
}

func rawWrite(fd int, buf unsafe.Pointer, n int) (int, Errno) {
	r, _, e := syscall.Syscall(syscall.SYS_WRITE, uintptr(fd), uintptr(buf), uintptr(n))
	return int(r), e
}

func rawRead(fd int, buf unsafe.Pointer, n int) (int, Errno) {
	r, _, e := syscall.Syscall(syscall.SYS_READ, uintptr(fd), uintptr(buf), uintptr(n))
	return int(r), e
}

// atFDCWD is AT_FDCWD: resolve relative paths against the working directory.
var atFDCWD = -100

func rawOpen(path *byte, flags, mode int) (int, Errno) {
	r, _, e := syscall.Syscall6(syscall.SYS_OPENAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(path)),
		uintptr(flags|syscall.O_CLOEXEC), uintptr(mode), 0, 0)
	return int(r), e
}

func rawClose(fd int) (int, Errno) {
	r, _, e := syscall.Syscall(syscall.SYS_CLOSE, uintptr(fd), 0, 0)
	return int(r), e
}

func rawLseek(fd int, off int64, whence int) (int64, Errno) {
	r, _, e := syscall.Syscall(syscall.SYS_LSEEK, uintptr(fd), uintptr(off), uintptr(whence))
	return int64(r), e
}

func rawMmap(addr unsafe.Pointer, length, prot, flags, fd int, off int64) (unsafe.Pointer, Errno) {
	r, _, e := syscall.Syscall6(syscall.SYS_MMAP, uintptr(addr), uintptr(length),
		uintptr(prot), uintptr(flags), uintptr(fd), uintptr(off))
	if e != 0 {
		return nil, e
	}
	// r is a kernel mapping outside the Go heap; reinterpret rather than convert.
	return *(*unsafe.Pointer)(unsafe.Pointer(&r)), 0
}

func rawMremap(addr unsafe.Pointer, oldLen, newLen int) (unsafe.Pointer, Errno) {
	const mremapMayMove = 1
	r, _, e := syscall.Syscall6(syscall.SYS_MREMAP, uintptr(addr), uintptr(oldLen),
		uintptr(newLen), mremapMayMove, 0, 0)
	if e != 0 {
		return nil, e
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&r)), 0
}

func rawMunmap(addr unsafe.Pointer, length int) (int, Errno) {
	r, _, e := syscall.Syscall(syscall.SYS_MUNMAP, uintptr(addr), uintptr(length), 0)
	return int(r), e
}

// Write writes n bytes from buf to fd.
func Write(fd int, buf unsafe.Pointer, n int) int { return ret(rawWrite(fd, buf, n)) }

// Read reads up to n bytes from fd into buf.
func Read(fd int, buf unsafe.Pointer, n int) int { return ret(rawRead(fd, buf, n)) }

// Open opens path (a C string). arm64 has no open(2), so this uses openat.
func Open(path *byte, flags, mode int) int { return ret(rawOpen(path, flags, mode)) }

// Close closes fd.
func Close(fd int) int { return ret(rawClose(fd)) }

// Lseek repositions the file offset. It returns -1 and sets errno on failure.
func Lseek(fd int, off int64, whence int) int64 {
	r, e := rawLseek(fd, off, whence)
	if e != 0 {
		SetErrno(e)
		return -1
	}
	return r
}

// Unlink removes a file. arm64 has no unlink(2), so this uses unlinkat.
func Unlink(path *byte) int {
	r, _, e := syscall.Syscall(syscall.SYS_UNLINKAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(path)), 0)
	return ret(int(r), e)
}

// Fsync flushes fd's data to stable storage.
func Fsync(fd int) int {
	r, _, e := syscall.Syscall(syscall.SYS_FSYNC, uintptr(fd), 0, 0)
	return ret(int(r), e)
}

// Getpid returns the process ID.
func Getpid() int {
	r, _, _ := syscall.RawSyscall(syscall.SYS_GETPID, 0, 0, 0)
	return int(r)
}

// Kill sends sig to pid.
func Kill(pid, sig int) int {
	r, _, e := syscall.Syscall(syscall.SYS_KILL, uintptr(pid), uintptr(sig), 0)
	return ret(int(r), e)
}

// Isatty reports whether fd refers to a terminal.
func Isatty(fd int) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}

// ExitNow terminates the process immediately (_exit): no atexit handlers, no flushing.
func ExitNow(code int) {
	syscall.Syscall(syscall.SYS_EXIT_GROUP, uintptr(code), 0, 0)
}

// Mmap maps memory. It returns nil and sets errno on failure.
func Mmap(addr unsafe.Pointer, length, prot, flags, fd int, off int64) unsafe.Pointer {
	p, e := rawMmap(addr, length, prot, flags, fd, off)
	if e != 0 {
		SetErrno(e)
	}
	return p
}

// Mremap resizes a mapping, allowing the kernel to move it. Returns nil on failure.
func Mremap(addr unsafe.Pointer, oldLen, newLen int) unsafe.Pointer {
	p, e := rawMremap(addr, oldLen, newLen)
	if e != 0 {
		SetErrno(e)
	}
	return p
}

// Munmap unmaps a region returned by Mmap.
func Munmap(addr unsafe.Pointer, length int) int { return ret(rawMunmap(addr, length)) }
