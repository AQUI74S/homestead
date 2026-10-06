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
		{ID: 3, Label: "Kindergeld", Direction: "in", Kind: "einkommen", CycleDays: 30, LastCents: 75000,
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

func TestForecastEnd(t *testing.T) {
	end := day("2026-10-26") // usual salary day
	cases := []struct {
		next    string
		current bool
		want    string
	}{
		{"2026-11-01", true, "2026-11-01"},  // salary expected later: forecast until then
		{"2026-11-01", false, "2026-10-26"}, // other months keep their period
		{"2026-10-20", true, "2026-10-26"},  // earlier salary: never shorter than the period
		{"", true, "2026-10-26"},            // no salary series
	}
	for _, c := range cases {
		if got := forecastEnd(end, c.next, c.current).Format("2006-01-02"); got != c.want {
			t.Errorf("forecastEnd(%q, %v) = %s, want %s", c.next, c.current, got, c.want)
		}
	}
}

func TestWithExpected(t *testing.T) {
	id := func(v int64) *int64 { return &v }
	lines := []store.CategoryLine{
		{ID: 1, Group: "bills", Name: "Miete / Hauskredit", IstCents: 0},
		{ID: 2, Group: "bills", Name: "Gas & Heizung", IstCents: 20000},
		{ID: 3, Group: "income", Name: "Kindergeld", IstCents: 0},
	}
	items := []Item{
		{Label: "Deutsche Bank", CategoryID: id(1), Date: "2026-10-15", Amount: -80000, Status: ItemOpen},
		{Label: "Commerzbank", CategoryID: id(1), Date: "2026-10-30", Amount: -50000, Status: ItemOpen},
		{Label: "Vattenfall Gas", CategoryID: id(2), Date: "2026-10-01", Amount: -20000, Status: ItemPaid},
		{Label: "Familienkasse", CategoryID: id(3), Date: "2026-10-10", Amount: 75000, Status: ItemOpen},
		{Label: "Handwerker (manuell)", Kind: "fixkosten", Date: "2026-10-20", Amount: -9000, Status: ItemOpen},
	}
	got := withExpected(lines, items, false)
	want := map[string]int64{"Miete / Hauskredit": 130000, "Gas & Heizung": 20000, "Kindergeld": 75000, unassignedLabel: 9000}
	for _, l := range got {
		if l.ExpectedCents != want[l.Name] {
			t.Errorf("%s: expected %d, want %d", l.Name, l.ExpectedCents, want[l.Name])
		}
	}
	if len(got) != 4 || got[0].OpenDates[1] != "2026-10-30" || got[3].Group != "bills" {
		t.Errorf("lines: %+v", got)
	}
	// a past month expects nothing more than was booked
	for _, l := range withExpected([]store.CategoryLine{{ID: 1, Group: "bills", IstCents: 500}}, items, true) {
		if l.ExpectedCents != 500 || l.OpenDates != nil {
			t.Errorf("past month: %+v", l)
		}
	}
}
