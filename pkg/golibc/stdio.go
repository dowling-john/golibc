//go:build linux

package golibc

import (
	"bytes"
	"math"
	"sync"
	"unsafe"
)

const (
	EOF    = -1
	BUFSIZ = 8192

	SEEK_SET = 0
	SEEK_CUR = 1
	SEEK_END = 2

	IOFBF = 0 // fully buffered
	IOLBF = 1 // line buffered
	IONBF = 2 // unbuffered
)

// File is a buffered stream over a file descriptor. A File is not safe for
// concurrent use by multiple goroutines.
type File struct {
	fd                int
	canRead, canWrite bool
	mode              int
	modeSet           bool
	rbuf              []byte
	rpos, rend        int
	wbuf              []byte // len is the number of pending bytes
	ungetc            byte
	hasUnget          bool
	eof, err          bool
}

var (
	Stdin  = &File{fd: 0, canRead: true}
	Stdout = &File{fd: 1, canWrite: true}
	Stderr = &File{fd: 2, canWrite: true, mode: IONBF, modeSet: true}

	filesMu   sync.Mutex
	openFiles = []*File{Stdin, Stdout, Stderr}
)

func (f *File) bufMode() int {
	if !f.modeSet {
		f.modeSet = true
		f.mode = IOFBF
		if f.canWrite && Isatty(f.fd) {
			f.mode = IOLBF
		}
	}
	return f.mode
}

// writeAll writes all of p to fd, retrying on EINTR and short writes. It returns bytes written.
func writeAll(fd int, p []byte) int {
	done := 0
	for done < len(p) {
		n := Write(fd, unsafe.Pointer(&p[done]), len(p)-done)
		if n < 0 {
			if Errno == EINTR {
				continue
			}
			break
		}
		done += n
	}
	return done
}

func (f *File) flushW() int {
	if len(f.wbuf) == 0 {
		return 0
	}
	n := writeAll(f.fd, f.wbuf)
	ok := n == len(f.wbuf)
	f.wbuf = f.wbuf[:0]
	if !ok {
		f.err = true
		return EOF
	}
	return 0
}

// unread is how far the kernel offset is ahead of the logical position.
func (f *File) unread() int {
	n := f.rend - f.rpos
	if f.hasUnget {
		n++
	}
	return n
}

// dropRead discards buffered input, moving the descriptor back when seekable.
func (f *File) dropRead() {
	if n := f.unread(); n > 0 {
		Lseek(f.fd, int64(-n), SEEK_CUR)
	}
	f.rpos, f.rend, f.hasUnget = 0, 0, false
}

func writeBytes(f *File, data []byte) int {
	if !f.canWrite {
		f.err = true
		Errno = EBADF
		return 0
	}
	f.dropRead()
	mode := f.bufMode()
	if mode == IONBF {
		n := writeAll(f.fd, data)
		if n < len(data) {
			f.err = true
		}
		return n
	}
	if f.wbuf == nil {
		f.wbuf = make([]byte, 0, BUFSIZ)
	}
	total, rest := len(data), data
	for len(rest) > 0 {
		if len(f.wbuf) == 0 && len(rest) >= cap(f.wbuf) { // big write: skip the buffer
			n := writeAll(f.fd, rest)
			if n < len(rest) {
				f.err = true
				return total - len(rest) + n
			}
			break
		}
		n := min(cap(f.wbuf)-len(f.wbuf), len(rest))
		f.wbuf = append(f.wbuf, rest[:n]...)
		rest = rest[n:]
		if len(f.wbuf) == cap(f.wbuf) && f.flushW() != 0 {
			return total - len(rest)
		}
	}
	if mode == IOLBF && bytes.IndexByte(data, '\n') >= 0 && f.flushW() != 0 {
		return 0
	}
	return total
}

func fill(f *File) bool {
	if !f.canRead {
		f.err = true
		Errno = EBADF
		return false
	}
	f.flushW()
	if f == Stdin {
		Fflush(Stdout)
	}
	if f.rbuf == nil {
		size := BUFSIZ
		if f.bufMode() == IONBF {
			size = 1
		}
		f.rbuf = make([]byte, size)
	}
	for {
		n := Read(f.fd, unsafe.Pointer(&f.rbuf[0]), len(f.rbuf))
		switch {
		case n > 0:
			f.rpos, f.rend = 0, n
			return true
		case n == 0:
			f.eof = true
			return false
		case Errno == EINTR:
			continue
		default:
			f.err = true
			return false
		}
	}
}

func readBytes(f *File, dst []byte) int {
	n := 0
	if f.hasUnget && len(dst) > 0 {
		dst[0], f.hasUnget, n = f.ungetc, false, 1
	}
	for n < len(dst) {
		if f.rpos < f.rend {
			c := copy(dst[n:], f.rbuf[f.rpos:f.rend])
			f.rpos += c
			n += c
			continue
		}
		if !fill(f) {
			break
		}
	}
	return n
}

func parseMode(mode *byte) (flags int, read, write, ok bool) {
	plus := Strchr(mode, '+') != nil
	switch at(mode, 0) {
	case 'r':
		flags, read, write = O_RDONLY, true, plus
	case 'w':
		flags, read, write = O_WRONLY|O_CREAT|O_TRUNC, plus, true
	case 'a':
		flags, read, write = O_WRONLY|O_CREAT|O_APPEND, plus, true
	default:
		return 0, false, false, false
	}
	if plus {
		flags = flags&^(O_RDONLY|O_WRONLY) | O_RDWR
	}
	return flags, read, write, true
}

func register(f *File) *File {
	filesMu.Lock()
	openFiles = append(openFiles, f)
	filesMu.Unlock()
	return f
}

// Fopen opens path with a C mode string ("r", "w", "a", each optionally with "+" and "b").
func Fopen(path, mode *byte) *File {
	flags, r, w, ok := parseMode(mode)
	if !ok {
		Errno = EINVAL
		return nil
	}
	fd := Open(path, flags, 0o666)
	if fd < 0 {
		return nil
	}
	return register(&File{fd: fd, canRead: r, canWrite: w})
}

// Fdopen wraps an existing descriptor.
func Fdopen(fd int, mode *byte) *File {
	_, r, w, ok := parseMode(mode)
	if !ok {
		Errno = EINVAL
		return nil
	}
	return register(&File{fd: fd, canRead: r, canWrite: w})
}

func Fileno(f *File) int { return f.fd }

// Fclose flushes and closes f.
func Fclose(f *File) int {
	rc := f.flushW()
	if Close(f.fd) != 0 {
		rc = EOF
	}
	filesMu.Lock()
	for i, g := range openFiles {
		if g == f {
			openFiles = append(openFiles[:i], openFiles[i+1:]...)
			break
		}
	}
	filesMu.Unlock()
	return rc
}

// Fflush writes buffered output; Fflush(nil) flushes every open stream.
func Fflush(f *File) int {
	if f != nil {
		return f.flushW()
	}
	filesMu.Lock()
	files := append([]*File(nil), openFiles...)
	filesMu.Unlock()
	rc := 0
	for _, g := range files {
		if g.flushW() != 0 {
			rc = EOF
		}
	}
	return rc
}

// Setvbuf sets the buffering mode (IOFBF, IOLBF, IONBF). Unlike C it never
// takes a caller-supplied buffer; size > 0 resizes the internal one.
func Setvbuf(f *File, mode, size int) int {
	if mode < IOFBF || mode > IONBF {
		return -1
	}
	f.flushW()
	f.mode, f.modeSet = mode, true
	if size > 0 && mode != IONBF {
		f.wbuf, f.rbuf = make([]byte, 0, size), nil
	} else {
		f.wbuf, f.rbuf = nil, nil
	}
	return 0
}

func Fwrite(ptr unsafe.Pointer, size, nmemb int, f *File) int {
	if size <= 0 || nmemb <= 0 || size > math.MaxInt/nmemb {
		return 0
	}
	return writeBytes(f, mem(ptr, size*nmemb)) / size
}

func Fread(ptr unsafe.Pointer, size, nmemb int, f *File) int {
	if size <= 0 || nmemb <= 0 || size > math.MaxInt/nmemb {
		return 0
	}
	return readBytes(f, mem(ptr, size*nmemb)) / size
}

func Fputc(c int, f *File) int {
	b := [1]byte{byte(c)}
	if writeBytes(f, b[:]) != 1 {
		return EOF
	}
	return int(b[0])
}

func Putc(c int, f *File) int { return Fputc(c, f) }
func Putchar(c int) int       { return Fputc(c, Stdout) }

func Fputs(s *byte, f *File) int {
	n := Strlen(s)
	if writeBytes(f, mem(unsafe.Pointer(s), n)) != n {
		return EOF
	}
	return 0
}

// Puts writes s and a newline to stdout.
func Puts(s *byte) int {
	if Fputs(s, Stdout) == EOF || Fputc('\n', Stdout) == EOF {
		return EOF
	}
	return 0
}

func Fgetc(f *File) int {
	if f.hasUnget {
		f.hasUnget = false
		return int(f.ungetc)
	}
	if f.rpos >= f.rend && !fill(f) {
		return EOF
	}
	c := f.rbuf[f.rpos]
	f.rpos++
	return int(c)
}

func Getc(f *File) int { return Fgetc(f) }
func Getchar() int     { return Fgetc(Stdin) }

// Ungetc pushes one byte back onto f.
func Ungetc(c int, f *File) int {
	if c == EOF || f.hasUnget {
		return EOF
	}
	f.ungetc, f.hasUnget, f.eof = byte(c), true, false
	return int(byte(c))
}

// Fgets reads at most n-1 bytes, stopping after a newline, and NUL-terminates buf.
func Fgets(buf *byte, n int, f *File) *byte {
	if n <= 0 {
		return nil
	}
	i := 0
	for i < n-1 {
		c := Fgetc(f)
		if c == EOF {
			break
		}
		put(buf, i, byte(c))
		i++
		if c == '\n' {
			break
		}
	}
	if i == 0 {
		return nil
	}
	put(buf, i, 0)
	return buf
}

// Fseek repositions f. It returns 0 on success and -1 on failure.
func Fseek(f *File, off int64, whence int) int {
	f.flushW()
	if whence == SEEK_CUR {
		off -= int64(f.unread())
	}
	f.rpos, f.rend, f.hasUnget, f.eof = 0, 0, false, false
	if Lseek(f.fd, off, whence) < 0 {
		return -1
	}
	return 0
}

// Ftell returns the logical position of f, or -1.
func Ftell(f *File) int64 {
	pos := Lseek(f.fd, 0, SEEK_CUR)
	if pos < 0 {
		return -1
	}
	return pos - int64(f.unread()) + int64(len(f.wbuf))
}

func Rewind(f *File) { Fseek(f, 0, SEEK_SET); f.err = false }

func Feof(f *File) bool   { return f.eof }
func Ferror(f *File) bool { return f.err }
func Clearerr(f *File)    { f.eof, f.err = false, false }

// Remove deletes a file.
func Remove(path *byte) int { return Unlink(path) }

// Perror prints s, a colon, and the message for Errno to stderr.
func Perror(s *byte) {
	if s != nil && *s != 0 {
		Fputs(s, Stderr)
		Fputs(CStr(": "), Stderr)
	}
	Fputs(Strerror(int(Errno)), Stderr)
	Fputc('\n', Stderr)
}
