//go:build linux

package golibc

import "strconv"

// time.h calendar functions: broken-down time, conversion and formatting.
//
// Gmtime, Timegm and Strftime are implemented here from scratch and need no
// time-zone data. Localtime and Mktime find their zone through tz.go, which
// reads the TZ environment variable as glibc does: a zone name or file from
// the standard library's time package, /etc/localtime when TZ is unset, or a
// POSIX rule string such as "EST5EDT,M3.2.0,M11.1.0".

// Tm is a broken-down time, like C's struct tm.
type Tm struct {
	Sec, Min, Hour int
	Mday           int // day of the month, 1-31
	Mon            int // months since January, 0-11
	Year           int // years since 1900
	Wday           int // days since Sunday, 0-6
	Yday           int // days since January 1, 0-365
	Isdst          int // >0 daylight saving time in effect, 0 not, <0 unknown
	Gmtoff         int64
	Zone           string
}

var (
	weekdayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	monthNames   = [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

// daysFromCivil returns the days since 1970-01-01 of a proleptic Gregorian date.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (m + 9) % 12 // March = 0
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// civilFromDays is the inverse of daysFromCivil.
func civilFromDays(z int64) (y int64, m, d int) {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = int(doy - (153*mp+2)/5 + 1)
	m = int(mp + 3)
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return
}

// setCalendar sets the calendar fields of tm from a count of seconds since the epoch
// that is already shifted into the target zone's local time.
func setCalendar(tm *Tm, local int64) {
	days := floorDiv(local, 86400)
	rem := int(local - days*86400)
	y, m, d := civilFromDays(days)
	tm.Sec, tm.Min, tm.Hour = rem%60, rem/60%60, rem/3600
	tm.Year, tm.Mon, tm.Mday = int(y-1900), m-1, d
	tm.Wday = int(floorMod(days+4, 7)) // 1970-01-01 was a Thursday
	tm.Yday = int(days - daysFromCivil(y, 1, 1))
}

// GmtimeR converts t (seconds since the epoch) to UTC broken-down time in *tm and returns tm.
func GmtimeR(t int64, tm *Tm) *Tm {
	setCalendar(tm, t)
	tm.Isdst, tm.Gmtoff, tm.Zone = 0, 0, "GMT"
	return tm
}

// Gmtime is GmtimeR into a fresh Tm.
func Gmtime(t int64) *Tm { return GmtimeR(t, new(Tm)) }

// Timegm converts UTC broken-down time to seconds since the epoch. Fields out
// of range are normalized (month 12 becomes January of the next year, and so
// on) and tm is updated to the normalized values.
func Timegm(tm *Tm) int64 {
	y := int64(tm.Year) + 1900 + floorDiv(int64(tm.Mon), 12)
	m := floorMod(int64(tm.Mon), 12) + 1
	days := daysFromCivil(y, m, 1) + int64(tm.Mday) - 1
	t := days*86400 + int64(tm.Hour)*3600 + int64(tm.Min)*60 + int64(tm.Sec)
	GmtimeR(t, tm)
	return t
}

// LocaltimeR converts t to broken-down time in the local zone. Like glibc's
// localtime_r it reads TZ once; call Tzset to read it again.
func LocaltimeR(t int64, tm *Tm) *Tm { return localtimeIn(currentZone(false), t, tm) }

// Localtime is LocaltimeR into a fresh Tm. Like glibc's localtime it notices
// when TZ has changed.
func Localtime(t int64) *Tm { return localtimeIn(currentZone(true), t, new(Tm)) }

func localtimeIn(z *zone, t int64, tm *Tm) *Tm {
	name, off, dst := z.lookup(t)
	setCalendar(tm, t+int64(off))
	tm.Gmtoff, tm.Zone = int64(off), name
	tm.Isdst = 0
	if dst {
		tm.Isdst = 1
	}
	return tm
}

// Mktime converts local broken-down time to seconds since the epoch,
// normalizing out-of-range fields and updating tm (including Wday, Yday, Isdst
// and the zone). A time skipped by a daylight-saving change (02:30 on a
// spring-forward day) is read with the offset in force before the change, so
// it becomes 03:30 daylight time, as in glibc. A time that occurs twice (a
// fall-back hour) is resolved by the Isdst hint (1 for the daylight reading,
// 0 for standard) and otherwise taken as its first occurrence. glibc's choice
// without a hint depends on earlier calls, so this is the one place the two
// are not guaranteed to agree.
func Mktime(tm *Tm) int64 {
	z := currentZone(true)
	probe := Tm{Sec: tm.Sec, Min: tm.Min, Hour: tm.Hour, Mday: tm.Mday, Mon: tm.Mon, Year: tm.Year}
	wall := Timegm(&probe) // the wall-clock reading as if it were UTC
	offAt := func(t int64) int { _, off, _ := z.lookup(t); return off }
	before, after := offAt(wall-86400), offAt(wall+86400)
	valid := func(off int) bool { return offAt(wall-int64(off)) == off }
	off := before
	switch {
	case valid(before) && valid(after) && before != after: // a fall-back hour: the time occurs twice
		if tm.Isdst >= 0 { // honor the caller's hint; with none, take the first occurrence
			if _, _, dst := z.lookup(wall - int64(before)); dst != (tm.Isdst > 0) {
				off = after
			}
		}
	case !valid(before) && valid(after):
		off = after
	}
	t := wall - int64(off)
	localtimeIn(z, t, tm)
	return t
}

func Difftime(t1, t0 int64) float64 { return float64(t1 - t0) }

// isoWeek returns the ISO 8601 week-based year and week number of tm.
func isoWeek(tm *Tm) (year, week int) {
	year = tm.Year + 1900
	week = (tm.Yday - (tm.Wday+6)%7 + 10) / 7
	switch {
	case week < 1:
		year--
		week = weeksInYear(year)
	case week > weeksInYear(year):
		year++
		week = 1
	}
	return
}

func weeksInYear(y int) int {
	p := func(y int) int { return (y + y/4 - y/100 + y/400) % 7 }
	if p(y) == 4 || p(y-1) == 3 {
		return 53
	}
	return 52
}

func pad(n int, width int, padc byte) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = string(padc) + s
	}
	return s
}

func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c - 'A' + 'a'
		}
	}
	return string(b)
}

func hour12(h int) int {
	if h%12 == 0 {
		return 12
	}
	return h % 12
}

// strftimeSub formats a composite conversion by expanding its equivalent format.
func strftimeSub(format string, tm *Tm) string { return string(strftime(nil, CStr(format), tm)) }

func strftime(dst []byte, f *byte, tm *Tm) []byte {
	for i := 0; ; {
		c := at(f, i)
		if c == 0 {
			return dst
		}
		i++
		if c != '%' {
			dst = append(dst, c)
			continue
		}
		start := i - 1
		var padFlag byte
		var upper, swap bool
	flags:
		for {
			switch at(f, i) {
			case '_', '-', '0':
				padFlag = at(f, i)
			case '^':
				upper = true
			case '#':
				swap = true
			default:
				break flags
			}
			i++
		}
		width := -1
		for Isdigit(int(at(f, i))) {
			if width < 0 {
				width = 0
			}
			width = width*10 + int(at(f, i)-'0')
			i++
		}
		if m := at(f, i); m == 'E' || m == 'O' { // locale modifiers: no effect in the C locale
			i++
		}
		conv := at(f, i)
		if conv == 0 {
			return append(dst, f2s(f, start)...) // dangling '%' is copied through
		}
		i++

		var text string
		var num, defWidth int
		var defPad byte
		numeric := false
		isText := true
		textCase := byte(0) // 'u' upper, 'l' lower under the '#' flag
		year := tm.Year + 1900

		switch conv {
		case '%':
			text = "%"
		case 'n':
			text = "\n"
		case 't':
			text = "\t"
		case 'a', 'A', 'h', 'b', 'B':
			var name string
			if conv == 'a' || conv == 'A' {
				name = weekdayNames[(tm.Wday%7+7)%7]
			} else {
				name = monthNames[(tm.Mon%12+12)%12]
			}
			if conv == 'a' || conv == 'h' || conv == 'b' {
				name = name[:3]
			}
			text, textCase = name, 'u'
		case 'p', 'P':
			text = "AM"
			if tm.Hour >= 12 {
				text = "PM"
			}
			if conv == 'P' {
				text = lowerASCII(text)
			} else {
				textCase = 'l'
			}
		case 'Z':
			if tm.Isdst >= 0 {
				text = tm.Zone
			}
			textCase = 'l'
		case 'z':
			if tm.Isdst >= 0 {
				off, sign := tm.Gmtoff, "+"
				if off < 0 {
					off, sign = -off, "-"
				}
				// glibc emits the sign as its own field-width-padded item, then the
				// number hhmm (default width 4, zero padded) under the same flags.
				fillc := byte(' ')
				if padFlag == '0' {
					fillc = '0'
				}
				if padFlag != '-' {
					for n := width - 1; n > 0; n-- {
						dst = append(dst, fillc)
					}
				}
				dst = append(dst, sign...)
				num, defWidth, defPad = int(off/3600)*100+int(off/60%60), 4, '0'
				isText, numeric = false, true
			} else {
				continue
			}
		case 'c':
			text = strftimeSub("%a %b %e %H:%M:%S %Y", tm)
		case 'D', 'x':
			text = strftimeSub("%m/%d/%y", tm)
		case 'F':
			text = strftimeSub("%Y-%m-%d", tm)
		case 'T', 'X':
			text = strftimeSub("%H:%M:%S", tm)
		case 'R':
			text = strftimeSub("%H:%M", tm)
		case 'r':
			text = strftimeSub("%I:%M:%S %p", tm)
		default:
			isText = false
			numeric = true
			defPad = '0'
			defWidth = 2
			switch conv {
			case 'C':
				num = int(floorDiv(int64(year), 100))
			case 'd':
				num = tm.Mday
			case 'e':
				num, defPad = tm.Mday, ' '
			case 'g':
				y, _ := isoWeek(tm)
				num = int(floorMod(int64(y), 100))
			case 'G':
				num, _ = isoWeek(tm)
				defWidth = 0
			case 'H':
				num = tm.Hour
			case 'I':
				num = hour12(tm.Hour)
			case 'j':
				num, defWidth = tm.Yday+1, 3
			case 'k':
				num, defPad = tm.Hour, ' '
			case 'l':
				num, defPad = hour12(tm.Hour), ' '
			case 'm':
				num = tm.Mon + 1
			case 'M':
				num = tm.Min
			case 'S':
				num = tm.Sec
			case 's':
				cp := *tm
				num, defWidth = int(Mktime(&cp)), 0
			case 'u':
				num, defWidth = tm.Wday, 1
				if num == 0 {
					num = 7
				}
			case 'U':
				num = (tm.Yday + 7 - tm.Wday) / 7
			case 'V':
				_, num = isoWeek(tm)
			case 'w':
				num, defWidth = tm.Wday, 1
			case 'W':
				num = (tm.Yday + 7 - (tm.Wday+6)%7) / 7
			case 'y':
				num = int(floorMod(int64(year), 100))
			case 'Y':
				num, defWidth = year, 0
			default: // unknown conversion: copy it through (padded like text if a width was given)
				text, isText, numeric = f2s(f, start)[:i-start], true, false
			}
		}

		if isText {
			if upper || (swap && textCase == 'u') {
				text = upperASCII(text)
			} else if swap && textCase == 'l' {
				text = lowerASCII(text)
			}
			fillc := byte(' ')
			if padFlag == '0' {
				fillc = '0'
			}
			for n := width - len(text); n > 0; n-- {
				dst = append(dst, fillc)
			}
			dst = append(dst, text...)
			continue
		}
		_ = numeric
		w, p := defWidth, defPad
		switch padFlag {
		case '-':
			w = 0
		case '_':
			p = ' '
		case '0':
			p = '0'
		}
		if width >= 0 && padFlag != '-' {
			w = width
		}
		dst = append(dst, pad(num, w, p)...)
	}
}

// f2s returns the C string at f from byte offset start.
func f2s(f *byte, start int) string { return GoStr(advance(f, start)) }

// Strftime formats tm into dst according to the C format string format and
// returns the number of bytes written, not counting the NUL. If the result and
// its NUL do not fit in max bytes it returns 0 and the contents of dst are
// undefined, as in C.
//
// Supported conversions are a A b B c C d D e F g G h H I j k l m M n p P r R s S t
// T u U V w W x X y Y z Z and %%, with the glibc flags _ - 0 ^ # and a field
// width. E and O modifiers are accepted and ignored.
func Strftime(dst *byte, max int, format *byte, tm *Tm) int {
	out := strftime(nil, format, tm)
	if len(out)+1 > max {
		return 0
	}
	for i, c := range out {
		put(dst, i, c)
	}
	put(dst, len(out), 0)
	return len(out)
}

// Asctime formats tm like "Thu Jan  1 00:00:00 1970\n". The result is Go-managed.
func Asctime(tm *Tm) *byte { return CStr(strftimeSub("%a %b %e %H:%M:%S %Y\n", tm)) }

// Ctime is Asctime(Localtime(t)).
func Ctime(t int64) *byte { return Asctime(Localtime(t)) }
