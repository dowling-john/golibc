//go:build linux

// hello is a demo that uses only golibc for I/O, memory and formatting.
package main

import (
	"unsafe"

	"github.com/johndowling/golibc/pkg/golibc"
)

func main() {
	golibc.Printf(golibc.CStr("hello from golibc, pid %d\n"), golibc.Getpid())

	// Heap memory straight from mmap: a malloc'd, sorted int array.
	n := 8
	a := (*[8]int32)(golibc.Malloc(n * 4))
	for i := range a {
		a[i] = int32(golibc.Rand() % 100)
	}
	golibc.Qsort(unsafe.Pointer(a), n, 4, func(x, y unsafe.Pointer) int {
		return int(*(*int32)(x) - *(*int32)(y))
	})
	for i := range a {
		if i > 0 {
			golibc.Fputs(golibc.CStr(", "), golibc.Stdout)
		}
		golibc.Printf(golibc.CStr("%d"), a[i])
	}
	golibc.Puts(golibc.CStr(""))
	golibc.Free(unsafe.Pointer(a))

	golibc.Printf(golibc.CStr("pi=%.5f  hex=%#x  [%-6s] [%6s]\n"), golibc.M_PI, 48879, golibc.CStr("ab"), golibc.CStr("cd"))
	golibc.Exit(0) // flushes stdio
}
