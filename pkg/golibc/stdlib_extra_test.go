//go:build linux

package golibc

import (
	"math"
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestSetenvFamily(t *testing.T) {
	saved := os.Environ()
	t.Cleanup(func() {
		syscall.Clearenv()
		for _, kv := range saved {
			k, v, _ := strings.Cut(kv, "=")
			syscall.Setenv(k, v)
		}
	})
	get := func(name string) string { return GoStr(Getenv(CStr(name))) }

	if Setenv(CStr("GOLIBC_T1"), CStr("one"), 1) != 0 || get("GOLIBC_T1") != "one" {
		t.Fatal("Setenv did not set the variable")
	}
	Setenv(CStr("GOLIBC_T1"), CStr("kept?"), 0)
	if get("GOLIBC_T1") != "one" {
		t.Error("Setenv with overwrite 0 replaced an existing value")
	}
	Setenv(CStr("GOLIBC_T1"), CStr("two"), 1)
	if get("GOLIBC_T1") != "two" {
		t.Error("Setenv with overwrite 1 did not replace the value")
	}
	Setenv(CStr("GOLIBC_T9"), CStr("new"), 0)
	if get("GOLIBC_T9") != "new" {
		t.Error("Setenv with overwrite 0 did not set a missing variable")
	}

	for _, bad := range []string{"", "A=B"} {
		SetErrno(0)
		if Setenv(CStr(bad), CStr("x"), 1) != -1 || GetErrno() != EINVAL {
			t.Errorf("Setenv(%q) should fail with EINVAL", bad)
		}
		SetErrno(0)
		if Unsetenv(CStr(bad)) != -1 || GetErrno() != EINVAL {
			t.Errorf("Unsetenv(%q) should fail with EINVAL", bad)
		}
	}

	if Putenv(CStr("GOLIBC_T2=a=b")) != 0 || get("GOLIBC_T2") != "a=b" {
		t.Errorf("Putenv = %q, want the text after the first '='", get("GOLIBC_T2"))
	}
	Putenv(CStr("GOLIBC_T2")) // no '=': removes it, as in glibc
	if Getenv(CStr("GOLIBC_T2")) != nil {
		t.Error("Putenv without '=' should remove the variable")
	}
	if Unsetenv(CStr("GOLIBC_T1")) != 0 || Getenv(CStr("GOLIBC_T1")) != nil {
		t.Error("Unsetenv left the variable set")
	}

	Setenv(CStr("GOLIBC_T3"), CStr("visible"), 1)
	st := System(CStr(`[ "$GOLIBC_T3" = visible ]`))
	if !WIFEXITED(st) || WEXITSTATUS(st) != 0 {
		t.Errorf("a child process did not see Setenv (status %#x)", st)
	}

	if Clearenv() != 0 || len(os.Environ()) != 0 {
		t.Errorf("Clearenv left %d variables", len(os.Environ()))
	}
}

func TestStrtof(t *testing.T) {
	cases := []struct {
		s    string
		want float32
		rest string
	}{
		{"3.14159", 3.14159, ""},
		{"0.1", 0.1, ""},
		{"  -2.5e3xyz", -2500, "xyz"},
		{"16777217", 16777216, ""}, // 2^24+1 is not a float32
		{"1e-50", 0, ""},
		{"abc", 0, "abc"},
	}
	for _, c := range cases {
		var end *byte
		if got := Strtof(CStr(c.s), &end); got != c.want || GoStr(end) != c.rest {
			t.Errorf("Strtof(%q) = %v rest %q; want %v rest %q", c.s, got, GoStr(end), c.want, c.rest)
		}
	}
	SetErrno(0)
	if got := Strtof(CStr("1e39"), nil); !math.IsInf(float64(got), 1) || GetErrno() != ERANGE {
		t.Errorf("Strtof(1e39) = %v errno %v; want +Inf and ERANGE", got, GetErrno())
	}
	if got := Strtod(CStr("16777217"), nil); got != 16777217 {
		t.Errorf("Strtod must keep full precision, got %v", got)
	}
	if got := Strtold(CStr("2.5"), nil); got != 2.5 {
		t.Errorf("Strtold(2.5) = %v", got)
	}
}

func TestReallocarray(t *testing.T) {
	p := Reallocarray(nil, 4, 8)
	if p == nil {
		t.Fatal("Reallocarray(nil, 4, 8) failed")
	}
	*(*int64)(p) = 42
	p = Reallocarray(p, 100, 8)
	if p == nil || *(*int64)(p) != 42 {
		t.Fatal("Reallocarray lost the contents")
	}
	Free(p)

	SetErrno(0)
	if Reallocarray(nil, math.MaxInt/2+1, 2) != nil || GetErrno() != ENOMEM {
		t.Error("an overflowing product should fail with ENOMEM")
	}
	q := Malloc(16)
	if Reallocarray(q, -1, 8) != nil {
		t.Error("a negative count should fail")
	}
	Free(q) // the original block must still be valid after a failed call
}

// The helper re-executes this test binary and leaves through QuickExit.
func TestQuickExitHelper(t *testing.T) {
	if os.Getenv("GOLIBC_HELPER") != "quickexit" {
		t.Skip("helper for TestQuickExit")
	}
	Atexit(func() { Fputs(CStr("atexit\n"), Stdout); Fflush(Stdout) })
	AtQuickExit(func() { Dprintf(1, CStr("first\n")) })
	AtQuickExit(func() { Dprintf(1, CStr("second\n")) })
	Fputs(CStr("unflushed\n"), Stdout)
	QuickExit(5)
	os.Exit(99)
}

func TestQuickExit(t *testing.T) {
	self, _ := os.Executable()
	p := newPipe(t)
	env := append(syscall.Environ(), "GOLIBC_HELPER=quickexit")
	pid := spawnWithStdout(t, self, []string{self, "-test.run=^TestQuickExitHelper$"}, env, p[1])
	var st int
	Waitpid(pid, &st, 0)
	Close(p[1])
	buf := make([]byte, 256)
	n := Read(p[0], unsafe.Pointer(&buf[0]), len(buf))
	if !WIFEXITED(st) || WEXITSTATUS(st) != 5 {
		t.Errorf("child status = %#x, want exit 5", st)
	}
	if got := string(buf[:max(n, 0)]); got != "second\nfirst\n" {
		t.Errorf("child output = %q, want the quick-exit handlers in reverse order and nothing flushed or run by Atexit", got)
	}
}

// spawnWithStdout starts prog with its standard output sent to fd.
func spawnWithStdout(t *testing.T, prog string, argv, env []string, fd int) int {
	t.Helper()
	pid, err := syscall.ForkExec(prog, argv, &syscall.ProcAttr{Env: env, Files: []uintptr{0, uintptr(fd), 2}})
	if err != nil {
		t.Fatal(err)
	}
	return pid
}
