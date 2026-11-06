//go:build linux

package golibc

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func send(t *testing.T, fd int, s string) {
	t.Helper()
	b := []byte(s)
	if n := Send(fd, ptr(b), len(b), 0); n != len(b) {
		t.Fatalf("Send = %d (errno %v)", n, GetErrno())
	}
}

func recv(t *testing.T, fd int, max int) string {
	t.Helper()
	b := make([]byte, max)
	n := Recv(fd, ptr(b), max, 0)
	if n < 0 {
		t.Fatalf("Recv: errno %v", GetErrno())
	}
	return string(b[:n])
}

func TestSockaddrRoundTrip(t *testing.T) {
	cases := []Sockaddr{
		&SockaddrIn{Port: 8080, Addr: [4]byte{10, 1, 2, 3}},
		&SockaddrIn6{Port: 443, Addr: [16]byte{0xfe, 0x80, 15: 1}, Flowinfo: 7, ScopeID: 3},
		&SockaddrUn{Path: "/tmp/sock"},
		&SockaddrUn{Path: "\x00abstract-name"},
		&SockaddrUn{Path: ""},
		&SockaddrNl{Pid: 42, Groups: 0x5},
	}
	for _, sa := range cases {
		b := sa.marshal()
		got := sockaddrFrom(append(b, make([]byte, 20)...), len(b)) // kernel buffers are larger than the address
		if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", sa) {
			t.Errorf("round trip: got %#v, want %#v", got, sa)
		}
	}
	if (&SockaddrUn{Path: string(make([]byte, 200))}).marshal() != nil || (&SockaddrUn{Path: "/" + string(make([]byte, 107))}).marshal() != nil {
		t.Error("over-long Unix paths must be rejected")
	}
	if b := (&SockaddrIn{Port: 0x1234}).marshal(); b[2] != 0x12 || b[3] != 0x34 {
		t.Error("port must be big-endian on the wire")
	}
}

func TestTCPLoopbackEcho(t *testing.T) {
	srv := Socket(AF_INET, SOCK_STREAM|SOCK_CLOEXEC, 0)
	if srv < 0 {
		t.Fatalf("Socket: %v", GetErrno())
	}
	defer Close(srv)
	if SetsockoptInt(srv, SOL_SOCKET, SO_REUSEADDR, 1) != 0 {
		t.Fatal("SO_REUSEADDR")
	}
	if Bind(srv, &SockaddrIn{Addr: [4]byte{127, 0, 0, 1}}) != 0 || Listen(srv, 4) != 0 {
		t.Fatalf("Bind/Listen: %v", GetErrno())
	}
	addr := Getsockname(srv).(*SockaddrIn)
	if addr.Port == 0 || addr.Addr != [4]byte{127, 0, 0, 1} {
		t.Fatalf("Getsockname = %+v", addr)
	}

	got := make(chan string, 1)
	go func() {
		c, peer := Accept(srv)
		defer Close(c)
		if p, ok := peer.(*SockaddrIn); !ok || p.Addr != [4]byte{127, 0, 0, 1} || p.Port == 0 {
			got <- fmt.Sprintf("bad peer %#v", peer)
			return
		}
		buf := make([]byte, 64)
		n := Recv(c, ptr(buf), len(buf), 0)
		Send(c, ptr(buf), n, 0) // echo
		got <- string(buf[:n])
	}()

	cli := Socket(AF_INET, SOCK_STREAM, 0)
	defer Close(cli)
	SetsockoptInt(cli, IPPROTO_TCP, TCP_NODELAY, 1)
	if Connect(cli, addr) != 0 {
		t.Fatalf("Connect: %v", GetErrno())
	}
	if p := Getpeername(cli).(*SockaddrIn); p.Port != addr.Port {
		t.Errorf("Getpeername port %d, want %d", p.Port, addr.Port)
	}
	send(t, cli, "hello sockets")
	if echoed := recv(t, cli, 64); echoed != "hello sockets" {
		t.Errorf("echo = %q", echoed)
	}
	if s := <-got; s != "hello sockets" {
		t.Errorf("server saw %q", s)
	}
	if Shutdown(cli, SHUT_WR) != 0 {
		t.Error("Shutdown")
	}
	if recv(t, cli, 8) != "" { // server closed: orderly EOF
		t.Error("expected EOF after peer close")
	}
	var typ int
	if GetsockoptInt(cli, SOL_SOCKET, SO_TYPE, &typ) != 0 || typ != SOCK_STREAM {
		t.Errorf("SO_TYPE = %d", typ)
	}
	var soerr int
	if GetsockoptInt(cli, SOL_SOCKET, SO_ERROR, &soerr) != 0 || soerr != 0 {
		t.Errorf("SO_ERROR = %d", soerr)
	}
}

func TestUDPSendtoRecvfrom(t *testing.T) {
	a, b := Socket(AF_INET, SOCK_DGRAM, 0), Socket(AF_INET, SOCK_DGRAM, 0)
	defer Close(a)
	defer Close(b)
	lo := [4]byte{127, 0, 0, 1}
	Bind(a, &SockaddrIn{Addr: lo})
	Bind(b, &SockaddrIn{Addr: lo})
	msg := []byte("datagram")
	if Sendto(b, ptr(msg), len(msg), 0, Getsockname(a)) != len(msg) {
		t.Fatalf("Sendto: %v", GetErrno())
	}
	buf := make([]byte, 32)
	n, from := Recvfrom(a, ptr(buf), len(buf), 0)
	if string(buf[:n]) != "datagram" {
		t.Errorf("got %q", buf[:max(n, 0)])
	}
	if f, ok := from.(*SockaddrIn); !ok || f.Port != Getsockname(b).(*SockaddrIn).Port {
		t.Errorf("sender = %#v", from)
	}
	// MSG_PEEK leaves the datagram queued; MSG_DONTWAIT on an empty queue fails with EAGAIN.
	Sendto(b, ptr(msg), len(msg), 0, Getsockname(a))
	if Recv(a, ptr(buf), len(buf), MSG_PEEK) != len(msg) || Recv(a, ptr(buf), len(buf), 0) != len(msg) {
		t.Error("MSG_PEEK should not consume")
	}
	if Recv(a, ptr(buf), len(buf), MSG_DONTWAIT) != -1 || GetErrno() != EAGAIN {
		t.Errorf("empty queue: errno %v", GetErrno())
	}
}

func TestIPv6Loopback(t *testing.T) {
	s := Socket(AF_INET6, SOCK_STREAM, 0)
	defer Close(s)
	if Bind(s, &SockaddrIn6{Addr: [16]byte{15: 1}}) != 0 { // ::1
		t.Skipf("IPv6 loopback unavailable here: %v", GetErrno())
	}
	Listen(s, 1)
	c := Socket(AF_INET6, SOCK_STREAM, 0)
	defer Close(c)
	if Connect(c, Getsockname(s)) != 0 {
		t.Fatalf("Connect: %v", GetErrno())
	}
	if a, ok := Getpeername(c).(*SockaddrIn6); !ok || a.Addr != [16]byte{15: 1} {
		t.Errorf("peer = %#v", Getpeername(c))
	}
}

func TestUnixSockets(t *testing.T) {
	var p [2]int
	if Socketpair(AF_UNIX, SOCK_STREAM, 0, &p) != 0 {
		t.Fatal("Socketpair")
	}
	send(t, p[0], "ping")
	if recv(t, p[1], 8) != "ping" {
		t.Error("socketpair")
	}
	Close(p[0])
	Close(p[1])

	for _, path := range []string{filepath.Join(t.TempDir(), "s.sock"), "\x00golibc-test-" + fmt.Sprint(Getpid())} {
		srv := Socket(AF_UNIX, SOCK_STREAM, 0)
		if Bind(srv, &SockaddrUn{Path: path}) != 0 || Listen(srv, 1) != 0 {
			t.Fatalf("bind %q: %v", path, GetErrno())
		}
		if got := Getsockname(srv).(*SockaddrUn); got.Path != path {
			t.Errorf("Getsockname = %q, want %q", got.Path, path)
		}
		cli := Socket(AF_UNIX, SOCK_STREAM, 0)
		if Connect(cli, &SockaddrUn{Path: path}) != 0 {
			t.Fatalf("connect %q: %v", path, GetErrno())
		}
		c, _ := Accept(srv)
		send(t, cli, "unix")
		if recv(t, c, 8) != "unix" {
			t.Error("unix stream")
		}
		for _, fd := range []int{c, cli, srv} {
			Close(fd)
		}
	}
}

func TestPassFileDescriptor(t *testing.T) {
	var sp, pipe [2]int
	Socketpair(AF_UNIX, SOCK_STREAM, 0, &sp)
	Pipe(&pipe)
	defer func() { Close(sp[0]); Close(sp[1]); Close(pipe[0]); Close(pipe[1]) }()
	b := []byte{'x'} // stream sockets need at least one data byte to carry ancillary data
	if Sendmsg(sp[0], ptr(b), 1, UnixRights(pipe[0]), nil, 0) != 1 {
		t.Fatalf("Sendmsg: %v", GetErrno())
	}
	oob := make([]byte, CmsgSpace(4))
	buf := make([]byte, 4)
	n, oobn, _, _ := Recvmsg(sp[1], ptr(buf), len(buf), oob, 0)
	if n != 1 {
		t.Fatalf("Recvmsg = %d (%v)", n, GetErrno())
	}
	fds := ParseUnixRights(oob[:oobn])
	for _, fd := range fds {
		defer Close(fd)
	}
	if len(fds) != 1 || fds[0] == pipe[0] {
		t.Fatalf("received fds %v (sent %d): the kernel must give the receiver a new descriptor", fds, pipe[0])
	}
	msg := []byte("through the new fd")
	Write(pipe[1], ptr(msg), len(msg))
	rb := make([]byte, 32)
	if k := Read(fds[0], ptr(rb), len(rb)); string(rb[:k]) != "through the new fd" {
		t.Errorf("read via passed fd: %q", rb[:max(k, 0)])
	}
}

func TestPassCredentials(t *testing.T) {
	var sp [2]int
	Socketpair(AF_UNIX, SOCK_DGRAM, 0, &sp)
	defer func() { Close(sp[0]); Close(sp[1]) }()
	if SetsockoptInt(sp[1], SOL_SOCKET, SO_PASSCRED, 1) != 0 {
		t.Fatal("SO_PASSCRED")
	}
	b := []byte("c")
	cred := &Ucred{Pid: int32(Getpid()), Uid: uint32(Getuid()), Gid: uint32(Getgid())}
	if Sendmsg(sp[0], ptr(b), 1, UnixCredentials(cred), nil, 0) != 1 {
		t.Fatalf("Sendmsg: %v", GetErrno())
	}
	oob := make([]byte, CmsgSpace(12)+64)
	_, oobn, _, _ := Recvmsg(sp[1], ptr(b), 1, oob, 0)
	got := ParseUnixCredentials(oob[:oobn])
	if got == nil || *got != *cred {
		t.Errorf("credentials = %+v, want %+v", got, cred)
	}
	var peer Ucred
	l := int(unsafe.Sizeof(peer))
	if Getsockopt(sp[0], SOL_SOCKET, SO_PEERCRED, unsafe.Pointer(&peer), &l) != 0 || peer.Pid != cred.Pid {
		t.Errorf("SO_PEERCRED = %+v", peer)
	}
}

func TestCmsgEncoding(t *testing.T) {
	a := MarshalCmsg(1, 2, []byte{1, 2, 3})
	b := MarshalCmsg(3, 4, []byte{9, 9, 9, 9, 9, 9, 9, 9})
	if len(a) != CmsgSpace(3) || len(b) != CmsgSpace(8) {
		t.Fatalf("lengths %d %d", len(a), len(b))
	}
	msgs := ParseCmsgs(append(append([]byte{}, a...), b...))
	if len(msgs) != 2 || msgs[0].Level != 1 || msgs[0].Type != 2 || len(msgs[0].Data) != 3 || msgs[1].Level != 3 || len(msgs[1].Data) != 8 {
		t.Errorf("parsed %+v", msgs)
	}
	if len(ParseCmsgs([]byte{1, 2, 3})) != 0 {
		t.Error("truncated header must be ignored")
	}
	bad := MarshalCmsg(1, 1, []byte{1})
	binary.NativeEndian.PutUint64(bad, 9999) // claims more bytes than exist
	if len(ParseCmsgs(bad)) != 0 {
		t.Error("oversized length must be ignored")
	}
}

// Dump the interface list over netlink: exercises SockaddrNl, Bind, Getsockname and Send/Recv.
func TestNetlinkRoute(t *testing.T) {
	fd := Socket(AF_NETLINK, SOCK_RAW|SOCK_CLOEXEC, NETLINK_ROUTE)
	if fd < 0 {
		t.Skipf("netlink unavailable: %v", GetErrno())
	}
	defer Close(fd)
	if Bind(fd, &SockaddrNl{}) != 0 {
		t.Fatalf("Bind: %v", GetErrno())
	}
	if nl, ok := Getsockname(fd).(*SockaddrNl); !ok || nl.Pid == 0 {
		t.Errorf("kernel should assign a port id: %#v", Getsockname(fd))
	}
	// struct nlmsghdr (16 bytes) + struct rtgenmsg (1 byte, padded): RTM_GETLINK with NLM_F_REQUEST|NLM_F_DUMP.
	req := make([]byte, 20)
	binary.NativeEndian.PutUint32(req[0:], 20)
	binary.NativeEndian.PutUint16(req[4:], 18)        // RTM_GETLINK
	binary.NativeEndian.PutUint16(req[6:], 0x1|0x300) // NLM_F_REQUEST | NLM_F_DUMP
	binary.NativeEndian.PutUint32(req[8:], 1)
	if Sendto(fd, ptr(req), len(req), 0, &SockaddrNl{}) != len(req) {
		t.Fatalf("Sendto: %v", GetErrno())
	}
	buf := make([]byte, 65536)
	n, from := Recvfrom(fd, ptr(buf), len(buf), 0)
	if n < 16 {
		t.Fatalf("Recvfrom = %d (%v)", n, GetErrno())
	}
	if f, ok := from.(*SockaddrNl); !ok || f.Pid != 0 {
		t.Errorf("reply should come from the kernel (pid 0): %#v", from)
	}
	if typ := binary.NativeEndian.Uint16(buf[4:]); typ != 16 { // RTM_NEWLINK: at least the loopback interface
		t.Errorf("first reply type = %d, want RTM_NEWLINK (16)", typ)
	}
}

func TestSocketErrors(t *testing.T) {
	if Socket(9999, SOCK_STREAM, 0) != -1 || GetErrno() != EAFNOSUPPORT {
		t.Errorf("bad domain: errno %v", GetErrno())
	}
	if Bind(-1, &SockaddrIn{}) != -1 || GetErrno() != EBADF {
		t.Errorf("bad fd: errno %v", GetErrno())
	}
	if Bind(3, nil) != -1 || GetErrno() != EINVAL {
		t.Error("nil address")
	}
	long := Socket(AF_UNIX, SOCK_STREAM, 0)
	if Bind(long, &SockaddrUn{Path: string(make([]byte, 300))}) != -1 || GetErrno() != EINVAL {
		t.Error("over-long path")
	}
	Close(long)
	// A listener that is then closed leaves a port nobody serves: connect is refused.
	s := Socket(AF_INET, SOCK_STREAM, 0)
	Bind(s, &SockaddrIn{Addr: [4]byte{127, 0, 0, 1}})
	dead := Getsockname(s)
	Close(s)
	c := Socket(AF_INET, SOCK_STREAM, 0)
	defer Close(c)
	if Connect(c, dead) != -1 || GetErrno() != ECONNREFUSED {
		t.Errorf("connect to closed port: errno %v", GetErrno())
	}
	f, _ := os.Open("/dev/null")
	defer f.Close()
	if Listen(int(f.Fd()), 1) != -1 || GetErrno() != ENOTSOCK {
		t.Errorf("listen on a file: errno %v", GetErrno())
	}
}

func TestInet(t *testing.T) {
	if Htons(0x1234) != 0x3412 || Ntohs(Htons(80)) != 80 || Htonl(0x01020304) != 0x04030201 || Ntohl(Htonl(0xdeadbeef)) != 0xdeadbeef {
		t.Error("byte order")
	}
	var v4 [4]byte
	var v6 [16]byte
	if InetPton(AF_INET, CStr("192.168.1.20"), ptr(v4[:])) != 1 || v4 != [4]byte{192, 168, 1, 20} {
		t.Errorf("v4 = %v", v4)
	}
	if InetPton(AF_INET, CStr("256.1.1.1"), ptr(v4[:])) != 0 || InetPton(AF_INET, CStr("::1"), ptr(v4[:])) != 0 {
		t.Error("invalid v4 must return 0")
	}
	if InetPton(AF_INET6, CStr("2001:db8::ff00:42:8329"), ptr(v6[:])) != 1 || v6[0] != 0x20 || v6[15] != 0x29 {
		t.Errorf("v6 = %v", v6)
	}
	if InetPton(AF_INET6, CStr("fe80::1%eth0"), ptr(v6[:])) != 0 {
		t.Error("zones are not accepted by inet_pton")
	}
	if InetPton(99, CStr("1.2.3.4"), ptr(v4[:])) != -1 || GetErrno() != EAFNOSUPPORT {
		t.Error("bad family")
	}
	buf := make([]byte, 64)
	for _, c := range []struct {
		af   int
		in   string
		want string
	}{
		{AF_INET, "10.0.0.255", "10.0.0.255"},
		{AF_INET6, "2001:0db8:0000:0000:0000:ff00:0042:8329", "2001:db8::ff00:42:8329"},
		{AF_INET6, "1:0:3:4:5:6:7:8", "1:0:3:4:5:6:7:8"}, // a single zero group is not compressed
		{AF_INET6, "::", "::"},
		{AF_INET6, "::ffff:1.2.3.4", "::ffff:1.2.3.4"},
	} {
		var raw [16]byte
		InetPton(c.af, CStr(c.in), ptr(raw[:]))
		if got := InetNtop(c.af, ptr(raw[:]), &buf[0], len(buf)); GoStr(got) != c.want {
			t.Errorf("ntop(%q) = %q, want %q", c.in, GoStr(got), c.want)
		}
	}
	if InetNtop(AF_INET, ptr(v4[:]), &buf[0], 7) != nil || GetErrno() != ENOSPC {
		t.Error("short buffer must fail with ENOSPC")
	}
}
