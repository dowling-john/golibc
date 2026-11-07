//go:build linux

package golibc

// Strptime (time.h): the inverse of Strftime, following glibc's strptime in
// the C locale. It fills only the fields the format names, then derives the
// weekday, the day of the year, or the month and day from the ones it did
// read, as glibc does.

type strptimeState struct {
	s  *byte // the input
	rp int   // how much of it has been read

	haveI, isPM          bool // %I was read, and %p said PM
	century              int  // from %C, or -1
	wantCentury          bool // %y was read, so the century applies to it
	wantXday             bool // a date field was read, so the weekday and yday are derived
	haveWday, haveYday   bool
	haveMon, haveMday    bool
	haveUweek, haveWweek bool
	weekNo               int
}

// cumulative days before each month, in common and leap years
var monYday = [2][13]int{
	{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334, 365},
	{0, 31, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335, 366},
}

func leapIdx(year int) int {
	if leapYear(int64(1900 + year)) {
		return 1
	}
	return 0
}

// Strptime parses the C string s according to format and stores what it finds
// in tm. It returns a pointer to the first byte of s that was not consumed, or
// nil if s does not match.
//
// Supported conversions are a A b B c C d D e F g G h H I j k l m M n p r R s
// S t T u U V w W x X y Y z Z and %%. Whitespace in format matches any amount
// (including none) in s, names match without regard to case, and the flags and
// field widths that Strftime accepts are skipped. The E and O modifiers are
// accepted where glibc accepts them. %Z reads a zone name but does not use it,
// and %z reads +hhmm, +hh:mm, +hh or Z into Gmtoff.
func Strptime(s, format *byte, tm *Tm) *byte {
	st := &strptimeState{s: s, century: -1}
	if !st.parse(GoStr(format), tm) {
		return nil
	}
	if st.haveI && st.isPM {
		tm.Hour += 12
	}
	if st.century != -1 {
		if st.wantCentury {
			tm.Year = tm.Year%100 + (st.century-19)*100
		} else {
			tm.Year = (st.century - 19) * 100 // only the century, not the year
		}
	}
	monthOK := func() bool { return st.haveMon || (tm.Mon >= 0 && tm.Mon <= 11) }
	if st.wantXday && !st.haveWday {
		if !(st.haveMon && st.haveMday) && st.haveYday {
			// The month and day were not read, so compute them from the day of the year.
			mon, mday := dateOfYday(leapIdx(tm.Year), tm.Yday)
			if !st.haveMon {
				tm.Mon = mon
			}
			if !st.haveMday {
				tm.Mday = mday
			}
			st.haveMon, st.haveMday = true, true
		}
		if monthOK() {
			tm.Wday = weekdayOf(tm)
		}
	}
	if st.wantXday && !st.haveYday && monthOK() {
		tm.Yday = monYday[leapIdx(tm.Year)][tm.Mon] + tm.Mday - 1
	}
	if (st.haveUweek || st.haveWweek) && st.haveWday {
		saveWday, saveMday, saveMon := tm.Wday, tm.Mday, tm.Mon
		wOffset := 1
		if st.haveUweek {
			wOffset = 0
		}
		tm.Mday, tm.Mon = 1, 0
		tm.Wday = weekdayOf(tm)
		if st.haveMday {
			tm.Mday = saveMday
		}
		if st.haveMon {
			tm.Mon = saveMon
		}
		if !st.haveYday {
			tm.Yday = (7-(tm.Wday-wOffset))%7 + (st.weekNo-1)*7 + (saveWday-wOffset+7)%7
		}
		if !st.haveMday || !st.haveMon {
			mon, mday := dateOfYday(leapIdx(tm.Year), tm.Yday)
			if !st.haveMon {
				tm.Mon = mon
			}
			if !st.haveMday {
				tm.Mday = mday
			}
		}
		tm.Wday = saveWday
	}
	return advance(s, st.rp)
}

// weekdayOf is the day of the week of the date in tm. It is glibc's
// day_of_the_week formula, including its truncating division, so it agrees
// with glibc even for years before 1 and for days that do not exist (February
// 30), which a calendar calculation would not.
func weekdayOf(tm *Tm) int {
	corr := 1900 + tm.Year
	if tm.Mon < 2 {
		corr--
	}
	neg := 0
	if (corr/4)%25 < 0 {
		neg = 1
	}
	w := -473 + 365*(tm.Year-70) + corr/4 - (corr/4)/25 + neg + ((corr/4)/25)/4 + monYdayAt(0, tm.Mon) + tm.Mday - 1
	return (w%7 + 7) % 7
}

// dateOfYday turns a day of the year into a month (0-11) and day of the month
// the way glibc does, without range checks: a day past the end of the year
// gives month 12 and a day of the month counted on from December, and a day
// before January 1 gives month -1.
func dateOfYday(leap, yday int) (mon, mday int) {
	t := 0 // the first month that starts after yday
	for t <= 12 && monYday[leap][t] <= yday {
		t++
	}
	return t - 1, yday - monYdayAt(leap, t-1) + 1
}

// monYdayAt is monYday[leap][i]. glibc reads the table without checking i, and
// for i == -1 (a day-of-year before January 1) the leap row reads the last
// entry of the common row; this reproduces that and stays in bounds.
func monYdayAt(leap, i int) int {
	switch {
	case i >= 0 && i <= 12:
		return monYday[leap][i]
	case i == -1 && leap == 1:
		return monYday[0][12]
	}
	return 0
}

func (st *strptimeState) cur() byte { return at(st.s, st.rp) }

func (st *strptimeState) skipSpace() {
	for Isspace(int(st.cur())) {
		st.rp++
	}
}

// match consumes name if the input has it, ignoring case.
func (st *strptimeState) match(name string) bool {
	for i := 0; i < len(name); i++ {
		c := at(st.s, st.rp+i)
		if c == 0 || Tolower(int(c)) != Tolower(int(name[i])) {
			return false
		}
	}
	st.rp += len(name)
	return true
}

// number reads an integer of at most n digits in [from, to], after any
// leading whitespace.
func (st *strptimeState) number(from, to, n int) (int, bool) {
	st.skipSpace()
	if c := st.cur(); c < '0' || c > '9' {
		return 0, false
	}
	val := 0
	for {
		val = val*10 + int(st.cur()-'0')
		st.rp++
		n--
		if c := st.cur(); !(n > 0 && val*10 <= to && c >= '0' && c <= '9') {
			break
		}
	}
	return val, val >= from && val <= to
}

// name matches one of names (full form first, then the three-letter form for
// each) and returns its index.
func (st *strptimeState) name(names []string) (int, bool) {
	for i, n := range names {
		if st.match(n) || st.match(n[:3]) {
			return i, true
		}
	}
	return 0, false
}

func (st *strptimeState) parse(format string, tm *Tm) bool {
	for i := 0; i < len(format); {
		c := format[i]
		i++
		if Isspace(int(c)) { // whitespace in the format matches any amount of it
			st.skipSpace()
			continue
		}
		if c != '%' {
			if st.cur() != c {
				return false
			}
			st.rp++
			continue
		}
		// strftime flags and a field width are accepted and ignored
		for i < len(format) && (format[i] == '-' || format[i] == '_' || format[i] == '0' || format[i] == '^' || format[i] == '#') {
			i++
		}
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}
		if i >= len(format) {
			return false
		}
		conv := format[i]
		i++
		switch conv {
		case 'E', 'O':
			if i >= len(format) {
				return false
			}
			next := format[i]
			i++
			valid := "cCxXY"
			if conv == 'O' {
				valid = "deHImMSUVwWy"
			}
			if !containsByte(valid, next) {
				return false
			}
			conv = next
		}
		if !st.convert(conv, tm) {
			return false
		}
	}
	return true
}

func containsByte(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func (st *strptimeState) convert(conv byte, tm *Tm) bool {
	var ok bool
	var v int
	switch conv {
	case '%':
		if st.cur() != '%' {
			return false
		}
		st.rp++
	case 'a', 'A':
		if v, ok = st.name(weekdayNames[:]); !ok {
			return false
		}
		tm.Wday, st.haveWday = v, true
	case 'b', 'B', 'h':
		if v, ok = st.name(monthNames[:]); !ok {
			return false
		}
		tm.Mon, st.haveMon, st.wantXday = v, true, true
	case 'c':
		st.wantXday = true
		return st.parse("%a %b %e %H:%M:%S %Y", tm)
	case 'C':
		if v, ok = st.number(0, 99, 2); !ok {
			return false
		}
		st.century, st.wantXday = v, true
	case 'd', 'e':
		if v, ok = st.number(1, 31, 2); !ok {
			return false
		}
		tm.Mday, st.haveMday, st.wantXday = v, true, true
	case 'F':
		st.wantXday = true
		return st.parse("%Y-%m-%d", tm)
	case 'D', 'x':
		st.wantXday = true
		return st.parse("%m/%d/%y", tm)
	case 'k', 'H':
		if v, ok = st.number(0, 23, 2); !ok {
			return false
		}
		tm.Hour, st.haveI = v, false
	case 'l', 'I':
		if v, ok = st.number(1, 12, 2); !ok {
			return false
		}
		tm.Hour, st.haveI = v%12, true
	case 'j':
		if v, ok = st.number(1, 366, 3); !ok {
			return false
		}
		tm.Yday, st.haveYday = v-1, true
	case 'm':
		if v, ok = st.number(1, 12, 2); !ok {
			return false
		}
		tm.Mon, st.haveMon, st.wantXday = v-1, true, true
	case 'M':
		if v, ok = st.number(0, 59, 2); !ok {
			return false
		}
		tm.Min = v
	case 'n', 't':
		st.skipSpace()
	case 'p':
		switch {
		case st.match("AM"):
			st.isPM = false
		case st.match("PM"):
			st.isPM = true
		default:
			return false
		}
	case 'r':
		return st.parse("%I:%M:%S %p", tm)
	case 'R':
		return st.parse("%H:%M", tm)
	case 's':
		if c := st.cur(); c < '0' || c > '9' {
			return false
		}
		var secs int64
		for c := st.cur(); c >= '0' && c <= '9'; c = st.cur() {
			secs = secs*10 + int64(c-'0')
			st.rp++
		}
		LocaltimeR(secs, tm)
	case 'S':
		if v, ok = st.number(0, 61, 2); !ok {
			return false
		}
		tm.Sec = v
	case 'T', 'X':
		return st.parse("%H:%M:%S", tm)
	case 'u':
		if v, ok = st.number(1, 7, 1); !ok {
			return false
		}
		tm.Wday, st.haveWday = v%7, true
	case 'g':
		if _, ok = st.number(0, 99, 2); !ok {
			return false
		}
	case 'G':
		if c := st.cur(); c < '0' || c > '9' {
			return false
		}
		for c := st.cur(); c >= '0' && c <= '9'; c = st.cur() {
			st.rp++
		}
	case 'U':
		if v, ok = st.number(0, 53, 2); !ok {
			return false
		}
		st.weekNo, st.haveUweek = v, true
	case 'W':
		if v, ok = st.number(0, 53, 2); !ok {
			return false
		}
		st.weekNo, st.haveWweek = v, true
	case 'V':
		if _, ok = st.number(0, 53, 2); !ok {
			return false
		}
	case 'w':
		if v, ok = st.number(0, 6, 1); !ok {
			return false
		}
		tm.Wday, st.haveWday = v, true
	case 'y':
		if v, ok = st.number(0, 99, 2); !ok {
			return false
		}
		if v < 69 {
			v += 100
		}
		tm.Year, st.wantCentury, st.wantXday = v, true, true
	case 'Y':
		if v, ok = st.number(0, 9999, 4); !ok {
			return false
		}
		tm.Year, st.wantCentury, st.wantXday = v-1900, false, true
	case 'Z':
		st.skipSpace()
		for c := st.cur(); c != 0 && !Isspace(int(c)); c = st.cur() {
			st.rp++
		}
	case 'z':
		return st.parseZone(tm)
	default:
		return false
	}
	return true
}

// parseZone reads a UTC offset: Z, +hh, +hhmm or +hh:mm.
func (st *strptimeState) parseZone(tm *Tm) bool {
	st.skipSpace()
	if st.cur() == 'Z' {
		st.rp++
		tm.Gmtoff = 0
		return true
	}
	if c := st.cur(); c != '+' && c != '-' {
		return false
	}
	neg := st.cur() == '-'
	st.rp++
	val, n := 0, 0
	for n < 4 && st.cur() >= '0' && st.cur() <= '9' {
		val = val*10 + int(st.cur()-'0')
		st.rp++
		n++
		if st.cur() == ':' && n == 2 {
			if d := at(st.s, st.rp+1); d >= '0' && d <= '9' {
				st.rp++
			}
		}
	}
	switch {
	case n == 2:
		val *= 100
	case n != 4 || val%100 >= 60:
		return false
	}
	tm.Gmtoff = int64(val/100*3600 + val%100*60)
	if neg {
		tm.Gmtoff = -tm.Gmtoff
	}
	return true
}
