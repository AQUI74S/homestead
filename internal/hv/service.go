package hv

import (
	"context"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

// Reassign re-assigns all transactions of the property management accounts (manual ones are kept).
func Reassign(ctx context.Context, st *store.Store) error {
	txns, err := st.HVTransactions(ctx, time.Time{}, time.Time{})
	if err != nil || len(txns) == 0 {
		return err
	}
	leases, err := st.Leases(ctx)
	if err != nil {
		return err
	}
	props, err := st.Properties(ctx)
	if err != nil {
		return err
	}
	rules, err := st.HVRules(ctx)
	if err != nil {
		return err
	}
	own, err := st.OwnIBANs(ctx)
	if err != nil {
		return err
	}
	return st.ApplyHVAssignments(ctx, Assign(AssignInput{Txns: txns, Leases: leases, Properties: props, Rules: rules, OwnIBANs: own}))
}

// Data is everything the property management reports are computed from.
type Data struct {
	Properties []store.Property
	Leases     []store.Lease
	Txns       []store.HVTxn
	FirstData  time.Time        // earliest transaction on a rent account
	Ledgers    map[int64]Ledger // per lease ID
	Today      time.Time
}

// Load reads properties, leases and rent account transactions and computes the ledgers.
func Load(ctx context.Context, st *store.Store) (*Data, error) {
	d := &Data{Ledgers: map[int64]Ledger{}, Today: domain.Today()}
	var err error
	if d.Properties, err = st.Properties(ctx); err != nil {
		return nil, err
	}
	if d.Leases, err = st.Leases(ctx); err != nil {
		return nil, err
	}
	if d.Txns, err = st.HVTransactions(ctx, time.Time{}, time.Time{}); err != nil {
		return nil, err
	}
	if d.FirstData, err = st.FirstHVDate(ctx); err != nil {
		return nil, err
	}
	for _, l := range d.Leases {
		d.Ledgers[l.ID] = ComputeLedger(l, d.Txns, d.FirstData, d.Today)
	}
	return d, nil
}

// Lease returns the lease with this ID.
func (d *Data) Lease(id int64) (store.Lease, bool) {
	for _, l := range d.Leases {
		if l.ID == id {
			return l, true
		}
	}
	return store.Lease{}, false
}

// Property returns the property with this ID, or the first one for id 0.
func (d *Data) Property(id int64) (*store.Property, bool) {
	for i := range d.Properties {
		if d.Properties[i].ID == id || (id == 0 && i == 0) {
			return &d.Properties[i], true
		}
	}
	return nil, false
}

// LeaseRow is a lease with its ledger row for the month on the overview.
type LeaseRow struct {
	store.Lease
	Row     *MonthRow `json:"row"`
	Balance int64     `json:"balance"`
	Active  bool      `json:"active"`
}

// Totals sums up the month on the overview.
type Totals struct {
	Soll    int64 `json:"soll"`
	Paid    int64 `json:"paid"`
	Open    int64 `json:"open"`
	Arrears int64 `json:"arrears"` // all outstanding rent
}

// Overview is the start page of the property management.
type Overview struct {
	Month      string           `json:"month"`
	Properties []store.Property `json:"properties"`
	Leases     []LeaseRow       `json:"leases"`
	Deadlines  []Deadline       `json:"deadlines"`
	Unassigned int              `json:"unassigned"`
	Totals     Totals           `json:"totals"`
	Accounts   []store.Account  `json:"accounts"`
}

// BuildOverview summarizes a month: rent due and paid per lease, urgent and upcoming
// deadlines, and how many transactions still need to be assigned.
func BuildOverview(d *Data, month string, reminders []store.Reminder, accounts []store.Account) Overview {
	if !domain.ValidMonth(month) {
		month = d.Today.Format(domain.MonthLayout)
	}
	ov := Overview{Month: month, Properties: d.Properties, Leases: []LeaseRow{}, Deadlines: []Deadline{}, Accounts: accounts}
	for _, l := range d.Leases {
		lg := d.Ledgers[l.ID]
		lr := LeaseRow{Lease: l, Balance: lg.Balance}
		for i := range lg.Months {
			if lg.Months[i].Month == month {
				m := lg.Months[i]
				lr.Row = &m
				ov.Totals.Soll += m.Soll
				ov.Totals.Paid += m.Paid
				ov.Totals.Open += m.Open
			}
		}
		lr.Active = l.End == "" || !day(l.End).Before(d.Today)
		if lg.Balance > 0 {
			ov.Totals.Arrears += lg.Balance
		}
		if lr.Active || lr.Row != nil || lg.Balance != 0 {
			ov.Leases = append(ov.Leases, lr)
		}
	}
	limit := d.Today.AddDate(0, UpcomingMonths, 0).Format(domain.DateLayout)
	for _, dl := range Deadlines(d.Properties, d.Leases, d.Ledgers, reminders, d.Today) {
		if dl.Urgent || dl.Date <= limit {
			ov.Deadlines = append(ov.Deadlines, dl)
		}
	}
	for _, t := range d.Txns {
		if IsUnassigned(t) {
			ov.Unassigned++
		}
	}
	return ov
}
