//go:build linux

package golibc

import "testing"

// newPipe returns a pipe that is closed when the test ends.
func newPipe(t *testing.T) [2]int {
	t.Helper()
	var p [2]int
	if Pipe(&p) != 0 {
		t.Fatal("Pipe")
	}
	t.Cleanup(func() { Close(p[0]); Close(p[1]) })
	return p
}
