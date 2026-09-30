package domain

import (
	"fmt"
	"math"
	"time"
)

// Cycle is the interval of a recurring payment in days. Month-based cycles are
// stored with their average length (30, 61, 91, 182, 365) but stepped
// calendar-exactly, so a payment on the 15th stays on the 15th.
type Cycle int

const (
	Weekly     Cycle = 7
	Biweekly   Cycle = 14
	Monthly    Cycle = 30
	Bimonthly  Cycle = 61
	Quarterly  Cycle = 91
	HalfYearly Cycle = 182
	Yearly     Cycle = 365
)

// Cycles lists the supported cycles, shortest first.
var Cycles = []Cycle{Weekly, Biweekly, Monthly, Bimonthly, Quarterly, HalfYearly, Yearly}

// Valid reports whether c is one of the supported cycles.
func (c Cycle) Valid() bool {
	for _, x := range Cycles {
		if c == x {
			return true
		}
	}
	return false
}

// months returns the length in calendar months for month-based cycles, else 0.
func (c Cycle) months() int {
	switch c {
	case Monthly:
		return 1
	case Bimonthly:
		return 2
	case Quarterly:
		return 3
	case HalfYearly:
		return 6
	case Yearly:
		return 12
	}
	return 0
}

// MonthBased reports whether the cycle is counted in calendar months.
func (c Cycle) MonthBased() bool { return c.months() > 0 }

// Step moves d by k cycles (k may be negative).
func (c Cycle) Step(d time.Time, k int) time.Time {
	if m := c.months(); m > 0 {
		return d.AddDate(0, m*k, 0)
	}
	return d.AddDate(0, 0, int(c)*k)
}

// PerYear returns how many payments fall into a year.
func (c Cycle) PerYear() float64 {
	if m := c.months(); m > 0 {
		return 12 / float64(m)
	}
	switch c {
	case Weekly:
		return 52
	case Biweekly:
		return 26
	}
	if c <= 0 {
		return 0
	}
	return 365 / float64(c)
}

// MonthlyCents converts an amount per payment into an amount per month.
func (c Cycle) MonthlyCents(cents int64) int64 {
	return int64(math.Round(float64(cents) * c.PerYear() / 12))
}

// Label returns the German name of the cycle for the UI.
func (c Cycle) Label() string {
	switch c {
	case Weekly:
		return "Wöchentlich"
	case Biweekly:
		return "Alle 2 Wochen"
	case Monthly:
		return "Monatlich"
	case Bimonthly:
		return "Alle 2 Monate"
	case Quarterly:
		return "Vierteljährlich"
	case HalfYearly:
		return "Halbjährlich"
	case Yearly:
		return "Jährlich"
	}
	return fmt.Sprintf("Alle %d Tage", int(c))
}
