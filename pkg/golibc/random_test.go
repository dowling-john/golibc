//go:build linux

package golibc

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestRandomGolden checks Rand, Random and the *rand48 family against glibc
// (regenerate with scripts/gen_golden.sh). Each case seeds every generator,
// then draws from them in the same order as the C generator program.
func TestRandomGolden(t *testing.T) {
	cases, golden := casesAndGolden(t, "random")
	for i, seed := range cases {
		var n uint64
		if seed == "-" {
			randGen = newRandGen(1) // the state before any seeding call
			rand48X = 0
		} else {
			n, _ = strconv.ParseUint(seed, 10, 32)
			Srand(uint(n))
			Srand48(int(n))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s rand:", seed)
		for j := 0; j < 10; j++ {
			fmt.Fprintf(&b, " %d", Rand())
		}
		if seed != "-" {
			Srandom(uint(n))
		}
		b.WriteString(" random:")
		for j := 0; j < 5; j++ {
			fmt.Fprintf(&b, " %d", Random())
		}
		b.WriteString(" lrand48:")
		for j := 0; j < 5; j++ {
			fmt.Fprintf(&b, " %d", Lrand48())
		}
		b.WriteString(" mrand48:")
		for j := 0; j < 5; j++ {
			fmt.Fprintf(&b, " %d", Mrand48())
		}
		b.WriteString(" drand48:")
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&b, " %s", strconv.FormatFloat(Drand48(), 'g', 17, 64))
		}
		xs := [3]uint16{1, 2, 3}
		b.WriteString(" nrand48:")
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&b, " %d", Nrand48(&xs))
		}
		fmt.Fprintf(&b, " xs=%d,%d,%d", xs[0], xs[1], xs[2])
		xs = [3]uint16{1, 2, 3}
		b.WriteString(" jrand48:")
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&b, " %d", Jrand48(&xs))
		}
		xs = [3]uint16{1, 2, 3}
		b.WriteString(" erand48:")
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&b, " %s", strconv.FormatFloat(Erand48(&xs), 'g', 17, 64))
		}
		if got := b.String(); got != golden[i] {
			t.Errorf("seed %s:\n got  %s\n glibc %s", seed, got, golden[i])
		}
	}
}

func TestRandomSharesState(t *testing.T) {
	Srand(7)
	want := []int{Rand(), Rand(), Rand()}
	Srandom(7)
	for i, w := range want {
		if got := Random(); got != w {
			t.Errorf("Random()[%d] = %d, want %d from the shared Rand state", i, got, w)
		}
	}
}
