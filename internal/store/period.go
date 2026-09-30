package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Budget periods: by default a "month" runs from one salary payment to the next.
// A period is named after the month it mostly covers:
// salary on 28 Sep -> period "October" (28 Sep to 27 Oct).

// PeriodCalc computes period boundaries from the actual salary dates.
type PeriodCalc struct {
	Mode       string // salary | calendar
	SeriesID   int64  // salary series used (0 = none found)
	SeriesName string
	byLabel    map[string]time.Time // month label -> actual salary date
	typDay     int                  // usual day of month (for extrapolation)
	today      time.Time            // reference day for salaries that are due but not booked yet
}

// salaryGraceDays is how long a new period waits for a late salary before it
// starts on the usual salary day anyway.
const salaryGraceDays = 7

// labelOf maps a salary date to the month it funds.
func labelOf(d time.Time) string { return d.AddDate(0, 0, 10).Format("2006-01") }

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
	id, _ := strconv.ParseInt(st["salary_series"], 10, 64)
	q := `SELECT id, label FROM recurring WHERE id=$1`
	args := []any{id}
	if id == 0 {
		// automatic: largest regular monthly income
		q = `SELECT id, label FROM recurring WHERE direction='in' AND kind='einkommen' AND status<>'ignored' AND cycle_days=30
			AND (account_id IS NULL OR account_id IN (SELECT id FROM accounts WHERE book='haushalt'))
			ORDER BY ended, avg_amount DESC LIMIT 1`
		args = nil
	}
	err = s.DB.QueryRowContext(ctx, q, args...).Scan(&pc.SeriesID, &pc.SeriesName)
	if err == sql.ErrNoRows {
		pc.Mode = "calendar" // no salary detected yet
		return pc, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT booking_date FROM transactions WHERE recurring_id=$1 ORDER BY 1`, pc.SeriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []int
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		l := labelOf(d)
		if old, ok := pc.byLabel[l]; !ok || d.Before(old) {
			pc.byLabel[l] = d
		}
		days = append(days, d.Day())
	}
	if len(days) == 0 {
		pc.Mode = "calendar"
		return pc, rows.Err()
	}
	sort.Ints(days)
	pc.typDay = days[len(days)/2]
	return pc, rows.Err()
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
	// extrapolate using the usual salary day, in the previous month or the month itself
	for _, m := range []time.Time{first.AddDate(0, -1, 0), first} {
		day := pc.typDay
		if last := m.AddDate(0, 1, -1).Day(); day > last {
			day = last
		}
		c := time.Date(m.Year(), m.Month(), day, 0, 0, 0, 0, time.UTC)
		if labelOf(c) == first.Format("2006-01") {
			// The salary is due (or a few days overdue) but not booked yet: the new
			// period only starts once it arrives, so today still belongs to the old one.
			if !pc.today.IsZero() && !c.After(pc.today) && pc.today.Sub(c) <= salaryGraceDays*24*time.Hour {
				return pc.today.AddDate(0, 0, 1)
			}
			return c
		}
	}
	return first
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
	Mode     string `json:"mode"`
	Salary   string `json:"salary_series"`
	SeriesID int64  `json:"salary_series_id"`
}

func (pc *PeriodCalc) Describe(month string) Period {
	s, e, _ := pc.Range(month)
	return Period{Month: month, Start: s.Format("2006-01-02"), End: e.AddDate(0, 0, -1).Format("2006-01-02"),
		Mode: pc.Mode, Salary: pc.SeriesName, SeriesID: pc.SeriesID}
}
