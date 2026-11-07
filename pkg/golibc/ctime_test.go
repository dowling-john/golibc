//go:build linux

package golibc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func casesAndGolden(t *testing.T, name string) (cases, golden []string) {
	t.Helper()
	for _, l := range readLines(t, "testdata/"+name+".cases") {
		if l != "" && !strings.HasPrefix(l, "#") {
			cases = append(cases, l)
		}
	}
	golden = readLines(t, "testdata/"+name+".golden")
	if len(cases) != len(golden) {
		t.Fatalf("%d cases but %d golden lines", len(cases), len(golden))
	}
	return
}

// TestStrftimeGolden checks every conversion, flag and width against real
// glibc output (regenerate with scripts/gen_golden.sh). The generator ran
// with TZ unset, so times are UTC.
func TestStrftimeGolden(t *testing.T) {
	cases, golden := casesAndGolden(t, "strftime")
	failures := 0
	for i, c := range cases {
		tsText, format, _ := strings.Cut(c, "\t")
		ts, _ := strconv.ParseInt(tsText, 10, 64)
		buf := make([]byte, 512)
		n := Strftime(&buf[0], len(buf), CStr(format), Gmtime(ts))
		if got := fmt.Sprintf("%d [%s]", n, buf[:n]); got != golden[i] {
			if failures++; failures <= 40 {
				t.Errorf("strftime(%q) at %d = %q, glibc says %q", format, ts, got, golden[i])
			}
		}
	}
	if failures > 40 {
		t.Errorf("... and %d more mismatches", failures-40)
	}
}

func TestGmtimeGolden(t *testing.T) {
	cases, golden := casesAndGolden(t, "gmtime")
	for i, c := range cases {
		ts, _ := strconv.ParseInt(c, 10, 64)
		tm := Gmtime(ts)
		got := fmt.Sprintf("%d %d %d %d %d %d %d %d %d", tm.Sec, tm.Min, tm.Hour, tm.Mday, tm.Mon, tm.Year, tm.Wday, tm.Yday, tm.Isdst)
		if got != golden[i] {
			t.Errorf("gmtime(%d) = %q, glibc says %q", ts, got, golden[i])
		}
		if tm.Zone != "GMT" || tm.Gmtoff != 0 {
			t.Errorf("gmtime(%d) zone = %q off %d", ts, tm.Zone, tm.Gmtoff)
		}
		if back := Timegm(Gmtime(ts)); back != ts {
			t.Errorf("Timegm(Gmtime(%d)) = %d", ts, back)
		}
	}
}

func TestTimegmNormalizes(t *testing.T) {
	tm := &Tm{Year: 124, Mon: 14, Mday: 31, Hour: 25, Min: 61, Sec: 61} // month 14 = March 2025; the overflowing time fields carry into April 1, 02:02:01
	got := Timegm(tm)
	if want := time.Date(2025, 3, 31, 25, 61, 61, 0, time.UTC).Unix(); got != want {
		t.Errorf("Timegm = %d, want %d", got, want)
	}
	if tm.Year != 125 || tm.Mon != 3 || tm.Mday != 1 || tm.Hour != 2 || tm.Min != 2 || tm.Sec != 1 {
		t.Errorf("fields not normalized: %+v", tm)
	}
	neg := &Tm{Year: 70, Mon: -1, Mday: 1} // month -1 = December 1969
	if Timegm(neg); neg.Year != 69 || neg.Mon != 11 {
		t.Errorf("negative month: %+v", neg)
	}
}

func TestStrftimeBufferAndLiterals(t *testing.T) {
	tm := Gmtime(0)
	buf := make([]byte, 32)
	if n := Strftime(&buf[0], len(buf), CStr("%Y-%m-%d"), tm); n != 10 || GoStr(&buf[0]) != "1970-01-01" {
		t.Errorf("n=%d %q", n, GoStr(&buf[0]))
	}
	// Exactly fits (10 bytes + NUL), one byte short returns 0.
	if Strftime(&buf[0], 11, CStr("%Y-%m-%d"), tm) != 10 || Strftime(&buf[0], 10, CStr("%Y-%m-%d"), tm) != 0 {
		t.Error("max includes the terminating NUL")
	}
	if Strftime(&buf[0], 1, CStr(""), tm) != 0 || buf[0] != 0 {
		t.Error("empty format fits in one byte and writes only the NUL")
	}
	if n := Strftime(&buf[0], len(buf), CStr("a%tb%nc"), tm); n != 5 || GoStr(&buf[0]) != "a\tb\nc" {
		t.Errorf("%%t/%%n: %q", GoStr(&buf[0]))
	}
	if GoStr(Asctime(tm)) != "Thu Jan  1 00:00:00 1970\n" {
		t.Errorf("Asctime = %q", GoStr(Asctime(tm)))
	}
	if Difftime(10, 4) != 6 {
		t.Error("Difftime")
	}
}

func TestLocaltimeMatchesStdlib(t *testing.T) {
	// Whatever zone the host uses, Localtime/Mktime must agree with the standard library.
	for _, ts := range []int64{0, 1704067200, 1720000000, 1700000000, -86400, 2147483647} {
		tm := Localtime(ts)
		want := time.Unix(ts, 0)
		name, off := want.Zone()
		if tm.Year+1900 != want.Year() || tm.Mon+1 != int(want.Month()) || tm.Mday != want.Day() ||
			tm.Hour != want.Hour() || tm.Min != want.Minute() || tm.Sec != want.Second() ||
			tm.Zone != name || tm.Gmtoff != int64(off) || tm.Wday != int(want.Weekday()) || tm.Yday != want.YearDay()-1 {
			t.Errorf("Localtime(%d) = %+v, stdlib %v", ts, tm, want)
		}
		cp := *tm
		if back := Mktime(&cp); back != ts {
			t.Errorf("Mktime(Localtime(%d)) = %d", ts, back)
		}
	}
	if (&Tm{}) == nil || Mktime(&Tm{Year: 100, Mon: 0, Mday: 1}) != time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local).Unix() {
		t.Error("Mktime of a plain date")
	}
	var tv Timeval
	if Gettimeofday(&tv) != 0 || abs64(tv.Sec-Time()) > 1 || tv.Usec < 0 || tv.Usec >= 1_000_000 {
		t.Errorf("Gettimeofday = %+v", tv)
	}
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// Zone rules need a known TZ before the standard library first reads it, so
// the real check runs in a child process with TZ set.
func TestLocaltimeZoneHelper(t *testing.T) {
	if os.Getenv("GOLIBC_TZ_HELPER") == "" {
		t.Skip("helper for TestLocaltimeInZone")
	}
	if _, err := time.LoadLocation("America/New_York"); err != nil {
		t.Skip("no zone data: ", err)
	}
	winter := Localtime(1704067200) // 2024-01-01 00:00:00 UTC
	if winter.Hour != 19 || winter.Mday != 31 || winter.Mon != 11 || winter.Year != 123 || winter.Zone != "EST" || winter.Gmtoff != -18000 || winter.Isdst != 0 {
		t.Errorf("winter = %+v", winter)
	}
	summer := Localtime(1720000000) // 2024-07-03 09:46:40 UTC
	if summer.Hour != 5 || summer.Zone != "EDT" || summer.Gmtoff != -14400 || summer.Isdst != 1 {
		t.Errorf("summer = %+v", summer)
	}
	buf := make([]byte, 64)
	n := Strftime(&buf[0], len(buf), CStr("%Y-%m-%d %H:%M:%S %Z %z"), summer)
	if got := string(buf[:n]); got != "2024-07-03 05:46:40 EDT -0400" {
		t.Errorf("formatted = %q", got)
	}
	tm := &Tm{Year: 124, Mon: 6, Mday: 3, Hour: 5, Min: 46, Sec: 40}
	if got := Mktime(tm); got != 1720000000 || tm.Isdst != 1 || tm.Zone != "EDT" || tm.Wday != 3 {
		t.Errorf("Mktime = %d %+v", got, tm)
	}
	gap := &Tm{Year: 124, Mon: 2, Mday: 10, Hour: 2, Min: 30} // 2:30 does not exist on spring-forward day
	Mktime(gap)
	if gap.Hour != 3 || gap.Min != 30 {
		t.Errorf("nonexistent time normalized to %02d:%02d, want 03:30", gap.Hour, gap.Min)
	}
	// 01:30 on fall-back day occurs twice: first as EDT, then as EST. The hint picks.
	for _, c := range []struct {
		hint int
		zone string
		t    int64
	}{{-1, "EDT", 1730611800}, {1, "EDT", 1730611800}, {0, "EST", 1730615400}} {
		fold := &Tm{Year: 124, Mon: 10, Mday: 3, Hour: 1, Min: 30, Isdst: c.hint}
		if got := Mktime(fold); got != c.t || fold.Zone != c.zone {
			t.Errorf("fold with Isdst=%d: t=%d %s, want %d %s", c.hint, got, fold.Zone, c.t, c.zone)
		}
	}
	if GoStr(Ctime(1720000000)) != "Wed Jul  3 05:46:40 2024\n" {
		t.Errorf("Ctime = %q", GoStr(Ctime(1720000000)))
	}
}

func TestLocaltimeInZone(t *testing.T) {
	self, _ := os.Executable()
	env := append(syscall.Environ(), "TZ=America/New_York", "GOLIBC_TZ_HELPER=1")
	pid := Spawn(CStr(self), []string{self, "-test.run=^TestLocaltimeZoneHelper$", "-test.v"}, env)
	if pid <= 0 {
		t.Fatalf("Spawn: %v", GetErrno())
	}
	var st int
	Waitpid(pid, &st, 0)
	if !WIFEXITED(st) || WEXITSTATUS(st) != 0 {
		t.Errorf("zone helper failed (status %#x); see its output above", st)
	}
}

func fmtTm(tm *Tm) string {
	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d %s", tm.Sec, tm.Min, tm.Hour, tm.Mday, tm.Mon, tm.Year, tm.Wday, tm.Yday, tm.Isdst, tm.Gmtoff, tm.Zone)
}

// Runs inside a child process whose TZ is GOLIBC_TZ_GOLDEN, checking every
// localtime.cases line for that zone against glibc's localtime_r and mktime.
func TestLocaltimeGoldenHelper(t *testing.T) {
	tz := os.Getenv("GOLIBC_TZ_GOLDEN")
	if tz == "" {
		t.Skip("helper for TestLocaltimeGolden")
	}
	Tzset()
	if z := currentZone(true); z.rule == nil && z.loc == time.UTC && tz != "UTC" {
		t.Skip("no zone data for ", tz)
	}
	cases, golden := casesAndGolden(t, "localtime")
	for i, c := range cases {
		f := strings.Split(c, "\t")
		if f[0] != tz {
			continue
		}
		var got string
		if f[1] == "l" {
			ts, _ := strconv.ParseInt(f[2], 10, 64)
			got = fmtTm(Localtime(ts))
		} else {
			n := make([]int, 6)
			for k := range n {
				n[k], _ = strconv.Atoi(f[2+k])
			}
			tm := &Tm{Year: n[0] - 1900, Mon: n[1] - 1, Mday: n[2], Hour: n[3], Min: n[4], Sec: n[5], Isdst: -1}
			ts := Mktime(tm)
			got = fmt.Sprintf("t=%d %s", ts, fmtTm(tm))
		}
		if got != golden[i] {
			t.Errorf("%s %s: got %q, glibc says %q", tz, strings.Join(f[1:], " "), got, golden[i])
		}
	}
}

func TestLocaltimeGolden(t *testing.T) {
	cases, _ := casesAndGolden(t, "localtime")
	seen := map[string]bool{}
	self, _ := os.Executable()
	for _, c := range cases {
		tz, _, _ := strings.Cut(c, "\t")
		if seen[tz] {
			continue
		}
		seen[tz] = true
		t.Run(strings.ReplaceAll(tz, "/", "_"), func(t *testing.T) {
			env := append(syscall.Environ(), "TZ="+tz, "GOLIBC_TZ_GOLDEN="+tz)
			pid := Spawn(CStr(self), []string{self, "-test.run=^TestLocaltimeGoldenHelper$"}, env)
			if pid <= 0 {
				t.Fatalf("Spawn: %v", GetErrno())
			}
			var st int
			Waitpid(pid, &st, 0)
			if !WIFEXITED(st) || WEXITSTATUS(st) != 0 {
				t.Errorf("zone %s disagrees with glibc (status %#x); details above", tz, st)
			}
		})
	}
}
