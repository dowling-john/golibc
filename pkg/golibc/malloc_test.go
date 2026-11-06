//go:build linux

package golibc

import (
	"math"
	"sync"
	"syscall"
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
			t.Fatalf("Malloc(%d) failed: %v", n, GetErrno())
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
	if Malloc(-1) != nil || GetErrno() != ENOMEM {
		t.Error("negative size should fail with ENOMEM")
	}
	SetErrno(0)
	if Malloc(math.MaxInt) != nil || GetErrno() != ENOMEM { // rejected before any syscall
		t.Errorf("huge allocation: errno = %v", GetErrno())
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
	q := Realloc(p, 4000000) // modest shrink of a large block keeps it
	if q != p {
		t.Error("shrink within the large range should not move")
	}
	checkPattern(t, q, 100, 6)
	q = Realloc(q, 100) // big shrink moves to a small block and releases the mapping
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

// captureHeapFatal replaces the abort hook so heap errors can be asserted on.
func captureHeapFatal(t *testing.T) *string {
	t.Helper()
	var msg string
	old := heapFatal
	heapFatal = func(m string) { msg = m }
	t.Cleanup(func() { heapFatal = old })
	return &msg
}

func TestDoubleFreeDetected(t *testing.T) {
	msg := captureHeapFatal(t)
	p := Malloc(64)
	Free(p)
	Free(p)
	if *msg != "free(): double free detected" {
		t.Errorf("small double free: %q", *msg)
	}
	*msg = ""
	q := Malloc(100000) // large block: cached on free, so the header stays mapped
	Free(q)
	Free(q)
	if *msg != "free(): double free detected" {
		t.Errorf("large double free: %q", *msg)
	}
}

func TestInvalidFreeDetected(t *testing.T) {
	msg := captureHeapFatal(t)
	buf := make([]byte, 64)
	for i := range buf {
		buf[i] = 0x5a
	}
	Free(unsafe.Pointer(&buf[32])) // header bytes are garbage, not a valid magic
	if *msg != "free(): invalid pointer" {
		t.Errorf("invalid free: %q", *msg)
	}
	*msg = ""
	if Realloc(unsafe.Pointer(&buf[32]), 10) != nil || *msg == "" {
		t.Error("Realloc of an invalid pointer should fail loudly")
	}
}

func TestLargeBlockCacheReuse(t *testing.T) {
	p := Malloc(300000)
	fillPattern(p, 300000, 1)
	Free(p)
	q := Malloc(290000) // within 25% of the cached block: reused
	if q != p {
		t.Errorf("cached large block not reused (%p vs %p)", p, q)
	}
	Free(q)
	// A recycled block is dirty, so Calloc must clear it.
	z := Calloc(1, 300000)
	for i, v := range mem(z, 300000) {
		if v != 0 {
			t.Fatalf("Calloc over cached block: byte %d = %d", i, v)
		}
	}
	Free(z)
	// A far larger request must not take the cached block.
	big := Malloc(3000000)
	if big == p {
		t.Error("oversized request took a small cached block")
	}
	Free(big)
}

func TestLargeShrinkMovesToSmall(t *testing.T) {
	p := Malloc(1 << 20)
	fillPattern(p, 100, 4)
	q := Realloc(p, 100)
	checkPattern(t, q, 100, 4)
	if hdrOf(q).class == largeClass {
		t.Error("shrinking far below the large threshold should move to a small block")
	}
	Free(q)
}

// Blocks allocated in one goroutine and freed in another land in different
// shards. The allocator must find them again instead of growing forever.
func TestProducerConsumerDoesNotGrow(t *testing.T) {
	run := func(rounds int) {
		ch := make(chan unsafe.Pointer, 64)
		done := make(chan struct{})
		go func() {
			for p := range ch {
				Free(p)
			}
			close(done)
		}()
		for i := 0; i < rounds; i++ {
			ch <- Malloc(200)
		}
		close(ch)
		<-done
	}
	run(1000) // warm up
	arena.mu.Lock()
	before := arena.left
	arena.mu.Unlock()
	run(200000) // at 256 bytes per block this would need ~50 MiB if blocks leaked
	arena.mu.Lock()
	after := arena.left
	arena.mu.Unlock()
	if used := int64(before) - int64(after); used > 4<<20 {
		t.Errorf("heap grew by %d bytes across 200000 allocations", used)
	}
}

func TestErrnoUnderConcurrency(t *testing.T) {
	// Internals must not depend on the shared errno: hammer pipes (which
	// retry on EINTR internally) while other goroutines churn errno.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					SetErrno(Errno(1 + i))
					Close(-1) // sets EBADF
				}
			}
		}(i)
	}
	for i := 0; i < 200; i++ {
		var p [2]int
		syscall.Pipe(p[:])
		w, r := Fdopen(p[1], CStr("w")), Fdopen(p[0], CStr("r"))
		Fputs(CStr("data\n"), w)
		Fclose(w)
		buf := make([]byte, 16)
		if GoStr(Fgets(&buf[0], 16, r)) != "data\n" {
			t.Fatal("stdio result depended on errno state")
		}
		Fclose(r)
	}
	close(stop)
	wg.Wait()
}
