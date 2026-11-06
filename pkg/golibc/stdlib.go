//go:build linux

package golibc

import (
	"math"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

const (
	RAND_MAX     = 1<<31 - 1
	EXIT_SUCCESS = 0
	EXIT_FAILURE = 1
)

// CStr returns a NUL-terminated copy of s. The memory is Go-managed (no Free needed).
func CStr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// GoStr converts a C string to a Go string.
func GoStr(p *byte) string {
	if p == nil {
		return ""
	}
	return string(unsafe.Slice(p, Strlen(p)))
}

func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

func setEnd(endp **byte, s *byte, i int) {
	if endp != nil {
		*endp = (*byte)(unsafe.Add(unsafe.Pointer(s), i))
	}
}

// parseUint scans the integer syntax shared by the strto* family.
func parseUint(s *byte, endp **byte, base int) (mag uint64, neg, ovf bool) {
	i := 0
	for Isspace(int(at(s, i))) {
		i++
	}
	switch at(s, i) {
	case '-':
		neg = true
		i++
	case '+':
		i++
	}
	if (base == 0 || base == 16) && at(s, i) == '0' && at(s, i+1)|0x20 == 'x' && digitVal(at(s, i+2)) >= 0 && digitVal(at(s, i+2)) < 16 {
		i += 2
		base = 16
	} else if base == 0 {
		base = 10
		if at(s, i) == '0' {
			base = 8
		}
	}
	if base < 2 || base > 36 {
		Errno = EINVAL
		setEnd(endp, s, 0)
		return 0, false, false
	}
	start := i
	for {
		d := digitVal(at(s, i))
		if d < 0 || d >= base {
			break
		}
		if mag > (math.MaxUint64-uint64(d))/uint64(base) {
			ovf = true
		} else {
			mag = mag*uint64(base) + uint64(d)
		}
		i++
	}
	if i == start { // no digits: no conversion performed
		setEnd(endp, s, 0)
		return 0, false, false
	}
	setEnd(endp, s, i)
	return
}

// Strtol converts the initial part of s to a signed integer in the given base (0 = auto).
func Strtol(s *byte, endp **byte, base int) int64 {
	mag, neg, ovf := parseUint(s, endp, base)
	if neg {
		if ovf || mag > 1<<63 {
			Errno = ERANGE
			return math.MinInt64
		}
		return -int64(mag)
	}
	if ovf || mag > math.MaxInt64 {
		Errno = ERANGE
		return math.MaxInt64
	}
	return int64(mag)
}

// Strtoul converts the initial part of s to an unsigned integer. Like C, a
// leading '-' negates modulo 2^64.
func Strtoul(s *byte, endp **byte, base int) uint64 {
	mag, neg, ovf := parseUint(s, endp, base)
	if ovf {
		Errno = ERANGE
		return math.MaxUint64
	}
	if neg {
		return -mag
	}
	return mag
}

// long long is 64-bit here, so the ll variants are the same functions.
func Strtoll(s *byte, endp **byte, base int) int64   { return Strtol(s, endp, base) }
func Strtoull(s *byte, endp **byte, base int) uint64 { return Strtoul(s, endp, base) }

func Atoi(s *byte) int    { return int(Strtol(s, nil, 10)) }
func Atol(s *byte) int64  { return Strtol(s, nil, 10) }
func Atoll(s *byte) int64 { return Strtol(s, nil, 10) }

// Strtod converts the initial part of s to a float64 (decimal, inf, nan).
func Strtod(s *byte, endp **byte) float64 {
	i := 0
	for Isspace(int(at(s, i))) {
		i++
	}
	start := i
	if c := at(s, i); c == '+' || c == '-' {
		i++
	}
	word := func(w string) bool {
		for j := 0; j < len(w); j++ {
			if at(s, i+j)|0x20 != w[j] {
				return false
			}
		}
		i += len(w)
		return true
	}
	switch {
	case word("infinity"), word("inf"), word("nan"):
	default:
		n1 := 0
		for Isdigit(int(at(s, i))) {
			i++
			n1++
		}
		n2 := 0
		if at(s, i) == '.' {
			i++
			for Isdigit(int(at(s, i))) {
				i++
				n2++
			}
		}
		if n1+n2 == 0 {
			setEnd(endp, s, 0)
			return 0
		}
		if at(s, i)|0x20 == 'e' {
			j := i + 1
			if c := at(s, j); c == '+' || c == '-' {
				j++
			}
			if Isdigit(int(at(s, j))) {
				for Isdigit(int(at(s, j))) {
					j++
				}
				i = j
			}
		}
	}
	v, err := strconv.ParseFloat(string(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(s), start)), i-start)), 64)
	if err != nil && v != 0 { // overflow to ±Inf; plain syntax errors can't happen here
		Errno = ERANGE
	}
	setEnd(endp, s, i)
	return v
}

func Atof(s *byte) float64 { return Strtod(s, nil) }

func Abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func Labs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

type DivT struct{ Quot, Rem int }

func Div(num, den int) DivT { return DivT{num / den, num % den} }

func swap(a, b unsafe.Pointer, size int) {
	x, y := mem(a, size), mem(b, size)
	for i := range x {
		x[i], y[i] = y[i], x[i]
	}
}

// Qsort sorts n elements of size bytes in place. It is a heapsort: O(n log n)
// worst case, no recursion, no allocation, and not stable.
func Qsort(base unsafe.Pointer, n, size int, cmp func(a, b unsafe.Pointer) int) {
	if n < 2 || size <= 0 {
		return
	}
	el := func(i int) unsafe.Pointer { return unsafe.Add(base, i*size) }
	sift := func(root, end int) {
		for {
			child := 2*root + 1
			if child >= end {
				return
			}
			if child+1 < end && cmp(el(child), el(child+1)) < 0 {
				child++
			}
			if cmp(el(root), el(child)) >= 0 {
				return
			}
			swap(el(root), el(child), size)
			root = child
		}
	}
	for i := n/2 - 1; i >= 0; i-- {
		sift(i, n)
	}
	for end := n - 1; end > 0; end-- {
		swap(el(0), el(end), size)
		sift(0, end)
	}
}

// Bsearch searches a sorted array for key, returning a pointer to a match or nil.
func Bsearch(key, base unsafe.Pointer, n, size int, cmp func(a, b unsafe.Pointer) int) unsafe.Pointer {
	lo, hi := 0, n
	for lo < hi {
		mid := lo + (hi-lo)/2
		p := unsafe.Add(base, mid*size)
		switch r := cmp(key, p); {
		case r == 0:
			return p
		case r < 0:
			hi = mid
		default:
			lo = mid + 1
		}
	}
	return nil
}

var (
	randMu    sync.Mutex
	randState uint64 = 1
)

// Srand seeds Rand.
func Srand(seed uint) {
	randMu.Lock()
	randState = uint64(seed)
	randMu.Unlock()
}

// Rand returns a pseudo-random int in [0, RAND_MAX] (64-bit LCG, top bits).
func Rand() int {
	randMu.Lock()
	randState = randState*6364136223846793005 + 1442695040888963407
	v := randState >> 33
	randMu.Unlock()
	return int(v)
}

// Getenv returns the value of the named variable as a C string, or nil.
// The returned memory is Go-managed.
func Getenv(name *byte) *byte {
	if v, ok := syscall.Getenv(GoStr(name)); ok {
		return CStr(v)
	}
	return nil
}

var (
	exitMu    sync.Mutex
	exitFuncs []func()
)

// Atexit registers fn to run, in reverse order, when Exit is called.
func Atexit(fn func()) int {
	exitMu.Lock()
	exitFuncs = append(exitFuncs, fn)
	exitMu.Unlock()
	return 0
}

func runAtexit() {
	for {
		exitMu.Lock()
		n := len(exitFuncs)
		if n == 0 {
			exitMu.Unlock()
			return
		}
		fn := exitFuncs[n-1]
		exitFuncs = exitFuncs[:n-1]
		exitMu.Unlock()
		fn()
	}
}

// Exit runs atexit handlers, flushes all stdio streams, and terminates the
// process. Programs using golibc stdio must exit through this (or call
// Fflush(nil)) or buffered output is lost.
func Exit(code int) {
	runAtexit()
	Fflush(nil)
	ExitNow(code)
}

// Abort terminates the process abnormally with SIGABRT, without flushing.
func Abort() {
	Kill(Getpid(), int(syscall.SIGABRT))
	ExitNow(134)
}
