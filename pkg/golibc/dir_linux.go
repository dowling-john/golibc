package golibc

import (
	"syscall"
	"unsafe"
)

// Directory iteration (dirent.h), built on getdents64.

// Dirent types for Dirent.Type.
const (
	DT_UNKNOWN = 0
	DT_FIFO    = 1
	DT_CHR     = 2
	DT_DIR     = 4
	DT_BLK     = 6
	DT_REG     = 8
	DT_LNK     = 10
	DT_SOCK    = 12
)

// Dirent is one directory entry. Name is NUL-terminated; read it with GoStr(&e.Name[0]).
type Dirent struct {
	Ino  uint64
	Type uint8
	Name [256]byte
}

// DIR is an open directory stream.
type DIR struct {
	fd       int
	buf      []byte
	pos, end int
	ent      Dirent
}

// Opendir opens a directory for reading.
func Opendir(p *byte) *DIR {
	fd, e := rawOpen(p, O_RDONLY|O_DIRECTORY, 0)
	if e != 0 {
		SetErrno(e)
		return nil
	}
	return &DIR{fd: fd, buf: make([]byte, 8192)}
}

// Readdir returns the next entry, or nil at the end (or on error, with errno set).
// The result is only valid until the next call on the same DIR.
func Readdir(d *DIR) *Dirent {
	for {
		if d.pos >= d.end {
			r, _, e := syscall.Syscall(syscall.SYS_GETDENTS64, uintptr(d.fd),
				uintptr(unsafe.Pointer(&d.buf[0])), uintptr(len(d.buf)))
			if e == EINTR {
				continue
			}
			if e != 0 {
				SetErrno(e)
				return nil
			}
			if r == 0 {
				return nil
			}
			d.pos, d.end = 0, int(r)
		}
		rec := d.buf[d.pos:]
		reclen := int(rec[16]) | int(rec[17])<<8
		d.pos += reclen
		d.ent = Dirent{Ino: *(*uint64)(unsafe.Pointer(&rec[0])), Type: rec[18]}
		for i := 0; i < len(d.ent.Name)-1 && 19+i < reclen && rec[19+i] != 0; i++ {
			d.ent.Name[i] = rec[19+i]
		}
		return &d.ent
	}
}

// Rewinddir restarts iteration from the first entry.
func Rewinddir(d *DIR) {
	Lseek(d.fd, 0, SEEK_SET)
	d.pos, d.end = 0, 0
}

// Closedir closes the directory stream.
func Closedir(d *DIR) int { return Close(d.fd) }
