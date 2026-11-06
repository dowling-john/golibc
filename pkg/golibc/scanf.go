//go:build linux

package golibc

import (
	"math"
	"strconv"
)

// scanf family. Arguments are pointers to the destinations, and the pointer
// type decides the width, so length modifiers (h, hh, l, ll, z, j, t, L) are
// accepted and ignored:
//
//	%d %i %u %o %x %X   *int *int8..*int64 *uint *uint8..*uint64 *uintptr
//	%f %e %g %E %G      *float32 *float64
//	%s %[set]           *byte (a C buffer; a NUL is appended, so size it) or *string
//	%c                  *byte (width bytes are written, no NUL)
//	%n                  any integer pointer: characters consumed so far
//	%*d                 scan and discard
//
// The result is the number of assigned items, or EOF if input ended before
// anything was assigned. Not supported: %p, %a, wide characters, positional
// arguments. One deviation from C: scanf backs up at most one character, so
// an input such as "0xg" read with %x is a matching failure rather than "0"
// followed by "xg".

const (
	scanOK = iota
	scanMatchFail
	scanInputFail
)

type scanner struct {
	next func() int // next byte, or EOF
	back int        // pushed-back byte, or noBack
	n    int        // bytes consumed so far
}

const noBack = -2

func newScanner(next func() int) *scanner { return &scanner{next: next, back: noBack} }

func (s *scanner) get() int {
	c := s.back
	if c != noBack {
		s.back = noBack
	} else {
		c = s.next()
	}
	if c != EOF {
		s.n++
	}
	return c
}

func (s *scanner) unget(c int) {
	if c != EOF {
		s.n--
	}
	s.back = c
}

func (s *scanner) peek() int {
	c := s.get()
	s.unget(c)
	return c
}

func (s *scanner) skipSpace() {
	for Isspace(s.peek()) {
		s.get()
	}
}

// failure classifies an item that produced nothing. Reaching end of input
// before consuming any of the item is an input failure; after consuming some
// of it (a bare sign, a lone '.') it is a matching failure, as in glibc.
func (s *scanner) failure(start int) int {
	if s.peek() == EOF && s.n == start {
		return scanInputFail
	}
	return scanMatchFail
}

func digitOf(c int) int {
	if c < 0 {
		return -1
	}
	return digitVal(byte(c))
}

// scanInt reads an integer item. conv is one of d i u o x X.
func (s *scanner) scanInt(conv byte, width int) (v uint64, status int) {
	s.skipSpace()
	start := s.n
	left := width
	if left == 0 {
		left = math.MaxInt
	}
	if s.peek() == EOF {
		return 0, scanInputFail
	}
	neg := false
	if c := s.peek(); c == '+' || c == '-' {
		s.get()
		left--
		neg = c == '-'
	}
	base := 10
	switch conv {
	case 'i':
		base = 0
	case 'o':
		base = 8
	case 'x', 'X':
		base = 16
	}
	seen := false
	if left > 0 && (base == 0 || base == 16) && s.peek() == '0' {
		s.get()
		left--
		seen = true
		if left > 0 && s.peek()|0x20 == 'x' {
			s.get()
			left--
			base, seen = 16, false
		} else if base == 0 {
			base = 8
		}
	} else if base == 0 {
		base = 10
	}
	for left > 0 {
		d := digitOf(s.peek())
		if d < 0 || d >= base {
			break
		}
		s.get()
		left--
		seen = true
		v = v*uint64(base) + uint64(d)
	}
	if !seen {
		return 0, s.failure(start)
	}
	if neg {
		v = -v
	}
	return v, scanOK
}

// scanFloat reads a floating-point item (decimal, inf, nan).
func (s *scanner) scanFloat(width int) (float64, int) {
	s.skipSpace()
	start := s.n
	left := width
	if left == 0 {
		left = math.MaxInt
	}
	if s.peek() == EOF {
		return 0, scanInputFail
	}
	var text []byte
	take := func() { text = append(text, byte(s.get())); left-- }
	isDigit := func() bool { return left > 0 && Isdigit(s.peek()) }
	if c := s.peek(); c == '+' || c == '-' {
		take()
	}
	word := func(w string) bool { // case-insensitive literal; consumes what matches
		for i := 0; i < len(w); i++ {
			if left <= 0 || s.peek()|0x20 != int(w[i]) {
				return false
			}
			take()
		}
		return true
	}
	switch c := s.peek() | 0x20; {
	case c == 'i':
		if !word("inf") {
			return 0, scanMatchFail
		}
		word("inity")
	case c == 'n':
		if !word("nan") {
			return 0, scanMatchFail
		}
	default:
		n1, n2 := 0, 0
		for isDigit() {
			take()
			n1++
		}
		if left > 0 && s.peek() == '.' {
			take()
			for isDigit() {
				take()
				n2++
			}
		}
		if n1+n2 == 0 {
			return 0, s.failure(start)
		}
		if left > 0 && s.peek()|0x20 == 'e' {
			take()
			if left > 0 && (s.peek() == '+' || s.peek() == '-') {
				take()
			}
			if !isDigit() {
				return 0, scanMatchFail
			}
			for isDigit() {
				take()
			}
		}
	}
	v, err := strconv.ParseFloat(string(text), 64)
	if err != nil && v == 0 {
		return 0, scanMatchFail
	}
	return v, scanOK
}

func storeInt(dst any, v uint64) bool {
	switch p := dst.(type) {
	case *int:
		*p = int(v)
	case *int8:
		*p = int8(v)
	case *int16:
		*p = int16(v)
	case *int32:
		*p = int32(v)
	case *int64:
		*p = int64(v)
	case *uint:
		*p = uint(v)
	case *uint8:
		*p = uint8(v)
	case *uint16:
		*p = uint16(v)
	case *uint32:
		*p = uint32(v)
	case *uint64:
		*p = v
	case *uintptr:
		*p = uintptr(v)
	default:
		return false
	}
	return true
}

func storeFloat(dst any, v float64) bool {
	switch p := dst.(type) {
	case *float64:
		*p = v
	case *float32:
		*p = float32(v)
	default:
		return false
	}
	return true
}

func storeText(dst any, text []byte, terminate bool) bool {
	switch p := dst.(type) {
	case *byte:
		for i, c := range text {
			put(p, i, c)
		}
		if terminate {
			put(p, len(text), 0)
		}
	case *string:
		*p = string(text)
	default:
		return false
	}
	return true
}

// scanSet parses a %[...] set starting just after '['. It returns the
// membership table and the index just past the closing ']'.
func scanSet(f *byte, i int) (set [256]bool, end int, ok bool) {
	negate := false
	if at(f, i) == '^' {
		negate = true
		i++
	}
	first := true
	for {
		c := at(f, i)
		if c == 0 {
			return set, i, false
		}
		if c == ']' && !first {
			break
		}
		first = false
		if at(f, i+1) == '-' && at(f, i+2) != ']' && at(f, i+2) != 0 && at(f, i+2) >= c {
			for x := int(c); x <= int(at(f, i+2)); x++ {
				set[x] = true
			}
			i += 3
			continue
		}
		set[c] = true
		i++
	}
	if negate {
		for x := range set {
			set[x] = !set[x]
		}
	}
	return set, i + 1, true
}

func scan(s *scanner, f *byte, args []any) int {
	assigned, ai := 0, 0
	inputFail := false
	i := 0
loop:
	for {
		c := at(f, i)
		if c == 0 {
			break
		}
		i++
		if Isspace(int(c)) {
			s.skipSpace()
			continue
		}
		if c != '%' {
			if got := s.get(); got != int(c) {
				s.unget(got)
				inputFail = got == EOF
				break loop
			}
			continue
		}
		if at(f, i) == '%' {
			i++
			s.skipSpace()
			if got := s.get(); got != '%' {
				s.unget(got)
				inputFail = got == EOF
				break loop
			}
			continue
		}
		suppress := false
		if at(f, i) == '*' {
			suppress = true
			i++
		}
		width := 0
		for Isdigit(int(at(f, i))) {
			width = width*10 + int(at(f, i)-'0')
			i++
		}
		for {
			m := at(f, i)
			if m != 'h' && m != 'l' && m != 'L' && m != 'z' && m != 'j' && m != 't' && m != 'q' {
				break
			}
			i++
		}
		conv := at(f, i)
		if conv == 0 {
			break
		}
		i++

		var dst any
		if !suppress {
			if ai >= len(args) {
				break loop
			}
			dst = args[ai]
			ai++
		}
		status := scanOK
		switch conv {
		case 'd', 'i', 'u', 'o', 'x', 'X':
			var v uint64
			if v, status = s.scanInt(conv, width); status == scanOK && !suppress && !storeInt(dst, v) {
				break loop
			}
		case 'f', 'F', 'e', 'E', 'g', 'G':
			var v float64
			if v, status = s.scanFloat(width); status == scanOK && !suppress && !storeFloat(dst, v) {
				break loop
			}
		case 's':
			s.skipSpace()
			var text []byte
			for width == 0 || len(text) < width {
				if ch := s.peek(); ch == EOF || Isspace(ch) {
					break
				}
				text = append(text, byte(s.get()))
			}
			switch {
			case len(text) > 0:
				if !suppress && !storeText(dst, text, true) {
					break loop
				}
			case s.peek() == EOF:
				status = scanInputFail
			default:
				status = scanMatchFail
			}
		case 'c':
			w := max(width, 1)
			text := make([]byte, 0, w)
			for len(text) < w {
				ch := s.get()
				if ch == EOF {
					s.unget(ch)
					break
				}
				text = append(text, byte(ch))
			}
			if len(text) == 0 {
				status = scanInputFail
			} else if !suppress && !storeText(dst, text, false) {
				break loop // a short read stores what it got, as glibc does
			}
		case '[':
			set, end, ok := scanSet(f, i)
			if !ok {
				break loop
			}
			i = end
			var text []byte
			for width == 0 || len(text) < width {
				ch := s.peek()
				if ch == EOF || !set[ch] {
					break
				}
				text = append(text, byte(s.get()))
			}
			switch {
			case len(text) > 0:
				if !suppress && !storeText(dst, text, true) {
					break loop
				}
			case s.peek() == EOF:
				status = scanInputFail
			default:
				status = scanMatchFail
			}
		case 'n':
			if !suppress && !storeInt(dst, uint64(s.n)) {
				break loop
			}
			continue // %n neither consumes input nor counts as a conversion
		default:
			break loop
		}
		if status != scanOK {
			inputFail = status == scanInputFail
			break loop
		}
		if !suppress {
			assigned++
		}
	}
	if assigned == 0 && inputFail {
		return EOF
	}
	return assigned
}

// Sscanf reads formatted input from the C string str.
func Sscanf(str, format *byte, args ...any) int {
	pos := 0
	return scan(newScanner(func() int {
		c := at(str, pos)
		if c == 0 {
			return EOF
		}
		pos++
		return int(c)
	}), format, args)
}

// Fscanf reads formatted input from f.
func Fscanf(f *File, format *byte, args ...any) int {
	s := newScanner(func() int { return Fgetc(f) })
	n := scan(s, format, args)
	if s.back != noBack && s.back != EOF {
		Ungetc(s.back, f) // return the lookahead byte to the stream
	}
	return n
}

// Scanf reads formatted input from stdin.
func Scanf(format *byte, args ...any) int { return Fscanf(Stdin, format, args...) }
