//go:build linux

package golibc

import (
	"fmt"
	"testing"
	"unsafe"
)

func BenchmarkMemchr(b *testing.B) {
	for _, n := range []int{16, 256, 4096, 65536} {
		buf := make([]byte, n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.SetBytes(int64(n))
			for i := 0; i < b.N; i++ {
				if Memchr(unsafe.Pointer(&buf[0]), 1, n) != nil {
					b.Fatal("found a byte that is not there")
				}
			}
		})
	}
}
