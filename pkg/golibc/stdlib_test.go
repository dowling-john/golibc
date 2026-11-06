//go:build linux

package golibc

import (
	"math"
	"sort"
	"testing"
	"unsafe"
)

func TestStrtol(t *testing.T) {
	cases := []struct {
		s    string
		base int
		want int64
		rest string
	}{
		{"42", 10, 42, ""},
		{"  -17xyz", 10, -17, "xyz"},
		{"+5", 10, 5, ""},
		{"0x1F", 0, 31, ""},
		{"0X1f", 16, 31, ""},
		{"0x", 0, 0, "x"}, // "0" parses, "x" left over
		{"0x", 16, 0, "x"},
		{"017", 0, 15, ""},
		{"017", 10, 17, ""},
		{"zz", 36, 35*36 + 35, ""},
		{"101", 2, 5, ""},
		{"abc", 10, 0, "abc"}, // no digits: endp = start
		{"", 10, 0, ""},
		{"  ", 10, 0, "  "},
		{"9223372036854775807", 10, math.MaxInt64, ""},
		{"-9223372036854775808", 10, math.MinInt64, ""},
	}
	for _, c := range cases {
		var end *byte
		if got := Strtol(CStr(c.s), &end, c.base); got != c.want || GoStr(end) != c.rest {
			t.Errorf("Strtol(%q,%d) = %d rest %q; want %d rest %q", c.s, c.base, got, GoStr(end), c.want, c.rest)
		}
	}
	Errno = 0
	if got := Strtol(CStr("9223372036854775808"), nil, 10); got != math.MaxInt64 || Errno != ERANGE {
		t.Errorf("overflow: %d %v", got, Errno)
	}
	Errno = 0
	if got := Strtol(CStr("-99999999999999999999"), nil, 10); got != math.MinInt64 || Errno != ERANGE {
		t.Errorf("underflow: %d %v", got, Errno)
	}
	Errno = 0
	Strtol(CStr("1"), nil, 1)
	if Errno != EINVAL {
		t.Error("bad base should set EINVAL")
	}
}

func TestStrtoul(t *testing.T) {
	if got := Strtoul(CStr("18446744073709551615"), nil, 10); got != math.MaxUint64 {
		t.Errorf("max: %d", got)
	}
	if got := Strtoul(CStr("-1"), nil, 10); got != math.MaxUint64 {
		t.Errorf("-1 wraps: %d", got)
	}
	Errno = 0
	if got := Strtoul(CStr("18446744073709551616"), nil, 10); got != math.MaxUint64 || Errno != ERANGE {
		t.Errorf("overflow: %d %v", got, Errno)
	}
	if Atoi(CStr("  123abc")) != 123 || Atol(CStr("-9")) != -9 {
		t.Error("Atoi/Atol")
	}
}

func TestStrtod(t *testing.T) {
	cases := []struct {
		s    string
		want float64
		rest string
	}{
		{"3.14", 3.14, ""},
		{"  -2.5e3xyz", -2500, "xyz"},
		{".5", 0.5, ""},
		{"5.", 5, ""},
		{"1e", 1, "e"},
		{"1e+", 1, "e+"},
		{"1e+2", 100, ""},
		{"abc", 0, "abc"},
		{".", 0, "."},
		{"inf", math.Inf(1), ""},
		{"-Infinity", math.Inf(-1), ""},
		{"1e400", math.Inf(1), ""},
	}
	for _, c := range cases {
		var end *byte
		if got := Strtod(CStr(c.s), &end); got != c.want || GoStr(end) != c.rest {
			t.Errorf("Strtod(%q) = %v rest %q; want %v rest %q", c.s, got, GoStr(end), c.want, c.rest)
		}
	}
	if !math.IsNaN(Atof(CStr("nan"))) {
		t.Error("nan")
	}
	Errno = 0
	Strtod(CStr("1e400"), nil)
	if Errno != ERANGE {
		t.Error("overflow should set ERANGE")
	}
}

func cmpInt(a, b unsafe.Pointer) int {
	x, y := *(*int32)(a), *(*int32)(b)
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func TestQsortBsearch(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 10, 1000} {
		a := make([]int32, n)
		for i := range a {
			a[i] = int32(Rand() % 100) // plenty of duplicates
		}
		want := append([]int32(nil), a...)
		sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
		if n > 0 {
			Qsort(unsafe.Pointer(&a[0]), n, 4, cmpInt)
		}
		for i := range a {
			if a[i] != want[i] {
				t.Fatalf("n=%d: mismatch at %d", n, i)
			}
		}
		if n > 0 {
			key := a[n/2]
			p := Bsearch(unsafe.Pointer(&key), unsafe.Pointer(&a[0]), n, 4, cmpInt)
			if p == nil || *(*int32)(p) != key {
				t.Errorf("Bsearch miss for present key, n=%d", n)
			}
			miss := int32(1000)
			if Bsearch(unsafe.Pointer(&miss), unsafe.Pointer(&a[0]), n, 4, cmpInt) != nil {
				t.Error("Bsearch found absent key")
			}
		}
	}
}

func TestQsortStructs(t *testing.T) {
	type rec struct {
		key int64
		pad [3]int64
	}
	recs := []rec{{key: 3}, {key: 1}, {key: 2}}
	for i := range recs {
		recs[i].pad[1] = recs[i].key * 10
	}
	Qsort(unsafe.Pointer(&recs[0]), len(recs), int(unsafe.Sizeof(recs[0])), func(a, b unsafe.Pointer) int {
		return int((*rec)(a).key - (*rec)(b).key)
	})
	for i, r := range recs {
		if r.key != int64(i+1) || r.pad[1] != r.key*10 {
			t.Errorf("recs[%d] = %+v", i, r)
		}
	}
}

func TestRand(t *testing.T) {
	Srand(42)
	a, b := Rand(), Rand()
	Srand(42)
	if Rand() != a || Rand() != b {
		t.Error("Srand not reproducible")
	}
	for i := 0; i < 10000; i++ {
		if r := Rand(); r < 0 || r > RAND_MAX {
			t.Fatalf("Rand out of range: %d", r)
		}
	}
}

func TestStrtokAndCase(t *testing.T) {
	buf := []byte("  one, two,,three\x00")
	var got []string
	for tok := Strtok(&buf[0], CStr(", ")); tok != nil; tok = Strtok(nil, CStr(", ")) {
		got = append(got, GoStr(tok))
	}
	if len(got) != 3 || got[0] != "one" || got[1] != "two" || got[2] != "three" {
		t.Errorf("tokens = %q", got)
	}
	if Strcasecmp(CStr("HeLLo"), CStr("hello")) != 0 || Strcasecmp(CStr("a"), CStr("B")) >= 0 {
		t.Error("Strcasecmp")
	}
	if Strncasecmp(CStr("ABCx"), CStr("abcy"), 3) != 0 {
		t.Error("Strncasecmp")
	}
	if GoStr(Strerror(int(ENOENT))) != "no such file or directory" {
		t.Errorf("Strerror = %q", GoStr(Strerror(int(ENOENT))))
	}
}

func TestMisc(t *testing.T) {
	if Abs(-5) != 5 || Labs(-5) != 5 || Abs(3) != 3 {
		t.Error("Abs")
	}
	if d := Div(-7, 2); d.Quot != -3 || d.Rem != -1 {
		t.Errorf("Div = %+v", d) // truncates toward zero, like C
	}
	if Sqrt(16) != 4 || Floor(-1.5) != -2 || Fmod(7, 3) != 1 || !Isnan(Sqrt(-1)) {
		t.Error("math")
	}
	if GoStr(Getenv(CStr("PATH"))) == "" || Getenv(CStr("GOLIBC_SURELY_UNSET")) != nil {
		t.Error("Getenv")
	}
	if Time() < 1_700_000_000 {
		t.Error("Time")
	}
	var a, b Timespec
	ClockGettime(CLOCK_MONOTONIC, &a)
	Usleep(2000)
	ClockGettime(CLOCK_MONOTONIC, &b)
	if d := (b.Sec-a.Sec)*1e9 + (b.Nsec - a.Nsec); d < 2_000_000 {
		t.Errorf("Usleep(2000) only slept %dns", d)
	}
}

func TestAtexitOrder(t *testing.T) {
	var order []int
	Atexit(func() { order = append(order, 1) })
	Atexit(func() { order = append(order, 2) })
	runAtexit()
	if len(order) != 2 || order[0] != 2 || order[1] != 1 {
		t.Errorf("order = %v, want [2 1]", order)
	}
}
