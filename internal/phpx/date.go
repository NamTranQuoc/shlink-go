package phpx

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrMalformedDate mirrors PHP's DateMalformedStringException, which upstream lets bubble up as a 500 error.
var ErrMalformedDate = errors.New("failed to parse time string")

var (
	isoDateTime = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})(?:[T ](\d{1,2}):(\d{2})(?::(\d{2})(?:[.,](\d{1,9}))?)?)?\s*(Z|z|UTC|GMT|[+-]\d{2}:?\d{2}|[+-]\d{2})?$`)
	yearMonth   = regexp.MustCompile(`^\d{4}-\d{1,2}$`)
	relative    = regexp.MustCompile(`^([+-]?\s*\d+)\s*(sec|secs|second|seconds|min|mins|minute|minutes|hour|hours|day|days|week|weeks|fortnight|fortnights|month|months|year|years)(\s+ago)?$`)
)

// ParseDate is a pragmatic port of Chronos::parse() / PHP's DateTime constructor for the formats Shlink clients send:
// ISO-8601 dates and datetimes (with or without offset), "@timestamp", and the common relative formats ("now",
// "today", "tomorrow", "yesterday", "midnight", "noon", "+1 day", "2 weeks ago", ...). Values without an offset are
// read in loc. Anything else returns ErrMalformedDate.
func ParseDate(s string, loc *time.Location, now time.Time) (time.Time, error) {
	now = now.In(loc)
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "", "now":
		return now, nil
	case "today", "midnight":
		return startOfDay(now), nil
	case "tomorrow":
		return startOfDay(now).AddDate(0, 0, 1), nil
	case "yesterday":
		return startOfDay(now).AddDate(0, 0, -1), nil
	case "noon":
		d := startOfDay(now)
		return d.Add(12 * time.Hour), nil
	}
	if strings.HasPrefix(v, "@") {
		n, err := strconv.ParseInt(v[1:], 10, 64)
		if err != nil {
			return time.Time{}, ErrMalformedDate
		}
		return time.Unix(n, 0).In(time.UTC), nil
	}
	trimmed := strings.TrimSpace(s)
	if yearMonth.MatchString(trimmed) {
		// gnudateshorter: the first day of the month
		trimmed += "-01"
	}
	if m := isoDateTime.FindStringSubmatch(trimmed); m != nil {
		return parseISO(m, loc)
	}
	if m := relative.FindStringSubmatch(v); m != nil {
		n, _ := strconv.Atoi(strings.ReplaceAll(m[1], " ", ""))
		if m[3] != "" {
			n = -n
		}
		return addRelative(now, n, m[2]), nil
	}
	return time.Time{}, ErrMalformedDate
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func parseISO(m []string, loc *time.Location) (time.Time, error) {
	atoi := func(s string) int {
		n, _ := strconv.Atoi(s)
		return n
	}
	year, month, day := atoi(m[1]), atoi(m[2]), atoi(m[3])
	hour, minute, sec := atoi(m[4]), atoi(m[5]), atoi(m[6])
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 24 || minute > 59 || sec > 60 {
		return time.Time{}, ErrMalformedDate
	}
	nsec := 0
	if m[7] != "" {
		frac := (m[7] + "000000000")[:9]
		nsec = atoi(frac)
	}
	zone := loc
	switch tz := m[8]; {
	case tz == "":
	case strings.EqualFold(tz, "z") || strings.EqualFold(tz, "utc") || strings.EqualFold(tz, "gmt"):
		zone = time.UTC
	default:
		sign := 1
		if tz[0] == '-' {
			sign = -1
		}
		digits := strings.ReplaceAll(tz[1:], ":", "")
		h := atoi(digits[:2])
		mm := 0
		if len(digits) >= 4 {
			mm = atoi(digits[2:4])
		}
		zone = time.FixedZone("", sign*(h*3600+mm*60))
	}
	// time.Date normalizes overflowing values (e.g. day 31 in a 30-day month) like PHP does.
	return time.Date(year, time.Month(month), day, hour, minute, sec, nsec, zone), nil
}

func addRelative(t time.Time, n int, unit string) time.Time {
	switch {
	case strings.HasPrefix(unit, "sec"):
		return t.Add(time.Duration(n) * time.Second)
	case strings.HasPrefix(unit, "min"):
		return t.Add(time.Duration(n) * time.Minute)
	case strings.HasPrefix(unit, "hour"):
		return t.Add(time.Duration(n) * time.Hour)
	case strings.HasPrefix(unit, "day"):
		return t.AddDate(0, 0, n)
	case strings.HasPrefix(unit, "fortnight"):
		return t.AddDate(0, 0, 14*n)
	case strings.HasPrefix(unit, "week"):
		return t.AddDate(0, 0, 7*n)
	case strings.HasPrefix(unit, "month"):
		return t.AddDate(0, n, 0)
	default:
		return t.AddDate(n, 0, 0)
	}
}

// AtomString formats like Chronos::toAtomString() (DATE_ATOM), in the time's own location.
func AtomString(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-07:00")
}

// DateTimeString formats like Chronos::toDateTimeString().
func DateTimeString(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
