package store

import (
	"testing"
	"time"
)

func day(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func TestPeriodWaitsForLateSalary(t *testing.T) {
	// salary usually on the 30th, last booked on 29 Aug (period "September")
	pc := &PeriodCalc{Mode: "salary", typDay: 30, byLabel: map[string]time.Time{"2026-09": day("2026-08-29")}}

	// 30 Sep: salary due today but not booked yet -> still September
	pc.today = day("2026-09-30")
	if got := pc.Current(pc.today); got != "2026-09" {
		t.Fatalf("salary not booked yet: current = %s, want 2026-09", got)
	}
	if s, e, _ := pc.Range("2026-09"); !s.Equal(day("2026-08-29")) || !e.Equal(day("2026-10-01")) {
		t.Errorf("September range = %s – %s", s.Format("02.01."), e.Format("02.01."))
	}

	// salary arrives on 1 Oct -> October starts that day
	pc.today = day("2026-10-01")
	pc.byLabel["2026-10"] = day("2026-10-01")
	if got := pc.Current(pc.today); got != "2026-10" {
		t.Errorf("after salary: current = %s, want 2026-10", got)
	}

	// no salary for more than a week -> the period starts on the usual day anyway
	delete(pc.byLabel, "2026-10")
	pc.today = day("2026-10-08")
	if s, _, _ := pc.Range("2026-10"); !s.Equal(day("2026-09-30")) {
		t.Errorf("after grace period: October starts %s, want 30.09.", s.Format("02.01."))
	}

	// future periods keep the usual salary day
	pc.today = day("2026-09-30")
	if s, _, _ := pc.Range("2026-11"); !s.Equal(day("2026-10-30")) {
		t.Errorf("November starts %s, want 30.10.", s.Format("02.01."))
	}
}
