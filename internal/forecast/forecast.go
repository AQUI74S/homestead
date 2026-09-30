// Package forecast computes which recurring payments are still expected
// in a budget period.
package forecast

import (
	"sort"
	"time"

	"github.com/AQUI74S/homestead/internal/store"
)

type Item struct {
	RecurringID int64  `json:"recurring_id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Date        string `json:"date"`
	Amount      int64  `json:"amount"` // signed: incoming positive
	Status      string `json:"status"` // bezahlt | offen | ueberfaellig
	Manual      bool   `json:"manual"`
	CategoryID  *int64 `json:"category_id"`
	AccountID   *int64 `json:"account_id"`
}

type Result struct {
	Items      []Item           `json:"items"`
	OpenIn     int64            `json:"open_in"`      // still expected income
	OpenOut    int64            `json:"open_out"`     // still expected expenses (positive)
	OpenByKind map[string]int64 `json:"open_by_kind"` // positive, per kind
	PaidOut    int64            `json:"paid_out"`
}

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

// Step returns the k-th next due date (monthly and yearly cycles calendar-exact).
func Step(d time.Time, cycle, k int) time.Time {
	switch cycle {
	case 30:
		return d.AddDate(0, k, 0)
	case 61:
		return d.AddDate(0, 2*k, 0)
	case 91:
		return d.AddDate(0, 3*k, 0)
	case 182:
		return d.AddDate(0, 6*k, 0)
	case 365:
		return d.AddDate(k, 0, 0)
	}
	return d.AddDate(0, 0, cycle*k)
}

// Compute determines paid and still open due dates for the period [start, end).
func Compute(rec []store.Recurring, start, end, today time.Time) Result {
	res := Result{Items: []Item{}, OpenByKind: map[string]int64{}}
	for _, r := range rec {
		if r.Ended || r.Status == "ignored" || r.CycleDays <= 0 {
			continue
		}
		sign := int64(-1)
		if r.Direction == "in" {
			sign = 1
		}
		last := day(r.LastDate)
		// Typical day of month: weekend bookings often land earlier, so take the later
		// of first and last date (e.g. installment on the 30th, booked on the 28th in August).
		typDay := last.Day()
		if f := day(r.FirstDate); !f.IsZero() && f.Day() > typDay {
			typDay = f.Day()
		}
		base := Item{RecurringID: r.ID, Label: r.Label, Kind: r.Kind, Manual: r.Manual, CategoryID: r.CategoryID, AccountID: r.AccountID}
		// last actual payment falls within the period
		if !r.Manual && !last.Before(start) && last.Before(end) {
			it := base
			it.Date, it.Amount, it.Status = r.LastDate, sign*r.LastCents, "bezahlt"
			res.Items = append(res.Items, it)
			if sign < 0 {
				res.PaidOut += r.LastCents
			}
		}
		for k := 1; k <= 60; k++ {
			d := Step(last, r.CycleDays, k)
			if r.CycleDays >= 30 && r.CycleDays <= 182 {
				d = withDay(d, typDay)
			}
			if !d.Before(end) {
				break
			}
			if d.Before(start) {
				continue
			}
			it := base
			it.Date, it.Amount, it.Status = d.Format("2006-01-02"), sign*r.LastCents, "offen"
			if d.Before(today.AddDate(0, 0, -3)) {
				it.Status = "ueberfaellig"
			}
			res.Items = append(res.Items, it)
			if sign > 0 {
				res.OpenIn += r.LastCents
			} else {
				res.OpenOut += r.LastCents
				res.OpenByKind[r.Kind] += r.LastCents
			}
		}
	}
	sort.SliceStable(res.Items, func(i, j int) bool { return res.Items[i].Date < res.Items[j].Date })
	return res
}

// withDay sets the day of month (capped at the month's length).
func withDay(d time.Time, dd int) time.Time {
	if last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day(); dd > last {
		dd = last
	}
	return time.Date(d.Year(), d.Month(), dd, 0, 0, 0, 0, time.UTC)
}
