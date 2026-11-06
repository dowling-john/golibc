//go:build linux

package golibc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFcntl(t *testing.T) {
	p := newPipe(t)
	fl := Fcntl(p[0], F_GETFL, 0)
	if fl < 0 || fl&O_NONBLOCK != 0 {
		t.Fatalf("F_GETFL = %#x", fl)
	}
	b := make([]byte, 1)
	if Fcntl(p[0], F_SETFL, fl|O_NONBLOCK) != 0 || Read(p[0], ptr(b), 1) != -1 || GetErrno() != EAGAIN {
		t.Errorf("O_NONBLOCK read on an empty pipe should give EAGAIN, got errno %v", GetErrno())
	}
	if Fcntl(p[0], F_SETFD, 0) != 0 || Fcntl(p[0], F_GETFD, 0)&FD_CLOEXEC != 0 {
		t.Error("clearing FD_CLOEXEC")
	}
	d := Fcntl(p[0], F_DUPFD_CLOEXEC, 100)
	if d < 100 || Fcntl(d, F_GETFD, 0)&FD_CLOEXEC == 0 {
		t.Errorf("F_DUPFD_CLOEXEC = %d", d)
	}
	Close(d)
	if Fcntl(-1, F_GETFL, 0) != -1 || GetErrno() != EBADF {
		t.Error("bad fd")
	}
}

func TestFcntlLock(t *testing.T) {
	path := CStr(filepath.Join(t.TempDir(), "lock"))
	fd := Open(path, O_RDWR|O_CREAT, 0o600)
	defer Close(fd)
	lk := Flock_t{Type: F_WRLCK, Whence: 0}
	if FcntlLock(fd, F_SETLK, &lk) != 0 {
		t.Fatalf("F_SETLK: %v", GetErrno())
	}
	// Locks are per-process, so asking about our own lock reports it unlocked.
	q := Flock_t{Type: F_WRLCK}
	if FcntlLock(fd, F_GETLK, &q) != 0 || q.Type != F_UNLCK {
		t.Errorf("F_GETLK = %+v", q)
	}
	lk.Type = F_UNLCK
	if FcntlLock(fd, F_SETLK, &lk) != 0 {
		t.Error("unlock")
	}
}

func TestIoctl(t *testing.T) {
	p := newPipe(t)
	b := []byte("12345")
	Write(p[1], ptr(b), len(b))
	var n int32
	if Ioctl(p[0], FIONREAD, ptr32(&n)) != 0 || n != 5 {
		t.Errorf("FIONREAD = %d", n)
	}
	var ws Winsize
	if Ioctl(p[0], TIOCGWINSZ, ptrOf(&ws)) != -1 || GetErrno() != ENOTTY {
		t.Errorf("TIOCGWINSZ on a pipe: errno %v", GetErrno())
	}
	if IoctlInt(p[0], FIONREAD, 0) != -1 {
		t.Error("IoctlInt with a bad pointer argument should fail")
	}
}

func TestLinks(t *testing.T) {
	dir := t.TempDir()
	p := func(n string) *byte { return CStr(filepath.Join(dir, n)) }
	os.WriteFile(filepath.Join(dir, "target"), []byte("data"), 0o600)

	if Symlink(CStr("target"), p("sym")) != 0 {
		t.Fatalf("Symlink: %v", GetErrno())
	}
	buf := make([]byte, 64)
	if n := Readlink(p("sym"), &buf[0], len(buf)); n != 6 || string(buf[:n]) != "target" {
		t.Errorf("Readlink = %q", buf[:max(n, 0)])
	}
	if n := Readlink(p("sym"), &buf[0], 3); n != 3 || string(buf[:3]) != "tar" { // silently truncated
		t.Errorf("truncated Readlink = %d %q", n, buf[:3])
	}
	if Readlink(p("target"), &buf[0], len(buf)) != -1 || GetErrno() != EINVAL {
		t.Error("Readlink on a regular file should give EINVAL")
	}
	if Readlink(p("nope"), &buf[0], len(buf)) != -1 || GetErrno() != ENOENT {
		t.Error("Readlink missing")
	}
	if Symlink(CStr("x"), p("sym")) != -1 || GetErrno() != EEXIST {
		t.Error("Symlink over an existing name")
	}

	if Link(p("target"), p("hard")) != 0 {
		t.Fatalf("Link: %v", GetErrno())
	}
	var st Stat_t
	Stat(p("target"), &st)
	if st.Nlink != 2 {
		t.Errorf("nlink = %d", st.Nlink)
	}
	if Link(p("missing"), p("hard2")) != -1 || GetErrno() != ENOENT {
		t.Error("Link of a missing file")
	}
}
