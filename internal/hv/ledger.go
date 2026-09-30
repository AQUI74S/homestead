package hv

import (
	"sort"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

// MonthRow is the rent due (Soll) vs. paid (Ist) of a lease for one month.
type MonthRow struct {
	Month  string `json:"month"`
	Due    string `json:"due_date"`
	Soll   int64  `json:"soll"`
	Paid   int64  `json:"paid"`
	Open   int64  `json:"open"`
	Status string `json:"status"` // Status*
}

type Ledger struct {
	LeaseID  int64         `json:"lease_id"`
	Months   []MonthRow    `json:"months"`
	Balance  int64         `json:"balance"` // outstanding amount (positive = arrears, negative = credit)
	PaidSum  int64         `json:"paid_sum"`
	SollSum  int64         `json:"soll_sum"`
	Payments []store.HVTxn `json:"payments"`
	From     string        `json:"from"`
}

// TrackStart determines from when due vs. paid is tracked: the configured value, otherwise
// the later of lease start and start of account data.
func TrackStart(l store.Lease, firstData time.Time) time.Time {
	if l.TrackFrom != "" {
		return monthStart(day(l.TrackFrom))
	}
	s := monthStart(day(l.Start))
	if !firstData.IsZero() && monthStart(firstData).After(s) {
		s = monthStart(firstData)
	}
	return s
}

// ComputeLedger allocates rent payments in order to the due months (oldest first).
func ComputeLedger(l store.Lease, payments []store.HVTxn, firstData, today time.Time) Ledger {
	lg := Ledger{LeaseID: l.ID, Months: []MonthRow{}, Payments: []store.HVTxn{}}
	start := TrackStart(l, firstData)
	lg.From = start.Format(domain.DateLayout)
	last := monthStart(today)
	if l.End != "" && monthStart(day(l.End)).Before(last) {
		last = monthStart(day(l.End))
	}
	var paidTotal int64
	sort.Slice(payments, func(i, j int) bool { return payments[i].Date < payments[j].Date })
	for _, p := range payments {
		if p.LeaseID == nil || *p.LeaseID != l.ID || p.CostType != CostRent {
			continue
		}
		if day(p.Date).Before(start.AddDate(0, 0, -earlyPaymentDays)) { // payments before the tracked period
			continue
		}
		paidTotal += p.AmountCents
		lg.Payments = append(lg.Payments, p)
	}
	lg.PaidSum = paidTotal
	remaining := paidTotal
	for m := start; !m.After(last); m = m.AddDate(0, 1, 0) {
		next := m.AddDate(0, 1, 0)
		days := occupiedDays(l, m, next)
		if days == 0 {
			continue
		}
		cold, nk := RentAt(l, m)
		full := cold + nk
		soll := full
		if total := domain.DaysBetween(m, next); days < total {
			soll = full * int64(days) / int64(total)
		}
		dueDay := l.DueDay
		if dueDay < 1 {
			dueDay = DefaultDueDay
		}
		due := time.Date(m.Year(), m.Month(), dueDay, 0, 0, 0, 0, time.UTC)
		row := MonthRow{Month: m.Format(domain.MonthLayout), Due: due.Format(domain.DateLayout), Soll: soll}
		pay := remaining
		if pay > soll {
			pay = soll
		}
		if pay < 0 {
			pay = 0
		}
		row.Paid, row.Open = pay, soll-pay
		remaining -= pay
		switch {
		case row.Open == 0:
			row.Status = StatusPaid
		case today.Before(due.AddDate(0, 0, 1)):
			row.Status = StatusDue
		case row.Paid > 0:
			row.Status = StatusPartial
		default:
			row.Status = StatusOpen
		}
		if !today.Before(due) {
			lg.SollSum += soll
		}
		lg.Months = append(lg.Months, row)
	}
	lg.Balance = lg.SollSum - paidTotal
	// newest months first
	for i, j := 0, len(lg.Months)-1; i < j; i, j = i+1, j-1 {
		lg.Months[i], lg.Months[j] = lg.Months[j], lg.Months[i]
	}
	return lg
}
