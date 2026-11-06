package golibc

import (
	"syscall"
	"unsafe"
)

// File-system, descriptor and identity calls (unistd.h, sys/stat.h).
// Like the rest of the syscall layer they return -1 (or nil) and set errno on failure.

// Stat_t is the result of Stat, Lstat and Fstat.
type Stat_t = syscall.Stat_t

const (
	S_IFMT   = syscall.S_IFMT
	S_IFREG  = syscall.S_IFREG
	S_IFDIR  = syscall.S_IFDIR
	S_IFLNK  = syscall.S_IFLNK
	S_IFIFO  = syscall.S_IFIFO
	S_IFCHR  = syscall.S_IFCHR
	S_IFBLK  = syscall.S_IFBLK
	S_IFSOCK = syscall.S_IFSOCK

	F_OK = 0
	R_OK = 4
	W_OK = 2
	X_OK = 1

	atSymlinkNofollow = 0x100
	atRemovedir       = 0x200
)

func S_ISREG(m uint32) bool  { return m&S_IFMT == S_IFREG }
func S_ISDIR(m uint32) bool  { return m&S_IFMT == S_IFDIR }
func S_ISLNK(m uint32) bool  { return m&S_IFMT == S_IFLNK }
func S_ISFIFO(m uint32) bool { return m&S_IFMT == S_IFIFO }
func S_ISCHR(m uint32) bool  { return m&S_IFMT == S_IFCHR }
func S_ISBLK(m uint32) bool  { return m&S_IFMT == S_IFBLK }

// Pointer arguments are converted to uintptr inside each syscall.Syscall
// call expression. The compiler only keeps the referent alive and in place for
// the duration of the call when the conversion happens there, so these must
// not be hoisted into helpers.

// Stat fills st for path, following symlinks. arm64 has no stat(2), so this uses newfstatat.
func Stat(p *byte, st *Stat_t) int {
	r, _, e := syscall.Syscall6(sysFstatat, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(st)), 0, 0, 0)
	return ret(int(r), e)
}

// Lstat is Stat without following a final symlink.
func Lstat(p *byte, st *Stat_t) int {
	r, _, e := syscall.Syscall6(sysFstatat, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(st)), atSymlinkNofollow, 0, 0)
	return ret(int(r), e)
}

func Fstat(fd int, st *Stat_t) int {
	r, _, e := syscall.Syscall(syscall.SYS_FSTAT, uintptr(fd), uintptr(unsafe.Pointer(st)), 0)
	return ret(int(r), e)
}

func Mkdir(p *byte, mode int) int {
	r, _, e := syscall.Syscall(syscall.SYS_MKDIRAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)), uintptr(mode))
	return ret(int(r), e)
}

func Rmdir(p *byte) int {
	r, _, e := syscall.Syscall(syscall.SYS_UNLINKAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)), atRemovedir)
	return ret(int(r), e)
}

func Rename(oldp, newp *byte) int {
	r, _, e := syscall.Syscall6(syscall.SYS_RENAMEAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(oldp)),
		uintptr(atFDCWD), uintptr(unsafe.Pointer(newp)), 0, 0)
	return ret(int(r), e)
}

func Chdir(p *byte) int {
	r, _, e := syscall.Syscall(syscall.SYS_CHDIR, uintptr(unsafe.Pointer(p)), 0, 0)
	return ret(int(r), e)
}

// Getcwd stores the working directory in buf (size bytes) and returns buf, or nil on failure.
func Getcwd(buf *byte, size int) *byte {
	_, _, e := syscall.Syscall(syscall.SYS_GETCWD, uintptr(unsafe.Pointer(buf)), uintptr(size), 0)
	if e != 0 {
		SetErrno(e)
		return nil
	}
	return buf
}

func Chmod(p *byte, mode int) int {
	r, _, e := syscall.Syscall(syscall.SYS_FCHMODAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)), uintptr(mode))
	return ret(int(r), e)
}

// Access checks permissions: mode is F_OK or a combination of R_OK, W_OK, X_OK.
func Access(p *byte, mode int) int {
	r, _, e := syscall.Syscall(syscall.SYS_FACCESSAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)), uintptr(mode))
	return ret(int(r), e)
}

func Truncate(p *byte, length int64) int {
	r, _, e := syscall.Syscall(syscall.SYS_TRUNCATE, uintptr(unsafe.Pointer(p)), uintptr(length), 0)
	return ret(int(r), e)
}

func Ftruncate(fd int, length int64) int {
	r, _, e := syscall.Syscall(syscall.SYS_FTRUNCATE, uintptr(fd), uintptr(length), 0)
	return ret(int(r), e)
}

// Readlink stores the target of the symlink at p in buf (at most n bytes) and
// returns its length. As in C, the result is not NUL-terminated and is
// silently truncated if n is too small.
func Readlink(p, buf *byte, n int) int {
	r, _, e := syscall.Syscall6(syscall.SYS_READLINKAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(buf)), uintptr(n), 0, 0)
	return ret(int(r), e)
}

// Symlink creates a symbolic link at linkpath pointing to target.
func Symlink(target, linkpath *byte) int {
	r, _, e := syscall.Syscall(syscall.SYS_SYMLINKAT, uintptr(unsafe.Pointer(target)), uintptr(atFDCWD),
		uintptr(unsafe.Pointer(linkpath)))
	return ret(int(r), e)
}

// Link creates a hard link newp for the existing file oldp.
func Link(oldp, newp *byte) int {
	r, _, e := syscall.Syscall6(syscall.SYS_LINKAT, uintptr(atFDCWD), uintptr(unsafe.Pointer(oldp)),
		uintptr(atFDCWD), uintptr(unsafe.Pointer(newp)), 0, 0)
	return ret(int(r), e)
}

func Dup(fd int) int {
	r, _, e := syscall.Syscall(syscall.SYS_DUP, uintptr(fd), 0, 0)
	return ret(int(r), e)
}

// Dup2 makes newfd a copy of oldfd. (arm64 has only dup3, so this uses it.)
func Dup2(oldfd, newfd int) int {
	if oldfd == newfd { // dup3 rejects this case; dup2 just validates the descriptor
		if Fcntl(oldfd, F_GETFD, 0) < 0 {
			return -1
		}
		return newfd
	}
	r, _, e := syscall.Syscall(syscall.SYS_DUP3, uintptr(oldfd), uintptr(newfd), 0)
	return ret(int(r), e)
}

// Pipe creates a pipe: fds[0] is the read end, fds[1] the write end.
func Pipe(fds *[2]int) int {
	var p [2]int32
	r, _, e := syscall.Syscall(syscall.SYS_PIPE2, uintptr(unsafe.Pointer(&p)), 0, 0)
	if e != 0 {
		return ret(int(r), e)
	}
	fds[0], fds[1] = int(p[0]), int(p[1])
	return 0
}

// File-descriptor control (fcntl.h) and device control (sys/ioctl.h).

const (
	F_DUPFD         = 0
	F_GETFD         = 1
	F_SETFD         = 2
	F_GETFL         = 3
	F_SETFL         = 4
	F_GETLK         = 5
	F_SETLK         = 6
	F_SETLKW        = 7
	F_DUPFD_CLOEXEC = 1030
	FD_CLOEXEC      = 1

	F_RDLCK = 0
	F_WRLCK = 1
	F_UNLCK = 2

	O_NONBLOCK = syscall.O_NONBLOCK
	O_CLOEXEC  = syscall.O_CLOEXEC

	TIOCGWINSZ = syscall.TIOCGWINSZ
	TIOCSWINSZ = syscall.TIOCSWINSZ
	FIONREAD   = syscall.TIOCINQ
)

// Flock_t describes a file lock for F_GETLK, F_SETLK and F_SETLKW.
type Flock_t = syscall.Flock_t

// Winsize is the terminal size used with TIOCGWINSZ and TIOCSWINSZ.
type Winsize struct{ Row, Col, Xpixel, Ypixel uint16 }

// Fcntl performs an fcntl command whose argument is an integer (F_DUPFD, F_GETFD,
// F_SETFD, F_GETFL, F_SETFL, F_DUPFD_CLOEXEC, ...).
func Fcntl(fd, cmd, arg int) int {
	r, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(cmd), uintptr(arg))
	return ret(int(r), e)
}

// FcntlLock performs a lock command (F_GETLK, F_SETLK, F_SETLKW) on fd.
func FcntlLock(fd, cmd int, fl *Flock_t) int {
	r, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(cmd), uintptr(unsafe.Pointer(fl)))
	return ret(int(r), e)
}

// Ioctl performs a device control request whose argument is a pointer.
func Ioctl(fd int, req uintptr, arg unsafe.Pointer) int {
	r, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	return ret(int(r), e)
}

// IoctlInt performs a device control request whose argument is an integer.
func IoctlInt(fd int, req uintptr, arg int) int {
	r, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	return ret(int(r), e)
}

func idcall(trap uintptr) int {
	r, _, _ := syscall.RawSyscall(trap, 0, 0, 0)
	return int(r)
}

func Getppid() int { return idcall(syscall.SYS_GETPPID) }
func Getuid() int  { return idcall(syscall.SYS_GETUID) }
func Geteuid() int { return idcall(syscall.SYS_GETEUID) }
func Getgid() int  { return idcall(syscall.SYS_GETGID) }
func Getegid() int { return idcall(syscall.SYS_GETEGID) }

// Umask sets the file-creation mask and returns the previous one.
func Umask(mask int) int {
	r, _, _ := syscall.RawSyscall(syscall.SYS_UMASK, uintptr(mask), 0, 0)
	return int(r)
}

// Sleep sleeps for sec seconds.
func Sleep(sec uint) int {
	ts := Timespec{Sec: int64(sec)}
	return Nanosleep(&ts, nil)
}
