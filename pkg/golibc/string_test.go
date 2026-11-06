//go:build linux

package golibc

import (
	"testing"
	"unsafe"
)

// cstr returns a NUL-terminated copy of s with extra spare capacity.
func cstr(s string, spare int) *byte {
	b := make([]byte, len(s)+1+spare)
	copy(b, s)
	return &b[0]
}

func gostr(p *byte) string { return string(unsafe.Slice(p, Strlen(p))) }

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func TestStrlen(t *testing.T) {
	for _, s := range []string{"", "a", "hello"} {
		if got := Strlen(cstr(s, 0)); got != len(s) {
			t.Errorf("Strlen(%q) = %d", s, got)
		}
	}
	if got := Strnlen(cstr("hello", 0), 3); got != 3 {
		t.Errorf("Strnlen = %d", got)
	}
	if got := Strnlen(cstr("hi", 0), 10); got != 2 {
		t.Errorf("Strnlen = %d", got)
	}
}

func TestStrcmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"", "", 0}, {"a", "a", 0}, {"a", "b", -1}, {"b", "a", 1}, {"ab", "a", 1}, {"a", "ab", -1}, {"\xff", "a", 1}}
	for _, c := range cases {
		if got := sign(Strcmp(cstr(c.a, 0), cstr(c.b, 0))); got != c.want {
			t.Errorf("Strcmp(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if Strncmp(cstr("abcX", 0), cstr("abcY", 0), 3) != 0 {
		t.Error("Strncmp should ignore bytes past n")
	}
	if Strncmp(cstr("abcX", 0), cstr("abcY", 0), 4) >= 0 {
		t.Error("Strncmp should see difference at n=4")
	}
}

func TestStrcpyCat(t *testing.T) {
	d := cstr("", 16)
	Strcpy(d, cstr("foo", 0))
	Strcat(d, cstr("bar", 0))
	if got := gostr(d); got != "foobar" {
		t.Errorf("got %q", got)
	}
	Strncat(d, cstr("bazqux", 0), 3)
	if got := gostr(d); got != "foobarbaz" {
		t.Errorf("Strncat got %q", got)
	}
}

func TestStrncpyPads(t *testing.T) {
	buf := []byte{'x', 'x', 'x', 'x', 'x', 'x'}
	Strncpy(&buf[0], cstr("ab", 0), 5)
	if string(buf) != "ab\x00\x00\x00x" {
		t.Errorf("got %q", buf)
	}
	// No terminator when src is at least n long.
	buf2 := []byte("zzzzz")
	Strncpy(&buf2[0], cstr("abcdef", 0), 3)
	if string(buf2) != "abczz" {
		t.Errorf("got %q", buf2)
	}
}

func TestSearch(t *testing.T) {
	s := cstr("hello world", 0)
	if p := Strchr(s, 'o'); p == nil || gostr(p) != "o world" {
		t.Error("Strchr")
	}
	if p := Strrchr(s, 'o'); p == nil || gostr(p) != "orld" {
		t.Error("Strrchr")
	}
	if Strchr(s, 'z') != nil {
		t.Error("Strchr miss")
	}
	if p := Strchr(s, 0); p == nil || *p != 0 {
		t.Error("Strchr NUL")
	}
	if p := Strstr(s, cstr("wor", 0)); p == nil || gostr(p) != "world" {
		t.Error("Strstr")
	}
	if Strstr(s, cstr("xyz", 0)) != nil {
		t.Error("Strstr miss")
	}
	if Strstr(s, cstr("", 0)) != s {
		t.Error("Strstr empty needle")
	}
	if got := Strspn(cstr("aabbcc", 0), cstr("ab", 0)); got != 4 {
		t.Errorf("Strspn = %d", got)
	}
	if got := Strcspn(cstr("abcdef", 0), cstr("dx", 0)); got != 3 {
		t.Errorf("Strcspn = %d", got)
	}
	if p := Strpbrk(s, cstr("wl", 0)); p == nil || gostr(p) != "llo world" {
		t.Error("Strpbrk")
	}
}

func TestMem(t *testing.T) {
	a := []byte("0123456789")
	Memmove(unsafe.Pointer(&a[2]), unsafe.Pointer(&a[0]), 5) // overlapping, forward
	if string(a) != "0101234789" {
		t.Errorf("Memmove fwd: %q", a)
	}
	a = []byte("0123456789")
	Memmove(unsafe.Pointer(&a[0]), unsafe.Pointer(&a[2]), 5) // overlapping, backward
	if string(a) != "2345656789" {
		t.Errorf("Memmove back: %q", a)
	}
	b := make([]byte, 4)
	Memset(unsafe.Pointer(&b[0]), 0x1ff, 4) // only low byte used
	if string(b) != "\xff\xff\xff\xff" {
		t.Errorf("Memset: %q", b)
	}
	c := make([]byte, 4)
	Memcpy(unsafe.Pointer(&c[0]), unsafe.Pointer(&b[0]), 4)
	if Memcmp(unsafe.Pointer(&b[0]), unsafe.Pointer(&c[0]), 4) != 0 {
		t.Error("Memcpy/Memcmp")
	}
	c[2] = 1
	if Memcmp(unsafe.Pointer(&b[0]), unsafe.Pointer(&c[0]), 4) <= 0 {
		t.Error("Memcmp ordering")
	}
	if p := Memchr(unsafe.Pointer(&c[0]), 1, 4); p != unsafe.Pointer(&c[2]) {
		t.Error("Memchr")
	}
	if Memchr(unsafe.Pointer(&c[0]), 7, 4) != nil {
		t.Error("Memchr miss")
	}
	// n == 0 must be safe even with nil pointers.
	Memcpy(nil, nil, 0)
	Memmove(nil, nil, 0)
}

func TestCtype(t *testing.T) {
	for c := -1; c < 256; c++ {
		want := func(f func(rune) bool) bool { return c >= 0 && c < 128 && f(rune(c)) }
		if got := Isdigit(c); got != want(func(r rune) bool { return r >= '0' && r <= '9' }) {
			t.Errorf("Isdigit(%d)", c)
		}
		if got := Isalpha(c); got != want(func(r rune) bool { return r|0x20 >= 'a' && r|0x20 <= 'z' }) {
			t.Errorf("Isalpha(%d)", c)
		}
	}
	if Toupper('a') != 'A' || Toupper('A') != 'A' || Toupper('1') != '1' {
		t.Error("Toupper")
	}
	if Tolower('Z') != 'z' || Tolower('z') != 'z' {
		t.Error("Tolower")
	}
	if !Isspace('\n') || !Isspace(' ') || Isspace('x') {
		t.Error("Isspace")
	}
}
