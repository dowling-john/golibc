//go:build linux

package golibc

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func tmpPath(t *testing.T, name string) *byte {
	return CStr(filepath.Join(t.TempDir(), name))
}

func readFile(t *testing.T, p *byte) string {
	b, err := os.ReadFile(GoStr(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFileRoundTrip(t *testing.T) {
	path := tmpPath(t, "a.txt")
	f := Fopen(path, CStr("w"))
	if f == nil {
		t.Fatalf("Fopen: %v", GetErrno())
	}
	Fprintf(f, CStr("line %d\n"), 1)
	Fputs(CStr("line 2\n"), f)
	Fputc('x', f)
	Fputc('y', f)
	if readFile(t, path) != "" {
		t.Error("data should still be buffered")
	}
	if Fclose(f) != 0 {
		t.Fatal("Fclose")
	}
	if got := readFile(t, path); got != "line 1\nline 2\nxy" {
		t.Fatalf("file = %q", got)
	}

	f = Fopen(path, CStr("r"))
	buf := make([]byte, 64)
	if p := Fgets(&buf[0], len(buf), f); p == nil || GoStr(p) != "line 1\n" {
		t.Errorf("Fgets 1 = %q", GoStr(p))
	}
	if p := Fgets(&buf[0], 4, f); GoStr(p) != "lin" { // n-1 bytes max
		t.Errorf("Fgets short = %q", GoStr(p))
	}
	if Fgetc(f) != 'e' || Fgetc(f) != ' ' || Fgetc(f) != '2' || Ungetc('Z', f) != 'Z' || Fgetc(f) != 'Z' || Fgetc(f) != '\n' {
		t.Error("Fgetc/Ungetc")
	}
	if n := Fread(unsafe.Pointer(&buf[0]), 1, 10, f); n != 2 || string(buf[:2]) != "xy" {
		t.Errorf("Fread tail = %d %q", n, buf[:n])
	}
	if !Feof(f) || Fgetc(f) != EOF || Fgets(&buf[0], 8, f) != nil {
		t.Error("EOF handling")
	}
	Clearerr(f)
	if Feof(f) {
		t.Error("Clearerr")
	}
	Fclose(f)
}

func TestFseekFtell(t *testing.T) {
	path := tmpPath(t, "b.txt")
	f := Fopen(path, CStr("w+"))
	Fputs(CStr("0123456789"), f)
	if Ftell(f) != 10 {
		t.Errorf("Ftell after write = %d", Ftell(f))
	}
	if Fseek(f, 3, SEEK_SET) != 0 || Fgetc(f) != '3' {
		t.Error("Fseek SET")
	}
	if Ftell(f) != 4 {
		t.Errorf("Ftell = %d (buffered read must not leak)", Ftell(f))
	}
	if Fseek(f, 2, SEEK_CUR) != 0 || Fgetc(f) != '6' { // relative to logical pos 4
		t.Error("Fseek CUR with buffered read")
	}
	if Fseek(f, -1, SEEK_END) != 0 || Fgetc(f) != '9' {
		t.Error("Fseek END")
	}
	Rewind(f)
	Fputc('A', f) // read -> write switch must land at offset 0
	Fflush(f)
	if got := readFile(t, path); got != "A123456789" {
		t.Errorf("after rewrite: %q", got)
	}
	Fclose(f)
}

func TestFopenModes(t *testing.T) {
	path := tmpPath(t, "c.txt")
	if Fopen(path, CStr("r")) != nil || GetErrno() != ENOENT {
		t.Errorf("open missing file: errno %v", GetErrno())
	}
	if Fopen(path, CStr("q")) != nil || GetErrno() != EINVAL {
		t.Error("bad mode")
	}
	f := Fopen(path, CStr("wb"))
	Fputs(CStr("one"), f)
	Fclose(f)
	f = Fopen(path, CStr("a"))
	Fputs(CStr("two"), f)
	Fclose(f)
	if got := readFile(t, path); got != "onetwo" {
		t.Errorf("append: %q", got)
	}
	f = Fopen(path, CStr("r"))
	if Fputc('x', f) != EOF || !Ferror(f) {
		t.Error("writing a read-only stream must fail")
	}
	Fclose(f)
	if Remove(path) != 0 || Fopen(path, CStr("r")) != nil {
		t.Error("Remove")
	}
}

func TestLargeWritesAndBuffering(t *testing.T) {
	path := tmpPath(t, "d.bin")
	f := Fopen(path, CStr("w"))
	big := []byte(strings.Repeat("0123456789", 5000)) // larger than BUFSIZ
	if n := Fwrite(unsafe.Pointer(&big[0]), 1, len(big), f); n != len(big) {
		t.Fatalf("Fwrite = %d", n)
	}
	Fputc('!', f)
	Fclose(f)
	if got := readFile(t, path); got != string(big)+"!" {
		t.Errorf("large write mismatch (len %d)", len(got))
	}

	// Unbuffered streams write through immediately; line-buffered flush on newline.
	f = Fopen(path, CStr("w"))
	Setvbuf(f, IONBF, 0)
	Fputc('a', f)
	if readFile(t, path) != "a" {
		t.Error("IONBF should write through")
	}
	Setvbuf(f, IOLBF, 0)
	Fputs(CStr("bc"), f)
	if readFile(t, path) != "a" {
		t.Error("IOLBF should hold partial line")
	}
	Fputs(CStr("d\ne"), f)
	if readFile(t, path) != "abcd\n" && readFile(t, path) != "abcd\ne" {
		t.Errorf("IOLBF newline flush: %q", readFile(t, path))
	}
	Fclose(f)
	if Setvbuf(f, 99, 0) != -1 {
		t.Error("bad Setvbuf mode")
	}
}

func TestFflushAll(t *testing.T) {
	path := tmpPath(t, "e.txt")
	f := Fopen(path, CStr("w"))
	Fputs(CStr("pending"), f)
	if Fflush(nil) != 0 || readFile(t, path) != "pending" {
		t.Error("Fflush(nil) should flush every open stream")
	}
	Fclose(f)
}

// Stdout is process-global, so redirect fd 1 to a file to observe it.
func TestStdoutRedirect(t *testing.T) {
	path := tmpPath(t, "out.txt")
	file, err := os.Create(GoStr(path))
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := syscall.Dup(1)
	syscall.Dup3(int(file.Fd()), 1, 0)
	Printf(CStr("hello %s %d\n"), CStr("world"), 42)
	Puts(CStr("puts line"))
	Putchar('!')
	Fflush(Stdout)
	syscall.Dup3(saved, 1, 0)
	syscall.Close(saved)
	file.Close()
	if got := readFile(t, path); got != "hello world 42\nputs line\n!" {
		t.Errorf("stdout = %q", got)
	}
	Dprintf(2, CStr(""))
}

func TestFdopenAndPipe(t *testing.T) {
	var p [2]int
	syscall.Pipe(p[:])
	w, r := Fdopen(p[1], CStr("w")), Fdopen(p[0], CStr("r"))
	Fprintf(w, CStr("%d\n%d\n"), 10, 20)
	Fclose(w)
	buf := make([]byte, 16)
	if GoStr(Fgets(&buf[0], 16, r)) != "10\n" || Atoi(Fgets(&buf[0], 16, r)) != 20 || Fgets(&buf[0], 16, r) != nil {
		t.Error("pipe round trip")
	}
	Fclose(r)
}
