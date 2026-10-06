package budget

import (
	"context"
	"math"
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
// over the last up to averageMonths full budget months.
//
// Each account only counts the months it has data for: an account whose
// transactions go back six months is averaged over six months, not over the
// twelve of an older account (months without data would count as nothing spent
// or earned). The second result is the longest window used.
func Averages(ctx context.Context, st *store.Store, p *Periods, today time.Time) (map[int64]int64, int, error) {
	cur, _ := time.Parse(domain.MonthLayout, p.Current(today))
	end, _, err := p.Range(cur.Format(domain.MonthLayout))
	if err != nil {
		return nil, 0, err
	}
	// starts[k] is the start of the (k+1)-th full budget month before the current one
	var starts []time.Time
	for k := 1; k <= averageMonths; k++ {
		s, _, err := p.Range(cur.AddDate(0, -k, 0).Format(domain.MonthLayout))
		if err != nil {
			break
		}
		starts = append(starts, s)
	}
	firsts, err := st.FirstBookingPerAccount(ctx)
	if err != nil {
		return nil, 0, err
	}
	var parts []accountSums
	for acc, first := range firsts {
		n := monthsWithData(starts, first)
		if n == 0 {
			continue
		}
		sums, err := st.AccountCategorySums(ctx, acc, starts[n-1], end)
		if err != nil {
			return nil, 0, err
		}
		parts = append(parts, accountSums{months: n, sums: sums})
	}
	avg, n := combineAverages(parts)
	return avg, n, nil
}

// accountSums are the category sums of one account over its months with data.
type accountSums struct {
	months int
	sums   []store.CategorySum
}

// monthsWithData counts the budget months (starts, newest first) that lie
// completely after the first booking of an account.
func monthsWithData(starts []time.Time, first time.Time) int {
	n := 0
	for _, s := range starts {
		if s.Before(first) {
			break
		}
		n++
	}
	return n
}

// combineAverages adds up the monthly averages of the accounts per category
// (expenses and income positive, categories with a negative total left out)
// and returns them with the longest window.
func combineAverages(parts []accountSums) (map[int64]int64, int) {
	total := map[int64]float64{}
	longest := 0
	for _, a := range parts {
		longest = max(longest, a.months)
		for _, c := range a.sums {
			v := float64(c.Cents)
			if c.Group != domain.GroupIncome {
				v = -v
			}
			total[c.ID] += v / float64(a.months)
		}
	}
	out := map[int64]int64{}
	for id, v := range total {
		if v > 0 {
			out[id] = int64(math.Round(v))
		}
	}
	return out, longest
}

// SuggestBudgets sets the budgets of the variable spending to the average rounded
// up to full 10 € (see Averages); the other groups are planned from contracts.
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
