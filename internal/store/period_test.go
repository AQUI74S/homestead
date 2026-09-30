package store

import (
	"testing"
	"time"
)

func day(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func pay(d string, euros int64, rec int64, label string) salaryPayment {
	return salaryPayment{date: day(d), cents: euros * 100, recurringID: rec, label: label}
}

func TestPeriodWaitsForLateSalary(t *testing.T) {
	pc := &PeriodCalc{Mode: "salary", byLabel: map[string]time.Time{}, today: day("2026-09-30")}
	pc.apply([]salaryPayment{pay("2026-06-30", 3000, 1, "Gehalt"), pay("2026-07-30", 3000, 1, "Gehalt"), pay("2026-08-29", 3000, 1, "Gehalt")})

	// 30 Sep: salary due today but not booked yet -> still September
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
	if s, _, _ := pc.Range("2026-10"); !s.Equal(day("2026-09-29")) {
		t.Errorf("after grace period: October starts %s, want 29.09. (usual offset)", s.Format("02.01."))
	}
	// future periods keep the usual salary day
	pc.today = day("2026-09-30")
	if s, _, _ := pc.Range("2026-11"); !s.Equal(day("2026-10-31")) && !s.Equal(day("2026-10-30")) {
		t.Errorf("November starts %s, want end of October", s.Format("02.01."))
	}
}

func TestPeriodFirstOfSeveralSalaries(t *testing.T) {
	// two selected salaries: A at the end of the month, B on the 1st – the first starts the period
	pc := &PeriodCalc{Mode: "salary", byLabel: map[string]time.Time{}, today: day("2026-10-15")}
	pc.apply([]salaryPayment{
		pay("2026-08-01", 2100, 2, "Gehalt B"), pay("2026-08-28", 3400, 1, "Gehalt A"),
		pay("2026-09-01", 2100, 2, "Gehalt B"), pay("2026-09-29", 3400, 1, "Gehalt A"),
		pay("2026-10-01", 2100, 2, "Gehalt B"),
	})
	if s, e, _ := pc.Range("2026-10"); !s.Equal(day("2026-09-29")) || e.Before(day("2026-10-27")) {
		t.Errorf("October = %s – %s, want start 29.09.", s.Format("02.01."), e.Format("02.01."))
	}
	if len(pc.SeriesIDs) != 2 {
		t.Errorf("series = %v", pc.SeriesIDs)
	}
}

func TestAutoLargeIncomeAroundMonthChange(t *testing.T) {
	pc := &PeriodCalc{Mode: "salary", byLabel: map[string]time.Time{}, today: day("2026-09-30")}
	var pays []salaryPayment
	for _, m := range []string{"2026-04", "2026-05", "2026-06", "2026-07", "2026-08"} {
		pays = append(pays,
			pay(m+"-28", 3400, 1, "Gehalt"),    // salary at month end
			pay(m+"-10", 777, 5, "Kindergeld"), // outside window and small
			pay(m+"-02", 250, 0, ""),           // in window but too small
		)
	}
	pays = append(pays, pay("2026-08-15", 2500, 0, "")) // large but mid-month
	got := pc.largeAroundMonthChange(pays)
	for _, p := range got {
		if p.recurringID != 1 {
			t.Errorf("unexpected period starter %s %d", p.date.Format("02.01."), p.cents)
		}
	}
	if len(got) != 5 {
		t.Errorf("found %d starters, want 5", len(got))
	}
	pc.apply(got)
	if s, _, _ := pc.Range("2026-08"); !s.Equal(day("2026-07-28")) {
		t.Errorf("August starts %s", s.Format("02.01."))
	}
}
