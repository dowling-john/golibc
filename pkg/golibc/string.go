//go:build linux

package golibc

import (
	"bytes"
	"unsafe"
)

// Char is a C `char *` style pointer to a NUL-terminated string.
// Memory is raw: callers own bounds, as in C.

func at(p *byte, i int) byte { return *(*byte)(unsafe.Add(unsafe.Pointer(p), i)) }

func put(p *byte, i int, b byte) { *(*byte)(unsafe.Add(unsafe.Pointer(p), i)) = b }

func mem(p unsafe.Pointer, n int) []byte { return unsafe.Slice((*byte)(p), n) }

// Memcpy copies n bytes from src to dst. The regions must not overlap.
func Memcpy(dst, src unsafe.Pointer, n int) unsafe.Pointer {
	if n > 0 {
		copy(mem(dst, n), mem(src, n))
	}
	return dst
}

// Memmove copies n bytes from src to dst. The regions may overlap.
func Memmove(dst, src unsafe.Pointer, n int) unsafe.Pointer {
	if n > 0 {
		copy(mem(dst, n), mem(src, n)) // Go's copy has memmove semantics
	}
	return dst
}

// Memset fills n bytes at p with the low byte of c.
func Memset(p unsafe.Pointer, c int, n int) unsafe.Pointer {
	b := mem(p, n)
	for i := range b {
		b[i] = byte(c)
	}
	return p
}

// Memcmp compares n bytes and returns <0, 0, or >0.
func Memcmp(a, b unsafe.Pointer, n int) int {
	x, y := mem(a, n), mem(b, n)
	for i := range x {
		if x[i] != y[i] {
			return int(x[i]) - int(y[i])
		}
	}
	return 0
}

// Memchr returns a pointer to the first byte equal to c in the first n bytes, or nil.
func Memchr(p unsafe.Pointer, c int, n int) unsafe.Pointer {
	if n <= 0 {
		return nil
	}
	if i := bytes.IndexByte(mem(p, n), byte(c)); i >= 0 {
		return unsafe.Add(p, i)
	}
	return nil
}

// Strlen returns the length of s, excluding the terminating NUL.
func Strlen(s *byte) int {
	n := 0
	for at(s, n) != 0 {
		n++
	}
	return n
}

// Strnlen is Strlen limited to max bytes.
func Strnlen(s *byte, max int) int {
	n := 0
	for n < max && at(s, n) != 0 {
		n++
	}
	return n
}

// Strcpy copies src, including its NUL, to dst.
func Strcpy(dst, src *byte) *byte {
	i := 0
	for {
		c := at(src, i)
		put(dst, i, c)
		if c == 0 {
			return dst
		}
		i++
	}
}

// Strncpy copies at most n bytes from src, NUL-padding dst if src is shorter.
func Strncpy(dst, src *byte, n int) *byte {
	i := 0
	for ; i < n; i++ {
		c := at(src, i)
		put(dst, i, c)
		if c == 0 {
			i++
			break
		}
	}
	for ; i < n; i++ {
		put(dst, i, 0)
	}
	return dst
}

// Strcat appends src to the end of dst.
func Strcat(dst, src *byte) *byte {
	Strcpy((*byte)(unsafe.Add(unsafe.Pointer(dst), Strlen(dst))), src)
	return dst
}

// Strncat appends at most n bytes of src to dst, always NUL-terminating.
func Strncat(dst, src *byte, n int) *byte {
	d := Strlen(dst)
	i := 0
	for ; i < n; i++ {
		c := at(src, i)
		if c == 0 {
			break
		}
		put(dst, d+i, c)
	}
	put(dst, d+i, 0)
	return dst
}

// Strcmp compares two strings and returns <0, 0, or >0.
func Strcmp(a, b *byte) int {
	for i := 0; ; i++ {
		x, y := at(a, i), at(b, i)
		if x != y || x == 0 {
			return int(x) - int(y)
		}
	}
}

// Strncmp compares at most n bytes of two strings.
func Strncmp(a, b *byte, n int) int {
	for i := 0; i < n; i++ {
		x, y := at(a, i), at(b, i)
		if x != y || x == 0 {
			return int(x) - int(y)
		}
	}
	return 0
}

// Strchr returns a pointer to the first c in s (c == 0 finds the terminator), or nil.
func Strchr(s *byte, c int) *byte {
	for i := 0; ; i++ {
		v := at(s, i)
		if v == byte(c) {
			return (*byte)(unsafe.Add(unsafe.Pointer(s), i))
		}
		if v == 0 {
			return nil
		}
	}
}

// Strrchr returns a pointer to the last c in s, or nil.
func Strrchr(s *byte, c int) *byte {
	var last *byte
	for i := 0; ; i++ {
		v := at(s, i)
		if v == byte(c) {
			last = (*byte)(unsafe.Add(unsafe.Pointer(s), i))
		}
		if v == 0 {
			return last
		}
	}
}

// Strstr returns a pointer to the first occurrence of needle in hay, or nil.
func Strstr(hay, needle *byte) *byte {
	nl := Strlen(needle)
	if nl == 0 {
		return hay
	}
	for i := 0; at(hay, i) != 0; i++ {
		if Strncmp((*byte)(unsafe.Add(unsafe.Pointer(hay), i)), needle, nl) == 0 {
			return (*byte)(unsafe.Add(unsafe.Pointer(hay), i))
		}
	}
	return nil
}

func inSet(set *byte, c byte) bool {
	for i := 0; at(set, i) != 0; i++ {
		if at(set, i) == c {
			return true
		}
	}
	return false
}

// Strspn returns the length of the initial segment of s made only of bytes in accept.
func Strspn(s, accept *byte) int {
	n := 0
	for at(s, n) != 0 && inSet(accept, at(s, n)) {
		n++
	}
	return n
}

// Strcspn returns the length of the initial segment of s containing no bytes from reject.
func Strcspn(s, reject *byte) int {
	n := 0
	for at(s, n) != 0 && !inSet(reject, at(s, n)) {
		n++
	}
	return n
}

// Strpbrk returns a pointer to the first byte in s that is in accept, or nil.
func Strpbrk(s, accept *byte) *byte {
	for i := 0; at(s, i) != 0; i++ {
		if inSet(accept, at(s, i)) {
			return (*byte)(unsafe.Add(unsafe.Pointer(s), i))
		}
	}
	return nil
}
