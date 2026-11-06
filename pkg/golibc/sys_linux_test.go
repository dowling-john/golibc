package golibc

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestWriteReadPipe(t *testing.T) {
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		t.Fatal(err)
	}
	msg := []byte("hi")
	if n := Write(p[1], unsafe.Pointer(&msg[0]), len(msg)); n != 2 {
		t.Fatalf("Write = %d", n)
	}
	buf := make([]byte, 8)
	if n := Read(p[0], unsafe.Pointer(&buf[0]), len(buf)); n != 2 || string(buf[:2]) != "hi" {
		t.Fatalf("Read = %d %q", n, buf)
	}
	if Close(p[0]) != 0 || Close(p[1]) != 0 {
		t.Fatal("Close")
	}
	if Close(p[0]) != -1 || Errno != syscall.EBADF {
		t.Errorf("double close: errno = %v", Errno)
	}
}

func TestMmap(t *testing.T) {
	const size = 1 << 16
	p := Mmap(nil, size, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANON, -1, 0)
	if p == nil {
		t.Fatalf("Mmap failed: %v", Errno)
	}
	b := unsafe.Slice((*byte)(p), size)
	if b[0] != 0 || b[size-1] != 0 {
		t.Error("anonymous map should be zeroed")
	}
	b[size-1] = 7
	if Munmap(p, size) != 0 {
		t.Errorf("Munmap: %v", Errno)
	}
}
