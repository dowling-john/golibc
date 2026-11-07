//go:build linux

package golibc

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Time zones (tzset and the TZ environment variable).
//
// TZ is read the way glibc reads it:
//
//   - unset: /etc/localtime (UTC if it is missing);
//   - empty: UTC;
//   - a zone name ("America/New_York", optionally with a leading ':') or an
//     absolute path to a zone file: the zone data, which comes from the
//     standard library's time package and its search path;
//   - anything else is read as a POSIX rule string: "EST5EDT,M3.2.0,M11.1.0",
//     "<+0545>-5:45", "CET-1CEST,M3.5.0,M10.5.0/3".
//
// A name is tried as a zone file before it is read as a rule, as glibc does,
// so "EST5EDT" uses the zone data where it is installed and the built-in US
// rules where it is not. A string that is neither a zone nor a valid rule
// gives UTC, labelled with whatever abbreviation could be read from it.

var (
	// Tzname holds the standard and daylight saving abbreviations, Timezone
	// the seconds west of UTC of standard time, and Daylight whether the zone
	// has ever used daylight saving time. Tzset (and every conversion that
	// re-reads TZ) updates them. For a zone read from a file they are the most
	// recent standard and daylight abbreviations in its history, as in glibc,
	// so Asia/Tokyo reports JST and JDT even though it stopped using daylight
	// saving time in 1951.
	Tzname   = [2]string{"UTC", "UTC"}
	Timezone int64
	Daylight int

	tzMu     sync.Mutex
	tzLoaded bool
	tzRaw    string
	tzSet    bool
	tzCur    *zone
)

// zone is either a zone read from tz data (loc) or a POSIX rule.
type zone struct {
	loc  *time.Location
	rule *tzRule
}

var utcZone = &zone{loc: time.UTC}

// lookup returns the abbreviation, offset east of UTC and daylight saving
// state in force at t.
func (z *zone) lookup(t int64) (name string, off int, dst bool) {
	if z.rule != nil {
		return z.rule.lookup(t)
	}
	lt := time.Unix(t, 0).In(z.loc)
	name, off = lt.Zone()
	return name, off, lt.IsDST()
}

// Tzset re-reads the TZ environment variable. Localtime, Ctime and Mktime do
// this themselves whenever TZ has changed, and LocaltimeR reads it once, as in
// glibc, so a program only needs Tzset to read TZ before its first LocaltimeR
// call or to pick up a change that LocaltimeR would otherwise not see.
func Tzset() {
	tzMu.Lock()
	defer tzMu.Unlock()
	raw, set := syscall.Getenv("TZ")
	loadZone(raw, set)
}

// currentZone returns the zone for the present value of TZ. With always false
// the zone is read once and kept; with always true it is read again whenever
// TZ has changed.
func currentZone(always bool) *zone {
	tzMu.Lock()
	defer tzMu.Unlock()
	if tzLoaded && !always {
		return tzCur
	}
	raw, set := syscall.Getenv("TZ")
	if !tzLoaded || raw != tzRaw || set != tzSet {
		loadZone(raw, set)
	}
	return tzCur
}

// loadZone installs the zone TZ describes. The caller holds tzMu.
func loadZone(raw string, set bool) {
	var z *zone
	switch {
	case !set:
		z = fileZone("/etc/localtime")
	case raw == "":
		z = utcZone
	default:
		name := strings.TrimPrefix(raw, ":")
		z = namedZone(name)
		if z == nil && raw[0] != ':' {
			z = &zone{rule: parseTZRule(raw)}
		}
	}
	if z == nil {
		z = utcZone
	}
	tzCur, tzLoaded, tzRaw, tzSet = z, true, raw, set
	setTzVars(z)
}

func fileZone(path string) *zone {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	loc, err := time.LoadLocationFromTZData("", data)
	if err != nil {
		return nil
	}
	return &zone{loc: loc}
}

// namedZone finds a zone by absolute path or by name; it returns nil if there
// is no such zone.
func namedZone(name string) *zone {
	switch {
	case name == "" || name == "Local":
		return nil
	case name[0] == '/':
		return fileZone(name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return &zone{loc: loc}
}

// setTzVars fills Tzname, Timezone and Daylight from z.
func setTzVars(z *zone) {
	if r := z.rule; r != nil {
		Tzname = [2]string{r.stdName, r.dstName}
		Timezone, Daylight = -int64(r.stdOff), 0
		if r.hasDST {
			Daylight = 1
		}
		return
	}
	// A zone file has no fixed standard and daylight pair, so take the most
	// recent of each, searching back month by month from now.
	var std, dst string
	var stdOff int
	now := time.Now().UTC()
	for y := now.Year(); y >= 1900 && (std == "" || dst == ""); y-- {
		for m := time.December; m >= time.January; m-- {
			t := time.Date(y, m, 15, 12, 0, 0, 0, time.UTC)
			if t.After(now) {
				continue
			}
			name, off, isDST := z.lookup(t.Unix())
			if isDST && dst == "" {
				dst = name
			}
			if !isDST && std == "" {
				std, stdOff = name, off
			}
		}
	}
	if std == "" { // daylight saving time throughout
		std = dst
	}
	Tzname = [2]string{std, std}
	Timezone, Daylight = -int64(stdOff), 0
	if dst != "" {
		Tzname[1] = dst
		Daylight = 1
	}
}

// A tzRule is a POSIX TZ rule: std offset [dst [offset] [,start[/time],end[/time]]].
type tzRule struct {
	stdName, dstName string
	stdOff, dstOff   int // seconds east of UTC
	hasDST           bool
	start, end       ruleDate
}

// A ruleDate says when a transition happens: Jn (day 1-365, February 29 never
// counted), n (day 0-365, leap days counted) or Mm.w.d (day d of week w of
// month m, with week 5 meaning the last), at secs after local midnight.
type ruleDate struct {
	kind       byte // 'J', 'D' or 'M'
	n          int  // day for J and D, month for M
	week, wday int
	secs       int
}

var (
	defaultStart = ruleDate{kind: 'M', n: 3, week: 2, wday: 0, secs: 7200}
	defaultEnd   = ruleDate{kind: 'M', n: 11, week: 1, wday: 0, secs: 7200}
)

// lookup is the zone lookup for a rule. The transitions are computed for the
// UTC year of t, as glibc does.
func (r *tzRule) lookup(t int64) (string, int, bool) {
	if !r.hasDST {
		return r.stdName, r.stdOff, false
	}
	y, _, _ := civilFromDays(floorDiv(t, 86400))
	start := r.start.change(y, r.stdOff) // the start is read in standard time
	end := r.end.change(y, r.dstOff)     // the end is read in daylight saving time
	var dst bool
	if start > end { // southern hemisphere: daylight saving time spans the new year
		dst = t >= start || t < end
	} else {
		dst = t >= start && t < end
	}
	if dst {
		return r.dstName, r.dstOff, true
	}
	return r.stdName, r.stdOff, false
}

func leapYear(y int64) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

// change returns the instant of the transition in year, given the offset of
// the clock the rule is written in.
//
// For a year before 1970 glibc counts the day from the start of 1970 instead
// of the start of that year (while still using that year's calendar for the
// weekday and the leap day), which puts every transition after the epoch. A
// time before 1970 is therefore always outside daylight saving time in the
// northern hemisphere and always inside it in the southern, and this does the
// same.
func (d ruleDate) change(year int64, off int) int64 {
	jan1 := daysFromCivil(year, 1, 1)
	origin := jan1
	if year < 1970 {
		origin = 0
	}
	var days int64
	switch d.kind {
	case 'J':
		days = origin + int64(d.n) - 1
		if leapYear(year) && d.n >= 60 {
			days++
		}
	case 'D':
		days = origin + int64(d.n)
	default:
		first := daysFromCivil(year, int64(d.n), 1)
		next := daysFromCivil(year, int64(d.n)+1, 1)
		if d.n == 12 {
			next = daysFromCivil(year+1, 1, 1)
		}
		firstWday := int(floorMod(first+4, 7))
		day := 1 + (d.wday-firstWday+7)%7 + (d.week-1)*7
		if int64(day) > next-first {
			day -= 7
		}
		days = origin + (first - jan1) + int64(day) - 1
	}
	return days*86400 + int64(d.secs) - int64(off)
}

// parseTZRule reads a POSIX TZ string. It always returns a rule: when the
// string is not valid, the part that could be read is kept and the rest is
// treated as UTC with no daylight saving time.
func parseTZRule(s string) *tzRule {
	p := &tzParser{s: s}
	r := &tzRule{}
	name, ok := p.name()
	if !ok {
		return r
	}
	r.stdName = name
	off, ok := p.offset()
	if !ok {
		return r
	}
	r.stdOff, r.dstName = -off, name // with no daylight saving time the two names are the same
	if p.done() {
		return r
	}
	dname, ok := p.name()
	if !ok {
		return r
	}
	dstOff := r.stdOff + 3600
	if !p.done() && p.peek() != ',' {
		o, ok := p.offset()
		if !ok {
			return r
		}
		dstOff = -o
	}
	start, end := defaultStart, defaultEnd
	if !p.done() && !(p.peek() == ',' && p.i == len(p.s)-1) {
		var ok1, ok2 bool
		if p.peek() != ',' {
			return r
		}
		p.i++
		start, ok1 = p.rule()
		if !ok1 || p.done() || p.peek() != ',' {
			return r
		}
		p.i++
		end, ok2 = p.rule()
		if !ok2 || !p.done() {
			return r
		}
	}
	r.dstName, r.dstOff, r.hasDST, r.start, r.end = dname, dstOff, true, start, end
	return r
}

type tzParser struct {
	s string
	i int
}

func (p *tzParser) done() bool { return p.i >= len(p.s) }

func (p *tzParser) peek() byte {
	if p.done() {
		return 0
	}
	return p.s[p.i]
}

func isAlphaByte(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// name reads a zone abbreviation: three or more letters, or <...>.
func (p *tzParser) name() (string, bool) {
	if p.peek() == '<' {
		end := strings.IndexByte(p.s[p.i:], '>')
		if end < 0 {
			return "", false
		}
		name := p.s[p.i+1 : p.i+end]
		for i := 0; i < len(name); i++ {
			c := name[i]
			if !isAlphaByte(c) && !(c >= '0' && c <= '9') && c != '+' && c != '-' {
				return "", false
			}
		}
		if len(name) < 3 {
			return "", false
		}
		p.i += end + 1
		return name, true
	}
	start := p.i
	for p.i < len(p.s) && isAlphaByte(p.s[p.i]) {
		p.i++
	}
	if p.i-start < 3 {
		p.i = start
		return "", false
	}
	return p.s[start:p.i], true
}

// number reads one or more decimal digits, at most max of them.
func (p *tzParser) number(max int) (int, bool) {
	start, n := p.i, 0
	for p.i < len(p.s) && p.i-start < max && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		n = n*10 + int(p.s[p.i]-'0')
		p.i++
	}
	return n, p.i > start
}

// offset reads [+|-]hh[:mm[:ss]] and returns it in seconds, as written.
func (p *tzParser) offset() (int, bool) {
	sign := 1
	switch p.peek() {
	case '+':
		p.i++
	case '-':
		sign = -1
		p.i++
	}
	h, ok := p.number(3)
	if !ok || h > 167 {
		return 0, false
	}
	secs := h * 3600
	if p.peek() == ':' {
		p.i++
		m, ok := p.number(2)
		if !ok || m > 59 {
			return 0, false
		}
		secs += m * 60
		if p.peek() == ':' {
			p.i++
			s, ok := p.number(2)
			if !ok || s > 59 {
				return 0, false
			}
			secs += s
		}
	}
	return sign * secs, true
}

// rule reads Jn, n or Mm.w.d, each optionally followed by /time.
func (p *tzParser) rule() (ruleDate, bool) {
	d := ruleDate{secs: 7200}
	switch c := p.peek(); {
	case c == 'M':
		p.i++
		d.kind = 'M'
		var ok bool
		if d.n, ok = p.number(2); !ok || d.n < 1 || d.n > 12 || p.peek() != '.' {
			return d, false
		}
		p.i++
		if d.week, ok = p.number(1); !ok || d.week < 1 || d.week > 5 || p.peek() != '.' {
			return d, false
		}
		p.i++
		if d.wday, ok = p.number(1); !ok || d.wday > 6 {
			return d, false
		}
	case c == 'J':
		p.i++
		d.kind = 'J'
		var ok bool
		if d.n, ok = p.number(3); !ok || d.n < 1 || d.n > 365 {
			return d, false
		}
	case c >= '0' && c <= '9':
		d.kind = 'D'
		var ok bool
		if d.n, ok = p.number(3); !ok || d.n > 365 {
			return d, false
		}
	default:
		return d, false
	}
	if p.peek() == '/' {
		p.i++
		secs, ok := p.offset()
		if !ok {
			return d, false
		}
		d.secs = secs
	}
	return d, true
}
