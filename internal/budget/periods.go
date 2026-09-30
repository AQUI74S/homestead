// Package budget computes the household budget: how budget months are cut,
// budget vs. actual per category, averages and suggestions, and the forecast of
// what is still coming in and going out until the end of the month.
package budget

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

// Budget periods: by default a "month" runs from one salary payment to the next.
// A period is named after the month it mostly covers:
// salary on 28 Sep -> period "October" (28 Sep to 27 Oct).

// Periods computes period boundaries from the actual salary dates.
//
// Which payments start a period:
//   - selected salary series (setting "salary_series", comma-separated IDs):
//     the first of them to arrive for a period starts it;
//   - otherwise automatically: the first large income (at least autoMinShare of the
//     typical monthly income) booked between the 22nd and the 5th, i.e. around the
//     turn of the month.
type Periods struct {
	Mode       domain.PeriodMode
	Auto       bool                 // salary mode without selected series
	SeriesIDs  []int64              // recurring series whose payments start periods
	SeriesName string               // description for the UI
	byLabel    map[string]time.Time // month label -> first salary date
	offset     int                  // usual start in days relative to the 1st of the label month
	today      time.Time            // reference day for salaries that are due but not booked yet
}

const (
	// salaryGraceDays is how long a new period waits for a late salary before it
	// starts on the usual salary day anyway.
	salaryGraceDays = 7

	// salaryLeadDays: a salary this many days before the 1st funds that month.
	salaryLeadDays = 10

	// Automatic detection: window around the turn of the month, minimum share of the
	// typical monthly income, and how many past months define "typical".
	autoWindowFrom     = 22 // day of month (inclusive) ...
	autoWindowTo       = 5  // ... until this day of the following month
	autoMinShare       = 0.3
	autoTypicalMonths  = 6
	maxSeriesNameParts = 3
)

// SalaryLabel maps a salary date to the month it funds
// (salary on 28 Sep -> "2026-10").
func SalaryLabel(d time.Time) string {
	return d.AddDate(0, 0, salaryLeadDays).Format(domain.MonthLayout)
}

// ParseSeriesIDs parses the comma-separated salary series setting.
func ParseSeriesIDs(v string) []int64 {
	var ids []int64
	for _, f := range strings.Split(v, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// LoadPeriods reads the settings and salary payments and returns the period calculator.
func LoadPeriods(ctx context.Context, st *store.Store) (*Periods, error) {
	settings, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	p := &Periods{Mode: settings.PeriodMode(), byLabel: map[string]time.Time{}, today: domain.Today()}
	if p.Mode == domain.PeriodCalendar {
		return p, nil
	}
	ids := ParseSeriesIDs(settings[domain.SettingSalarySeries])
	pays, err := st.IncomePayments(ctx, ids)
	if err != nil {
		return nil, err
	}
	p.Auto = len(ids) == 0
	if p.Auto {
		pays = p.largeAroundMonthChange(pays)
	}
	p.apply(pays)
	return p, nil
}

// largeAroundMonthChange keeps incomes of at least autoMinShare of the typical monthly
// income (median of the last complete calendar months) that arrive around the turn
// of the month.
func (p *Periods) largeAroundMonthChange(pays []store.IncomePayment) []store.IncomePayment {
	thisMonth := domain.FirstOfMonth(p.today)
	sums := map[string]int64{}
	for _, x := range pays {
		if x.Date.Before(thisMonth) && !x.Date.Before(thisMonth.AddDate(0, -autoTypicalMonths, 0)) {
			sums[x.Date.Format(domain.MonthLayout)] += x.Cents
		}
	}
	var monthly []int64
	for _, v := range sums {
		monthly = append(monthly, v)
	}
	if len(monthly) == 0 {
		return nil
	}
	sort.Slice(monthly, func(i, j int) bool { return monthly[i] < monthly[j] })
	minCents := int64(float64(monthly[len(monthly)/2]) * autoMinShare)
	var out []store.IncomePayment
	for _, x := range pays {
		if d := x.Date.Day(); x.Cents >= minCents && (d >= autoWindowFrom || d <= autoWindowTo) {
			out = append(out, x)
		}
	}
	return out
}

// apply takes the first payment per period as its start and derives the usual offset.
func (p *Periods) apply(pays []store.IncomePayment) {
	starter := map[string]store.IncomePayment{}
	for _, x := range pays {
		l := SalaryLabel(x.Date)
		if old, ok := starter[l]; !ok || x.Date.Before(old.Date) {
			starter[l] = x
		}
	}
	if len(starter) == 0 {
		p.Mode = domain.PeriodCalendar // no salary found yet
		return
	}
	var offsets []int
	seen := map[int64]bool{}
	var names []string
	for l, x := range starter {
		p.byLabel[l] = x.Date
		first, _ := time.Parse(domain.MonthLayout, l)
		offsets = append(offsets, int(x.Date.Sub(first).Hours()/24))
		if x.RecurringID > 0 && !seen[x.RecurringID] {
			seen[x.RecurringID] = true
			p.SeriesIDs = append(p.SeriesIDs, x.RecurringID)
			if x.Label != "" && len(names) < maxSeriesNameParts {
				names = append(names, x.Label)
			}
		}
	}
	sort.Ints(offsets)
	p.offset = offsets[len(offsets)/2]
	sort.Strings(names)
	p.SeriesName = strings.Join(names, ", ")
}

// start returns the first day of the period with this label.
func (p *Periods) start(label time.Time) time.Time {
	first := domain.FirstOfMonth(label)
	if p.Mode == domain.PeriodCalendar {
		return first
	}
	if d, ok := p.byLabel[first.Format(domain.MonthLayout)]; ok {
		return d
	}
	// extrapolate using the usual start offset
	c := first.AddDate(0, 0, p.offset)
	// The salary is due (or a few days overdue) but not booked yet: the new period
	// only starts once it arrives, so today still belongs to the old one.
	if !p.today.IsZero() && !c.After(p.today) && p.today.Sub(c) <= salaryGraceDays*24*time.Hour {
		return p.today.AddDate(0, 0, 1)
	}
	return c
}

// ErrMonth reports a month that is not in the format "YYYY-MM".
type ErrMonth string

func (e ErrMonth) Error() string { return fmt.Sprintf("Monat %q: erwartet JJJJ-MM", string(e)) }

func parseMonth(month string) (time.Time, error) {
	m, err := time.Parse(domain.MonthLayout, month)
	if err != nil {
		return m, ErrMonth(month)
	}
	return m, nil
}

// Range returns the start (inclusive) and end (exclusive) of period "YYYY-MM".
func (p *Periods) Range(month string) (time.Time, time.Time, error) {
	m, err := parseMonth(month)
	if err != nil {
		return m, m, err
	}
	start := p.start(m)
	end := p.start(m.AddDate(0, 1, 0))
	if !end.After(start) {
		end = start.AddDate(0, 1, 0)
	}
	return start, end, nil
}

// Current returns the label of the period containing the given day.
func (p *Periods) Current(day time.Time) string {
	day = domain.Day(day)
	base := domain.FirstOfMonth(day)
	for _, off := range []int{0, 1, -1} {
		l := base.AddDate(0, off, 0).Format(domain.MonthLayout)
		s, e, _ := p.Range(l)
		if !day.Before(s) && day.Before(e) {
			return l
		}
	}
	return base.Format(domain.MonthLayout)
}

// MonthOrCurrent returns month if it is a valid "YYYY-MM", else the current period.
func (p *Periods) MonthOrCurrent(month string) string {
	if domain.ValidMonth(month) {
		return month
	}
	return p.Current(time.Now())
}

// Period describes a budget period for the UI.
type Period struct {
	Month  string            `json:"month"`
	Start  string            `json:"start"`
	End    string            `json:"end"` // last day (inclusive)
	Mode   domain.PeriodMode `json:"mode"`
	Auto   bool              `json:"auto"`
	Salary string            `json:"salary_series"`
}

func (p *Periods) Describe(month string) Period {
	s, e, _ := p.Range(month)
	return Period{Month: month, Start: s.Format(domain.DateLayout), End: e.AddDate(0, 0, -1).Format(domain.DateLayout),
		Mode: p.Mode, Auto: p.Auto, Salary: p.SeriesName}
}
