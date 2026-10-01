package budget

import (
	"testing"
	"time"

	"github.com/AQUI74S/homestead/internal/store"
)

func TestSumBalances(t *testing.T) {
	c := func(v int64) *int64 { return &v }
	at := func(s string) *time.Time { d := day(s); return &d }
	accts := []store.Account{
		{Active: true, BalanceCents: c(120000), BalanceAt: at("2026-09-30")},
		{Active: true, BalanceCents: c(80000), BalanceAt: at("2026-09-29")},
		{Active: true}, // no balance fetched yet
		{Active: false, BalanceCents: c(1000000)}, // not included in evaluations
	}
	sum, oldest, missing := SumBalances(accts)
	if sum != 120000+80000 || missing != 1 || !oldest.Equal(day("2026-09-29")) {
		t.Errorf("sum=%d oldest=%v missing=%d", sum, oldest, missing)
	}
}

func TestNextSalary(t *testing.T) {
	rec := []store.Recurring{
		{ID: 1, Label: "Gehalt A", Direction: "in", Kind: "einkommen", CycleDays: 30, LastCents: 340000,
			FirstDate: "2026-05-29", LastDate: "2026-08-29"},
		{ID: 2, Label: "Gehalt B", Direction: "in", Kind: "einkommen", CycleDays: 30, LastCents: 210000,
			FirstDate: "2026-06-01", LastDate: "2026-09-01"},
		{ID: 3, Label: "Kindergeld", Direction: "in", Kind: "einkommen", CycleDays: 30, LastCents: 77700,
			LastDate: "2026-09-10"},
	}
	// September runs from 29.08.; salary A is late (expected 29.09., today 01.10.)
	sum, date := nextSalary(rec, []int64{1, 2}, "2026-09", day("2026-08-29"), day("2026-10-01"))
	if sum != 340000+210000 || date != "2026-09-29" {
		t.Errorf("next salary = %d on %s, want 5500,00 on 29.09.", sum, date)
	}
	if sum, date := nextSalary(rec, nil, "2026-09", day("2026-08-29"), day("2026-10-01")); sum != 0 || date != "" {
		t.Errorf("without salary series: %d %s", sum, date)
	}
}
