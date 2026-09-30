package budget

import (
	"context"
	"sort"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

const (
	// TrendMonths is the number of budget months in the trend chart.
	TrendMonths = 12
	// averageMonths is the longest window for budget averages; over a full year
	// annual and quarterly payments are spread correctly.
	averageMonths = 12
	// suggestRoundCents: suggested budgets are rounded up to full 10 €.
	suggestRoundCents = 1000
)

// MonthReport is budget vs. actual per category for one budget month.
type MonthReport struct {
	Month         string                 `json:"month"`
	Lines         []store.CategoryLine   `json:"lines"`
	IncomeBy      map[domain.Owner]int64 `json:"income_by_owner"`
	Uncategorized int                    `json:"uncategorized"`
	AvgPeriods    int                    `json:"avg_periods"` // number of months in the average
	Period        Period                 `json:"period"`
}

// Report computes budget and actuals per category for a budget month, including
// the monthly averages of the previous months.
func Report(ctx context.Context, st *store.Store, p *Periods, month string) (*MonthReport, error) {
	start, end, err := p.Range(month)
	if err != nil {
		return nil, err
	}
	rep := &MonthReport{Month: month, Period: p.Describe(month)}
	if rep.Lines, err = st.CategoryLines(ctx, month, start, end); err != nil {
		return nil, err
	}
	if rep.IncomeBy, err = st.IncomeByOwner(ctx, start, end); err != nil {
		return nil, err
	}
	if rep.Uncategorized, err = st.CountUncategorized(ctx, start, end); err != nil {
		return nil, err
	}
	if avg, n, err := Averages(ctx, st, p, time.Now()); err == nil {
		rep.AvgPeriods = n
		for i := range rep.Lines {
			rep.Lines[i].AvgCents = avg[rep.Lines[i].ID]
		}
	}
	return rep, nil
}

// TrendPoint holds the totals per group of one budget month (all positive).
type TrendPoint struct {
	Month    string `json:"month"`
	Income   int64  `json:"income"`
	Bills    int64  `json:"bills"`
	Expenses int64  `json:"expenses"`
	Savings  int64  `json:"savings"`
	Debts    int64  `json:"debts"`
}

// Trend returns the totals per group for the last n budget months up to and including month.
func Trend(ctx context.Context, st *store.Store, p *Periods, month string, n int) ([]TrendPoint, error) {
	m, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	out := make([]TrendPoint, n)
	starts := make([]time.Time, n+1)
	for i := 0; i < n; i++ {
		l := m.AddDate(0, i-n+1, 0).Format(domain.MonthLayout)
		out[i].Month = l
		starts[i], starts[i+1], _ = p.Range(l)
	}
	sums, err := st.DailyGroupSums(ctx, starts[0], starts[n])
	if err != nil {
		return nil, err
	}
	for _, s := range sums {
		i := sort.Search(n, func(i int) bool { return s.Day.Before(starts[i+1]) })
		if i >= n || s.Day.Before(starts[i]) {
			continue
		}
		pt := &out[i]
		switch s.Group {
		case domain.GroupIncome:
			pt.Income += s.Cents
		case domain.GroupBills:
			pt.Bills -= s.Cents
		case domain.GroupExpenses:
			pt.Expenses -= s.Cents
		case domain.GroupSavings:
			pt.Savings -= s.Cents
		case domain.GroupDebts:
			pt.Debts -= s.Cents
		}
	}
	return out, nil
}

// Averages returns the monthly average per category (cents, expenses and income positive)
// over the last up to averageMonths full budget months – only as many as there is data for.
func Averages(ctx context.Context, st *store.Store, p *Periods, today time.Time) (map[int64]int64, int, error) {
	cur, _ := time.Parse(domain.MonthLayout, p.Current(today))
	end, _, err := p.Range(cur.Format(domain.MonthLayout))
	if err != nil {
		return nil, 0, err
	}
	out := map[int64]int64{}
	first, ok, err := st.FirstHouseholdBooking(ctx)
	if err != nil || !ok {
		return out, 0, err
	}
	n, start := 0, end
	for k := 1; k <= averageMonths; k++ {
		s, _, err := p.Range(cur.AddDate(0, -k, 0).Format(domain.MonthLayout))
		if err != nil || s.Before(first) {
			break
		}
		n, start = k, s
	}
	if n == 0 {
		return out, 0, nil
	}
	sums, err := st.CategorySums(ctx, start, end)
	if err != nil {
		return nil, 0, err
	}
	for _, c := range sums {
		sum := c.Cents
		if c.Group != domain.GroupIncome {
			sum = -sum
		}
		if sum > 0 {
			out[c.ID] = sum / int64(n)
		}
	}
	return out, n, nil
}

// SuggestBudgets sets budgets to the average rounded up to full 10 € (see Averages).
// Without overwrite, only where no budget is set yet. With overwrite, all budgets are
// recalculated (even to 0) and monthly overrides from the current budget month onward
// are removed. Returns the number of changed budgets and of months averaged.
func SuggestBudgets(ctx context.Context, st *store.Store, p *Periods, today time.Time, overwrite bool) (int, int, error) {
	avg, n, err := Averages(ctx, st, p, today)
	if err != nil || n == 0 {
		return 0, n, err
	}
	want := map[int64]int64{}
	for id, v := range avg {
		want[id] = (v + suggestRoundCents - 1) / suggestRoundCents * suggestRoundCents
	}
	keep := func(current, next int64) bool { return !overwrite && (current != 0 || next == 0) }
	clearFrom := ""
	if overwrite {
		clearFrom = p.Current(today)
	}
	changed, err := st.ApplyDefaultBudgets(ctx, want, keep, clearFrom)
	return changed, n, err
}
