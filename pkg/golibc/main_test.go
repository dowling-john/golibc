//go:build linux

package golibc

import (
	"fmt"
	"os"
	"testing"
)

func openFDCount() int {
	d, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(d)
}

// TestMain fails the run if the tests leave descriptors open. The slack covers
// the Go runtime's own poller descriptors, which it opens lazily.
func TestMain(m *testing.M) {
	before := openFDCount()
	code := m.Run()
	if before >= 0 && code == 0 {
		if leaked := openFDCount() - before; leaked > 4 {
			fmt.Fprintf(os.Stderr, "FAIL: tests leaked %d file descriptors\n", leaked)
			code = 1
		}
	}
	os.Exit(code)
}
