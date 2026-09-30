package forecast

import (
	"testing"

	"github.com/AQUI74S/homestead/internal/store"
)

func TestCompute(t *testing.T) {
	rec := []store.Recurring{
		{ID: 1, Label: "Miete", Direction: "out", Kind: "fixkosten", CycleDays: 30, LastCents: 120000, LastDate: "2026-10-01"},
		{ID: 2, Label: "Netflix", Direction: "out", Kind: "abo", CycleDays: 30, LastCents: 1399, LastDate: "2026-09-05"},
		{ID: 3, Label: "Kfz-Versicherung", Direction: "out", Kind: "fixkosten", CycleDays: 365, LastCents: 61200, LastDate: "2025-10-15", Status: "confirmed"},
		{ID: 4, Label: "Gehalt", Direction: "in", Kind: "einkommen", CycleDays: 30, LastCents: 341255, LastDate: "2026-09-28"},
		{ID: 5, Label: "Rundfunk", Direction: "out", Kind: "fixkosten", CycleDays: 91, LastCents: 5508, LastDate: "2026-07-15"},
		{ID: 6, Label: "AltAbo", Direction: "out", Kind: "abo", CycleDays: 30, LastCents: 999, LastDate: "2026-05-10", Ended: true},
		{ID: 7, Label: "Ignoriert", Direction: "out", Kind: "abo", CycleDays: 30, LastCents: 500, LastDate: "2026-09-10", Status: "ignored"},
		{ID: 9, Label: "Hauskredit", Direction: "out", Kind: "kredit", CycleDays: 30, LastCents: 125000, FirstDate: "2026-06-30", LastDate: "2026-09-28"},
		{ID: 8, Label: "Hausrat (manuell)", Direction: "out", Kind: "fixkosten", CycleDays: 365, LastCents: 9000, LastDate: "2025-10-20", Manual: true},
	}
	// October period: 09-28 to 10-27, today 10-02
	res := Compute(rec, day("2026-09-28"), day("2026-10-28"), day("2026-10-02"))
	got := map[string]string{}
	for _, it := range res.Items {
		got[it.Label] += it.Status + "@" + it.Date + " "
	}
	want := map[string]string{
		"Miete":             "bezahlt@2026-10-01 ",
		"Netflix":           "offen@2026-10-05 ",
		"Kfz-Versicherung":  "offen@2026-10-15 ",
		"Gehalt":            "bezahlt@2026-09-28 ",
		"Rundfunk":          "offen@2026-10-15 ",
		"Hausrat (manuell)": "offen@2026-10-20 ",
		"Hauskredit":        "bezahlt@2026-09-28 ",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected items: %v", got)
	}
	if res.OpenOut != 1399+61200+5508+9000 || res.OpenIn != 0 || res.PaidOut != 120000+125000 {
		t.Errorf("totals: open_out=%d open_in=%d paid=%d", res.OpenOut, res.OpenIn, res.PaidOut)
	}
	if res.OpenByKind["abo"] != 1399 {
		t.Errorf("abo: %d", res.OpenByKind["abo"])
	}
	// Future December period: everything open, salary expected
	res = Compute(rec, day("2026-11-27"), day("2026-12-28"), day("2026-10-02"))
	if res.OpenIn != 341255 {
		t.Errorf("December salary expected: %d", res.OpenIn)
	}
}

func TestWeekendShift(t *testing.T) {
	// Installment on the 30th, booked on the 28th in August due to weekend -> next on 09-30
	rec := []store.Recurring{{ID: 1, Label: "Kredit", Direction: "out", Kind: "kredit", CycleDays: 30, LastCents: 100, FirstDate: "2026-06-30", LastDate: "2026-08-28"}}
	res := Compute(rec, day("2026-09-01"), day("2026-10-01"), day("2026-09-10"))
	if len(res.Items) != 1 || res.Items[0].Date != "2026-09-30" {
		t.Fatalf("%+v", res.Items)
	}
	// February: cap at end of month
	res = Compute(rec, day("2027-02-01"), day("2027-03-01"), day("2026-09-10"))
	if len(res.Items) != 1 || res.Items[0].Date != "2027-02-28" {
		t.Fatalf("February: %+v", res.Items)
	}
}

func TestDropNextSalary(t *testing.T) {
	res := Result{OpenIn: 350000 + 210000, Items: []Item{
		{RecurringID: 1, Date: "2026-08-29", Amount: 340000, Status: "bezahlt"},
		{RecurringID: 1, Date: "2026-09-30", Amount: 340000, Status: "offen"}, // funds October
		{RecurringID: 2, Date: "2026-09-15", Amount: 210000, Status: "offen"}, // second salary within September
		{RecurringID: 3, Date: "2026-09-10", Amount: 10000, Status: "offen"},
	}}
	res.OpenIn = 340000 + 210000 + 10000
	res.DropNextSalary([]int64{1, 2}, "2026-09")
	if len(res.Items) != 3 || res.OpenIn != 220000 {
		t.Errorf("next salary not removed correctly: %+v", res)
	}
}
