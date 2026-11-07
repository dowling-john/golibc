package golibc

import (
	"bytes"
	"math/rand/v2"
	"strconv"
	"unsafe"
)

// Fpos_t records a stream position for Fgetpos and Fsetpos.
type Fpos_t struct{ off int64 }

// Fgetpos stores the current position of f in pos. It returns 0, or -1 if f
// cannot report a position (a pipe, for example).
func Fgetpos(f *File, pos *Fpos_t) int {
	off := Ftell(f)
	if off < 0 {
		return -1
	}
	pos.off = off
	return 0
}

// Fsetpos moves f to a position saved by Fgetpos.
func Fsetpos(f *File, pos *Fpos_t) int { return Fseek(f, pos.off, SEEK_SET) }

// Setbuf turns buffering off when buf is nil and otherwise selects full
// buffering. As with Setvbuf, the caller's buffer is not used.
func Setbuf(f *File, buf *byte) {
	if buf == nil {
		Setvbuf(f, IONBF, 0)
		return
	}
	Setvbuf(f, IOFBF, BUFSIZ)
}

// Setlinebuf makes f line buffered.
func Setlinebuf(f *File) { Setvbuf(f, IOLBF, 0) }

// Getdelim reads from f up to and including the byte delim, into a buffer that
// it grows as needed. *lineptr must be nil or come from Malloc, and *n is its
// size; both are updated, and the caller releases the buffer with Free. The
// line is NUL-terminated. It returns the number of bytes read, not counting the
// NUL, or -1 at end of file with nothing read (or on error).
func Getdelim(lineptr **byte, n *int, delim int, f *File) int {
	if lineptr == nil || n == nil || f == nil {
		SetErrno(EINVAL)
		return -1
	}
	if *lineptr == nil || *n == 0 {
		p := Malloc(120)
		if p == nil {
			return -1
		}
		*lineptr, *n = (*byte)(p), 120
	}
	// ensure makes room for need bytes plus the terminating NUL.
	ensure := func(need int) bool {
		if need < *n {
			return true
		}
		size := max(2**n, need+1)
		p := Realloc(unsafe.Pointer(*lineptr), size)
		if p == nil {
			return false
		}
		*lineptr, *n = (*byte)(p), size
		return true
	}

	d := byte(delim)
	length := 0
	for {
		if f.hasUnget {
			f.hasUnget = false
			if !ensure(length + 1) {
				f.hasUnget = true
				return -1
			}
			put(*lineptr, length, f.ungetc)
			length++
			if f.ungetc == d {
				break
			}
			continue
		}
		if f.rpos >= f.rend && !fill(f) {
			break
		}
		chunk := f.rbuf[f.rpos:f.rend]
		take, found := len(chunk), false
		if i := bytes.IndexByte(chunk, d); i >= 0 {
			take, found = i+1, true
		}
		if !ensure(length + take) {
			return -1
		}
		copy(mem(unsafe.Add(unsafe.Pointer(*lineptr), length), take), chunk[:take])
		f.rpos += take
		length += take
		if found {
			break
		}
	}
	if length == 0 {
		return -1
	}
	put(*lineptr, length, 0)
	return length
}

// Getline reads one line, including its newline, as Getdelim does.
func Getline(lineptr **byte, n *int, f *File) int { return Getdelim(lineptr, n, '\n', f) }

// Tmpfile creates an unnamed temporary file open for reading and writing. It is
// deleted when closed or when the program exits.
func Tmpfile() *File {
	for range 100 {
		path := CStr("/tmp/tmpf" + strconv.FormatUint(rand.Uint64(), 36))
		fd := Open(path, O_RDWR|O_CREAT|O_EXCL|O_CLOEXEC, 0o600)
		if fd >= 0 {
			Unlink(path)
			return register(&File{fd: fd, canRead: true, canWrite: true})
		}
		if GetErrno() != EEXIST {
			return nil
		}
	}
	SetErrno(EEXIST)
	return nil
}
