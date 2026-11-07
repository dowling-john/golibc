//go:build linux

package golibc

import (
	"os"
	"strings"
	"testing"
	"unsafe"
)

func fileWith(t *testing.T, content string) *File {
	t.Helper()
	path := tmpPath(t, "in")
	if err := os.WriteFile(GoStr(path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f := Fopen(path, CStr("r"))
	if f == nil {
		t.Fatal("Fopen")
	}
	t.Cleanup(func() { Fclose(f) })
	return f
}

func TestGetline(t *testing.T) {
	long := strings.Repeat("x", 20000)
	f := fileWith(t, "one\ntwo\n\n"+long+"\nlast")
	var line *byte
	var size int
	t.Cleanup(func() { Free(unsafe.Pointer(line)) })

	want := []string{"one\n", "two\n", "\n", long + "\n", "last"}
	for i, w := range want {
		n := Getline(&line, &size, f)
		if n != len(w) || GoStr(line) != w {
			t.Fatalf("line %d: got %d bytes %.20q, want %d bytes %.20q", i, n, GoStr(line), len(w), w)
		}
		if size < n+1 {
			t.Fatalf("line %d: buffer size %d is too small for %d bytes and a NUL", i, size, n)
		}
	}
	if n := Getline(&line, &size, f); n != -1 || !Feof(f) {
		t.Errorf("at end of file Getline = %d, eof %v; want -1 and eof", n, Feof(f))
	}
}

func TestGetdelimAndUngetc(t *testing.T) {
	f := fileWith(t, "a,b,,c")
	var line *byte
	var size int
	t.Cleanup(func() { Free(unsafe.Pointer(line)) })

	var got []string
	for Getdelim(&line, &size, ',', f) >= 0 {
		got = append(got, GoStr(line))
	}
	if strings.Join(got, "|") != "a,|b,|,|c" {
		t.Errorf("Getdelim pieces = %q", got)
	}

	g := fileWith(t, "bc\nrest")
	if Fgetc(g) != 'b' {
		t.Fatal("Fgetc")
	}
	Ungetc('B', g)
	if n := Getline(&line, &size, g); n != 3 || GoStr(line) != "Bc\n" {
		t.Errorf("after Ungetc, Getline = %d %q; want the pushed-back byte first", n, GoStr(line))
	}
}

func TestGetlineErrors(t *testing.T) {
	SetErrno(0)
	if Getline(nil, nil, Stdin) != -1 || GetErrno() != EINVAL {
		t.Error("nil arguments should give EINVAL")
	}
	// A buffer the caller supplied is reused when it is big enough.
	f := fileWith(t, "hi\n")
	p := (*byte)(Malloc(64))
	size := 64
	orig := p
	if Getline(&p, &size, f) != 3 || p != orig || size != 64 {
		t.Error("Getline should reuse a large enough buffer")
	}
	Free(unsafe.Pointer(p))
	// Reading a write-only stream fails.
	w := Fopen(tmpPath(t, "w"), CStr("w"))
	defer Fclose(w)
	var line *byte
	var n int
	if Getline(&line, &n, w) != -1 {
		t.Error("Getline on a write-only stream should fail")
	}
	Free(unsafe.Pointer(line))
}

func TestFgetposFsetpos(t *testing.T) {
	f := fileWith(t, "0123456789")
	Fgetc(f)
	Fgetc(f)
	var pos Fpos_t
	if Fgetpos(f, &pos) != 0 {
		t.Fatal("Fgetpos")
	}
	if c := Fgetc(f); c != '2' {
		t.Fatalf("Fgetc = %c", c)
	}
	Fgetc(f)
	if Fsetpos(f, &pos) != 0 {
		t.Fatal("Fsetpos")
	}
	if c := Fgetc(f); c != '2' {
		t.Errorf("after Fsetpos Fgetc = %c, want 2", c)
	}
	p := newPipe(t)
	r := Fdopen(p[0], CStr("r"))
	if Fgetpos(r, &pos) != -1 {
		t.Error("Fgetpos on a pipe should fail")
	}
}

func TestSetbuf(t *testing.T) {
	p := newPipe(t)
	f := Fdopen(p[1], CStr("w"))
	Setbuf(f, nil) // unbuffered: bytes reach the pipe immediately
	Fputs(CStr("now"), f)
	buf := make([]byte, 8)
	if n := Read(p[0], unsafe.Pointer(&buf[0]), len(buf)); string(buf[:max(n, 0)]) != "now" {
		t.Errorf("unbuffered write read back %q", buf[:max(n, 0)])
	}
	Setlinebuf(f)
	Fputs(CStr("part"), f)
	Fputs(CStr("ial\n"), f) // the newline flushes the whole line
	if n := Read(p[0], unsafe.Pointer(&buf[0]), len(buf)); string(buf[:max(n, 0)]) != "partial\n" {
		t.Errorf("line buffered write read back %q", buf[:max(n, 0)])
	}
	Setbuf(f, (*byte)(unsafe.Pointer(&buf[0])))
	Fputs(CStr("held"), f)
	Fclose(f)
}

func TestTmpfile(t *testing.T) {
	f := Tmpfile()
	if f == nil {
		t.Fatal("Tmpfile")
	}
	Fputs(CStr("scratch data"), f)
	Rewind(f)
	buf := make([]byte, 32)
	n := Fread(unsafe.Pointer(&buf[0]), 1, len(buf), f)
	if string(buf[:n]) != "scratch data" {
		t.Errorf("read back %q", buf[:n])
	}
	if target, err := os.Readlink("/proc/self/fd/" + itoa(Fileno(f))); err != nil || !strings.HasSuffix(target, "(deleted)") {
		t.Errorf("temporary file should already be unlinked, fd points at %q (%v)", target, err)
	}
	Fclose(f)
}

// logf mimics a C function that takes "..." and passes them on as a va_list.
func logf(buf *byte, size int, format *byte, args ...any) int {
	return Vsnprintf(buf, size, format, args)
}

func TestVprintfFamily(t *testing.T) {
	out := make([]byte, 64)
	n := logf(&out[0], len(out), CStr("%s=%d [%5.2f]"), "pi", 3, 3.14159)
	if got := GoStr(&out[0]); got != "pi=3 [ 3.14]" || n != len(got) {
		t.Errorf("Vsnprintf = %q (%d)", got, n)
	}

	ap := VaList{"x", 7}
	if n := Vsprintf(&out[0], CStr("%s%d"), ap); GoStr(&out[0]) != "x7" || n != 2 {
		t.Errorf("Vsprintf = %q (%d)", GoStr(&out[0]), n)
	}

	var s *byte
	if n := Vasprintf(&s, CStr("<%s>"), VaList{"mem"}); GoStr(s) != "<mem>" || n != 5 {
		t.Errorf("Vasprintf = %q (%d)", GoStr(s), n)
	}
	Free(unsafe.Pointer(s))

	p := newPipe(t)
	if n := Vdprintf(p[1], CStr("fd %d"), VaList{9}); n != 4 {
		t.Errorf("Vdprintf wrote %d", n)
	}
	buf := make([]byte, 16)
	if k := Read(p[0], unsafe.Pointer(&buf[0]), len(buf)); string(buf[:max(k, 0)]) != "fd 9" {
		t.Errorf("Vdprintf wrote %q", buf[:max(k, 0)])
	}

	f := Tmpfile()
	defer Fclose(f)
	Vfprintf(f, CStr("%d-%d"), VaList{1, 2})
	Rewind(f)
	var a, b int32
	if Vfscanf(f, CStr("%d-%d"), VaList{&a, &b}) != 2 || a != 1 || b != 2 {
		t.Errorf("Vfscanf got %d %d", a, b)
	}
	var x, y int32
	if Vsscanf(CStr("10 20"), CStr("%d %d"), VaList{&x, &y}) != 2 || x != 10 || y != 20 {
		t.Errorf("Vsscanf got %d %d", x, y)
	}
}

func TestMemchr(t *testing.T) {
	b := []byte("hello, world")
	p := unsafe.Pointer(&b[0])
	if got := Memchr(p, ',', len(b)); got != unsafe.Add(p, 5) {
		t.Error("Memchr did not find ','")
	}
	if Memchr(p, 'z', len(b)) != nil {
		t.Error("Memchr found a byte that is not there")
	}
	if Memchr(p, ',', 5) != nil {
		t.Error("Memchr looked past n")
	}
	if Memchr(p, ',', 0) != nil || Memchr(nil, 'a', 0) != nil {
		t.Error("Memchr with n == 0 must return nil")
	}
	if Memchr(p, 0x168, len(b)) != p { // c is converted to a byte: 0x168 is 0x68, 'h'
		t.Error("Memchr must convert c to a byte")
	}
}
