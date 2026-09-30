package domain

import "time"

// Date formats used in the database, the API and budget month labels.
const (
	DateLayout  = "2006-01-02" // a day, e.g. "2026-09-30"
	MonthLayout = "2006-01"    // a (budget) month, e.g. "2026-10"
)

// Day returns midnight UTC of t's calendar day. All date arithmetic in the app
// works on such days, so that time zones and daylight saving time don't matter.
func Day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Today returns the current day (see Day).
func Today() time.Time { return Day(time.Now()) }

// ParseDate parses "YYYY-MM-DD" and returns the zero time for anything else.
func ParseDate(s string) time.Time {
	t, _ := time.Parse(DateLayout, s)
	return t
}

// ValidDate reports whether s is a date in DateLayout.
func ValidDate(s string) bool {
	_, err := time.Parse(DateLayout, s)
	return err == nil
}

// ValidMonth reports whether s is a month in MonthLayout.
func ValidMonth(s string) bool {
	_, err := time.Parse(MonthLayout, s)
	return err == nil
}

// FirstOfMonth returns the first day of t's month.
func FirstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// DaysBetween returns the number of whole days from a to b (rounded).
func DaysBetween(a, b time.Time) int {
	return int(b.Sub(a).Hours()/24 + 0.5)
}
