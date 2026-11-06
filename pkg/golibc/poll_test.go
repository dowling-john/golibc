//go:build linux

package golibc

import (
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPoll(t *testing.T) {
	p := newPipe(t)
	fds := []Pollfd{{Fd: int32(p[0]), Events: POLLIN}, {Fd: int32(p[1]), Events: POLLOUT}}
	if n := Poll(fds, 0); n != 1 || fds[0].Revents != 0 || fds[1].Revents&POLLOUT == 0 {
		t.Fatalf("idle pipe: n=%d %+v", n, fds)
	}
	start := time.Now()
	if Poll(fds[:1], 60) != 0 || time.Since(start) < 55*time.Millisecond {
		t.Errorf("timeout: returned after %v", time.Since(start))
	}
	b := []byte("x")
	Write(p[1], ptr(b), 1)
	if n := Poll(fds[:1], -1); n != 1 || fds[0].Revents&POLLIN == 0 {
		t.Errorf("readable: n=%d %+v", n, fds[0])
	}
	Read(p[0], ptr(b), 1)
	// A writer in another goroutine wakes an infinite wait.
	go func() { time.Sleep(30 * time.Millisecond); Write(p[1], ptr(b), 1) }()
	if Poll(fds[:1], -1) != 1 {
		t.Error("infinite wait should wake on data")
	}
	Close(p[1])
	Read(p[0], ptr(b), 1)
	fds[0].Revents = 0
	if Poll(fds[:1], 1000) != 1 || fds[0].Revents&POLLHUP == 0 {
		t.Errorf("closed writer should report POLLHUP: %+v", fds[0])
	}
	bad := []Pollfd{{Fd: 9999, Events: POLLIN}}
	if Poll(bad, 0) != 1 || bad[0].Revents&POLLNVAL == 0 {
		t.Errorf("invalid fd: %+v", bad[0])
	}
	if Poll(nil, 10) != 0 {
		t.Error("empty set is a plain sleep")
	}
}

func TestEpoll(t *testing.T) {
	p := newPipe(t)
	ep := EpollCreate1(EPOLL_CLOEXEC)
	if ep < 0 {
		t.Fatalf("EpollCreate1: %v", GetErrno())
	}
	defer Close(ep)
	ev := EpollEvent{Events: EPOLLIN, Fd: int32(p[0])}
	if EpollCtl(ep, EPOLL_CTL_ADD, p[0], &ev) != 0 {
		t.Fatalf("EPOLL_CTL_ADD: %v", GetErrno())
	}
	out := make([]EpollEvent, 4)
	if EpollWait(ep, out, 0) != 0 {
		t.Error("nothing ready yet")
	}
	b := []byte("x")
	Write(p[1], ptr(b), 1)
	if n := EpollWait(ep, out, 1000); n != 1 || out[0].Fd != int32(p[0]) || out[0].Events&EPOLLIN == 0 {
		t.Fatalf("EpollWait n=%d %+v", n, out[0])
	}
	// Level-triggered: still readable until drained.
	if EpollWait(ep, out, 0) != 1 {
		t.Error("level-triggered event should repeat")
	}
	// EPOLLONESHOT disarms after one report; MOD re-arms.
	ev.Events = EPOLLIN | EPOLLONESHOT
	EpollCtl(ep, EPOLL_CTL_MOD, p[0], &ev)
	EpollWait(ep, out, 0)
	if EpollWait(ep, out, 0) != 0 {
		t.Error("EPOLLONESHOT should disarm")
	}
	EpollCtl(ep, EPOLL_CTL_MOD, p[0], &ev)
	if EpollWait(ep, out, 0) != 1 {
		t.Error("MOD should re-arm")
	}
	if EpollCtl(ep, EPOLL_CTL_DEL, p[0], nil) != 0 || EpollWait(ep, out, 0) != 0 {
		t.Error("EPOLL_CTL_DEL")
	}
	if EpollCtl(ep, EPOLL_CTL_DEL, p[0], nil) != -1 || GetErrno() != ENOENT {
		t.Error("deleting an unregistered fd should fail with ENOENT")
	}
	if EpollWait(ep, nil, 0) != -1 || GetErrno() != EINVAL || EpollCreate(0) != -1 || Close(EpollCreate(1)) != 0 {
		t.Error("argument checks")
	}
}

func TestSelect(t *testing.T) {
	p := newPipe(t)
	var r Fdset
	FD_ZERO(&r)
	FD_SET(p[0], &r)
	if !FD_ISSET(p[0], &r) || FD_ISSET(p[1], &r) {
		t.Fatal("FD_SET/FD_ISSET")
	}
	tv := Timeval{Usec: 20000}
	start := time.Now()
	if Select(p[0]+1, &r, nil, nil, &tv) != 0 || time.Since(start) < 15*time.Millisecond {
		t.Errorf("timeout path: %v", time.Since(start))
	}
	FD_SET(p[0], &r)
	b := []byte("x")
	Write(p[1], ptr(b), 1)
	if Select(p[0]+1, &r, nil, nil, &tv) != 1 || !FD_ISSET(p[0], &r) {
		t.Error("readable pipe")
	}
	var w Fdset
	FD_SET(p[1], &w)
	if Select(p[1]+1, nil, &w, nil, &Timeval{}) != 1 || !FD_ISSET(p[1], &w) {
		t.Error("writable pipe")
	}
	FD_CLR(p[1], &w)
	if FD_ISSET(p[1], &w) {
		t.Error("FD_CLR")
	}
	if Select(-1, nil, nil, nil, &Timeval{}) != -1 || GetErrno() != EINVAL {
		t.Error("negative nfds")
	}
}

// interrupt arranges for the calling OS thread to be signalled while f runs,
// which makes any non-restartable wait return EINTR if it does not retry.
func interrupt(t *testing.T, f func()) (signals int) {
	t.Helper()
	got := make(chan int, 8)
	Signal(SIGUSR1, func(int) { got <- 1 }) // a registered handler, so the signal does not kill the process
	defer Signal(SIGUSR1, nil)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid, _, _ := syscall.RawSyscall(syscall.SYS_GETTID, 0, 0, 0)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 3; i++ { // several interruptions, spread through the wait
			time.Sleep(25 * time.Millisecond)
			syscall.Syscall(syscall.SYS_TGKILL, uintptr(Getpid()), tid, uintptr(SIGUSR1))
		}
		close(done)
	}()
	f()
	<-done
	return len(got)
}

func TestWaitsSurviveInterruption(t *testing.T) {
	p := newPipe(t)
	check := func(name string, want time.Duration, f func() int) {
		start := time.Now()
		var res int
		interrupt(t, func() { res = f() })
		if res != 0 {
			t.Errorf("%s returned %d (errno %v) instead of timing out", name, res, GetErrno())
		}
		if el := time.Since(start); el < want-5*time.Millisecond {
			t.Errorf("%s returned after %v, but the timeout was %v", name, el, want)
		}
	}
	check("Poll", 150*time.Millisecond, func() int { return Poll([]Pollfd{{Fd: int32(p[0]), Events: POLLIN}}, 150) })
	ep := EpollCreate1(0)
	defer Close(ep)
	EpollCtl(ep, EPOLL_CTL_ADD, p[0], &EpollEvent{Events: EPOLLIN, Fd: int32(p[0])})
	check("EpollWait", 150*time.Millisecond, func() int { return EpollWait(ep, make([]EpollEvent, 1), 150) })
	check("Select", 150*time.Millisecond, func() int {
		var r Fdset
		FD_SET(p[0], &r)
		return Select(p[0]+1, &r, nil, nil, &Timeval{Usec: 150000})
	})
	check("Nanosleep", 150*time.Millisecond, func() int { return Nanosleep(&Timespec{Nsec: 150_000_000}, nil) })
	check("Usleep", 150*time.Millisecond, func() int { return Usleep(150_000) })
}

func TestFdsetBounds(t *testing.T) {
	var s Fdset
	FD_SET(FD_SETSIZE-1, &s)
	if !FD_ISSET(FD_SETSIZE-1, &s) || FD_ISSET(FD_SETSIZE, &s) || FD_ISSET(-1, &s) {
		t.Error("boundary descriptors")
	}
	for _, fd := range []int{FD_SETSIZE, -1, 5000} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), "FD_SETSIZE") {
					t.Errorf("FD_SET(%d) should panic with an explanatory message, got %v", fd, r)
				}
			}()
			FD_SET(fd, &s)
		}()
	}
	if Select(FD_SETSIZE+1, nil, nil, nil, &Timeval{}) != -1 || GetErrno() != EINVAL {
		t.Error("nfds beyond FD_SETSIZE must fail with EINVAL")
	}
}
