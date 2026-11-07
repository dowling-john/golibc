package golibc

import "sync"

// Pseudo-random numbers. Rand and Random are glibc's default generator (the
// "TYPE_3" additive feedback generator with a 31-word state) and share one
// state, so a program seeded with Srand gets the same sequence it would on
// glibc. The *rand48 family is the 48-bit linear congruential generator.

const (
	randDeg = 31
	randSep = 3
)

var (
	randMu  sync.Mutex
	randGen = newRandGen(1)
)

type randGenerator struct {
	r    [randDeg]int32
	f, b int
}

func newRandGen(seed uint32) *randGenerator {
	g := &randGenerator{}
	g.seed(seed)
	return g
}

// seed follows glibc's srandom_r: a Lehmer generator fills the state, then
// the first 310 outputs are discarded.
func (g *randGenerator) seed(seed uint32) {
	if seed == 0 {
		seed = 1
	}
	word := int32(seed)
	g.r[0] = word
	for i := 1; i < randDeg; i++ {
		hi := int64(word) / 127773
		lo := int64(word) % 127773
		w := 16807*lo - 2836*hi
		word = int32(w)
		if word < 0 {
			word += 2147483647
		}
		g.r[i] = word
	}
	g.f, g.b = randSep, 0
	for i := 0; i < randDeg*10; i++ {
		g.next()
	}
}

func (g *randGenerator) next() int {
	g.r[g.f] = int32(uint32(g.r[g.f]) + uint32(g.r[g.b]))
	v := int(uint32(g.r[g.f]) >> 1)
	if g.f++; g.f >= randDeg {
		g.f = 0
	}
	if g.b++; g.b >= randDeg {
		g.b = 0
	}
	return v
}

// Srand seeds Rand and Random. Seed 0 is treated as 1, as in glibc.
func Srand(seed uint) {
	randMu.Lock()
	randGen.seed(uint32(seed))
	randMu.Unlock()
}

// Rand returns a pseudo-random int in [0, RAND_MAX].
func Rand() int {
	randMu.Lock()
	v := randGen.next()
	randMu.Unlock()
	return v
}

// Srandom is Srand.
func Srandom(seed uint) { Srand(seed) }

// Random is Rand.
func Random() int { return Rand() }

// The 48-bit generator: X(n+1) = (a*X(n) + c) mod 2^48.
const (
	rand48A    = 0x5DEECE66D
	rand48C    = 0xB
	rand48Mask = 1<<48 - 1
)

var (
	rand48Mu sync.Mutex
	rand48X  uint64 // glibc starts unseeded at zero, so the first Lrand48 is 0
)

func rand48Step(x uint64) uint64 { return (x*rand48A + rand48C) & rand48Mask }

func rand48Load(xsubi *[3]uint16) uint64 {
	return uint64(xsubi[0]) | uint64(xsubi[1])<<16 | uint64(xsubi[2])<<32
}

func rand48Store(xsubi *[3]uint16, x uint64) {
	xsubi[0], xsubi[1], xsubi[2] = uint16(x), uint16(x>>16), uint16(x>>32)
}

// rand48Next advances the shared state and returns the new value.
func rand48Next() uint64 {
	rand48Mu.Lock()
	rand48X = rand48Step(rand48X)
	x := rand48X
	rand48Mu.Unlock()
	return x
}

// Srand48 seeds the 48-bit generator: the seed becomes the high 32 bits and
// the low 16 bits are set to 0x330E.
func Srand48(seed int) {
	rand48Mu.Lock()
	rand48X = uint64(uint32(seed))<<16 | 0x330E
	rand48Mu.Unlock()
}

// Drand48 returns a float64 uniformly distributed in [0, 1).
func Drand48() float64 { return float64(rand48Next()) / (1 << 48) }

// Lrand48 returns a non-negative 31-bit integer.
func Lrand48() int { return int(rand48Next() >> 17) }

// Mrand48 returns a signed 32-bit integer.
func Mrand48() int { return int(int32(rand48Next() >> 16)) }

// Erand48 is Drand48 with the state held in xsubi, which it updates.
func Erand48(xsubi *[3]uint16) float64 {
	x := rand48Step(rand48Load(xsubi))
	rand48Store(xsubi, x)
	return float64(x) / (1 << 48)
}

// Nrand48 is Lrand48 with the state held in xsubi, which it updates.
func Nrand48(xsubi *[3]uint16) int {
	x := rand48Step(rand48Load(xsubi))
	rand48Store(xsubi, x)
	return int(x >> 17)
}

// Jrand48 is Mrand48 with the state held in xsubi, which it updates.
func Jrand48(xsubi *[3]uint16) int {
	x := rand48Step(rand48Load(xsubi))
	rand48Store(xsubi, x)
	return int(int32(x >> 16))
}
