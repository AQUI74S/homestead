package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

type CategoryLine struct {
	ID            int64  `json:"id"`
	Group         string `json:"group"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Sort          int    `json:"sort"`
	DefaultBudget int64  `json:"default_budget"`
	BudgetCents   int64  `json:"budget"`
	IstCents      int64  `json:"ist"`   // income positive, expenses positive (absolute amount)
	Count         int    `json:"count"` // number of transactions
	Overridden    bool   `json:"budget_overridden"`
	AvgCents      int64  `json:"avg"` // monthly average over the last up to 12 budget months
}

type MonthReport struct {
	Month         string           `json:"month"`
	Lines         []CategoryLine   `json:"lines"`
	IncomeBy      map[string]int64 `json:"income_by_owner"` // "A" | "B" | "" -> cents
	Uncategorized int              `json:"uncategorized"`
	AvgPeriods    int              `json:"avg_periods"` // number of months in the average
	Period        Period           `json:"period"`
}

// Report computes budget and actuals per category for a month.
func (s *Store) Report(ctx context.Context, pc *PeriodCalc, month string) (*MonthReport, error) {
	start, end, err := pc.Range(month)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.id, c.grp, c.slug, c.name, (c.budget*100)::bigint, c.sort,
			(COALESCE(b.amount, c.budget)*100)::bigint, b.amount IS NOT NULL,
			COALESCE((SELECT (sum(t.amount)*100)::bigint FROM transactions t JOIN accounts a ON a.id=t.account_id
				WHERE t.category_id=c.id AND a.active AND a.book='haushalt' AND t.booking_date >= $1 AND t.booking_date < $2), 0),
			(SELECT count(*) FROM transactions t JOIN accounts a ON a.id=t.account_id
				WHERE t.category_id=c.id AND a.active AND a.book='haushalt' AND t.booking_date >= $1 AND t.booking_date < $2)
		FROM categories c
		LEFT JOIN budgets b ON b.category_id=c.id AND b.month=$3
		ORDER BY c.grp, c.sort, c.name`, start, end, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rep := &MonthReport{Month: month, IncomeBy: map[string]int64{"A": 0, "B": 0, "": 0}, Period: pc.Describe(month)}
	for rows.Next() {
		var l CategoryLine
		var signed int64
		if err := rows.Scan(&l.ID, &l.Group, &l.Slug, &l.Name, &l.DefaultBudget, &l.Sort, &l.BudgetCents, &l.Overridden, &signed, &l.Count); err != nil {
			return nil, err
		}
		if l.Group == "income" {
			l.IstCents = signed
		} else {
			l.IstCents = -signed // expenses as a positive amount; refunds reduce it
		}
		rep.Lines = append(rep.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// income by account holder (for the couple split)
	orows, err := s.DB.QueryContext(ctx, `SELECT a.owner, COALESCE((sum(t.amount)*100)::bigint,0)
		FROM transactions t JOIN accounts a ON a.id=t.account_id JOIN categories c ON c.id=t.category_id
		WHERE c.grp='income' AND a.active AND a.book='haushalt' AND t.booking_date >= $1 AND t.booking_date < $2 GROUP BY a.owner`, start, end)
	if err != nil {
		return nil, err
	}
	defer orows.Close()
	for orows.Next() {
		var o string
		var v int64
		if err := orows.Scan(&o, &v); err != nil {
			return nil, err
		}
		rep.IncomeBy[o] = v
	}
	err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE a.active AND a.book='haushalt' AND c.slug IN ('sonstiges','einnahmen-sonst') AND t.category_source = 'auto' AND t.booking_date >= $1 AND t.booking_date < $2`, start, end).Scan(&rep.Uncategorized)
	return rep, err
}

type TrendPoint struct {
	Month    string `json:"month"`
	Income   int64  `json:"income"`
	Bills    int64  `json:"bills"`
	Expenses int64  `json:"expenses"`
	Savings  int64  `json:"savings"`
	Debts    int64  `json:"debts"`
}

// Trend returns the totals per group for the last n periods up to and including month.
func (s *Store) Trend(ctx context.Context, pc *PeriodCalc, month string, n int) ([]TrendPoint, error) {
	m, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("Monat %q: erwartet JJJJ-MM", month)
	}
	out := make([]TrendPoint, n)
	starts := make([]time.Time, n+1)
	for i := 0; i < n; i++ {
		l := m.AddDate(0, i-n+1, 0).Format("2006-01")
		out[i].Month = l
		starts[i], starts[i+1], _ = pc.Range(l)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT t.booking_date, c.grp, (sum(t.amount)*100)::bigint
		FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE a.active AND a.book='haushalt' AND c.grp <> 'transfer' AND t.booking_date >= $1 AND t.booking_date < $2
		GROUP BY 1, 2`, starts[0], starts[n])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d time.Time
		var g string
		var v int64
		if err := rows.Scan(&d, &g, &v); err != nil {
			return nil, err
		}
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		i := sort.Search(n, func(i int) bool { return d.Before(starts[i+1]) })
		if i >= n || d.Before(starts[i]) {
			continue
		}
		p := &out[i]
		switch g {
		case "income":
			p.Income += v
		case "bills":
			p.Bills -= v
		case "expenses":
			p.Expenses -= v
		case "savings":
			p.Savings -= v
		case "debts":
			p.Debts -= v
		}
	}
	return out, rows.Err()
}

// BudgetAverages returns the monthly average per category (cents, expenses and income positive)
// over the last up to 12 full budget months – only as many as there is data for.
// Over 12 months, annual and quarterly payments are spread correctly as well.
func (s *Store) BudgetAverages(ctx context.Context, pc *PeriodCalc, today time.Time) (map[int64]int64, int, error) {
	cur, _ := time.Parse("2006-01", pc.Current(today))
	end, _, err := pc.Range(cur.Format("2006-01"))
	if err != nil {
		return nil, 0, err
	}
	var first sql.NullTime
	if err := s.DB.QueryRowContext(ctx, `SELECT min(t.booking_date) FROM transactions t JOIN accounts a ON a.id=t.account_id
		WHERE a.active AND a.book='haushalt'`).Scan(&first); err != nil {
		return nil, 0, err
	}
	out := map[int64]int64{}
	if !first.Valid {
		return out, 0, nil
	}
	n, start := 0, end
	for k := 1; k <= 12; k++ {
		st, _, err := pc.Range(cur.AddDate(0, -k, 0).Format("2006-01"))
		if err != nil || st.Before(first.Time) {
			break
		}
		n, start = k, st
	}
	if n == 0 {
		return out, 0, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.grp, (sum(t.amount)*100)::bigint
		FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE a.active AND a.book='haushalt' AND c.grp <> 'transfer' AND t.booking_date >= $1 AND t.booking_date < $2
		GROUP BY c.id, c.grp`, start, end)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sum int64
		var grp string
		if err := rows.Scan(&id, &grp, &sum); err != nil {
			return nil, 0, err
		}
		if grp != "income" {
			sum = -sum
		}
		if sum > 0 {
			out[id] = sum / int64(n)
		}
	}
	return out, n, rows.Err()
}

// SuggestBudgets sets budgets to the average rounded up to 10 € (see BudgetAverages).
// Without overwrite, only where no budget is set yet. With overwrite, all budgets are
// recalculated (even to 0) and monthly overrides from the current budget month onward are removed.
func (s *Store) SuggestBudgets(ctx context.Context, pc *PeriodCalc, today time.Time, overwrite bool) (int, int, error) {
	avg, n, err := s.BudgetAverages(ctx, pc, today)
	if err != nil || n == 0 {
		return 0, n, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, n, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, (budget*100)::bigint FROM categories WHERE grp <> 'transfer'`)
	if err != nil {
		return 0, n, err
	}
	type cb struct{ id, budget int64 }
	var cats []cb
	for rows.Next() {
		var c cb
		if err := rows.Scan(&c.id, &c.budget); err != nil {
			rows.Close()
			return 0, n, err
		}
		cats = append(cats, c)
	}
	rows.Close()
	changed := 0
	for _, c := range cats {
		v := (avg[c.id] + 999) / 1000 * 1000 // round up to full 10 €
		if v == c.budget || (!overwrite && (c.budget != 0 || v == 0)) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE categories SET budget=($2::bigint)::numeric/100 WHERE id=$1`, c.id, v); err != nil {
			return 0, n, err
		}
		changed++
	}
	if overwrite {
		if _, err := tx.ExecContext(ctx, `DELETE FROM budgets WHERE month >= $1`, pc.Current(today)); err != nil {
			return 0, n, err
		}
	}
	return changed, n, tx.Commit()
}
