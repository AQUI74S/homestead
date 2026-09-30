package hv

import (
	"testing"

	"github.com/AQUI74S/homestead/internal/store"
)

func id(v int64) *int64 { return &v }

func lease() store.Lease {
	return store.Lease{ID: 1, UnitID: 10, UnitName: "EG", PropertyID: 5, PropertyName: "Talstr. 3",
		Tenant: store.Tenant{Name: "Anna Schmidt", IBAN: "DE11 2222"}, Start: "2025-01-01", RentCold: 80000, NKPrepay: 20000,
		DueDay: 3, Persons: 2, RentType: "fest"}
}

func TestLedgerFIFO(t *testing.T) {
	l := lease()
	pay := func(date string, amt int64) store.HVTxn {
		return store.HVTxn{Date: date, AmountCents: amt, LeaseID: id(1), CostType: "miete"}
	}
	// July paid in full, August half, September nothing; today is Sep 20.
	txs := []store.HVTxn{pay("2026-07-02", 100000), pay("2026-08-04", 50000)}
	lg := ComputeLedger(l, txs, day("2026-07-01"), day("2026-09-20"))
	if len(lg.Months) != 3 {
		t.Fatalf("months: %+v", lg.Months)
	}
	got := map[string]string{}
	for _, m := range lg.Months {
		got[m.Month] = m.Status
	}
	if got["2026-07"] != "bezahlt" || got["2026-08"] != "teilweise" || got["2026-09"] != "offen" {
		t.Errorf("status: %v", got)
	}
	if lg.Balance != 150000 {
		t.Errorf("arrears %d, want 1,500 €", lg.Balance)
	}
	// Before the due day the current month is not yet overdue
	lg = ComputeLedger(l, txs, day("2026-07-01"), day("2026-09-02"))
	if lg.Months[0].Status != "faellig" || lg.Balance != 50000 {
		t.Errorf("before due date: %s, balance %d", lg.Months[0].Status, lg.Balance)
	}
}

func TestRentSteps(t *testing.T) {
	l := lease()
	l.Steps = []store.RentStep{{ValidFrom: "2026-09-01", RentCold: 85000, NKPrepay: 22000}}
	if c, n := RentAt(l, day("2026-08-15")); c != 80000 || n != 20000 {
		t.Errorf("before step: %d %d", c, n)
	}
	if c, n := RentAt(l, day("2026-09-01")); c != 85000 || n != 22000 {
		t.Errorf("from step: %d %d", c, n)
	}
}

func TestSettlement(t *testing.T) {
	p := store.Property{ID: 5, Units: []store.Unit{{ID: 10, AreaCents: 6000}, {ID: 11, AreaCents: 4000}}}
	a := lease()                                                                                          // EG, 60 m², 2 persons, full year
	b := store.Lease{ID: 2, UnitID: 11, UnitName: "OG", PropertyID: 5, Tenant: store.Tenant{Name: "Ben"}, // OG, 40 m², from Jul 1
		Start: "2025-07-01", NKPrepay: 15000, Persons: 1}
	txs := []store.HVTxn{
		{Date: "2025-03-01", AmountCents: -100000, PropertyID: id(5), CostType: "grundsteuer"}, // 1,000 € by area
		{Date: "2025-05-01", AmountCents: -60000, PropertyID: id(5), CostType: "muell"},        // 600 € by persons
		{Date: "2025-05-01", AmountCents: -99999, PropertyID: id(5), CostType: "reparatur"},    // not allocable to tenants
	}
	st := ComputeSettlement(p, []store.Lease{a, b}, txs, nil, nil, 2025)
	if st.CostTotal != 160000 || len(st.Statements) != 2 {
		t.Fatalf("costs %d, tenants %d", st.CostTotal, len(st.Statements))
	}
	sa, sb := st.Statements[0], st.Statements[1]
	// Area: A 60 % full year = 600 €; B 40 % × 184/365 = 201.64 €
	// Persons: A 2×365=730, B 1×184 -> A 730/914 × 600 = 479.21 €, B 120.79 €
	if sa.CostSum != 60000+47921 || sb.CostSum != 20164+12079 {
		t.Errorf("shares A=%d B=%d", sa.CostSum, sb.CostSum)
	}
	if sa.Prepaid != 240000 || sb.Prepaid != 90000 {
		t.Errorf("prepayments A=%d B=%d", sa.Prepaid, sb.Prepaid)
	}
	if sa.Result != sa.CostSum-240000 {
		t.Errorf("result A=%d", sa.Result)
	}
	// OG vacancy in the first half-year is borne by the owner (area-based costs only)
	if st.OwnerShare != 160000-sa.CostSum-sb.CostSum || st.OwnerShare < 19000 {
		t.Errorf("owner share %d", st.OwnerShare)
	}
}

func TestAssign(t *testing.T) {
	l := lease()
	props := []store.Property{{ID: 5}}
	txs := []store.HVTxn{
		{ID: 1, Date: "2026-09-02", AmountCents: 100000, Counterparty: "Anna Schmidt", CounterpartyIBAN: "DE112222", Remittance: "Miete September", Source: "auto"},
		{ID: 2, Date: "2026-09-05", AmountCents: 100000, Counterparty: "Peter Schmidt", Remittance: "Miete Anna Schmidt EG", Source: "auto"},
		{ID: 3, Date: "2026-09-10", AmountCents: -45000, Counterparty: "Stadt Musterstadt", Remittance: "Grundsteuer B 3. Quartal", Source: "auto"},
		{ID: 4, Date: "2026-09-12", AmountCents: -8900, Counterparty: "EAW Abfallwirtschaft", Remittance: "Muellgebuehr", Source: "auto"},
		{ID: 5, Date: "2026-09-15", AmountCents: -50000, Counterparty: "Max", CounterpartyIBAN: "DE99", Remittance: "Entnahme", Source: "auto"},
		{ID: 6, Date: "2026-09-16", AmountCents: -1000, Counterparty: "X", Source: "manual", CostType: "reparatur"},
	}
	res := Assign(AssignInput{Txns: txs, Leases: []store.Lease{l}, Properties: props, OwnIBANs: map[string]bool{"DE99": true}})
	got := map[int64]store.HVAssign{}
	for _, a := range res {
		got[a.ID] = a
	}
	if a := got[1]; a.LeaseID == nil || *a.LeaseID != 1 || a.CostType != "miete" {
		t.Errorf("rent via IBAN: %+v", a)
	}
	if a := got[2]; a.LeaseID == nil || a.CostType != "miete" {
		t.Errorf("rent via name in remittance: %+v", a)
	}
	if got[3].CostType != "grundsteuer" || got[3].PropertyID == nil || got[4].CostType != "muell" {
		t.Errorf("cost types: %+v %+v", got[3], got[4])
	}
	if got[5].CostType != "entnahme" {
		t.Errorf("withdrawal: %+v", got[5])
	}
	if _, ok := got[6]; ok {
		t.Errorf("manual assignment overwritten")
	}
}

func TestAssignPropertyByAddress(t *testing.T) {
	props := []store.Property{{ID: 5, Name: "Talstraße 3"}, {ID: 6, Name: "ETW Beispielstadt", Address: "Rheinstraße 12, 10115 Beispielstadt"}}
	txs := []store.HVTxn{
		{ID: 1, Date: "2026-09-10", AmountCents: -18000, Counterparty: "Stadt Musterstadt", Remittance: "Grundsteuer B Talstr. 3", Source: "auto"},
		{ID: 2, Date: "2026-09-01", AmountCents: -31000, Counterparty: "WEG Rheinstrasse 12", Remittance: "Hausgeld Whg. 4", Source: "auto"},
		{ID: 3, Date: "2026-05-20", AmountCents: -12000, Counterparty: "Bezirksschornsteinfeger Klein", Remittance: "Kehr- und Messgebuehr", Source: "auto"},
	}
	got := map[int64]store.HVAssign{}
	for _, a := range Assign(AssignInput{Txns: txs, Properties: props}) {
		got[a.ID] = a
	}
	if p := got[1].PropertyID; p == nil || *p != 5 {
		t.Errorf("Talstr. -> property 5: %+v", got[1])
	}
	if p := got[2].PropertyID; p == nil || *p != 6 || got[2].CostType != "hausgeld" {
		t.Errorf("Rheinstraße -> property 6, Hausgeld: %+v", got[2])
	}
	if got[3].CostType != "schornsteinfeger" || got[3].PropertyID != nil {
		t.Errorf("chimney sweep without property: %+v", got[3])
	}
}
