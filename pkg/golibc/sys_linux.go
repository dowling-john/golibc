package golibc

import (
	"syscall"
	"unsafe"
)

// Thin Linux syscall layer built on the standard library's syscall package.
// Functions return -1 and set Errno on failure, as in C.

// Errno holds the error from the most recent failed call.
var Errno syscall.Errno

const (
	EINTR  = syscall.EINTR
	EBADF  = syscall.EBADF
	EINVAL = syscall.EINVAL
	ENOMEM = syscall.ENOMEM
	ERANGE = syscall.ERANGE
	ENOENT = syscall.ENOENT
)

const (
	O_RDONLY = syscall.O_RDONLY
	O_WRONLY = syscall.O_WRONLY
	O_RDWR   = syscall.O_RDWR
	O_CREAT  = syscall.O_CREAT
	O_TRUNC  = syscall.O_TRUNC
	O_APPEND = syscall.O_APPEND
	O_EXCL   = syscall.O_EXCL
)

func ret(r uintptr, e syscall.Errno) int {
	if e != 0 {
		Errno = e
		return -1
	}
	return int(r)
}

// Write writes n bytes from buf to fd.
func Write(fd int, buf unsafe.Pointer, n int) int {
	r, _, e := syscall.Syscall(syscall.SYS_WRITE, uintptr(fd), uintptr(buf), uintptr(n))
	return ret(r, e)
}

// Read reads up to n bytes from fd into buf.
func Read(fd int, buf unsafe.Pointer, n int) int {
	r, _, e := syscall.Syscall(syscall.SYS_READ, uintptr(fd), uintptr(buf), uintptr(n))
	return ret(r, e)
}

// Open opens path (a C string). arm64 has no open(2), so this uses openat.
func Open(path *byte, flags, mode int) int {
	atFDCWD := -100
	r, _, e := syscall.Syscall6(syscall.SYS_OPENAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(path)),
		uintptr(flags|syscall.O_CLOEXEC), uintptr(mode), 0, 0)
	return ret(r, e)
}

// Close closes fd.
func Close(fd int) int {
	r, _, e := syscall.Syscall(syscall.SYS_CLOSE, uintptr(fd), 0, 0)
	return ret(r, e)
}

// Lseek repositions the file offset. It returns -1 and sets Errno on failure.
func Lseek(fd int, off int64, whence int) int64 {
	r, _, e := syscall.Syscall(syscall.SYS_LSEEK, uintptr(fd), uintptr(off), uintptr(whence))
	if e != 0 {
		Errno = e
		return -1
	}
	return int64(r)
}

// Unlink removes a file. arm64 has no unlink(2), so this uses unlinkat.
func Unlink(path *byte) int {
	atFDCWD := -100
	r, _, e := syscall.Syscall(syscall.SYS_UNLINKAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(path)), 0)
	return ret(r, e)
}

// Fsync flushes fd's data to stable storage.
func Fsync(fd int) int {
	r, _, e := syscall.Syscall(syscall.SYS_FSYNC, uintptr(fd), 0, 0)
	return ret(r, e)
}

// Getpid returns the process ID.
func Getpid() int {
	r, _, _ := syscall.RawSyscall(syscall.SYS_GETPID, 0, 0, 0)
	return int(r)
}

// Kill sends sig to pid.
func Kill(pid, sig int) int {
	r, _, e := syscall.Syscall(syscall.SYS_KILL, uintptr(pid), uintptr(sig), 0)
	return ret(r, e)
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

// Mmap maps memory. It returns nil and sets Errno on failure.
func Mmap(addr unsafe.Pointer, length, prot, flags, fd int, off int64) unsafe.Pointer {
	r, _, e := syscall.Syscall6(syscall.SYS_MMAP, uintptr(addr), uintptr(length),
		uintptr(prot), uintptr(flags), uintptr(fd), uintptr(off))
	if e != 0 {
		Errno = e
		return nil
	}
	// r is a kernel mapping outside the Go heap; reinterpret rather than convert.
	return *(*unsafe.Pointer)(unsafe.Pointer(&r))
}

// Mremap resizes a mapping, allowing the kernel to move it. Returns nil on failure.
func Mremap(addr unsafe.Pointer, oldLen, newLen int) unsafe.Pointer {
	const mremapMayMove = 1
	r, _, e := syscall.Syscall6(syscall.SYS_MREMAP, uintptr(addr), uintptr(oldLen),
		uintptr(newLen), mremapMayMove, 0, 0)
	if e != 0 {
		Errno = e
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&r))
}

// Munmap unmaps a region returned by Mmap.
func Munmap(addr unsafe.Pointer, length int) int {
	r, _, e := syscall.Syscall(syscall.SYS_MUNMAP, uintptr(addr), uintptr(length), 0)
	return ret(r, e)
}
