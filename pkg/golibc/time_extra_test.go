//go:build linux

package golibc

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// setTZ points TZ at tz for the rest of the test and re-reads it, then
// restores the original zone afterwards.
func setTZ(t *testing.T, tz string) {
	t.Helper()
	t.Cleanup(Tzset) // runs after t.Setenv has put TZ back
	t.Setenv("TZ", tz)
	Tzset()
}

func unescapeCase(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t").Replace(s)
}

func fmtStrptime(in *byte, r *byte, tm *Tm) string {
	if r == nil {
		return "null"
	}
	off := int(uintptr(unsafe.Pointer(r)) - uintptr(unsafe.Pointer(in)))
	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d %d", off, tm.Sec, tm.Min, tm.Hour, tm.Mday, tm.Mon, tm.Year, tm.Wday, tm.Yday, tm.Isdst, tm.Gmtoff)
}

// TestStrptimeGolden checks Strptime against glibc's strptime (regenerate with
// scripts/gen_golden.sh): the offset it stops at, and every field of the Tm.
func TestStrptimeGolden(t *testing.T) {
	setTZ(t, "UTC") // %s goes through localtime, and the generator ran in UTC
	cases, golden := casesAndGolden(t, "strptime")
	for i, c := range cases {
		inp, format, _ := strings.Cut(c, "\t")
		in := CStr(unescapeCase(inp))
		var tm Tm
		got := fmtStrptime(in, Strptime(in, CStr(unescapeCase(format)), &tm), &tm)
		if got != golden[i] {
			t.Errorf("Strptime(%q, %q) = %s, glibc says %s", unescapeCase(inp), unescapeCase(format), got, golden[i])
		}
	}
}

func TestStrptimeRoundTrip(t *testing.T) {
	// Whatever Strftime writes, Strptime reads back.
	tm := Gmtime(1709210096) // 2024-02-29 12:34:56 UTC
	buf := make([]byte, 128)
	for _, format := range []string{"%Y-%m-%d %H:%M:%S", "%a %b %e %H:%M:%S %Y", "%d/%m/%y %I:%M:%S %p", "%F %T %z"} {
		n := Strftime(&buf[0], len(buf), CStr(format), tm)
		var back Tm
		if end := Strptime(&buf[0], CStr(format), &back); end == nil || int(uintptr(unsafe.Pointer(end))-uintptr(unsafe.Pointer(&buf[0]))) != n {
			t.Errorf("Strptime did not consume Strftime(%q) = %q", format, buf[:n])
			continue
		}
		if back.Year != tm.Year || back.Mon != tm.Mon || back.Mday != tm.Mday || back.Hour != tm.Hour || back.Min != tm.Min || back.Sec != tm.Sec {
			t.Errorf("round trip of %q gave %+v, want %+v", format, back, *tm)
		}
		if back.Wday != tm.Wday || back.Yday != tm.Yday {
			t.Errorf("round trip of %q derived wday %d yday %d, want %d %d", format, back.Wday, back.Yday, tm.Wday, tm.Yday)
		}
	}
}

// TestTzsetGolden checks Tzname, Timezone and Daylight against glibc for zone
// files and for POSIX rule strings.
func TestTzsetGolden(t *testing.T) {
	cases, golden := casesAndGolden(t, "tzset")
	t.Cleanup(Tzset)
	for i, tz := range cases {
		if namedZone(strings.TrimPrefix(tz, ":")) == nil && !strings.ContainsAny(tz, "0123456789<") && tz != "Invalid/Zone" && tz != "XX" {
			t.Logf("no zone data for %s, skipping", tz)
			continue
		}
		t.Setenv("TZ", tz)
		Tzset()
		got := fmt.Sprintf("%s %s %d %d", Tzname[0], Tzname[1], Timezone, Daylight)
		if got != golden[i] {
			t.Errorf("TZ=%q: got %q, glibc says %q", tz, got, golden[i])
		}
	}
}

func TestTZChanges(t *testing.T) {
	setTZ(t, "EST5EDT,M3.2.0,M11.1.0")
	const summer = 1720000000 // 2024-07-03 09:46:40 UTC
	if tm := Localtime(summer); tm.Zone != "EDT" || tm.Hour != 5 || tm.Isdst != 1 {
		t.Errorf("EST5EDT: %+v", tm)
	}
	// Localtime notices a new TZ by itself; LocaltimeR keeps the zone it has until Tzset.
	t.Setenv("TZ", "JST-9")
	if tm := Localtime(summer); tm.Zone != "JST" || tm.Hour != 18 || tm.Gmtoff != 32400 {
		t.Errorf("after TZ=JST-9, Localtime: %+v", tm)
	}
	t.Setenv("TZ", "UTC0")
	Tzset()
	if tm := LocaltimeR(summer, new(Tm)); tm.Zone != "UTC" || tm.Hour != 9 {
		t.Errorf("after Tzset, LocaltimeR: %+v", tm)
	}
	t.Setenv("TZ", "EST5EDT,M3.2.0,M11.1.0")
	if tm := LocaltimeR(summer, new(Tm)); tm.Zone != "UTC" {
		t.Errorf("LocaltimeR should keep the zone until Tzset, got %+v", tm)
	}
	Tzset()
	if tm := LocaltimeR(summer, new(Tm)); tm.Zone != "EDT" {
		t.Errorf("LocaltimeR after Tzset: %+v", tm)
	}

	// An empty TZ is UTC, and an unset one is whatever /etc/localtime says.
	t.Setenv("TZ", "")
	Tzset()
	if tm := Localtime(summer); tm.Zone != "UTC" || tm.Gmtoff != 0 {
		t.Errorf("empty TZ: %+v", tm)
	}
	syscall.Unsetenv("TZ")
	Tzset()
	want := time.Unix(summer, 0)
	if name, off := want.Zone(); Localtime(summer).Zone != name || Localtime(summer).Gmtoff != int64(off) {
		t.Errorf("unset TZ: %+v, want %s %d", Localtime(summer), name, off)
	}
}

func TestStrptimeDayOfYear(t *testing.T) {
	// Only the year and day of the year are read, so the date comes from them.
	var tm Tm
	in := CStr("2024 060")
	if Strptime(in, CStr("%Y %j"), &tm) == nil || tm.Mon != 1 || tm.Mday != 29 || tm.Wday != 4 || tm.Yday != 59 {
		t.Errorf("%%Y %%j gave %+v, want 29 February 2024, a Thursday, day 59", tm)
	}
}

func TestParseTZRule(t *testing.T) {
	for _, c := range []struct {
		tz             string
		std, dst       string
		stdOff, dstOff int
		hasDST         bool
	}{
		{"EST5EDT,M3.2.0,M11.1.0", "EST", "EDT", -18000, -14400, true},
		{"EST5EDT", "EST", "EDT", -18000, -14400, true}, // US rules by default
		{"CET-1CEST,M3.5.0,M10.5.0/3", "CET", "CEST", 3600, 7200, true},
		{"<+0545>-5:45", "+0545", "+0545", 20700, 0, false},
		{"NZST-12:00:00NZDT-13:00:00,M9.5.0,M4.1.0/3", "NZST", "NZDT", 43200, 46800, true},
		{"<-0330>3:30<-0230>,M3.2.0,M11.1.0", "-0330", "-0230", -12600, -9000, true},
		{"UTC0", "UTC", "UTC", 0, 0, false},
		{"Invalid/Zone", "Invalid", "", 0, 0, false},
		{"EST5EDT,M3.2.0", "EST", "EST", -18000, 0, false}, // a rule with no end is not a rule
		{"EST5EDT,M13.2.0,M11.1.0", "EST", "EST", -18000, 0, false},
		{"XX", "", "", 0, 0, false},
	} {
		r := parseTZRule(c.tz)
		if r.stdName != c.std || r.dstName != c.dst || r.stdOff != c.stdOff || r.hasDST != c.hasDST || (c.hasDST && r.dstOff != c.dstOff) {
			t.Errorf("%q = %+v, want std %q dst %q offsets %d %d dst=%v", c.tz, *r, c.std, c.dst, c.stdOff, c.dstOff, c.hasDST)
		}
	}
}

func TestRuleTransitions(t *testing.T) {
	r := parseTZRule("EST5EDT,M3.2.0,M11.1.0")
	for _, c := range []struct {
		t    int64
		name string
	}{
		{1710053999, "EST"}, {1710054000, "EDT"}, // 2024-03-10 07:00:00 UTC, 2:00 local
		{1730613599, "EDT"}, {1730613600, "EST"}, // 2024-11-03 06:00:00 UTC, 2:00 local
	} {
		if name, _, _ := r.lookup(c.t); name != c.name {
			t.Errorf("t=%d: %s, want %s", c.t, name, c.name)
		}
	}
	south := parseTZRule("AEST-10AEDT,M10.1.0,M4.1.0/3") // daylight saving time spans the new year
	if _, _, dst := south.lookup(1704067200); !dst {
		t.Error("Sydney in January should be on daylight saving time")
	}
	if _, _, dst := south.lookup(1720000000); dst {
		t.Error("Sydney in July should not be on daylight saving time")
	}
}

func TestClock(t *testing.T) {
	start := Clock()
	if start < 0 {
		t.Fatal("Clock failed")
	}
	deadline := time.Now().Add(30 * time.Millisecond)
	for time.Now().Before(deadline) { // burn processor time
	}
	if used := Clock() - start; used < 10_000 { // at least 10 ms of the 30 ms spent spinning
		t.Errorf("Clock advanced by %d microseconds after 30 ms of work", used)
	}
}

func TestClockGetres(t *testing.T) {
	for _, clock := range []int{CLOCK_REALTIME, CLOCK_MONOTONIC, CLOCK_PROCESS_CPUTIME_ID, CLOCK_MONOTONIC_RAW, CLOCK_BOOTTIME} {
		var res Timespec
		if ClockGetres(clock, &res) != 0 {
			t.Errorf("ClockGetres(%d) failed: %v", clock, GetErrno())
			continue
		}
		if res.Sec != 0 || res.Nsec < 1 || res.Nsec > 20_000_000 {
			t.Errorf("ClockGetres(%d) = %+v", clock, res)
		}
	}
	SetErrno(0)
	if ClockGetres(9999, &Timespec{}) != -1 || GetErrno() != EINVAL {
		t.Error("an invalid clock should fail with EINVAL")
	}
	if ClockGetres(CLOCK_MONOTONIC, nil) != 0 {
		t.Error("a nil res should be allowed")
	}
}

func TestClockNanosleep(t *testing.T) {
	begin := time.Now()
	if e := ClockNanosleep(CLOCK_MONOTONIC, 0, &Timespec{Nsec: 30_000_000}, nil); e != 0 {
		t.Fatalf("relative sleep returned %d", e)
	}
	if d := time.Since(begin); d < 30*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("relative sleep took %v", d)
	}

	var now Timespec
	ClockGettime(CLOCK_MONOTONIC, &now)
	deadline := Timespec{Sec: now.Sec, Nsec: now.Nsec + 30_000_000}
	if deadline.Nsec >= 1_000_000_000 {
		deadline.Sec, deadline.Nsec = deadline.Sec+1, deadline.Nsec-1_000_000_000
	}
	begin = time.Now()
	if e := ClockNanosleep(CLOCK_MONOTONIC, TIMER_ABSTIME, &deadline, nil); e != 0 {
		t.Fatalf("absolute sleep returned %d", e)
	}
	if d := time.Since(begin); d < 25*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("absolute sleep took %v", d)
	}
	begin = time.Now()
	ClockNanosleep(CLOCK_MONOTONIC, TIMER_ABSTIME, &deadline, nil) // already past
	if time.Since(begin) > 20*time.Millisecond {
		t.Error("an absolute time in the past should return at once")
	}

	SetErrno(0)
	if e := ClockNanosleep(9999, 0, &Timespec{}, nil); e != int(EINVAL) || GetErrno() != 0 {
		t.Errorf("an invalid clock returned %d (errno %v), want EINVAL as the return value only", e, GetErrno())
	}
	if e := ClockNanosleep(CLOCK_MONOTONIC, 0, &Timespec{Nsec: -1}, nil); e != int(EINVAL) {
		t.Errorf("a negative time returned %d, want EINVAL", e)
	}
}
