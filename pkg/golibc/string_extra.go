//go:build linux

package golibc

import (
	"syscall"
	"unsafe"
)

func advance(p *byte, n int) *byte { return (*byte)(unsafe.Add(unsafe.Pointer(p), n)) }

// Strdup returns a Malloc'd copy of s; release it with Free.
func Strdup(s *byte) *byte {
	n := Strlen(s) + 1
	p := Malloc(n)
	if p == nil {
		return nil
	}
	Memcpy(p, unsafe.Pointer(s), n)
	return (*byte)(p)
}

// Strndup returns a Malloc'd copy of at most n bytes of s, NUL-terminated.
func Strndup(s *byte, n int) *byte {
	n = Strnlen(s, n)
	p := Malloc(n + 1)
	if p == nil {
		return nil
	}
	Memcpy(p, unsafe.Pointer(s), n)
	put((*byte)(p), n, 0)
	return (*byte)(p)
}

func Strcasecmp(a, b *byte) int { return Strncasecmp(a, b, int(^uint(0)>>1)) }

func Strncasecmp(a, b *byte, n int) int {
	for i := 0; i < n; i++ {
		x, y := Tolower(int(at(a, i))), Tolower(int(at(b, i)))
		if x != y || x == 0 {
			return x - y
		}
	}
	return 0
}

// StrtokR splits s into tokens separated by bytes in delim, keeping its place in *save.
func StrtokR(s, delim *byte, save **byte) *byte {
	if s == nil {
		s = *save
	}
	if s == nil {
		return nil
	}
	s = advance(s, Strspn(s, delim))
	if *s == 0 {
		*save = nil
		return nil
	}
	end := advance(s, Strcspn(s, delim))
	if *end == 0 {
		*save = nil
	} else {
		*end = 0
		*save = advance(end, 1)
	}
	return s
}

var tokSave *byte

// Strtok is StrtokR with hidden global state; not safe for concurrent use.
func Strtok(s, delim *byte) *byte { return StrtokR(s, delim, &tokSave) }

// Strerror returns a message for errno value e. The memory is Go-managed.
func Strerror(e int) *byte { return CStr(syscall.Errno(e).Error()) }
