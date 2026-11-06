package golibc

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Signals (signal.h).
//
// The Go runtime owns the process's real signal handlers, so handlers
// registered here run on an ordinary goroutine after the runtime has received
// the signal. That makes them safer than C handlers (they may call anything,
// including Printf and Malloc) but not asynchronous: a handler does not
// interrupt running code.

const (
	SIGHUP  = int(syscall.SIGHUP)
	SIGINT  = int(syscall.SIGINT)
	SIGQUIT = int(syscall.SIGQUIT)
	SIGABRT = int(syscall.SIGABRT)
	SIGKILL = int(syscall.SIGKILL)
	SIGUSR1 = int(syscall.SIGUSR1)
	SIGUSR2 = int(syscall.SIGUSR2)
	SIGPIPE = int(syscall.SIGPIPE)
	SIGALRM = int(syscall.SIGALRM)
	SIGTERM = int(syscall.SIGTERM)
	SIGCHLD = int(syscall.SIGCHLD)
)

var (
	sigMu       sync.Mutex
	sigChannels = map[int]chan os.Signal{}
)

// Signal installs handler for sig, replacing any earlier one. A nil handler
// restores the default action. The handler runs on its own goroutine.
func Signal(sig int, handler func(sig int)) {
	sigMu.Lock()
	defer sigMu.Unlock()
	if old := sigChannels[sig]; old != nil {
		signal.Stop(old)
		close(old)
		delete(sigChannels, sig)
	}
	s := syscall.Signal(sig)
	if handler == nil {
		signal.Reset(s)
		return
	}
	ch := make(chan os.Signal, 8)
	sigChannels[sig] = ch
	signal.Notify(ch, s)
	go func() {
		for range ch {
			handler(sig)
		}
	}()
}

// SignalIgnore makes the process ignore sig.
func SignalIgnore(sig int) {
	Signal(sig, nil)
	signal.Ignore(syscall.Signal(sig))
}

// Raise sends sig to the calling process.
func Raise(sig int) int { return Kill(Getpid(), sig) }
