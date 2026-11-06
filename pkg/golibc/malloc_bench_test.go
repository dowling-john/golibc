//go:build linux

package golibc

import (
	"testing"
	"unsafe"
)

func BenchmarkMallocFreeSmall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Free(Malloc(64))
	}
}

func BenchmarkMallocFreeParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Free(Malloc(64))
		}
	})
}

// Mixed sizes with a rolling working set, closer to real allocation patterns.
func BenchmarkMallocMixed(b *testing.B) {
	var held [64]unsafe.Pointer
	sizes := [...]int{16, 24, 100, 512, 4096, 70000}
	for i := 0; i < b.N; i++ {
		j := i % len(held)
		Free(held[j])
		held[j] = Malloc(sizes[i%len(sizes)])
	}
	for _, p := range held {
		Free(p)
	}
}

func BenchmarkMallocLarge(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Free(Malloc(1 << 20))
	}
}

func BenchmarkRealloc(b *testing.B) {
	for i := 0; i < b.N; i++ {
		p := Malloc(16)
		for n := 32; n <= 1<<16; n *= 2 {
			p = Realloc(p, n)
		}
		Free(p)
	}
}
