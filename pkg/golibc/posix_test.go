//go:build linux

package golibc

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"
)

func listDir(t *testing.T, dir string) map[string]uint8 {
	t.Helper()
	d := Opendir(CStr(dir))
	if d == nil {
		t.Fatalf("Opendir(%q): %v", dir, GetErrno())
	}
	defer Closedir(d)
	names := map[string]uint8{}
	for e := Readdir(d); e != nil; e = Readdir(d) {
		names[GoStr(&e.Name[0])] = e.Type
	}
	return names
}

func TestFilesAndDirs(t *testing.T) {
	root := t.TempDir()
	p := func(name string) *byte { return CStr(filepath.Join(root, name)) }

	if Mkdir(p("sub"), 0o755) != 0 || Mkdir(p("sub"), 0o755) != -1 || GetErrno() != EEXIST {
		t.Errorf("Mkdir: errno %v", GetErrno())
	}
	for _, n := range []string{"a", "b"} {
		f := Fopen(p(n), CStr("w"))
		Fputs(CStr("hello"), f)
		Fclose(f)
	}
	os.Symlink("a", filepath.Join(root, "link"))

	got := listDir(t, root)
	want := map[string]uint8{".": DT_DIR, "..": DT_DIR, "a": DT_REG, "b": DT_REG, "sub": DT_DIR, "link": DT_LNK}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("entries = %v, want %v", got, want)
	}

	var st Stat_t
	if Stat(p("a"), &st) != 0 || st.Size != 5 || !S_ISREG(st.Mode) {
		t.Errorf("Stat a: %+v", st)
	}
	if Stat(p("sub"), &st) != 0 || !S_ISDIR(st.Mode) {
		t.Error("Stat sub")
	}
	if Lstat(p("link"), &st) != 0 || !S_ISLNK(st.Mode) {
		t.Error("Lstat should see the symlink itself")
	}
	if Stat(p("link"), &st) != 0 || !S_ISREG(st.Mode) {
		t.Error("Stat should follow the symlink")
	}
	if Stat(p("missing"), &st) != -1 || GetErrno() != ENOENT {
		t.Errorf("Stat missing: %v", GetErrno())
	}

	if Chmod(p("a"), 0o600) != 0 {
		t.Fatal("Chmod")
	}
	Stat(p("a"), &st)
	if st.Mode&0o777 != 0o600 {
		t.Errorf("mode = %o", st.Mode&0o777)
	}
	if Access(p("a"), R_OK|W_OK) != 0 || Access(p("missing"), F_OK) != -1 {
		t.Error("Access")
	}

	if Truncate(p("a"), 2) != 0 {
		t.Fatal("Truncate")
	}
	Stat(p("a"), &st)
	if st.Size != 2 {
		t.Errorf("size after Truncate = %d", st.Size)
	}
	fd := Open(p("a"), O_RDWR, 0)
	if Ftruncate(fd, 9) != 0 {
		t.Error("Ftruncate")
	}
	var fst Stat_t
	if Fstat(fd, &fst) != 0 || fst.Size != 9 {
		t.Errorf("Fstat size = %d", fst.Size)
	}
	Close(fd)

	if Rename(p("a"), p("c")) != 0 || Stat(p("a"), &st) != -1 || Stat(p("c"), &st) != 0 {
		t.Error("Rename")
	}
	if Rmdir(p("sub")) != 0 {
		t.Errorf("Rmdir empty dir: %v", GetErrno())
	}
	if Rmdir(p("b")) != -1 || GetErrno() != ENOTDIR {
		t.Errorf("Rmdir on a file: %v", GetErrno())
	}
	if Opendir(p("missing")) != nil || GetErrno() != ENOENT {
		t.Error("Opendir missing")
	}
}

func TestReaddirManyEntries(t *testing.T) {
	root := t.TempDir()
	const n = 1500 // far more than fits in one 8 KiB getdents buffer
	for i := 0; i < n; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("file-with-a-longish-name-%04d", i)), nil, 0o600)
	}
	names := listDir(t, root)
	if len(names) != n+2 {
		t.Fatalf("got %d entries, want %d", len(names), n+2)
	}
	d := Opendir(CStr(root))
	count := 0
	for Readdir(d) != nil {
		count++
	}
	Rewinddir(d)
	if Readdir(d) == nil {
		t.Error("Rewinddir should restart iteration")
	}
	Closedir(d)
	if count != n+2 {
		t.Errorf("counted %d", count)
	}
	var sorted []string
	for k := range names {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	if sorted[2] != "file-with-a-longish-name-0000" {
		t.Errorf("first file = %q", sorted[2])
	}
}

func TestCwd(t *testing.T) {
	want, _ := os.Getwd()
	buf := make([]byte, 4096)
	if p := Getcwd(&buf[0], len(buf)); p == nil || GoStr(p) != want {
		t.Errorf("Getcwd = %q, want %q", GoStr(p), want)
	}
	if Getcwd(&buf[0], 1) != nil || GetErrno() != ERANGE {
		t.Errorf("tiny buffer: errno %v", GetErrno())
	}
	t.Cleanup(func() { os.Chdir(want) })
	dir := t.TempDir()
	if Chdir(CStr(dir)) != 0 {
		t.Fatal("Chdir")
	}
	real, _ := filepath.EvalSymlinks(dir)
	if got := GoStr(Getcwd(&buf[0], len(buf))); got != real && got != dir {
		t.Errorf("after Chdir: %q", got)
	}
	if Chdir(CStr("/definitely/not/here")) != -1 || GetErrno() != ENOENT {
		t.Error("Chdir missing")
	}
}

func TestDupAndPipe(t *testing.T) {
	var fds [2]int
	if Pipe(&fds) != 0 {
		t.Fatal("Pipe")
	}
	w2 := Dup(fds[1])
	if w2 < 0 || w2 == fds[1] {
		t.Fatalf("Dup = %d", w2)
	}
	Close(fds[1])
	msg := []byte("via dup")
	Write(w2, ptr(msg), len(msg))
	Close(w2)
	buf := make([]byte, 16)
	if n := Read(fds[0], ptr(buf), len(buf)); n != len(msg) || string(buf[:n]) != "via dup" {
		t.Errorf("read %q", buf[:max(n, 0)])
	}
	if Dup2(fds[0], fds[0]) != fds[0] || Dup2(9999, 9999) != -1 {
		t.Error("Dup2 same-fd cases")
	}
	target := Dup(fds[0])
	if Dup2(fds[0], target) != target {
		t.Error("Dup2")
	}
	Close(target)
	Close(fds[0])
}

func TestIdentity(t *testing.T) {
	if Getuid() != os.Getuid() || Getgid() != os.Getgid() || Getppid() != os.Getppid() || Geteuid() != os.Geteuid() || Getegid() != os.Getegid() {
		t.Error("ids differ from os package")
	}
	old := Umask(0o077)
	if got := Umask(old); got != 0o077 {
		t.Errorf("Umask round trip = %o", got)
	}
}

func TestSpawnWaitSystem(t *testing.T) {
	pid := Spawn(CStr("/bin/sh"), []string{"sh", "-c", "exit 5"}, nil)
	if pid <= 0 {
		t.Fatalf("Spawn: %v", GetErrno())
	}
	var st int
	if Waitpid(pid, &st, 0) != pid || !WIFEXITED(st) || WEXITSTATUS(st) != 5 {
		t.Errorf("status = %#x", st)
	}
	if Waitpid(-1, nil, WNOHANG) != -1 || GetErrno() != ECHILD {
		t.Errorf("no children: errno %v", GetErrno())
	}
	if st := System(CStr("exit 3")); !WIFEXITED(st) || WEXITSTATUS(st) != 3 {
		t.Errorf("System exit status = %#x", st)
	}
	if st := System(CStr("kill -9 $$")); !WIFSIGNALED(st) || WTERMSIG(st) != SIGKILL {
		t.Errorf("System signal status = %#x", st)
	}
	if Spawn(CStr("/no/such/program"), []string{"x"}, nil) != -1 || GetErrno() != ENOENT {
		t.Errorf("Spawn missing program: errno %v", GetErrno())
	}
}

// The helper re-executes this test binary; it replaces itself via Execve.
func TestExecveHelper(t *testing.T) {
	if os.Getenv("GOLIBC_HELPER") != "execve" {
		t.Skip("helper for TestExecve")
	}
	Execve(CStr("/bin/sh"), []string{"sh", "-c", "exit 7"}, nil)
	os.Exit(99) // only reached if Execve failed
}

func TestExecve(t *testing.T) {
	self, _ := os.Executable()
	env := append(syscall.Environ(), "GOLIBC_HELPER=execve")
	pid := Spawn(CStr(self), []string{self, "-test.run=^TestExecveHelper$"}, env)
	if pid <= 0 {
		t.Fatal("Spawn")
	}
	var st int
	Waitpid(pid, &st, 0)
	if !WIFEXITED(st) || WEXITSTATUS(st) != 7 {
		t.Errorf("child status = %#x, want exit 7 from the exec'd shell", st)
	}
	if Execve(CStr("/no/such/program"), []string{"x"}, nil) != -1 || GetErrno() != ENOENT {
		t.Error("Execve failure should return -1")
	}
}

func TestSignals(t *testing.T) {
	got := make(chan int, 4)
	Signal(SIGUSR1, func(sig int) { got <- sig })
	for i := 0; i < 2; i++ { // one at a time: signals sent back-to-back may coalesce into one
		Raise(SIGUSR1)
		select {
		case s := <-got:
			if s != SIGUSR1 {
				t.Errorf("handler got signal %d", s)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("handler not called")
		}
	}
	// Replacing a handler stops the old one.
	other := make(chan int, 1)
	Signal(SIGUSR1, func(sig int) { other <- sig })
	Raise(SIGUSR1)
	select {
	case <-other:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement handler not called")
	}
	if len(got) != 0 {
		t.Error("old handler still ran")
	}
	// An ignored signal must not kill the process.
	SignalIgnore(SIGUSR2)
	Raise(SIGUSR2)
	time.Sleep(50 * time.Millisecond)
	Signal(SIGUSR1, nil)
}
