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

func ExampleSscanf() {
	var name string
	var age int
	var score float64
	n := golibc.Sscanf(golibc.CStr("ada 36 99.5"), golibc.CStr("%s %d %lf"), &name, &age, &score)
	fmt.Println(n, name, age, score)
	// Output: 3 ada 36 99.5
}

func ExampleSocketpair() {
	var fds [2]int
	golibc.Socketpair(golibc.AF_UNIX, golibc.SOCK_STREAM, 0, &fds)
	defer golibc.Close(fds[0])
	defer golibc.Close(fds[1])

	msg := []byte("ping")
	golibc.Send(fds[0], unsafe.Pointer(&msg[0]), len(msg), 0)

	// Wait up to a second for the other end to become readable, then read.
	ready := golibc.Poll([]golibc.Pollfd{{Fd: int32(fds[1]), Events: golibc.POLLIN}}, 1000)
	buf := make([]byte, 16)
	n := golibc.Recv(fds[1], unsafe.Pointer(&buf[0]), len(buf), 0)
	fmt.Println(ready, string(buf[:n]))
	// Output: 1 ping
}
