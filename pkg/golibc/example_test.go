//go:build linux

package golibc_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/dowling-john/golibc/pkg/golibc"
)

func ExampleSnprintf() {
	buf := make([]byte, 64)
	n := golibc.Snprintf(&buf[0], len(buf), golibc.CStr("%-6s|%5.2f|%#x"), golibc.CStr("pi"), math.Pi, 255)
	fmt.Println(string(buf[:n]))
	// Output: pi    | 3.14|0xff
}

func ExampleStrtol() {
	var rest *byte
	n := golibc.Strtol(golibc.CStr("  0x1F apples"), &rest, 0)
	fmt.Println(n, golibc.GoStr(rest))
	// Output: 31  apples
}

func ExampleMalloc() {
	p := golibc.Malloc(4 * 4)
	defer golibc.Free(p)
	nums := unsafe.Slice((*int32)(p), 4)
	for i := range nums {
		nums[i] = int32(10 - i*3)
	}
	golibc.Qsort(p, len(nums), 4, func(a, b unsafe.Pointer) int {
		return int(*(*int32)(a) - *(*int32)(b))
	})
	fmt.Println(nums)
	// Output: [1 4 7 10]
}

func ExampleFopen() {
	dir, _ := os.MkdirTemp("", "golibc")
	defer os.RemoveAll(dir)
	path := golibc.CStr(filepath.Join(dir, "notes.txt"))

	f := golibc.Fopen(path, golibc.CStr("w"))
	golibc.Fprintf(f, golibc.CStr("answer=%d\n"), 42)
	golibc.Fclose(f)

	f = golibc.Fopen(path, golibc.CStr("r"))
	defer golibc.Fclose(f)
	line := make([]byte, 32)
	fmt.Print(golibc.GoStr(golibc.Fgets(&line[0], len(line), f)))
	// Output: answer=42
}

func ExampleStrstr() {
	hay := golibc.CStr("the quick brown fox")
	fmt.Println(golibc.GoStr(golibc.Strstr(hay, golibc.CStr("brown"))))
	// Output: brown fox
}
