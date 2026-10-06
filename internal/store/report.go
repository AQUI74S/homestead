package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/lib/pq"
)

// CategoryLine is budget and actual spending of a category in a budget month.
type CategoryLine struct {
	ID            int64        `json:"id"`
	Group         domain.Group `json:"group"`
	Slug          string       `json:"slug"`
	Name          string       `json:"name"`
	Sort          int          `json:"sort"`
	DefaultBudget int64        `json:"default_budget"`
	BudgetCents   int64        `json:"budget"`
	IstCents      int64        `json:"ist"`   // income positive, expenses positive (absolute amount)
	Count         int          `json:"count"` // number of transactions
	Overridden    bool         `json:"budget_overridden"`
	AvgCents      int64        `json:"avg"` // monthly average over the last up to 12 budget months
	// Set by the budget package for groups planned by contracts (not variable spending):
	ExpectedCents int64    `json:"expected"`   // booked so far plus what the contracts still expect
	OpenDates     []string `json:"open_dates"` // due dates of those open contract payments
}

// CategoryLines returns budget (with the override for month) and actuals in [start, end)
// for every category, household accounts only.
func (s *Store) CategoryLines(ctx context.Context, month string, start, end time.Time) ([]CategoryLine, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (CategoryLine, error) {
		var l CategoryLine
		var signed int64
		err := sc.Scan(&l.ID, &l.Group, &l.Slug, &l.Name, &l.DefaultBudget, &l.Sort, &l.BudgetCents, &l.Overridden, &signed, &l.Count)
		if l.Group == domain.GroupIncome {
			l.IstCents = signed
		} else {
			l.IstCents = -signed // expenses as a positive amount; refunds reduce it
		}
		return l, err
	}, `
		SELECT c.id, c.grp, c.slug, c.name, (c.budget*100)::bigint, c.sort,
			(COALESCE(b.amount, c.budget)*100)::bigint, b.amount IS NOT NULL,
			COALESCE((SELECT (sum(t.amount)*100)::bigint FROM transactions t JOIN accounts a ON a.id=t.account_id
				WHERE t.category_id=c.id AND `+sqlHousehold+` AND t.booking_date >= $1 AND t.booking_date < $2), 0),
			(SELECT count(*) FROM transactions t JOIN accounts a ON a.id=t.account_id
				WHERE t.category_id=c.id AND `+sqlHousehold+` AND t.booking_date >= $1 AND t.booking_date < $2)
		FROM categories c
		LEFT JOIN budgets b ON b.category_id=c.id AND b.month=$3
		ORDER BY c.grp, c.sort, c.name`, start, end, month)
}

// IncomeByOwner sums the income in [start, end) per account holder (for the couple split).
func (s *Store) IncomeByOwner(ctx context.Context, start, end time.Time) (map[domain.Owner]int64, error) {
	type row struct {
		owner domain.Owner
		cents int64
	}
	rows, err := queryAll(ctx, s.DB, func(sc scanner) (row, error) {
		var r row
		return r, sc.Scan(&r.owner, &r.cents)
	}, `SELECT a.owner, COALESCE((sum(t.amount)*100)::bigint,0)
		FROM transactions t JOIN accounts a ON a.id=t.account_id JOIN categories c ON c.id=t.category_id
		WHERE `+sqlIsIncome+` AND `+sqlHousehold+` AND t.booking_date >= $1 AND t.booking_date < $2 GROUP BY a.owner`, start, end)
	if err != nil {
		return nil, err
	}
	out := map[domain.Owner]int64{domain.OwnerA: 0, domain.OwnerB: 0, domain.OwnerJoint: 0}
	for _, r := range rows {
		out[r.owner] = r.cents
	}
	return out, nil
}

// CountUncategorized counts household transactions in [start, end) that nobody has placed yet.
func (s *Store) CountUncategorized(ctx context.Context, start, end time.Time) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE `+sqlHousehold+` AND `+sqlCatchAllAuto+` AND t.booking_date >= $1 AND t.booking_date < $2`, start, end).Scan(&n)
	return n, err
}

// DayGroupSum is the signed sum of a category group on one day.
type DayGroupSum struct {
	Day   time.Time
	Group domain.Group
	Cents int64
}

// DailyGroupSums returns the household sums per day and group in [from, to), without transfers.
func (s *Store) DailyGroupSums(ctx context.Context, from, to time.Time) ([]DayGroupSum, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (DayGroupSum, error) {
		var d DayGroupSum
		err := sc.Scan(&d.Day, &d.Group, &d.Cents)
		d.Day = domain.Day(d.Day)
		return d, err
	}, `SELECT t.booking_date, c.grp, (sum(t.amount)*100)::bigint
		FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE `+sqlHousehold+` AND `+sqlNotTransfer+` AND t.booking_date >= $1 AND t.booking_date < $2
		GROUP BY 1, 2`, from, to)
}

// CategorySum is the signed sum of a category.
type CategorySum struct {
	ID    int64
	Group domain.Group
	Cents int64
}

// CategorySums returns the household sums per category in [from, to), without transfers.
func (s *Store) CategorySums(ctx context.Context, from, to time.Time) ([]CategorySum, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (CategorySum, error) {
		var c CategorySum
		return c, sc.Scan(&c.ID, &c.Group, &c.Cents)
	}, `SELECT c.id, c.grp, (sum(t.amount)*100)::bigint
		FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE `+sqlHousehold+` AND `+sqlNotTransfer+` AND t.booking_date >= $1 AND t.booking_date < $2
		GROUP BY c.id, c.grp`, from, to)
}

// AccountCategorySums is CategorySums for a single account.
func (s *Store) AccountCategorySums(ctx context.Context, accountID int64, from, to time.Time) ([]CategorySum, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (CategorySum, error) {
		var c CategorySum
		return c, sc.Scan(&c.ID, &c.Group, &c.Cents)
	}, `SELECT c.id, c.grp, (sum(t.amount)*100)::bigint
		FROM transactions t JOIN categories c ON c.id=t.category_id JOIN accounts a ON a.id=t.account_id
		WHERE `+sqlHousehold+` AND `+sqlNotTransfer+` AND t.account_id=$1 AND t.booking_date >= $2 AND t.booking_date < $3
		GROUP BY c.id, c.grp`, accountID, from, to)
}

// FirstBookingPerAccount returns the earliest booking date of each household account
// that has transactions: how far back its data goes.
func (s *Store) FirstBookingPerAccount(ctx context.Context) (map[int64]time.Time, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.account_id, min(t.booking_date) FROM transactions t JOIN accounts a ON a.id=t.account_id
		WHERE `+sqlHousehold+` GROUP BY t.account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id int64
		var first time.Time
		if err := rows.Scan(&id, &first); err != nil {
			return nil, err
		}
		out[id] = first
	}
	return out, rows.Err()
}

// FirstHouseholdBooking returns the earliest booking date on household accounts
// (ok=false without any transactions).
func (s *Store) FirstHouseholdBooking(ctx context.Context) (time.Time, bool, error) {
	var first sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT min(t.booking_date) FROM transactions t JOIN accounts a ON a.id=t.account_id
		WHERE `+sqlHousehold).Scan(&first)
	return first.Time, first.Valid, err
}

// IncomePayment is an incoming household payment that may start a budget period.
type IncomePayment struct {
	Date        time.Time
	Cents       int64
	RecurringID int64
	Label       string // label of its recurring series
}

// IncomePayments loads the payments of the given series or – without series – all
// household income except refunds, oldest first.
func (s *Store) IncomePayments(ctx context.Context, seriesIDs []int64) ([]IncomePayment, error) {
	q := `SELECT t.booking_date, (t.amount*100)::bigint, COALESCE(t.recurring_id, 0), COALESCE(r.label, '')
		FROM transactions t JOIN accounts a ON a.id=t.account_id
		LEFT JOIN categories c ON c.id=t.category_id LEFT JOIN recurring r ON r.id=t.recurring_id
		WHERE ` + sqlHousehold + ` AND t.amount > 0 AND `
	var args []any
	if len(seriesIDs) > 0 {
		q += `t.recurring_id = ANY($1::bigint[])`
		args = append(args, pq.Array(seriesIDs))
	} else {
		q += sqlIsIncome + ` AND c.slug <> '` + domain.SlugRefund + `'`
	}
	return queryAll(ctx, s.DB, func(sc scanner) (IncomePayment, error) {
		var p IncomePayment
		err := sc.Scan(&p.Date, &p.Cents, &p.RecurringID, &p.Label)
		p.Date = domain.Day(p.Date)
		return p, err
	}, q+` ORDER BY t.booking_date`, args...)
}
