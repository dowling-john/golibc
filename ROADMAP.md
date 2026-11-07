# Roadmap

golibc exists so that nobody needs cgo to reach C library behaviour from Go. It is for the **gaps**: things Go's standard library cannot do, or cannot do with C's semantics. Where Go already has a safe way to do something, use that and golibc does not duplicate it.

## The rule for adding something

Add a function to golibc only if **both** hold:

1. Go's standard library (`syscall`, `os`, `net`, `time`, `math`, ...) has no safe equivalent, or its behaviour differs from C in a way that breaks real programs.
2. A program that would otherwise reach for cgo, or code transpiled from C, needs it.

Exception: a function that has to share state with what golibc already provides is added even if Go has an equivalent. For example, `Setenv` must change the environment that golibc's `Getenv` reads, and `Getline` must work on a golibc `File`.

Tick a box when the function is implemented **and** tested. Where glibc's behaviour is subtle (formatting, parsing, ordering), check the test against real glibc output with `make golden`, as the printf, scanf and time tests do.

## Already implemented

See the table in the [README](README.md#what-is-included). In short: `string.h`, `ctype.h`, most of `stdlib.h`, `stdio.h`, the printf and scanf families, core `math.h` and `time.h`, `unistd.h`/`fcntl.h`, `sys/stat.h`, `dirent.h`, basic `signal.h` and `sys/mman.h`, process spawning, sockets, poll, epoll and select. printf, scanf, strftime and time zones are checked against glibc.

## Required: gaps Go does not fill

### Signals
Go only delivers signals over channels. It has no signal masks, no `sigaction` flags, and no way to wait for a signal synchronously. Today golibc has only `Signal`, `SignalIgnore`, `Raise` and `Kill`.

- [ ] `Sigset` with `Sigemptyset` `Sigfillset` `Sigaddset` `Sigdelset` `Sigismember`
- [ ] `Sigaction` (handler, mask, flags such as `SA_RESTART`)
- [ ] `Sigprocmask` `Sigpending` `Sigsuspend` (needs a design that respects the Go runtime owning the signal machinery)
- [ ] `Sigaltstack`
- [ ] `Alarm` `Setitimer` `Getitimer` `Pause`

### Linux file descriptors and I/O the `syscall` package lacks
- [ ] `Readv` `Writev` `Preadv` `Pwritev`
- [ ] `Eventfd`
- [ ] `Timerfd_create` `Timerfd_settime` `Timerfd_gettime`
- [ ] `Signalfd`
- [ ] `Pidfd_open` `Pidfd_send_signal`
- [ ] `Sysconf` (and `Pathconf`)
- [ ] `Getsid`, `Fstatat` (the `syscall` package has it on arm64 only), `Readlinkat`, `Posix_fadvise`

### Terminals
Go can issue the ioctls but offers no termios API and no pty helpers.

- [ ] `Tcgetattr` `Tcsetattr` `Cfmakeraw` `Cfsetspeed` `Cfsetispeed` `Cfsetospeed` `Tcdrain` `Tcflush` `Tcflow` `Tcsendbreak`
- [ ] `Posix_openpt` `Grantpt` `Unlockpt` `Ptsname` `Openpty` `Forkpty` (the last via `Spawn`, since there is no `Fork`)
- [ ] `Ttyname`

### Time
- [x] `Strptime`, following glibc and checked against it on about 500 inputs. Go's `time.Parse` has a different model and cannot parse C format strings.
- [x] `Tzset`, the `Tzname`, `Timezone` and `Daylight` variables, and **POSIX `TZ` rule strings** such as `EST5EDT,M3.2.0,M11.1.0`, checked against glibc across 24 zones and rule strings
- [x] `Clock` `ClockGetres` `ClockNanosleep`
- [ ] `Timer_create` family (optional: it needs `sigevent` plumbing through the Go runtime's signal handling, and nothing in the standard library gets in the way, so it waits until something needs it)

### Patterns, options and formats
- [ ] **POSIX regular expressions**: `Regcomp` `Regexec` `Regfree` `Regerror`. Go's `regexp.CompilePOSIX` already gives ERE syntax with leftmost-longest matching, so the real gaps are basic regular expressions (BRE) and back-references, which RE2 cannot do.
- [ ] `Fnmatch` and `Glob` with exact POSIX/glibc semantics (flags such as `FNM_PATHNAME`, `FNM_PERIOD`)
- [ ] `Getopt` `Getopt_long` `Getopt_long_only` with the `optind`/`optarg`/`optopt` behaviour C programs depend on
- [x] `v*` printf and scanf variants (`VaList` is `[]any`): `Vprintf` `Vfprintf` `Vsnprintf` `Vsprintf` `Vasprintf` `Vdprintf` `Vsscanf` `Vfscanf` `Vscanf`. Go's `fmt` uses different verbs. Needs a Go representation of `va_list`, most likely `[]any`.

### stdio features Go has no equivalent for
- [ ] `Fmemopen` `Open_memstream`
- [ ] `Flockfile` `Ftrylockfile` `Funlockfile` and the `_unlocked` variants, tied to the `File` thread-safety item below
- [ ] `Popen` `Pclose`, returning a golibc `File`
- [x] `Getline` `Getdelim` and `Fgetpos` `Fsetpos`, `Setbuf` `Setlinebuf`, `Tmpfile` (these work on golibc's own `File`, so Go's `bufio` cannot stand in)

### Memory
- [ ] `Posix_memalign` `Aligned_alloc` `Memalign` for alignments above 16 bytes. Go's heap cannot give stable, GC-free aligned memory.
- [x] `Reallocarray`
- [ ] `Malloc_usable_size`
- [ ] `Mprotect` `Madvise` `Mlock` `Munlock` `Msync`, but only if the `syscall` wrappers prove insufficient (check first)

### Environment and process state
- [x] `Setenv` `Putenv` `Unsetenv` `Clearenv`, sharing state with `Getenv`
- [x] `QuickExit` `AtQuickExit`

### Numerics where C and Go differ
- [x] `Strtof` and a defined answer for `Strtold` (Go has no long double, so it is `Strtod`)
- [x] glibc-compatible `Rand` `Srand` `Random` `Srandom` and `Drand48` `Lrand48` `Mrand48` `Erand48` `Nrand48` `Jrand48` `Srand48`, checked against glibc. `Rand` now follows glibc's sequence, so `Srand(1)` gives 1804289383 first.
- [ ] `Seed48` `Lcong48` `Initstate` `Setstate`
- [ ] `Fmax` `Fmin` with C's NaN and signed-zero rules (only if `math.Max`/`math.Min` are shown to differ)

### Networking
`net` resolves names safely, so these are thin C-shaped wrappers over it and are lower priority.

- [ ] `Getaddrinfo` `Freeaddrinfo` `Gai_strerror`, returning golibc `Sockaddr` values
- [ ] `Getifaddrs` `If_nametoindex` `If_indextoname`
- [ ] `Getservbyname` `Getprotobyname`

## Cross-cutting work

**Correctness**
- [ ] **errno is process-wide.** C's is per thread and Go has no goroutine-local storage, so this cannot be fully fixed. Decide whether to offer an explicit alternative (every API also returning the error) and document the answer.
- [ ] **A `File` is not goroutine-safe.** Add a lock per stream, or document the rule next to each stdio function.

**Testing**
- [ ] glibc differential tests for `Strtod`, `Strtol` and the number parsers
- [ ] Fuzz tests for the printf and scanf parsers
- [ ] `go test -race` in CI

**Release**
- [ ] First version tag, so `go get` resolves a version
- [ ] CHANGELOG
- [ ] A static-analysis step in CI beyond `go vet`

## Implementation approach: Go's code or ours

For each required item, whether to build on the standard library or write it here. The `syscall` availability below was checked with `go doc` on linux/amd64 and linux/arm64 using the local toolchain, and Go 1.23 should match because `syscall` is frozen.

**Build on the standard library** (less code, and Go's version is already tuned):

| Item | Use | Note |
|---|---|---|
| `Setenv` `Unsetenv` `Clearenv` `Putenv` | `syscall.Setenv` and friends | `Getenv` already reads `syscall.Getenv`, so the shared state comes for free. |
| `Strtof` | `strconv.ParseFloat(s, 32)` | Reuse the prefix scanner `Strtod` already has, so only the bit size changes. `Strtold` maps to `float64`. |
| `Regcomp` (ERE) | `regexp.CompilePOSIX` | Add a small BRE-to-ERE translator. Back-references need our own backtracking matcher. |
| `Getaddrinfo` `Getservbyname` `Getifaddrs` | `net.Resolver`, `net.LookupPort`, `net.Interfaces` | Wrap and convert to golibc `Sockaddr`. Importing `net` adds binary size, and without cgo there are no `nsswitch` plug-ins. A netlink version using our `SockaddrNl` avoids the import at the cost of more code. |
| `Alarm` `Setitimer` (real-time timer) | `time.AfterFunc` plus `Raise` | No syscall needed, and `Getitimer` is computed from the deadline. `ITIMER_VIRTUAL` and `ITIMER_PROF` need the kernel, and Go's profiler uses `SIGPROF`, so leave them out. |
| `Sigaction` `Sigset` | `os/signal.Notify`, as `Signal` already does | The runtime owns the real handlers, so installing our own with raw `rt_sigaction` would break Go. Flags like `SA_RESTART` cannot be honoured. |
| `Sigprocmask` `Sigsuspend` `Sigpending` | Channel delivery in `os/signal` | A true per-thread mask is impossible because goroutines move between threads. Emulate a process-wide blocked set that queues signals for the handler goroutines, and document the difference. |
| `Termios` ioctls | `syscall.Termios`, `syscall.TCGETS`/`TCSETS` | Issue them through the existing `Ioctl`. `Cfmakeraw` and `Cfsetspeed` are bit operations, so write those here. |
| `Sysconf` | `runtime.NumCPU`, `os.Getpagesize`, `syscall.Sysinfo`, `syscall.Getrlimit` | Only the constant table is new. |
| `v*` printf and scanf | The existing printf and scanf engines | Each is a one-line wrapper taking `[]any`. |
| `Popen` `Pclose` | The existing `Spawn`, `Pipe` and `Fdopen` | No new mechanism. |
| `Reallocarray` | `math/bits.Mul64` for the overflow check | |

**Write it here** (Go has nothing to reuse, or its version does not fit):

| Item | Why |
|---|---|
| `Readv` `Writev` `Eventfd` `Timerfd_*` `Signalfd` `Pidfd_*` `Getsid` `Readlinkat` `Posix_fadvise` `Clock_getres` `Clock_nanosleep` | Missing from `syscall`. Add the syscall numbers to the per-architecture files and call through the existing raw syscall layer. |
| `Strptime` | No Go equivalent. Share the name and weekday tables with `Strftime` and use our own `Timegm` for the arithmetic. |
| POSIX `TZ` rule strings and `Tzset` | Go's `time` parses them internally but exports nothing. Write the parser and the transition calculation (about 200 lines). |
| `Fnmatch` `Glob` | `filepath.Match` lacks the `FNM_*` flags and `[[:alpha:]]` classes and treats `/` differently. |
| `Getopt` `Getopt_long` | Pure logic, and `flag` is a different design. |
| `Getline` `Getdelim` `Fgetpos` `Fsetpos` `Setbuf` | These must work on our `File`'s own buffer. `Getline` should scan `rbuf[rpos:rend]` with `bytes.IndexByte`, since `bufio` would put a second buffer in front of ours and break `Ungetc` and `Fseek`. |
| `Fmemopen` `Open_memstream` | `File` needs a memory-backed mode with no file descriptor. |
| `Flockfile` `Funlockfile` | Needs a per-`File` mutex. Go has no goroutine identity, so a faithful recursive lock cannot be built. Lock inside every stdio call, provide the `_unlocked` variants, and document `Flockfile` as non-recursive. |
| `Posix_memalign` `Aligned_alloc` `Malloc_usable_size` | The allocator is ours. Power-of-two size classes may already satisfy alignment up to the class size if blocks are carved at multiples of it, so check that before adding anything. |
| `Drand48` family and `Random` | Small and exact: a 48-bit linear congruential generator, and glibc's additive-feedback generator for `Random`. |
| Pty (`Posix_openpt` `Grantpt` `Unlockpt` `Ptsname` `Openpty`) | Open `/dev/ptmx` and issue `TIOCGPTN` and `TIOCSPTLCK`, using existing `Open` and `Ioctl`. |
| `Quick_exit` | Trivial. |

### Speeding up what already exists

- [x] `Memchr` calls `bytes.IndexByte`, which uses SIMD. Measured with `make bench` on an amd64 container (Intel i9, before and after): 16 bytes 985 MB/s to 2.0 GB/s, 256 bytes 1.2 GB/s to 17 GB/s, 4 KiB 1.2 GB/s to 32 GB/s, 64 KiB 1.2 GB/s to 28 GB/s.
- [ ] `Strlen` is still a byte loop, on purpose. It has no length to give `bytes.IndexByte`, and reading ahead in chunks would run past the end of a Go-allocated string, which `go test -race` (checkptr) reports as an error. A word-at-a-time loop on aligned reads is the option to measure, if `Strlen` ever shows up as a bottleneck.
- [ ] Check the other `Mem*` and `Str*` functions the same way (`Memcmp`, `Strchr`, `Strstr` and so on), and measure before and after. `Memcpy` and `Memmove` already use `copy`.

## Covered by Go: not planned

Use these instead of looking for a golibc equivalent. Verify behaviour against your Go version before relying on a mapping.

| C | Go |
|---|---|
| `Strsep`, `Memrchr`, `Memmem`, `Strcasestr`, `Strchrnul`, `Stpcpy`, `Strlcpy`, `Memccpy` | `strings` and `bytes` (`Cut`, `LastIndexByte`, `Index`, `EqualFold`), `copy` |
| `Bzero`, `Ffs` | `clear()`, `math/bits.TrailingZeros` |
| `Isblank`, `Isascii`, `Toascii` | `unicode`, or a byte comparison |
| `Llabs`, `Ldiv`, `Lldiv` | the `-`, `/` and `%` operators |
| `Qsort_r` | `slices.SortFunc`, `sort.Slice` |
| `Mkstemp`, `Mkdtemp` | `os.CreateTemp`, `os.MkdirTemp` |
| `Realpath` | `filepath.EvalSymlinks` plus `filepath.Abs` |
| `Basename`, `Dirname` | `filepath.Base`, `filepath.Dir` |
| `Ftw`, `Nftw`, `Scandir` | `filepath.WalkDir`, `os.ReadDir` |
| `Chown`, `Lchown`, `Utimes`, `Pread`, `Pwrite` | `os.Chown`, `os.Lchown`, `os.Chtimes`, `File.ReadAt`, `File.WriteAt` |
| `Openat`, `Pipe2`, `Dup3`, `Mkfifo`, `Mknod`, `Fdatasync`, `Sync`, `Flock`, `Sendfile`, `Statvfs`, `Uname`, `Getrlimit`, `Setrlimit` | the `syscall` package |
| `Setsid`, `Setpgid`, `Getpgid`, `Getgroups`, `Getpriority`, `Setpriority`, `Wait4`, `Times` | the `syscall` package |
| `Execv`, `Execvp`, `Wait` | `os/exec`, `syscall.Exec`, `os.Process.Wait` |
| `Gethostname`, `Getpagesize` | `os.Hostname`, `os.Getpagesize` |
| `Getrandom` | `crypto/rand` |
| `Getpwnam`, `Getpwuid`, `Getgrnam` | `os/user` |
| `Syslog`, `Openlog` | `log/syslog` |
| `Inotify_*` | the `syscall` package (`InotifyInit1`, `InotifyAddWatch`) |
| `Frexp`, `Modf`, `Fma`, `Fdim`, `Nextafter`, `Remainder`, `Rint`, `Nearbyint`, `Lround`, `Expm1`, `Log1p`, `Asinh`, `Acosh`, `Atanh`, `Erf`, `Erfc`, `Tgamma`, `Lgamma` | `math` |
| `Inet_aton`, `Inet_addr`, `Inet_ntoa` | `net/netip` |

## Out of scope

Not planned, because they cannot be done safely or sensibly in Go:

- **`Fork`.** A raw `fork` in a multi-threaded Go program leaves the child without a working scheduler. `Spawn` and `Execve` cover the usual uses.
- **`Setjmp` / `Longjmp`.** They cannot be implemented safely under the Go runtime.
- **pthreads, semaphores and POSIX message queues.** Use goroutines and channels.
- **Locale and wide characters** (`Setlocale`, `Mbstowcs`, `Wcslen`, the `w*` functions). Only the C locale is supported.
- **macOS and Windows.** The package is Linux only.
