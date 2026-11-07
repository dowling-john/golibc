//go:build linux

package golibc

import (
	"math"
	"strconv"
	"unsafe"
)

// printf family. Arguments are Go values passed variadically. Each integer
// argument keeps its own Go width: an int32(-1) printed with %u gives
// 4294967295, an int(-1) gives 18446744073709551615, unless a length
// modifier (hh, h, l, ll, z, j, t) says otherwise. %s accepts *byte (a C
// string) or a Go string; %p accepts pointers; %f/%e/%g accept float32/64.
// Supported: flags "-+ #0", width and precision (also '*'), d i u o x X c s p
// f F e E g G %. Not supported: %n, %a, %lc/%ls, positional arguments.

type spec struct {
	minus, plus, space, alt, zero bool
	width, prec                   int
	hasPrec                       bool
	mod                           string
}

// argInt returns the integer value of a, sign-extended to 64 bits.
func argInt(a any) (bits uint64, signed bool, width int, ok bool) {
	switch v := a.(type) {
	case int:
		return uint64(v), true, 64, true
	case int8:
		return uint64(int64(v)), true, 8, true
	case int16:
		return uint64(int64(v)), true, 16, true
	case int32:
		return uint64(int64(v)), true, 32, true
	case int64:
		return uint64(v), true, 64, true
	case uint:
		return uint64(v), false, 64, true
	case uint8:
		return uint64(v), false, 8, true
	case uint16:
		return uint64(v), false, 16, true
	case uint32:
		return uint64(v), false, 32, true
	case uint64:
		return v, false, 64, true
	case uintptr:
		return uint64(v), false, 64, true
	case bool:
		if v {
			return 1, true, 32, true
		}
		return 0, true, 32, true
	}
	return 0, false, 0, false
}

func argFloat(a any) (float64, bool) {
	switch v := a.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	}
	return 0, false
}

func appendN(dst []byte, c byte, n int) []byte {
	for ; n > 0; n-- {
		dst = append(dst, c)
	}
	return dst
}

func upper(b []byte) []byte {
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return b
}

// emit appends prefix+body padded to the field width.
func emit(dst []byte, s *spec, prefix, body []byte, zeroOK bool) []byte {
	fill := s.width - len(prefix) - len(body)
	switch {
	case fill <= 0:
		dst = append(append(dst, prefix...), body...)
	case s.minus:
		dst = append(append(dst, prefix...), body...)
		dst = appendN(dst, ' ', fill)
	case s.zero && zeroOK:
		dst = append(dst, prefix...)
		dst = appendN(dst, '0', fill)
		dst = append(dst, body...)
	default:
		dst = appendN(dst, ' ', fill)
		dst = append(append(dst, prefix...), body...)
	}
	return dst
}

func fmtInt(conv byte, s *spec, bits uint64, signed bool, width int) (prefix, body []byte) {
	var mag uint64
	base := 10
	switch conv {
	case 'd', 'i':
		x := int64(bits)
		switch s.mod {
		case "hh":
			x = int64(int8(x))
		case "h":
			x = int64(int16(x))
		}
		if x < 0 {
			prefix = []byte{'-'}
			mag = uint64(-x)
		} else {
			mag = uint64(x)
			if s.plus {
				prefix = []byte{'+'}
			} else if s.space {
				prefix = []byte{' '}
			}
		}
	default:
		mag = bits
		switch {
		case s.mod == "hh":
			mag &= 0xff
		case s.mod == "h":
			mag &= 0xffff
		case s.mod == "" && signed && width < 64:
			mag &= 1<<width - 1
		}
		switch conv {
		case 'o':
			base = 8
		case 'x', 'X':
			base = 16
		}
	}
	if s.hasPrec && s.prec == 0 && mag == 0 {
		body = nil
	} else {
		body = strconv.AppendUint(nil, mag, base)
	}
	if len(body) < s.prec {
		body = append(appendN(nil, '0', s.prec-len(body)), body...)
	}
	if s.alt {
		switch conv {
		case 'o':
			if len(body) == 0 || body[0] != '0' {
				body = append([]byte{'0'}, body...)
			}
		case 'x', 'X':
			if mag != 0 {
				prefix = []byte{'0', 'x'}
			}
		}
	}
	if conv == 'X' {
		body, prefix = upper(body), upper(prefix)
	}
	return
}

func fmtFloat(conv byte, s *spec, f float64) (prefix, body []byte, zeroOK bool) {
	if math.Signbit(f) {
		prefix = []byte{'-'}
	} else if s.plus {
		prefix = []byte{'+'}
	} else if s.space {
		prefix = []byte{' '}
	}
	f = math.Abs(f)
	lower := conv | 0x20
	switch {
	case math.IsInf(f, 0):
		body = []byte("inf")
	case math.IsNaN(f):
		body, prefix = []byte("nan"), nil
	default:
		prec := 6
		if s.hasPrec {
			prec = s.prec
		}
		zeroOK = true
		switch lower {
		case 'f':
			body = strconv.AppendFloat(nil, f, 'f', prec, 64)
			if s.alt && prec == 0 {
				body = append(body, '.')
			}
		case 'e':
			body = strconv.AppendFloat(nil, f, 'e', prec, 64)
		default:
			if prec == 0 {
				prec = 1
			}
			body = strconv.AppendFloat(nil, f, 'g', prec, 64)
		}
	}
	if conv != lower {
		body = upper(body)
	}
	return
}

// format renders a C format string with args, appending to dst.
func format(dst []byte, f *byte, args []any) []byte {
	ai := 0
	nextArg := func() any {
		if ai < len(args) {
			ai++
			return args[ai-1]
		}
		return nil
	}
	starInt := func() int {
		v, _, _, _ := argInt(nextArg())
		return int(int32(v))
	}
	i := 0
	for {
		c := at(f, i)
		if c == 0 {
			return dst
		}
		i++
		if c != '%' {
			dst = append(dst, c)
			continue
		}
		var s spec
	flags:
		for {
			switch at(f, i) {
			case '-':
				s.minus = true
			case '+':
				s.plus = true
			case ' ':
				s.space = true
			case '#':
				s.alt = true
			case '0':
				s.zero = true
			default:
				break flags
			}
			i++
		}
		if at(f, i) == '*' {
			i++
			if s.width = starInt(); s.width < 0 {
				s.minus, s.width = true, -s.width
			}
		} else {
			for Isdigit(int(at(f, i))) {
				s.width = s.width*10 + int(at(f, i)-'0')
				i++
			}
		}
		if at(f, i) == '.' {
			i++
			s.hasPrec = true
			if at(f, i) == '*' {
				i++
				if s.prec = starInt(); s.prec < 0 {
					s.hasPrec, s.prec = false, 0
				}
			} else {
				for Isdigit(int(at(f, i))) {
					s.prec = s.prec*10 + int(at(f, i)-'0')
					i++
				}
			}
		}
	length:
		for {
			switch at(f, i) {
			case 'h', 'l', 'L', 'z', 'j', 't', 'q':
				s.mod += string(at(f, i))
				i++
			default:
				break length
			}
		}
		conv := at(f, i)
		if conv == 0 {
			return dst
		}
		i++
		switch conv {
		case '%':
			dst = append(dst, '%')
		case 'd', 'i', 'u', 'o', 'x', 'X':
			bits, signed, width, ok := argInt(nextArg())
			if !ok {
				continue
			}
			p, b := fmtInt(conv, &s, bits, signed, width)
			dst = emit(dst, &s, p, b, !s.hasPrec)
		case 'c':
			bits, _, _, ok := argInt(nextArg())
			if ok {
				dst = emit(dst, &s, nil, []byte{byte(bits)}, false)
			}
		case 's':
			var body []byte
			switch v := nextArg().(type) {
			case *byte:
				if v == nil {
					body = []byte("(null)")
				} else if s.hasPrec {
					body = unsafe.Slice(v, Strnlen(v, s.prec))
				} else {
					body = unsafe.Slice(v, Strlen(v))
				}
			case string:
				body = []byte(v)
			default:
				continue
			}
			if s.hasPrec && len(body) > s.prec {
				body = body[:s.prec]
			}
			dst = emit(dst, &s, nil, body, false)
		case 'p':
			var addr uintptr
			switch v := nextArg().(type) {
			case unsafe.Pointer:
				addr = uintptr(v)
			case *byte:
				addr = uintptr(unsafe.Pointer(v))
			case uintptr:
				addr = v
			default:
				continue
			}
			if addr == 0 {
				dst = emit(dst, &s, nil, []byte("(nil)"), false)
			} else {
				dst = emit(dst, &s, []byte("0x"), strconv.AppendUint(nil, uint64(addr), 16), true)
			}
		case 'f', 'F', 'e', 'E', 'g', 'G':
			v, ok := argFloat(nextArg())
			if !ok {
				continue
			}
			p, b, z := fmtFloat(conv, &s, v)
			dst = emit(dst, &s, p, b, z)
		default: // unknown conversion: echo it
			dst = append(dst, '%', conv)
		}
	}
}

// VaList stands in for C's va_list: the arguments a variadic caller received.
// The v* functions take it where C takes a va_list, so a variadic wrapper
// passes its own args straight through.
type VaList = []any

// Vsnprintf writes at most size bytes (including the NUL) to dst and returns
// the length the full output would have had, as in C.
func Vsnprintf(dst *byte, size int, f *byte, ap VaList) int {
	out := format(nil, f, ap)
	if size > 0 {
		n := min(len(out), size-1)
		copy(unsafe.Slice(dst, size), out[:n])
		put(dst, n, 0)
	}
	return len(out)
}

// Snprintf is Vsnprintf with the arguments listed out.
func Snprintf(dst *byte, size int, f *byte, args ...any) int {
	return Vsnprintf(dst, size, f, args)
}

// Vsprintf writes to dst, which must be large enough. It returns the length.
func Vsprintf(dst, f *byte, ap VaList) int {
	out := format(nil, f, ap)
	copy(unsafe.Slice(dst, len(out)+1), append(out, 0))
	return len(out)
}

// Sprintf is Vsprintf with the arguments listed out.
func Sprintf(dst, f *byte, args ...any) int { return Vsprintf(dst, f, args) }

// Vasprintf allocates the result with Malloc (release with Free) and stores it in *strp.
func Vasprintf(strp **byte, f *byte, ap VaList) int {
	out := format(nil, f, ap)
	p := Malloc(len(out) + 1)
	if p == nil {
		return -1
	}
	b := unsafe.Slice((*byte)(p), len(out)+1)
	copy(b, out)
	b[len(out)] = 0
	*strp = &b[0]
	return len(out)
}

// Asprintf is Vasprintf with the arguments listed out.
func Asprintf(strp **byte, f *byte, args ...any) int { return Vasprintf(strp, f, args) }

// Vfprintf writes formatted output to fp.
func Vfprintf(fp *File, f *byte, ap VaList) int {
	out := format(nil, f, ap)
	if writeBytes(fp, out) != len(out) {
		return -1
	}
	return len(out)
}

// Fprintf is Vfprintf with the arguments listed out.
func Fprintf(fp *File, f *byte, args ...any) int { return Vfprintf(fp, f, args) }

// Vprintf writes formatted output to stdout.
func Vprintf(f *byte, ap VaList) int { return Vfprintf(Stdout, f, ap) }

func Printf(f *byte, args ...any) int { return Vfprintf(Stdout, f, args) }

// Vdprintf writes straight to a file descriptor, bypassing stdio buffering.
func Vdprintf(fd int, f *byte, ap VaList) int {
	out := format(nil, f, ap)
	if writeAll(fd, out) != len(out) {
		return -1
	}
	return len(out)
}

// Dprintf is Vdprintf with the arguments listed out.
func Dprintf(fd int, f *byte, args ...any) int { return Vdprintf(fd, f, args) }
