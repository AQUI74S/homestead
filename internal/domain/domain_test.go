package domain

import (
	"testing"
	"time"
)

func TestCycleStep(t *testing.T) {
	d := ParseDate("2026-01-31")
	cases := []struct {
		c    Cycle
		k    int
		want string
	}{
		{Weekly, 1, "2026-02-07"},
		{Biweekly, 2, "2026-02-28"},
		{Monthly, 1, "2026-03-03"}, // Go normalizes 31 Feb like time.AddDate
		{Quarterly, 1, "2026-05-01"},
		{Yearly, -1, "2025-01-31"},
		{Cycle(10), 3, "2026-03-02"},
	}
	for _, c := range cases {
		if got := c.c.Step(d, c.k).Format(DateLayout); got != c.want {
			t.Errorf("%s.Step(%d) = %s, want %s", c.c.Label(), c.k, got, c.want)
		}
	}
	if got := Monthly.Step(ParseDate("2026-09-15"), 3).Format(DateLayout); got != "2026-12-15" {
		t.Errorf("monthly stays on the same day: got %s", got)
	}
}

func TestCyclePerYear(t *testing.T) {
	want := map[Cycle]float64{Weekly: 52, Biweekly: 26, Monthly: 12, Bimonthly: 6, Quarterly: 4, HalfYearly: 2, Yearly: 1}
	for c, w := range want {
		if got := c.PerYear(); got != w {
			t.Errorf("%s: %v per year, want %v", c.Label(), got, w)
		}
	}
	if !Quarterly.Valid() || Cycle(10).Valid() {
		t.Error("Valid")
	}
}

func TestHelpers(t *testing.T) {
	if got := Fold("Müller Straße"); got != "mueller strasse" {
		t.Errorf("Fold = %q", got)
	}
	if got := NormIBAN("de12 3456 7890"); got != "DE1234567890" {
		t.Errorf("NormIBAN = %q", got)
	}
	loc := time.FixedZone("CEST", 2*3600)
	if got := Day(time.Date(2026, 9, 30, 23, 30, 0, 0, loc)); !got.Equal(ParseDate("2026-09-30")) {
		t.Errorf("Day keeps the local calendar day: %v", got)
	}
	if ParseBook("verwaltung") != BookProperty || ParseBook("x") != BookHousehold {
		t.Error("ParseBook")
	}
	if DirectionOf(1) != DirectionIn || DirectionOf(-1) != DirectionOut {
		t.Error("DirectionOf")
	}
	if !ValidMonth("2026-10") || ValidMonth("2026-13") || !ValidDate("2026-02-28") || ValidDate("28.02.2026") {
		t.Error("ValidMonth/ValidDate")
	}
}
