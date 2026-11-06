//go:build linux

// Package golibc is a C standard library written in pure Go.
//
// It talks to the Linux kernel directly through raw system calls: no cgo, no
// libc, no third-party modules. Programs that use it build with
// CGO_ENABLED=0 into a single static binary that needs nothing installed on
// the target system except a kernel.
//
// The API follows C closely so that C code and C habits carry over: strings
// are NUL-terminated *byte pointers, memory is void* (unsafe.Pointer), errors
// are reported by returning -1 or nil and setting [Errno], and printf takes a
// C format string with variadic arguments. Names are the C names with an
// initial capital (strlen becomes [Strlen], fopen becomes [Fopen]).
//
// # Memory
//
// [Malloc] and friends allocate from mmap, outside the Go heap, so the
// garbage collector never sees that memory. Release it with [Free]. Strings
// built with [CStr] are ordinary Go memory and need no Free.
//
// # Buffered output
//
// Stdio streams keep their own buffers. End a program with [Exit] (or call
// Fflush(nil)) so buffered output is written; returning from main or calling
// os.Exit does not flush it.
//
// # Platform
//
// Only linux/amd64 and linux/arm64 are supported. On other systems the
// package fails to compile with a single explanatory error.
package golibc
