//go:build linux

package golibc

import (
	"math"
	"sync"
	"testing"
	"unsafe"
)

func fillPattern(p unsafe.Pointer, n int, seed byte) {
	b := mem(p, n)
	for i := range b {
		b[i] = seed + byte(i*7)
	}
}

func checkPattern(t *testing.T, p unsafe.Pointer, n int, seed byte) {
	t.Helper()
	for i, v := range mem(p, n) {
		if v != seed+byte(i*7) {
			t.Fatalf("corrupted at byte %d of %d", i, n)
		}
	}
}

func TestMallocAlignmentAndIsolation(t *testing.T) {
	sizes := []int{1, 15, 16, 17, 100, 4096, 65536, 65537, 1 << 20}
	var ptrs []unsafe.Pointer
	for i, n := range sizes {
		p := Malloc(n)
		if p == nil {
			t.Fatalf("Malloc(%d) failed: %v", n, Errno)
		}
		if uintptr(p)%16 != 0 {
			t.Errorf("Malloc(%d) = %p not 16-aligned", n, p)
		}
		fillPattern(p, n, byte(i))
		ptrs = append(ptrs, p)
	}
	for i, n := range sizes { // neighbours must not have overwritten each other
		checkPattern(t, ptrs[i], n, byte(i))
		Free(ptrs[i])
	}
}

func TestMallocReuse(t *testing.T) {
	p := Malloc(100)
	Free(p)
	if q := Malloc(100); q != p {
		t.Errorf("freed block not reused: %p vs %p", p, q)
	}
	Free(Malloc(0)) // zero-size returns a unique, freeable pointer
	if Malloc(0) == nil {
		t.Error("Malloc(0) = nil")
	}
	Free(nil)
}

func TestMallocFailure(t *testing.T) {
	if Malloc(-1) != nil || Errno != ENOMEM {
		t.Error("negative size should fail with ENOMEM")
	}
	Errno = 0
	if Malloc(math.MaxInt) != nil || Errno != ENOMEM { // rejected before any syscall
		t.Errorf("huge allocation: errno = %v", Errno)
	}
	if Calloc(1<<40, 1<<40) != nil {
		t.Error("Calloc overflow not detected")
	}
}

func TestCalloc(t *testing.T) {
	p := Malloc(256)
	fillPattern(p, 256, 9)
	Free(p)
	q := Calloc(16, 16) // same class: reused memory must come back zeroed
	for i, v := range mem(q, 256) {
		if v != 0 {
			t.Fatalf("Calloc byte %d = %d", i, v)
		}
	}
	Free(q)
	big := Calloc(1, 200000)
	if mem(big, 200000)[199999] != 0 {
		t.Error("large Calloc not zero")
	}
	Free(big)
}

func TestRealloc(t *testing.T) {
	p := Realloc(nil, 10)
	fillPattern(p, 10, 3)
	p = Realloc(p, 10000) // small -> larger class
	checkPattern(t, p, 10, 3)
	fillPattern(p, 10000, 5)
	p = Realloc(p, 300000) // small -> large
	checkPattern(t, p, 10000, 5)
	fillPattern(p, 300000, 6)
	p = Realloc(p, 5000000) // large -> larger (mremap)
	checkPattern(t, p, 300000, 6)
	q := Realloc(p, 100) // shrink keeps the block
	if q != p {
		t.Error("shrink should not move")
	}
	checkPattern(t, q, 100, 6)
	if Realloc(q, 0) != nil {
		t.Error("Realloc(p, 0) should free and return nil")
	}
}

func TestMallocConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			var held []unsafe.Pointer
			for i := 0; i < 2000; i++ {
				n := 1 + (i*31+g*17)%3000
				p := Malloc(n)
				fillPattern(p, n, byte(g))
				held = append(held, p)
				if len(held) > 20 {
					Free(held[0])
					held = held[1:]
				}
			}
			for _, p := range held {
				Free(p)
			}
		}(g)
	}
	wg.Wait()
}

func TestStrdup(t *testing.T) {
	p := Strdup(CStr("duplicate"))
	if GoStr(p) != "duplicate" {
		t.Error("Strdup")
	}
	Free(unsafe.Pointer(p))
	q := Strndup(CStr("abcdef"), 3)
	if GoStr(q) != "abc" {
		t.Error("Strndup")
	}
	Free(unsafe.Pointer(q))
}
