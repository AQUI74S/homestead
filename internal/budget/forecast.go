package budget

import (
	"sort"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

// ItemStatus says whether a due date of a recurring payment is settled.
type ItemStatus string

const (
	ItemPaid    ItemStatus = "bezahlt"
	ItemOpen    ItemStatus = "offen"
	ItemOverdue ItemStatus = "ueberfaellig"
)

const (
	// overdueAfterDays: an open due date this many days in the past counts as overdue.
	overdueAfterDays = 3
	// maxForecastSteps bounds the due dates looked at per series.
	maxForecastSteps = 60
	// lateBookingDays: a booking this many days before the typical day of month
	// is a late booking of the previous month (due on the 30th, booked on the 1st).
	lateBookingDays = 15
)

// Item is one due date of a recurring payment.
type Item struct {
	RecurringID int64       `json:"recurring_id"`
	Label       string      `json:"label"`
	Kind        domain.Kind `json:"kind"`
	Date        string      `json:"date"`
	Amount      int64       `json:"amount"` // signed: incoming positive
	Status      ItemStatus  `json:"status"`
	Manual      bool        `json:"manual"`
	CategoryID  *int64      `json:"category_id"`
	AccountID   *int64      `json:"account_id"`
}

// Forecast is the result of Compute for a date range.
type Forecast struct {
	Items      []Item                `json:"items"`
	OpenIn     int64                 `json:"open_in"`      // still expected income
	OpenOut    int64                 `json:"open_out"`     // still expected expenses (positive)
	OpenByKind map[domain.Kind]int64 `json:"open_by_kind"` // positive, per kind
	PaidOut    int64                 `json:"paid_out"`
}

// Compute determines paid and still open due dates for the range [start, end).
func Compute(rec []store.Recurring, start, end, today time.Time) Forecast {
	res := Forecast{Items: []Item{}, OpenByKind: map[domain.Kind]int64{}}
	overdue := today.AddDate(0, 0, -overdueAfterDays)
	for _, r := range rec {
		if !r.Active() {
			continue
		}
		sign := int64(-1)
		if r.Direction == domain.DirectionIn {
			sign = 1
		}
		last := domain.ParseDate(r.LastDate)
		// Typical day of month: weekend bookings often land earlier, so take the later
		// of first and last date (e.g. installment on the 30th, booked on the 28th in August).
		typDay := last.Day()
		if f := domain.ParseDate(r.FirstDate); !f.IsZero() && f.Day() > typDay {
			typDay = f.Day()
		}
		// Booked late across the month end (due on the 30th, booked on the 1st): the
		// payment belongs to the month before, so count the next steps from there.
		anchor := last
		monthly := r.CycleDays.MonthBased() && r.CycleDays != domain.Yearly
		if monthly && typDay-last.Day() > lateBookingDays {
			anchor = time.Date(last.Year(), last.Month()-1, 1, 0, 0, 0, 0, time.UTC)
		}
		base := Item{RecurringID: r.ID, Label: r.Label, Kind: r.Kind, Manual: r.Manual, CategoryID: r.CategoryID, AccountID: r.AccountID}
		// last actual payment falls within the range
		if !r.Manual && !last.Before(start) && last.Before(end) {
			it := base
			it.Date, it.Amount, it.Status = r.LastDate, sign*r.LastCents, ItemPaid
			res.Items = append(res.Items, it)
			if sign < 0 {
				res.PaidOut += r.LastCents
			}
		}
		for k := 1; k <= maxForecastSteps; k++ {
			d := r.CycleDays.Step(anchor, k)
			if monthly {
				d = withDay(d, typDay)
			}
			if !d.Before(end) {
				break
			}
			if d.Before(start) {
				continue
			}
			it := base
			it.Date, it.Amount, it.Status = d.Format(domain.DateLayout), sign*r.LastCents, ItemOpen
			if d.Before(overdue) {
				it.Status = ItemOverdue
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

// Upcoming returns the not yet paid due dates in [from, to).
func Upcoming(rec []store.Recurring, from, to time.Time) []Item {
	out := []Item{}
	for _, it := range Compute(rec, from, to, from).Items {
		if it.Status != ItemPaid {
			out = append(out, it)
		}
	}
	return out
}

// withDay sets the day of month (capped at the month's length).
func withDay(d time.Time, day int) time.Time {
	if last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day(); day > last {
		day = last
	}
	return time.Date(d.Year(), d.Month(), day, 0, 0, 0, 0, time.UTC)
}

// DropNextSalary removes open salary payments that fund a later period. In a
// salary-to-salary period the next salary may be due before this period ends
// (or while it waits for a late salary), but it belongs to the next period.
func (f *Forecast) DropNextSalary(seriesIDs []int64, periodLabel string) {
	ids := map[int64]bool{}
	for _, id := range seriesIDs {
		ids[id] = true
	}
	kept := f.Items[:0]
	for _, it := range f.Items {
		if ids[it.RecurringID] && it.Status != ItemPaid && it.Amount > 0 {
			if d, err := time.Parse(domain.DateLayout, it.Date); err == nil && SalaryLabel(d) > periodLabel {
				f.OpenIn -= it.Amount
				continue
			}
		}
		kept = append(kept, it)
	}
	f.Items = kept
}

// VariableRest estimates how much of the variable budget will still be spent
// between today and the end of the period (end is exclusive).
//
// Budgets are spread evenly over the period, so with 3 of 30 days left only a
// tenth of the monthly budget is expected. The estimate never exceeds what is
// left of the budget: categories that ran over eat into the others, and once
// the budget is used up nothing more is expected. It returns the estimate in
// cents and the number of days left.
func VariableRest(budget, spent int64, start, end, today time.Time) (int64, int) {
	total := domain.DaysBetween(start, end)
	if total <= 0 || budget <= 0 || !end.After(today) {
		return 0, 0
	}
	from := today
	if from.Before(start) {
		from = start
	}
	left := domain.DaysBetween(from, end)
	rest := budget - spent
	if rest <= 0 {
		return 0, left
	}
	if paced := budget * int64(left) / int64(total); paced < rest {
		rest = paced
	}
	return rest, left
}
