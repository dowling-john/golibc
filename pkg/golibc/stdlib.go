//go:build linux

package golibc

import (
	"math"
	"math/bits"
	"strconv"
	"strings"
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
		SetErrno(EINVAL)
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
			SetErrno(ERANGE)
			return math.MinInt64
		}
		return -int64(mag)
	}
	if ovf || mag > math.MaxInt64 {
		SetErrno(ERANGE)
		return math.MaxInt64
	}
	return int64(mag)
}

// Strtoul converts the initial part of s to an unsigned integer. Like C, a
// leading '-' negates modulo 2^64.
func Strtoul(s *byte, endp **byte, base int) uint64 {
	mag, neg, ovf := parseUint(s, endp, base)
	if ovf {
		SetErrno(ERANGE)
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
func Strtod(s *byte, endp **byte) float64 { return scanFloat(s, endp, 64) }

// Strtof is Strtod rounded to a float32. A value too large for a float32 gives
// ±Inf and sets errno to ERANGE.
func Strtof(s *byte, endp **byte) float32 { return float32(scanFloat(s, endp, 32)) }

// Strtold is Strtod. Go has no long double, so the result is a float64 and
// carries no more precision than Strtod does.
func Strtold(s *byte, endp **byte) float64 { return scanFloat(s, endp, 64) }

// scanFloat finds the longest numeric prefix of s and converts it for a float
// of the given width (32 or 64).
func scanFloat(s *byte, endp **byte, bits int) float64 {
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
	v, err := strconv.ParseFloat(string(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(s), start)), i-start)), bits)
	if err != nil && v != 0 { // overflow to ±Inf; plain syntax errors can't happen here
		SetErrno(ERANGE)
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

// swap exchanges two size-byte elements, a word at a time when alignment allows.
func swap(a, b unsafe.Pointer, size int) {
	if (uintptr(a)|uintptr(b)|uintptr(size))&7 == 0 {
		for ; size > 0; size -= 8 {
			x, y := (*uint64)(a), (*uint64)(b)
			*x, *y = *y, *x
			a, b = unsafe.Add(a, 8), unsafe.Add(b, 8)
		}
		return
	}
	x, y := mem(a, size), mem(b, size)
	for i := range x {
		x[i], y[i] = y[i], x[i]
	}
}

type sorter struct {
	base unsafe.Pointer
	size int
	cmp  func(a, b unsafe.Pointer) int
}

func (q *sorter) at(i int) unsafe.Pointer { return unsafe.Add(q.base, i*q.size) }
func (q *sorter) swap(i, j int)           { swap(q.at(i), q.at(j), q.size) }

func (q *sorter) insertion(lo, hi int) {
	for i := lo + 1; i < hi; i++ {
		for j := i; j > lo && q.cmp(q.at(j-1), q.at(j)) > 0; j-- {
			q.swap(j-1, j)
		}
	}
}

func (q *sorter) heapsort(lo, hi int) {
	n := hi - lo
	sift := func(root, end int) {
		for {
			child := 2*root + 1
			if child >= end {
				return
			}
			if child+1 < end && q.cmp(q.at(lo+child), q.at(lo+child+1)) < 0 {
				child++
			}
			if q.cmp(q.at(lo+root), q.at(lo+child)) >= 0 {
				return
			}
			q.swap(lo+root, lo+child)
			root = child
		}
	}
	for i := n/2 - 1; i >= 0; i-- {
		sift(i, n)
	}
	for end := n - 1; end > 0; end-- {
		q.swap(lo, lo+end)
		sift(0, end)
	}
}

// partition splits [lo,hi) around a median-of-three pivot and returns the
// pivot's final index. Requires hi-lo >= 3. The median-of-three leaves a
// sentinel at each end, so the scans below never run off the range.
func (q *sorter) partition(lo, hi int) int {
	mid := lo + (hi-lo)/2
	if q.cmp(q.at(mid), q.at(lo)) < 0 {
		q.swap(mid, lo)
	}
	if q.cmp(q.at(hi-1), q.at(mid)) < 0 {
		q.swap(hi-1, mid)
		if q.cmp(q.at(mid), q.at(lo)) < 0 {
			q.swap(mid, lo)
		}
	}
	q.swap(mid, hi-2) // park the pivot next to the high sentinel
	pivot := q.at(hi - 2)
	i, j := lo, hi-2
	for {
		for i++; q.cmp(q.at(i), pivot) < 0; i++ {
		}
		for j--; q.cmp(pivot, q.at(j)) < 0; j-- {
		}
		if i >= j {
			break
		}
		q.swap(i, j)
	}
	q.swap(i, hi-2)
	return i
}

func (q *sorter) sort(lo, hi, depth int) {
	for hi-lo > 12 {
		if depth == 0 { // too many bad pivots: fall back to the O(n log n) guarantee
			q.heapsort(lo, hi)
			return
		}
		depth--
		p := q.partition(lo, hi)
		if p-lo < hi-p-1 { // recurse into the smaller side so stack depth stays O(log n)
			q.sort(lo, p, depth)
			lo = p + 1
		} else {
			q.sort(p+1, hi, depth)
			hi = p
		}
	}
	q.insertion(lo, hi)
}

// Qsort sorts n elements of size bytes in place. It is an introsort:
// quicksort with median-of-three pivots, insertion sort for short ranges, and
// a heapsort fallback that bounds the worst case at O(n log n). It does not
// allocate and is not stable.
func Qsort(base unsafe.Pointer, n, size int, cmp func(a, b unsafe.Pointer) int) {
	if n < 2 || size <= 0 {
		return
	}
	q := sorter{base, size, cmp}
	q.sort(0, n, 2*bits.Len(uint(n)))
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

// Getenv returns the value of the named variable as a C string, or nil.
// The returned memory is Go-managed.
func Getenv(name *byte) *byte {
	if v, ok := syscall.Getenv(GoStr(name)); ok {
		return CStr(v)
	}
	return nil
}

// Setenv sets the named variable. With overwrite 0 an existing value is kept.
// It returns -1 with errno EINVAL if name is empty or contains '='. The
// change is visible to Getenv and to children started with Spawn.
func Setenv(name, value *byte, overwrite int) int {
	n := GoStr(name)
	if overwrite == 0 {
		if _, ok := syscall.Getenv(n); ok {
			return 0
		}
	}
	if syscall.Setenv(n, GoStr(value)) != nil {
		SetErrno(EINVAL)
		return -1
	}
	return 0
}

// Unsetenv removes the named variable. It returns -1 with errno EINVAL if
// name is empty or contains '='.
func Unsetenv(name *byte) int {
	n := GoStr(name)
	if n == "" || strings.IndexByte(n, '=') >= 0 {
		SetErrno(EINVAL)
		return -1
	}
	syscall.Unsetenv(n)
	return 0
}

// Putenv adds or replaces a variable from a "NAME=value" string, as glibc
// does; a string with no '=' removes the variable. Unlike C, the string is
// copied, so later changes to the caller's buffer do not affect the
// environment.
func Putenv(str *byte) int {
	s := GoStr(str)
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return Unsetenv(str)
	}
	if syscall.Setenv(s[:i], s[i+1:]) != nil {
		SetErrno(EINVAL)
		return -1
	}
	return 0
}

// Clearenv removes every variable.
func Clearenv() int {
	syscall.Clearenv()
	return 0
}

var (
	quickMu    sync.Mutex
	quickFuncs []func()
)

// AtQuickExit registers fn to run, in reverse order, when QuickExit is called.
func AtQuickExit(fn func()) int {
	quickMu.Lock()
	quickFuncs = append(quickFuncs, fn)
	quickMu.Unlock()
	return 0
}

// QuickExit runs the AtQuickExit handlers and ends the process. Unlike Exit it
// does not run Atexit handlers and does not flush stdio.
func QuickExit(code int) {
	for {
		quickMu.Lock()
		n := len(quickFuncs)
		if n == 0 {
			quickMu.Unlock()
			break
		}
		fn := quickFuncs[n-1]
		quickFuncs = quickFuncs[:n-1]
		quickMu.Unlock()
		fn()
	}
	ExitNow(code)
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
