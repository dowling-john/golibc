//go:build linux

package golibc

import "math"

// math.h, delegating to the standard library's pure-Go implementations.

const (
	M_E     = math.E
	M_PI    = math.Pi
	M_PI_2  = math.Pi / 2
	M_SQRT2 = math.Sqrt2
	M_LN2   = math.Ln2
	M_LN10  = math.Ln10
)

func Fabs(x float64) float64         { return math.Abs(x) }
func Sqrt(x float64) float64         { return math.Sqrt(x) }
func Cbrt(x float64) float64         { return math.Cbrt(x) }
func Pow(x, y float64) float64       { return math.Pow(x, y) }
func Exp(x float64) float64          { return math.Exp(x) }
func Exp2(x float64) float64         { return math.Exp2(x) }
func Log(x float64) float64          { return math.Log(x) }
func Log2(x float64) float64         { return math.Log2(x) }
func Log10(x float64) float64        { return math.Log10(x) }
func Sin(x float64) float64          { return math.Sin(x) }
func Cos(x float64) float64          { return math.Cos(x) }
func Tan(x float64) float64          { return math.Tan(x) }
func Asin(x float64) float64         { return math.Asin(x) }
func Acos(x float64) float64         { return math.Acos(x) }
func Atan(x float64) float64         { return math.Atan(x) }
func Atan2(y, x float64) float64     { return math.Atan2(y, x) }
func Sinh(x float64) float64         { return math.Sinh(x) }
func Cosh(x float64) float64         { return math.Cosh(x) }
func Tanh(x float64) float64         { return math.Tanh(x) }
func Floor(x float64) float64        { return math.Floor(x) }
func Ceil(x float64) float64         { return math.Ceil(x) }
func Round(x float64) float64        { return math.Round(x) }
func Trunc(x float64) float64        { return math.Trunc(x) }
func Fmod(x, y float64) float64      { return math.Mod(x, y) }
func Hypot(x, y float64) float64     { return math.Hypot(x, y) }
func Copysign(x, y float64) float64  { return math.Copysign(x, y) }
func Ldexp(x float64, e int) float64 { return math.Ldexp(x, e) }
func Isnan(x float64) bool           { return math.IsNaN(x) }
func Isinf(x float64) bool           { return math.IsInf(x, 0) }
