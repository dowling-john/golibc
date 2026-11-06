package golibc

import (
	"syscall"
	"unsafe"
)

// Process control (unistd.h, sys/wait.h).
//
// There is no Fork. A raw fork(2) in a multi-threaded Go program leaves the
// child with a single thread and no working scheduler, so its first garbage
// collection hangs. Spawn (a safe fork+exec from the standard library) and
// Execve cover what fork is normally used for.

// Waitpid options.
const (
	WNOHANG   = 1
	WUNTRACED = 2
)

func WIFEXITED(s int) bool   { return s&0x7f == 0 }
func WEXITSTATUS(s int) int  { return (s >> 8) & 0xff }
func WIFSIGNALED(s int) bool { return s&0x7f != 0 && s&0x7f != 0x7f }
func WTERMSIG(s int) int     { return s & 0x7f }

// Execve replaces the current process. It returns only on failure.
func Execve(p *byte, argv, envp []string) int {
	if err := syscall.Exec(GoStr(p), argv, envp); err != nil {
		SetErrno(err.(syscall.Errno))
	}
	return -1
}

// Spawn starts a new process running path with stdin, stdout and stderr
// inherited. A nil envp means the current environment. It returns the child's
// pid, or -1 on failure.
func Spawn(p *byte, argv, envp []string) int {
	if envp == nil {
		envp = syscall.Environ()
	}
	pid, err := syscall.ForkExec(GoStr(p), argv, &syscall.ProcAttr{Env: envp, Files: []uintptr{0, 1, 2}})
	if err != nil {
		SetErrno(err.(syscall.Errno))
		return -1
	}
	return pid
}

// Waitpid waits for a child to change state and stores its status in *status
// (which may be nil). Decode the status with WIFEXITED, WEXITSTATUS and friends.
func Waitpid(pid int, status *int, options int) int {
	for {
		var ws int32
		r, _, e := syscall.Syscall6(syscall.SYS_WAIT4, uintptr(pid), uintptr(unsafe.Pointer(&ws)), uintptr(options), 0, 0, 0)
		if e == EINTR {
			continue
		}
		if e != 0 {
			SetErrno(e)
			return -1
		}
		if status != nil {
			*status = int(ws)
		}
		return int(r)
	}
}

// System runs cmd with /bin/sh -c and returns the wait status (or -1). Like C's
// system, it needs a shell on the target system.
func System(cmd *byte) int {
	pid := Spawn(CStr("/bin/sh"), []string{"sh", "-c", GoStr(cmd)}, nil)
	if pid < 0 {
		return -1
	}
	var status int
	if Waitpid(pid, &status, 0) < 0 {
		return -1
	}
	return status
}
