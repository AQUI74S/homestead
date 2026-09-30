package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Budget periods: by default a "month" runs from one salary payment to the next.
// A period is named after the month it mostly covers:
// salary on 28 Sep -> period "October" (28 Sep to 27 Oct).

// PeriodCalc computes period boundaries from the actual salary dates.
//
// Which payments start a period:
//   - selected salary series (setting "salary_series", comma-separated IDs):
//     the first of them to arrive for a period starts it;
//   - otherwise automatically: the first large income (at least 30 % of the typical
//     monthly income) booked between the 22nd and the 5th, i.e. around the turn of
//     the month.
type PeriodCalc struct {
	Mode       string  // salary | calendar
	Auto       bool    // salary mode without selected series
	SeriesIDs  []int64 // recurring series whose payments start periods
	SeriesName string  // description for the UI
	byLabel    map[string]time.Time // month label -> first salary date
	offset     int                  // usual start in days relative to the 1st of the label month
	today      time.Time            // reference day for salaries that are due but not booked yet
}

// salaryGraceDays is how long a new period waits for a late salary before it
// starts on the usual salary day anyway.
const salaryGraceDays = 7

// Automatic detection: window around the turn of the month and minimum share of the
// typical monthly income.
const (
	autoWindowFrom = 22 // day of month (inclusive) ...
	autoWindowTo   = 5  // ... until this day of the following month
	autoMinShare   = 0.3
)

// SalaryLabel maps a salary date to the month it funds
// (salary on 28 Sep -> "2026-10").
func SalaryLabel(d time.Time) string { return d.AddDate(0, 0, 10).Format("2006-01") }

func labelOf(d time.Time) string { return SalaryLabel(d) }

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

type salaryPayment struct {
	date        time.Time
	cents       int64
	recurringID int64
	label       string // recurring series label
}

func (s *Store) PeriodCalc(ctx context.Context) (*PeriodCalc, error) {
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	pc := &PeriodCalc{Mode: st["period_mode"], byLabel: map[string]time.Time{},
		today: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)}
	if pc.Mode != "calendar" {
		pc.Mode = "salary"
	}
	if pc.Mode == "calendar" {
		return pc, nil
	}
	pays, err := s.incomePayments(ctx, ParseSeriesIDs(st["salary_series"]))
	if err != nil {
		return nil, err
	}
	pc.Auto = len(ParseSeriesIDs(st["salary_series"])) == 0
	if pc.Auto {
		pays = pc.largeAroundMonthChange(pays)
	}
	pc.apply(pays)
	return pc, nil
}

// incomePayments loads the candidate payments: those of the selected series, or –
// without a selection – all incomes (except refunds) on household accounts.
func (s *Store) incomePayments(ctx context.Context, ids []int64) ([]salaryPayment, error) {
	q := `SELECT t.booking_date, (t.amount*100)::bigint, COALESCE(t.recurring_id, 0), COALESCE(r.label, '')
		FROM transactions t JOIN accounts a ON a.id=t.account_id
		LEFT JOIN categories c ON c.id=t.category_id LEFT JOIN recurring r ON r.id=t.recurring_id
		WHERE a.active AND a.book='haushalt' AND t.amount > 0 AND `
	var args []any
	if len(ids) > 0 {
		q += `t.recurring_id = ANY($1::bigint[])`
		args = append(args, pq.Array(ids))
	} else {
		q += `c.grp = 'income' AND c.slug <> 'erstattung'`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY t.booking_date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []salaryPayment
	for rows.Next() {
		var p salaryPayment
		if err := rows.Scan(&p.date, &p.cents, &p.recurringID, &p.label); err != nil {
			return nil, err
		}
		p.date = time.Date(p.date.Year(), p.date.Month(), p.date.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, p)
	}
	return out, rows.Err()
}

// largeAroundMonthChange keeps incomes of at least autoMinShare of the typical monthly
// income (median of the last 6 complete calendar months) that arrive around the turn
// of the month.
func (pc *PeriodCalc) largeAroundMonthChange(pays []salaryPayment) []salaryPayment {
	thisMonth := time.Date(pc.today.Year(), pc.today.Month(), 1, 0, 0, 0, 0, time.UTC)
	sums := map[string]int64{}
	for _, p := range pays {
		if p.date.Before(thisMonth) && !p.date.Before(thisMonth.AddDate(0, -6, 0)) {
			sums[p.date.Format("2006-01")] += p.cents
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
	min := int64(float64(monthly[len(monthly)/2]) * autoMinShare)
	var out []salaryPayment
	for _, p := range pays {
		if d := p.date.Day(); p.cents >= min && (d >= autoWindowFrom || d <= autoWindowTo) {
			out = append(out, p)
		}
	}
	return out
}

// apply takes the first payment per period as its start and derives the usual offset.
func (pc *PeriodCalc) apply(pays []salaryPayment) {
	starter := map[string]salaryPayment{}
	for _, p := range pays {
		l := labelOf(p.date)
		if old, ok := starter[l]; !ok || p.date.Before(old.date) {
			starter[l] = p
		}
	}
	if len(starter) == 0 {
		pc.Mode = "calendar" // no salary found yet
		return
	}
	var offsets []int
	seen := map[int64]bool{}
	var names []string
	for l, p := range starter {
		pc.byLabel[l] = p.date
		first, _ := time.Parse("2006-01", l)
		offsets = append(offsets, int(p.date.Sub(first).Hours()/24))
		if p.recurringID > 0 && !seen[p.recurringID] {
			seen[p.recurringID] = true
			pc.SeriesIDs = append(pc.SeriesIDs, p.recurringID)
			if p.label != "" && len(names) < 3 {
				names = append(names, p.label)
			}
		}
	}
	sort.Ints(offsets)
	pc.offset = offsets[len(offsets)/2]
	sort.Strings(names)
	pc.SeriesName = strings.Join(names, ", ")
}

// start returns the first day of the period with this label.
func (pc *PeriodCalc) start(label time.Time) time.Time {
	first := time.Date(label.Year(), label.Month(), 1, 0, 0, 0, 0, time.UTC)
	if pc.Mode == "calendar" {
		return first
	}
	if d, ok := pc.byLabel[first.Format("2006-01")]; ok {
		return d
	}
	// extrapolate using the usual start offset
	c := first.AddDate(0, 0, pc.offset)
	// The salary is due (or a few days overdue) but not booked yet: the new period
	// only starts once it arrives, so today still belongs to the old one.
	if !pc.today.IsZero() && !c.After(pc.today) && pc.today.Sub(c) <= salaryGraceDays*24*time.Hour {
		return pc.today.AddDate(0, 0, 1)
	}
	return c
}

// Range returns the start (inclusive) and end (exclusive) of period "YYYY-MM".
func (pc *PeriodCalc) Range(month string) (time.Time, time.Time, error) {
	m, err := time.Parse("2006-01", month)
	if err != nil {
		return m, m, fmt.Errorf("Monat %q: erwartet JJJJ-MM", month)
	}
	start := pc.start(m)
	end := pc.start(m.AddDate(0, 1, 0))
	if !end.After(start) {
		end = start.AddDate(0, 1, 0)
	}
	return start, end, nil
}

// Current returns the label of the period containing the given day.
func (pc *PeriodCalc) Current(day time.Time) string {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	base := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, off := range []int{0, 1, -1} {
		l := base.AddDate(0, off, 0).Format("2006-01")
		s, e, _ := pc.Range(l)
		if !day.Before(s) && day.Before(e) {
			return l
		}
	}
	return base.Format("2006-01")
}

// Period describes a budget period for the UI.
type Period struct {
	Month    string `json:"month"`
	Start    string `json:"start"`
	End      string `json:"end"` // last day (inclusive)
	Mode   string `json:"mode"`
	Auto   bool   `json:"auto"`
	Salary string `json:"salary_series"`
}

func (pc *PeriodCalc) Describe(month string) Period {
	s, e, _ := pc.Range(month)
	return Period{Month: month, Start: s.Format("2006-01-02"), End: e.AddDate(0, 0, -1).Format("2006-01-02"),
		Mode: pc.Mode, Auto: pc.Auto, Salary: pc.SeriesName}
}
