//go:build linux

package golibc

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// scanDest is one destination from a scan.cases "kinds" column, with the same
// sentinel the C generator used so untouched values compare equal.
type scanDest struct {
	ptr  any
	show func() string
}

func newScanDest(kind string) scanDest {
	fmtVal := func(f string, v any) string {
		buf := make([]byte, 64)
		n := Snprintf(&buf[0], len(buf), CStr(f), v)
		return string(buf[:n])
	}
	switch kind {
	case "i":
		v := int32(-77777)
		return scanDest{&v, func() string { return fmtVal("%d", v) }}
	case "l":
		v := int64(-77777)
		return scanDest{&v, func() string { return fmtVal("%ld", v) }}
	case "h":
		v := int16(-7777)
		return scanDest{&v, func() string { return fmtVal("%d", v) }}
	case "hh":
		v := int8(-77)
		return scanDest{&v, func() string { return fmtVal("%d", v) }}
	case "u":
		v := uint32(77777)
		return scanDest{&v, func() string { return fmtVal("%u", v) }}
	case "ul":
		v := uint64(77777)
		return scanDest{&v, func() string { return fmtVal("%lu", v) }}
	case "d":
		v := float64(-7.5)
		return scanDest{&v, func() string { return fmtVal("%.17g", v) }}
	case "f":
		v := float32(-7.5)
		return scanDest{&v, func() string { return fmtVal("%.9g", v) }}
	case "s":
		b := make([]byte, 64)
		return scanDest{&b[0], func() string { return "[" + GoStr(&b[0]) + "]" }}
	case "c":
		b := []byte("????????\x00")
		return scanDest{&b[0], func() string { return "[" + GoStr(&b[0]) + "]" }}
	}
	panic("bad kind " + kind)
}

// TestScanfGolden checks every case against output produced by real glibc
// sscanf (regenerate with scripts/gen_golden.sh).
func TestScanfGolden(t *testing.T) {
	var cases []string
	for _, l := range readLines(t, "testdata/scan.cases") {
		if l != "" && !strings.HasPrefix(l, "#") {
			cases = append(cases, l)
		}
	}
	golden := readLines(t, "testdata/scan.golden")
	if len(cases) != len(golden) {
		t.Fatalf("%d cases but %d golden lines", len(cases), len(golden))
	}
	for i, c := range cases {
		parts := strings.Split(c, "\t")
		input, format := parts[0], parts[1]
		var dests []scanDest
		var args []any
		for _, k := range strings.Split(parts[2], ",") {
			if k != "" {
				d := newScanDest(k)
				dests = append(dests, d)
				args = append(args, d.ptr)
			}
		}
		ret := Sscanf(CStr(input), CStr(format), args...)
		got := "ret=" + itoa(ret)
		for _, d := range dests {
			got += " " + d.show()
		}
		if got != golden[i] {
			t.Errorf("sscanf(%q, %q) = %q, glibc says %q", input, format, got, golden[i])
		}
	}
}

func itoa(n int) string {
	buf := make([]byte, 24)
	m := Snprintf(&buf[0], len(buf), CStr("%d"), n)
	return string(buf[:m])
}

func TestScanfGoDestinations(t *testing.T) {
	var s string
	var n, pos int
	var f float32
	var u8 uint8
	if r := Sscanf(CStr("hello 42 2.5 200"), CStr("%s %d %f %hhu%n"), &s, &n, &f, &u8, &pos); r != 4 {
		t.Fatalf("ret = %d", r)
	}
	if s != "hello" || n != 42 || f != 2.5 || u8 != 200 || pos != 16 {
		t.Errorf("got %q %d %v %d %d", s, n, f, u8, pos)
	}
	// Too few arguments, or the wrong pointer type, stops the scan cleanly.
	var a int
	if r := Sscanf(CStr("1 2"), CStr("%d %d"), &a); r != 1 {
		t.Errorf("missing arg: ret = %d", r)
	}
	var wrong string
	if r := Sscanf(CStr("12"), CStr("%d"), &wrong); r != 0 {
		t.Errorf("wrong type: ret = %d", r)
	}
}

func TestFscanfLeavesLookahead(t *testing.T) {
	var p [2]int
	syscall.Pipe(p[:])
	w, r := Fdopen(p[1], CStr("w")), Fdopen(p[0], CStr("r"))
	Fputs(CStr("12 rest\n3.5\n"), w)
	Fclose(w)
	var n int
	if Fscanf(r, CStr("%d"), &n) != 1 || n != 12 {
		t.Fatal("first scan")
	}
	if Fgetc(r) != ' ' { // the space that ended the number must still be there
		t.Error("lookahead byte was lost")
	}
	buf := make([]byte, 32)
	if GoStr(Fgets(&buf[0], 32, r)) != "rest\n" {
		t.Error("remainder of line")
	}
	var x float64
	if Fscanf(r, CStr("%lf"), &x) != 1 || x != 3.5 {
		t.Errorf("float scan: %v", x)
	}
	if Fscanf(r, CStr("%d"), &n) != EOF {
		t.Error("end of input should return EOF")
	}
	Fclose(r)
}

func TestScanfMalloc(t *testing.T) {
	// %s writes into a C buffer: use one from Malloc.
	buf := (*byte)(Malloc(32))
	defer Free(unsafe.Pointer(buf))
	if Sscanf(CStr("token rest"), CStr("%31s"), buf) != 1 || GoStr(buf) != "token" {
		t.Errorf("got %q", GoStr(buf))
	}
}
