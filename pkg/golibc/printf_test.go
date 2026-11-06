//go:build linux

package golibc

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func parseArgs(t *testing.T, spec string) []any {
	var args []any
	if spec == "" {
		return nil
	}
	for _, a := range strings.Split(spec, ",") {
		k, v, _ := strings.Cut(a, ":")
		switch k {
		case "i", "c":
			n, _ := strconv.ParseInt(v, 10, 32)
			args = append(args, int32(n))
		case "l":
			n, _ := strconv.ParseInt(v, 10, 64)
			args = append(args, n)
		case "u":
			n, _ := strconv.ParseUint(v, 10, 32)
			args = append(args, uint32(n))
		case "ul":
			n, _ := strconv.ParseUint(v, 10, 64)
			args = append(args, n)
		case "s":
			args = append(args, CStr(v))
		case "d":
			switch v {
			case "inf":
				args = append(args, math.Inf(1))
			case "-inf":
				args = append(args, math.Inf(-1))
			case "nan":
				args = append(args, math.NaN())
			default:
				f, _ := strconv.ParseFloat(v, 64)
				args = append(args, f)
			}
		default:
			t.Fatalf("bad kind %q", k)
		}
	}
	return args
}

// TestPrintfGolden checks every case against output produced by real glibc
// printf (regenerate with scripts/gen_golden.sh).
func TestPrintfGolden(t *testing.T) {
	var cases []string
	for _, l := range readLines(t, "testdata/printf.cases") {
		if l != "" && !strings.HasPrefix(l, "#") {
			cases = append(cases, l)
		}
	}
	golden := readLines(t, "testdata/printf.golden")
	if len(cases) != len(golden) {
		t.Fatalf("%d cases but %d golden lines", len(cases), len(golden))
	}
	for i, c := range cases {
		format, args, _ := strings.Cut(c, "|")
		buf := make([]byte, 256)
		n := Snprintf(&buf[0], len(buf), CStr(format), parseArgs(t, args)...)
		if got := string(buf[:n]); got != golden[i] {
			t.Errorf("printf(%q, %s) = %q, glibc says %q", format, args, got, golden[i])
		}
	}
}

func TestSnprintfTruncation(t *testing.T) {
	buf := []byte("xxxxxxxxxx")
	n := Snprintf(&buf[0], 4, CStr("%s"), CStr("abcdefgh"))
	if n != 8 || string(buf[:5]) != "abc\x00x" {
		t.Errorf("n=%d buf=%q", n, buf)
	}
	n = Snprintf(&buf[0], 0, CStr("%d"), 12345) // size 0: count only, touch nothing
	if n != 5 || buf[0] != 'a' {
		t.Errorf("size 0: n=%d buf=%q", n, buf)
	}
	n = Snprintf(nil, 0, CStr("hello %d"), 1)
	if n != 7 {
		t.Errorf("nil dst count = %d", n)
	}
}

func TestPrintfGoTypes(t *testing.T) {
	// Go integers keep their width; strings and nil C strings are accepted.
	buf := make([]byte, 64)
	cases := []struct {
		f    string
		args []any
		want string
	}{
		{"%d %d", []any{7, int64(-8)}, "7 -8"},
		{"%u", []any{int32(-1)}, "4294967295"},
		{"%u", []any{-1}, "18446744073709551615"},
		{"%x", []any{uint8(255)}, "ff"},
		{"%s|%s", []any{"go", (*byte)(nil)}, "go|(null)"},
		{"%f", []any{float32(0.5)}, "0.500000"},
		{"%p", []any{unsafe.Pointer(nil)}, "(nil)"},
		{"%d", nil, ""}, // missing argument prints nothing
	}
	for _, c := range cases {
		n := Snprintf(&buf[0], len(buf), CStr(c.f), c.args...)
		if got := string(buf[:n]); got != c.want {
			t.Errorf("%q = %q, want %q", c.f, got, c.want)
		}
	}
	n := Snprintf(&buf[0], len(buf), CStr("%p"), unsafe.Pointer(&buf[0]))
	if s := string(buf[:n]); !strings.HasPrefix(s, "0x") || len(s) < 5 {
		t.Errorf("%%p = %q", s)
	}
}

func TestSprintfAsprintf(t *testing.T) {
	buf := make([]byte, 32)
	if n := Sprintf(&buf[0], CStr("%03d-%s"), 7, CStr("x")); n != 5 || GoStr(&buf[0]) != "007-x" {
		t.Errorf("Sprintf n=%d %q", n, GoStr(&buf[0]))
	}
	var p *byte
	if n := Asprintf(&p, CStr("%s/%d"), CStr("abc"), 12); n != 6 || GoStr(p) != "abc/12" {
		t.Errorf("Asprintf n=%d %q", n, GoStr(p))
	}
	Free(unsafe.Pointer(p))
}
