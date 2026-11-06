package golibc

import (
	"math"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Allocator: memory comes straight from mmap, invisible to the Go GC, so
// callers must Free what they Malloc.
//
// Small blocks (up to 64 KiB) are power-of-two size classes carved from 1 MiB
// arenas and recycled through free lists. The free lists are split into
// shards so goroutines rarely contend; a shard that runs dry steals from the
// others before carving new memory, which keeps producer/consumer patterns
// (allocate in one goroutine, free in another) from growing the heap.
//
// Large blocks get their own mapping. A few recently freed ones are cached so
// repeated large allocations skip the mmap/munmap round trip.
//
// Every payload is 16-byte aligned and preceded by a 16-byte header that
// carries a magic number, which is how Free detects double frees and
// pointers that did not come from Malloc.

const (
	hdrSize    = 16
	arenaSize  = 1 << 20
	numClasses = 13 // payload capacities 16 .. 64 KiB
	maxSmall   = 16 << (numClasses - 1)
	pageSize   = 4096

	largeClass = ^uint32(0)
	magicLive  = 0xA110C8ED
	magicFree  = 0xF4EEF4EE

	nShards = 8 // power of two

	largeCacheSlots    = 16
	largeCacheMaxBlock = 4 << 20  // bigger blocks go straight back to the kernel
	largeCacheMaxBytes = 16 << 20 // total bytes held in the cache
)

type header struct {
	size  uintptr // payload capacity in bytes
	class uint32  // size class index, or largeClass
	magic uint32
}

type shard struct {
	mu   sync.Mutex
	mask atomic.Uint32 // bit c set while free[c] is non-empty; read without the lock by stealers
	free [numClasses]unsafe.Pointer
	_    [64]byte // keep neighbouring shards off the same cache line
}

var (
	shards [nShards]shard

	arena struct {
		mu   sync.Mutex
		cur  unsafe.Pointer
		left uintptr
	}

	largeCache struct {
		mu    sync.Mutex
		n     int
		bytes int
		slots [largeCacheSlots]*header
	}
)

const protRW = syscall.PROT_READ | syscall.PROT_WRITE
const mapAnon = syscall.MAP_PRIVATE | syscall.MAP_ANON

// heapFatal reports heap corruption. It is a variable so tests can intercept it.
var heapFatal = func(msg string) {
	Dprintf(2, CStr("golibc: %s\n"), CStr(msg))
	Abort()
}

func hdrOf(p unsafe.Pointer) *header { return (*header)(unsafe.Add(p, -hdrSize)) }

func classFor(n int) int {
	c, sz := 0, 16
	for sz < n {
		sz <<= 1
		c++
	}
	return c
}

// shardIndex picks a shard from the caller's stack address. Each goroutine has
// its own stack, so concurrent goroutines spread across shards; any shard can
// serve or receive any block, so a poor hash only costs contention.
func shardIndex() int {
	var x byte
	a := uintptr(unsafe.Pointer(&x))
	return int((a>>13 ^ a>>17) & (nShards - 1))
}

func popFrom(s *shard, c int) unsafe.Pointer {
	p := s.free[c]
	if p != nil {
		next := *(*unsafe.Pointer)(p)
		s.free[c] = next
		if next == nil {
			s.mask.Store(s.mask.Load() &^ (1 << c))
		}
	}
	return p
}

func popSmall(c int) unsafe.Pointer {
	home := shardIndex()
	s := &shards[home]
	s.mu.Lock()
	p := popFrom(s, c)
	s.mu.Unlock()
	if p != nil {
		return p
	}
	for i := 1; i < nShards; i++ {
		o := &shards[(home+i)&(nShards-1)]
		if o.mask.Load()&(1<<c) == 0 {
			continue
		}
		o.mu.Lock()
		p = popFrom(o, c)
		o.mu.Unlock()
		if p != nil {
			return p
		}
	}
	return nil
}

func carve(need uintptr) (unsafe.Pointer, Errno) {
	arena.mu.Lock()
	if arena.left < need {
		a, e := rawMmap(nil, arenaSize, protRW, mapAnon, -1, 0)
		if e != 0 {
			arena.mu.Unlock()
			return nil, e
		}
		arena.cur, arena.left = a, arenaSize
	}
	blk := arena.cur
	arena.cur = unsafe.Add(arena.cur, need)
	arena.left -= need
	arena.mu.Unlock()
	return blk, 0
}

func takeCachedLarge(total int) *header {
	largeCache.mu.Lock()
	defer largeCache.mu.Unlock()
	best := -1
	for i := 0; i < largeCache.n; i++ {
		sz := int(largeCache.slots[i].size) + hdrSize
		if sz >= total && sz <= total+total/4 && (best < 0 || sz < int(largeCache.slots[best].size)+hdrSize) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	h := largeCache.slots[best]
	largeCache.n--
	largeCache.slots[best] = largeCache.slots[largeCache.n]
	largeCache.slots[largeCache.n] = nil
	largeCache.bytes -= int(h.size) + hdrSize
	return h
}

func freeLarge(h *header) {
	total := int(h.size) + hdrSize
	if total <= largeCacheMaxBlock {
		largeCache.mu.Lock()
		if largeCache.n < largeCacheSlots && largeCache.bytes+total <= largeCacheMaxBytes {
			h.magic = magicFree
			largeCache.slots[largeCache.n] = h
			largeCache.n++
			largeCache.bytes += total
			largeCache.mu.Unlock()
			return
		}
		largeCache.mu.Unlock()
	}
	h.magic = magicFree
	rawMunmap(unsafe.Pointer(h), total)
}

// alloc returns a block and whether its contents are known to be zero.
func alloc(n int) (p unsafe.Pointer, zeroed bool) {
	if n < 0 || n > math.MaxInt-hdrSize-pageSize {
		SetErrno(ENOMEM)
		return nil, false
	}
	if n == 0 {
		n = 1
	}
	if n > maxSmall {
		total := (n + hdrSize + pageSize - 1) &^ (pageSize - 1)
		if h := takeCachedLarge(total); h != nil {
			h.magic = magicLive
			return unsafe.Add(unsafe.Pointer(h), hdrSize), false
		}
		q, e := rawMmap(nil, total, protRW, mapAnon, -1, 0)
		if e != 0 {
			SetErrno(e)
			return nil, false
		}
		*(*header)(q) = header{size: uintptr(total - hdrSize), class: largeClass, magic: magicLive}
		return unsafe.Add(q, hdrSize), true
	}
	c := classFor(n)
	if p := popSmall(c); p != nil {
		hdrOf(p).magic = magicLive
		return p, false
	}
	capacity := uintptr(16) << c
	blk, e := carve(hdrSize + capacity)
	if e != 0 {
		SetErrno(e)
		return nil, false
	}
	*(*header)(blk) = header{size: capacity, class: uint32(c), magic: magicLive}
	return unsafe.Add(blk, hdrSize), true
}

// Malloc allocates n bytes. It returns nil and sets errno to ENOMEM (or the
// kernel's error) on failure.
func Malloc(n int) unsafe.Pointer {
	p, _ := alloc(n)
	return p
}

// Free releases memory from Malloc, Calloc or Realloc. Free(nil) is a no-op.
// Freeing the same pointer twice, or a pointer that did not come from this
// allocator, aborts the process with a message on stderr.
func Free(p unsafe.Pointer) {
	if p == nil {
		return
	}
	h := hdrOf(p)
	if h.magic != magicLive {
		if h.magic == magicFree {
			heapFatal("free(): double free detected")
		} else {
			heapFatal("free(): invalid pointer")
		}
		return
	}
	if h.class == largeClass {
		freeLarge(h)
		return
	}
	h.magic = magicFree
	c := int(h.class)
	s := &shards[shardIndex()]
	s.mu.Lock()
	*(*unsafe.Pointer)(p) = s.free[c]
	s.free[c] = p
	s.mask.Store(s.mask.Load() | 1<<c)
	s.mu.Unlock()
}

// Calloc allocates zeroed memory for nmemb elements of size bytes.
func Calloc(nmemb, size int) unsafe.Pointer {
	if nmemb < 0 || size < 0 || (nmemb != 0 && size > math.MaxInt/nmemb) {
		SetErrno(ENOMEM)
		return nil
	}
	n := nmemb * size
	p, zeroed := alloc(n)
	if p != nil && !zeroed {
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
	if h.magic != magicLive {
		Free(p) // reports the double free / invalid pointer
		return nil
	}
	if n >= 0 && uintptr(n) <= h.size && (h.class != largeClass || n > maxSmall) {
		return p
	}
	if h.class == largeClass && n > maxSmall && n <= math.MaxInt-hdrSize-pageSize {
		total := (n + hdrSize + pageSize - 1) &^ (pageSize - 1)
		q, e := rawMremap(unsafe.Pointer(h), int(h.size)+hdrSize, total)
		if e != 0 {
			SetErrno(e)
			return nil
		}
		(*header)(q).size = uintptr(total - hdrSize)
		return unsafe.Add(q, hdrSize)
	}
	q := Malloc(n)
	if q == nil {
		return nil
	}
	Memcpy(q, p, min(n, int(h.size)))
	Free(p)
	return q
}
