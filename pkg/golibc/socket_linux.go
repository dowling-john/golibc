package golibc

import (
	"encoding/binary"
	"math/bits"
	"net/netip"
	"syscall"
	"unsafe"
)

// Sockets (sys/socket.h, netinet/in.h, sys/un.h, linux/netlink.h).
//
// Calls return -1 (or nil) and set errno on failure, like the rest of the
// syscall layer. Where C passes a struct sockaddr plus a length, these take
// and return a Sockaddr value: *SockaddrIn, *SockaddrIn6, *SockaddrUn or
// *SockaddrNl. Blocking calls (Accept, Recv, Connect, ...) block the calling
// goroutine's OS thread, not the whole program.

const (
	AF_UNSPEC  = syscall.AF_UNSPEC
	AF_UNIX    = syscall.AF_UNIX
	AF_INET    = syscall.AF_INET
	AF_INET6   = syscall.AF_INET6
	AF_NETLINK = syscall.AF_NETLINK

	SOCK_STREAM    = syscall.SOCK_STREAM
	SOCK_DGRAM     = syscall.SOCK_DGRAM
	SOCK_RAW       = syscall.SOCK_RAW
	SOCK_SEQPACKET = syscall.SOCK_SEQPACKET
	SOCK_NONBLOCK  = syscall.SOCK_NONBLOCK
	SOCK_CLOEXEC   = syscall.SOCK_CLOEXEC

	SOL_SOCKET   = syscall.SOL_SOCKET
	SO_REUSEADDR = syscall.SO_REUSEADDR
	SO_REUSEPORT = 15
	SO_KEEPALIVE = syscall.SO_KEEPALIVE
	SO_BROADCAST = syscall.SO_BROADCAST
	SO_RCVBUF    = syscall.SO_RCVBUF
	SO_SNDBUF    = syscall.SO_SNDBUF
	SO_ERROR     = syscall.SO_ERROR
	SO_TYPE      = syscall.SO_TYPE
	SO_PASSCRED  = syscall.SO_PASSCRED
	SO_PEERCRED  = syscall.SO_PEERCRED
	SO_RCVTIMEO  = syscall.SO_RCVTIMEO
	SO_SNDTIMEO  = syscall.SO_SNDTIMEO

	IPPROTO_IP   = syscall.IPPROTO_IP
	IPPROTO_IPV6 = syscall.IPPROTO_IPV6
	IPPROTO_TCP  = syscall.IPPROTO_TCP
	IPPROTO_UDP  = syscall.IPPROTO_UDP
	TCP_NODELAY  = syscall.TCP_NODELAY
	IPV6_V6ONLY  = syscall.IPV6_V6ONLY

	MSG_OOB      = syscall.MSG_OOB
	MSG_PEEK     = syscall.MSG_PEEK
	MSG_DONTWAIT = syscall.MSG_DONTWAIT
	MSG_WAITALL  = syscall.MSG_WAITALL
	MSG_NOSIGNAL = syscall.MSG_NOSIGNAL
	MSG_TRUNC    = syscall.MSG_TRUNC
	MSG_CTRUNC   = syscall.MSG_CTRUNC

	SHUT_RD   = 0
	SHUT_WR   = 1
	SHUT_RDWR = 2

	SOMAXCONN = syscall.SOMAXCONN

	NETLINK_ROUTE          = syscall.NETLINK_ROUTE
	NETLINK_KOBJECT_UEVENT = syscall.NETLINK_KOBJECT_UEVENT
	NETLINK_GENERIC        = syscall.NETLINK_GENERIC

	SCM_RIGHTS      = syscall.SCM_RIGHTS
	SCM_CREDENTIALS = syscall.SCM_CREDENTIALS
)

// Ucred is the peer credential triple used with SO_PEERCRED and SCM_CREDENTIALS.
type Ucred = syscall.Ucred

// Sockaddr is a socket address. Its implementations are *SockaddrIn,
// *SockaddrIn6, *SockaddrUn and *SockaddrNl.
type Sockaddr interface {
	marshal() []byte // the kernel's wire form, exactly as long as the address
}

// SockaddrIn is an IPv4 address and port (port in host byte order).
type SockaddrIn struct {
	Port int
	Addr [4]byte
}

// SockaddrIn6 is an IPv6 address and port (port in host byte order).
type SockaddrIn6 struct {
	Port     int
	Addr     [16]byte
	Flowinfo uint32
	ScopeID  uint32
}

// SockaddrUn is a Unix-domain address. A Path starting with a NUL byte is an
// abstract socket name.
type SockaddrUn struct{ Path string }

// SockaddrNl is a netlink address.
type SockaddrNl struct{ Pid, Groups uint32 }

func family(b []byte, f uint16) { binary.NativeEndian.PutUint16(b, f) }

func (a *SockaddrIn) marshal() []byte {
	b := make([]byte, 16)
	family(b, AF_INET)
	binary.BigEndian.PutUint16(b[2:], uint16(a.Port))
	copy(b[4:], a.Addr[:])
	return b
}

func (a *SockaddrIn6) marshal() []byte {
	b := make([]byte, 28)
	family(b, AF_INET6)
	binary.BigEndian.PutUint16(b[2:], uint16(a.Port))
	binary.BigEndian.PutUint32(b[4:], a.Flowinfo)
	copy(b[8:], a.Addr[:])
	binary.NativeEndian.PutUint32(b[24:], a.ScopeID)
	return b
}

func (a *SockaddrUn) marshal() []byte {
	n := len(a.Path)
	switch {
	case n == 0: // unnamed
		b := make([]byte, 2)
		family(b, AF_UNIX)
		return b
	case a.Path[0] == 0: // abstract: the name is exactly n bytes, no terminator
		if n > 108 {
			return nil
		}
		b := make([]byte, 2+n)
		family(b, AF_UNIX)
		copy(b[2:], a.Path)
		return b
	default: // pathname: NUL-terminated
		if n > 107 {
			return nil
		}
		b := make([]byte, 2+n+1)
		family(b, AF_UNIX)
		copy(b[2:], a.Path)
		return b
	}
}

func (a *SockaddrNl) marshal() []byte {
	b := make([]byte, 12)
	family(b, AF_NETLINK)
	binary.NativeEndian.PutUint32(b[4:], a.Pid)
	binary.NativeEndian.PutUint32(b[8:], a.Groups)
	return b
}

// sockaddrFrom decodes an address the kernel wrote into b (n significant bytes).
func sockaddrFrom(b []byte, n int) Sockaddr {
	n = min(n, len(b))
	if n < 2 {
		return nil
	}
	switch binary.NativeEndian.Uint16(b) {
	case AF_INET:
		if n < 8 {
			return nil
		}
		a := &SockaddrIn{Port: int(binary.BigEndian.Uint16(b[2:]))}
		copy(a.Addr[:], b[4:8])
		return a
	case AF_INET6:
		if n < 28 {
			return nil
		}
		a := &SockaddrIn6{Port: int(binary.BigEndian.Uint16(b[2:])), Flowinfo: binary.BigEndian.Uint32(b[4:]),
			ScopeID: binary.NativeEndian.Uint32(b[24:])}
		copy(a.Addr[:], b[8:24])
		return a
	case AF_UNIX:
		path := b[2:n]
		if len(path) > 0 && path[0] != 0 { // pathname: stop at the terminator
			if i := indexByte(path, 0); i >= 0 {
				path = path[:i]
			}
		}
		return &SockaddrUn{Path: string(path)}
	case AF_NETLINK:
		if n < 12 {
			return nil
		}
		return &SockaddrNl{Pid: binary.NativeEndian.Uint32(b[4:]), Groups: binary.NativeEndian.Uint32(b[8:])}
	}
	return nil
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// addrBytes marshals sa, reporting EINVAL for an address that cannot be encoded.
func addrBytes(sa Sockaddr) ([]byte, bool) {
	if sa == nil {
		SetErrno(EINVAL)
		return nil, false
	}
	b := sa.marshal()
	if b == nil {
		SetErrno(EINVAL)
		return nil, false
	}
	return b, true
}

func Socket(domain, typ, protocol int) int {
	r, _, e := syscall.Syscall(syscall.SYS_SOCKET, uintptr(domain), uintptr(typ), uintptr(protocol))
	return ret(int(r), e)
}

func Socketpair(domain, typ, protocol int, fds *[2]int) int {
	var p [2]int32
	r, _, e := syscall.Syscall6(syscall.SYS_SOCKETPAIR, uintptr(domain), uintptr(typ), uintptr(protocol),
		uintptr(unsafe.Pointer(&p)), 0, 0)
	if e != 0 {
		return ret(int(r), e)
	}
	fds[0], fds[1] = int(p[0]), int(p[1])
	return 0
}

func Bind(fd int, sa Sockaddr) int {
	b, ok := addrBytes(sa)
	if !ok {
		return -1
	}
	r, _, e := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return ret(int(r), e)
}

func Connect(fd int, sa Sockaddr) int {
	b, ok := addrBytes(sa)
	if !ok {
		return -1
	}
	r, _, e := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return ret(int(r), e)
}

func Listen(fd, backlog int) int {
	r, _, e := syscall.Syscall(syscall.SYS_LISTEN, uintptr(fd), uintptr(backlog), 0)
	return ret(int(r), e)
}

// Accept4 accepts a connection. flags may include SOCK_NONBLOCK and SOCK_CLOEXEC.
// It returns the new descriptor and the peer's address, or -1 and nil.
func Accept4(fd, flags int) (int, Sockaddr) {
	var buf [128]byte
	n := uint32(len(buf))
	r, _, e := syscall.Syscall6(syscall.SYS_ACCEPT4, uintptr(fd), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)), uintptr(flags), 0, 0)
	if e != 0 {
		SetErrno(e)
		return -1, nil
	}
	return int(r), sockaddrFrom(buf[:], int(n))
}

// Accept is Accept4 with no flags. (arm64 has only accept4, so both use it.)
func Accept(fd int) (int, Sockaddr) { return Accept4(fd, 0) }

// Getsockname returns the local address of fd, or nil on failure.
func Getsockname(fd int) Sockaddr {
	var buf [128]byte
	n := uint32(len(buf))
	_, _, e := syscall.Syscall(syscall.SYS_GETSOCKNAME, uintptr(fd), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if e != 0 {
		SetErrno(e)
		return nil
	}
	return sockaddrFrom(buf[:], int(n))
}

// Getpeername returns the remote address of fd, or nil on failure.
func Getpeername(fd int) Sockaddr {
	var buf [128]byte
	n := uint32(len(buf))
	_, _, e := syscall.Syscall(syscall.SYS_GETPEERNAME, uintptr(fd), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if e != 0 {
		SetErrno(e)
		return nil
	}
	return sockaddrFrom(buf[:], int(n))
}

// Sendto sends n bytes from p. A nil sa means a connected socket.
func Sendto(fd int, p unsafe.Pointer, n, flags int, sa Sockaddr) int {
	if sa == nil {
		r, _, e := syscall.Syscall6(syscall.SYS_SENDTO, uintptr(fd), uintptr(p), uintptr(n), uintptr(flags), 0, 0)
		return ret(int(r), e)
	}
	b, ok := addrBytes(sa)
	if !ok {
		return -1
	}
	r, _, e := syscall.Syscall6(syscall.SYS_SENDTO, uintptr(fd), uintptr(p), uintptr(n), uintptr(flags),
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return ret(int(r), e)
}

func Send(fd int, p unsafe.Pointer, n, flags int) int { return Sendto(fd, p, n, flags, nil) }

// Recvfrom receives up to n bytes into p and returns the count and the sender's address
// (nil for connected stream sockets). It returns -1 on failure.
func Recvfrom(fd int, p unsafe.Pointer, n, flags int) (int, Sockaddr) {
	var buf [128]byte
	l := uint32(len(buf))
	r, _, e := syscall.Syscall6(syscall.SYS_RECVFROM, uintptr(fd), uintptr(p), uintptr(n), uintptr(flags),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&l)))
	if e != 0 {
		SetErrno(e)
		return -1, nil
	}
	return int(r), sockaddrFrom(buf[:], int(l))
}

func Recv(fd int, p unsafe.Pointer, n, flags int) int {
	r, _, e := syscall.Syscall6(syscall.SYS_RECVFROM, uintptr(fd), uintptr(p), uintptr(n), uintptr(flags), 0, 0)
	return ret(int(r), e)
}

// Sendmsg sends n bytes from p plus ancillary data from oob (see MarshalCmsg, UnixRights).
// to may be nil. It returns the number of data bytes sent, or -1.
func Sendmsg(fd int, p unsafe.Pointer, n int, oob []byte, to Sockaddr, flags int) int {
	iov := syscall.Iovec{Base: (*byte)(p), Len: uint64(n)}
	msg := syscall.Msghdr{Iov: &iov, Iovlen: 1}
	var name []byte
	if to != nil {
		var ok bool
		if name, ok = addrBytes(to); !ok {
			return -1
		}
		msg.Name, msg.Namelen = &name[0], uint32(len(name))
	}
	if len(oob) > 0 {
		msg.Control, msg.Controllen = &oob[0], uint64(len(oob))
	}
	r, _, e := syscall.Syscall(syscall.SYS_SENDMSG, uintptr(fd), uintptr(unsafe.Pointer(&msg)), uintptr(flags))
	return ret(int(r), e)
}

// Recvmsg receives up to n bytes into p and ancillary data into oob (decode it
// with ParseCmsgs). It returns the data length, the ancillary length, the
// received flags (MSG_TRUNC, MSG_CTRUNC, ...) and the sender's address. The
// length is -1 on failure.
func Recvmsg(fd int, p unsafe.Pointer, n int, oob []byte, flags int) (nr, oobn, rflags int, from Sockaddr) {
	var name [128]byte
	iov := syscall.Iovec{Base: (*byte)(p), Len: uint64(n)}
	msg := syscall.Msghdr{Name: &name[0], Namelen: uint32(len(name)), Iov: &iov, Iovlen: 1}
	if len(oob) > 0 {
		msg.Control, msg.Controllen = &oob[0], uint64(len(oob))
	}
	r, _, e := syscall.Syscall(syscall.SYS_RECVMSG, uintptr(fd), uintptr(unsafe.Pointer(&msg)), uintptr(flags))
	if e != 0 {
		SetErrno(e)
		return -1, 0, 0, nil
	}
	return int(r), int(msg.Controllen), int(msg.Flags), sockaddrFrom(name[:], int(msg.Namelen))
}

func Shutdown(fd, how int) int {
	r, _, e := syscall.Syscall(syscall.SYS_SHUTDOWN, uintptr(fd), uintptr(how), 0)
	return ret(int(r), e)
}

// Setsockopt sets a socket option from the vlen bytes at val.
func Setsockopt(fd, level, opt int, val unsafe.Pointer, vlen int) int {
	r, _, e := syscall.Syscall6(syscall.SYS_SETSOCKOPT, uintptr(fd), uintptr(level), uintptr(opt), uintptr(val), uintptr(vlen), 0)
	return ret(int(r), e)
}

// Getsockopt reads a socket option into the *vlen bytes at val and stores the
// actual length in *vlen.
func Getsockopt(fd, level, opt int, val unsafe.Pointer, vlen *int) int {
	n := uint32(*vlen)
	r, _, e := syscall.Syscall6(syscall.SYS_GETSOCKOPT, uintptr(fd), uintptr(level), uintptr(opt), uintptr(val),
		uintptr(unsafe.Pointer(&n)), 0)
	*vlen = int(n)
	return ret(int(r), e)
}

// SetsockoptInt sets an integer-valued option (SO_REUSEADDR, SO_RCVBUF, TCP_NODELAY, ...).
func SetsockoptInt(fd, level, opt, v int) int {
	i := int32(v)
	return Setsockopt(fd, level, opt, unsafe.Pointer(&i), 4)
}

// GetsockoptInt reads an integer-valued option into *out.
func GetsockoptInt(fd, level, opt int, out *int) int {
	var i int32
	n := 4
	if Getsockopt(fd, level, opt, unsafe.Pointer(&i), &n) < 0 {
		return -1
	}
	*out = int(i)
	return 0
}

// Byte-order conversion (arpa/inet.h). Both supported architectures are little-endian.
func Htons(v uint16) uint16 { return bits.ReverseBytes16(v) }
func Ntohs(v uint16) uint16 { return bits.ReverseBytes16(v) }
func Htonl(v uint32) uint32 { return bits.ReverseBytes32(v) }
func Ntohl(v uint32) uint32 { return bits.ReverseBytes32(v) }

// InetPton converts the text address src to binary form at dst (4 bytes for
// AF_INET, 16 for AF_INET6). It returns 1 on success, 0 if src is not a valid
// address of that family, and -1 for an unsupported family.
func InetPton(af int, src *byte, dst unsafe.Pointer) int {
	a, err := netip.ParseAddr(GoStr(src))
	if err != nil || a.Zone() != "" {
		if af != AF_INET && af != AF_INET6 {
			SetErrno(EAFNOSUPPORT)
			return -1
		}
		return 0
	}
	switch af {
	case AF_INET:
		if !a.Is4() {
			return 0
		}
		b := a.As4()
		copy(mem(dst, 4), b[:])
	case AF_INET6:
		if !a.Is6() {
			return 0
		}
		b := a.As16()
		copy(mem(dst, 16), b[:])
	default:
		SetErrno(EAFNOSUPPORT)
		return -1
	}
	return 1
}

// InetNtop converts the binary address at src to text in dst (size bytes). It
// returns dst, or nil with errno ENOSPC if the result does not fit.
func InetNtop(af int, src unsafe.Pointer, dst *byte, size int) *byte {
	var s string
	switch af {
	case AF_INET:
		s = netip.AddrFrom4([4]byte(mem(src, 4))).String()
	case AF_INET6:
		s = netip.AddrFrom16([16]byte(mem(src, 16))).String()
	default:
		SetErrno(EAFNOSUPPORT)
		return nil
	}
	if len(s)+1 > size {
		SetErrno(ENOSPC)
		return nil
	}
	copy(unsafe.Slice(dst, size), s)
	put(dst, len(s), 0)
	return dst
}

// Ancillary ("control") messages, the cmsghdr/CMSG_* part of sendmsg and recvmsg.

// Cmsg is one decoded ancillary message.
type Cmsg struct {
	Level, Type int
	Data        []byte
}

const cmsgHdrLen = 16

func CmsgAlign(n int) int { return (n + 7) &^ 7 }
func CmsgLen(n int) int   { return cmsgHdrLen + n }
func CmsgSpace(n int) int { return cmsgHdrLen + CmsgAlign(n) }

// MarshalCmsg encodes one ancillary message, ready to pass as the oob argument of Sendmsg.
func MarshalCmsg(level, typ int, data []byte) []byte {
	b := make([]byte, CmsgSpace(len(data)))
	binary.NativeEndian.PutUint64(b, uint64(CmsgLen(len(data))))
	binary.NativeEndian.PutUint32(b[8:], uint32(level))
	binary.NativeEndian.PutUint32(b[12:], uint32(typ))
	copy(b[cmsgHdrLen:], data)
	return b
}

// ParseCmsgs decodes the ancillary data returned by Recvmsg.
func ParseCmsgs(oob []byte) []Cmsg {
	var out []Cmsg
	for len(oob) >= cmsgHdrLen {
		l := int(binary.NativeEndian.Uint64(oob))
		if l < cmsgHdrLen || l > len(oob) {
			break
		}
		out = append(out, Cmsg{
			Level: int(int32(binary.NativeEndian.Uint32(oob[8:]))),
			Type:  int(int32(binary.NativeEndian.Uint32(oob[12:]))),
			Data:  oob[cmsgHdrLen:l],
		})
		step := CmsgAlign(l)
		if step >= len(oob) {
			break
		}
		oob = oob[step:]
	}
	return out
}

// UnixRights builds an SCM_RIGHTS message that passes file descriptors.
func UnixRights(fds ...int) []byte {
	data := make([]byte, 4*len(fds))
	for i, fd := range fds {
		binary.NativeEndian.PutUint32(data[4*i:], uint32(fd))
	}
	return MarshalCmsg(SOL_SOCKET, SCM_RIGHTS, data)
}

// ParseUnixRights extracts the descriptors from SCM_RIGHTS messages in oob.
func ParseUnixRights(oob []byte) []int {
	var fds []int
	for _, m := range ParseCmsgs(oob) {
		if m.Level == SOL_SOCKET && m.Type == SCM_RIGHTS {
			for i := 0; i+4 <= len(m.Data); i += 4 {
				fds = append(fds, int(int32(binary.NativeEndian.Uint32(m.Data[i:]))))
			}
		}
	}
	return fds
}

// UnixCredentials builds an SCM_CREDENTIALS message. Send it on a socket whose peer has
// SO_PASSCRED set; unprivileged senders may only claim their own credentials.
func UnixCredentials(c *Ucred) []byte {
	data := make([]byte, 12)
	binary.NativeEndian.PutUint32(data, uint32(c.Pid))
	binary.NativeEndian.PutUint32(data[4:], c.Uid)
	binary.NativeEndian.PutUint32(data[8:], c.Gid)
	return MarshalCmsg(SOL_SOCKET, SCM_CREDENTIALS, data)
}

// ParseUnixCredentials extracts the first SCM_CREDENTIALS message from oob, or nil.
func ParseUnixCredentials(oob []byte) *Ucred {
	for _, m := range ParseCmsgs(oob) {
		if m.Level == SOL_SOCKET && m.Type == SCM_CREDENTIALS && len(m.Data) >= 12 {
			return &Ucred{
				Pid: int32(binary.NativeEndian.Uint32(m.Data)),
				Uid: binary.NativeEndian.Uint32(m.Data[4:]),
				Gid: binary.NativeEndian.Uint32(m.Data[8:]),
			}
		}
	}
	return nil
}
