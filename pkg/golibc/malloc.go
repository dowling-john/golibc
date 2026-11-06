//go:build linux

package golibc

import (
	"math"
	"sync"
	"syscall"
	"unsafe"
)

// Allocator: memory comes straight from mmap, invisible to the Go GC, so
// callers must Free what they Malloc. Small blocks are power-of-two size
// classes carved from 1 MiB arenas and recycled through per-class free lists.
// Large blocks get their own mapping and are returned to the kernel on Free.
// Every payload is 16-byte aligned and preceded by a 16-byte header.

const (
	hdrSize    = 16
	arenaSize  = 1 << 20
	numClasses = 13 // payload capacities 16 .. 64 KiB
	maxSmall   = 16 << (numClasses - 1)
	pageSize   = 4096
	largeClass = ^uintptr(0)
)

type header struct {
	size  uintptr // payload capacity in bytes
	class uintptr // size class index, or largeClass
}

var (
	heapMu    sync.Mutex
	freeList  [numClasses]unsafe.Pointer // singly linked through each free payload
	arenaCur  unsafe.Pointer
	arenaLeft uintptr
)

const protRW = syscall.PROT_READ | syscall.PROT_WRITE
const mapAnon = syscall.MAP_PRIVATE | syscall.MAP_ANON

func hdrOf(p unsafe.Pointer) *header { return (*header)(unsafe.Add(p, -hdrSize)) }

func classFor(n int) int {
	c, sz := 0, 16
	for sz < n {
		sz <<= 1
		c++
	}
	return c
}

// Malloc allocates n bytes. It returns nil and sets Errno to ENOMEM on failure.
func Malloc(n int) unsafe.Pointer {
	if n < 0 || n > math.MaxInt-hdrSize-pageSize {
		Errno = ENOMEM
		return nil
	}
	if n == 0 {
		n = 1
	}
	if n > maxSmall {
		total := (n + hdrSize + pageSize - 1) &^ (pageSize - 1)
		p := Mmap(nil, total, protRW, mapAnon, -1, 0)
		if p == nil {
			Errno = ENOMEM
			return nil
		}
		*(*header)(p) = header{size: uintptr(total - hdrSize), class: largeClass}
		return unsafe.Add(p, hdrSize)
	}
	c := classFor(n)
	heapMu.Lock()
	defer heapMu.Unlock()
	if p := freeList[c]; p != nil {
		freeList[c] = *(*unsafe.Pointer)(p)
		return p
	}
	capacity := uintptr(16) << c
	need := hdrSize + capacity
	if arenaLeft < need {
		a := Mmap(nil, arenaSize, protRW, mapAnon, -1, 0)
		if a == nil {
			Errno = ENOMEM
			return nil
		}
		arenaCur, arenaLeft = a, arenaSize
	}
	blk := arenaCur
	arenaCur = unsafe.Add(arenaCur, need)
	arenaLeft -= need
	*(*header)(blk) = header{size: capacity, class: uintptr(c)}
	return unsafe.Add(blk, hdrSize)
}

// Free releases memory from Malloc, Calloc or Realloc. Free(nil) is a no-op.
func Free(p unsafe.Pointer) {
	if p == nil {
		return
	}
	h := hdrOf(p)
	if h.class == largeClass {
		Munmap(unsafe.Pointer(h), int(h.size)+hdrSize)
		return
	}
	heapMu.Lock()
	*(*unsafe.Pointer)(p) = freeList[h.class]
	freeList[h.class] = p
	heapMu.Unlock()
}

// Calloc allocates zeroed memory for nmemb elements of size bytes.
func Calloc(nmemb, size int) unsafe.Pointer {
	if nmemb < 0 || size < 0 || (nmemb != 0 && size > math.MaxInt/nmemb) {
		Errno = ENOMEM
		return nil
	}
	n := nmemb * size
	p := Malloc(n)
	if p != nil && hdrOf(p).class != largeClass { // fresh mappings are already zero
		Memset(p, 0, n)
	}
	return p
}

// Realloc resizes a block, preserving contents up to the smaller size. On
// failure it returns nil and leaves the original block valid.
func Realloc(p unsafe.Pointer, n int) unsafe.Pointer {
	if p == nil {
		return Malloc(n)
	}
	if n == 0 {
		Free(p)
		return nil
	}
	h := hdrOf(p)
	if n >= 0 && uintptr(n) <= h.size {
		return p
	}
	if h.class == largeClass && n > 0 && n <= math.MaxInt-hdrSize-pageSize {
		total := (n + hdrSize + pageSize - 1) &^ (pageSize - 1)
		if q := Mremap(unsafe.Pointer(h), int(h.size)+hdrSize, total); q != nil {
			(*header)(q).size = uintptr(total - hdrSize)
			return unsafe.Add(q, hdrSize)
		}
		Errno = ENOMEM
		return nil
	}
	q := Malloc(n)
	if q == nil {
		return nil
	}
	Memcpy(q, p, int(h.size))
	Free(p)
	return q
}
