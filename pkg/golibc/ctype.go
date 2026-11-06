//go:build linux

package golibc

// ctype functions use the C locale and take an int, as in C.

func Isdigit(c int) bool  { return c >= '0' && c <= '9' }
func Isupper(c int) bool  { return c >= 'A' && c <= 'Z' }
func Islower(c int) bool  { return c >= 'a' && c <= 'z' }
func Isalpha(c int) bool  { return Isupper(c) || Islower(c) }
func Isalnum(c int) bool  { return Isalpha(c) || Isdigit(c) }
func Isspace(c int) bool  { return c == ' ' || (c >= '\t' && c <= '\r') }
func Isxdigit(c int) bool { return Isdigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }
func Isprint(c int) bool  { return c >= ' ' && c <= '~' }
func Isgraph(c int) bool  { return c > ' ' && c <= '~' }
func Ispunct(c int) bool  { return Isgraph(c) && !Isalnum(c) }
func Iscntrl(c int) bool  { return (c >= 0 && c < ' ') || c == 0x7f }

func Toupper(c int) int {
	if Islower(c) {
		return c - 'a' + 'A'
	}
	return c
}

func Tolower(c int) int {
	if Isupper(c) {
		return c - 'A' + 'a'
	}
	return c
}
