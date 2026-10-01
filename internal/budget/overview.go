package budget

import (
	"context"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

// upcomingDays is the window of "Demnächst abgebucht", independent of the budget month.
const upcomingDays = 31

// Overview is everything the monthly budget page shows.
type Overview struct {
	Report       *MonthReport    `json:"report"`
	Trend        []TrendPoint    `json:"trend"`
	Accounts     []store.Account `json:"accounts"`
	AboMonthly   int64           `json:"abo_monthly"`   // subscriptions per month
	FixedMonthly int64           `json:"fixed_monthly"` // fixed costs and loans per month
	Period       Period          `json:"period"`
	CurrentMonth string          `json:"current_month"`
	Upcoming     []Item          `json:"upcoming"` // next upcomingDays, all accounts
	Forecast     MonthForecast   `json:"forecast"`
}

// MonthForecast is the outlook until the end of the budget month.
//
// For the running month it starts from the real balances: what is on the
// household accounts now, plus income and minus payments still expected before
// the next salary (which ends the month), minus the variable spending still to
// come. For other months it is the balance of the month: income minus spending.
type MonthForecast struct {
	Items      []Item                `json:"items"`
	OpenIn     int64                 `json:"open_in"`
	OpenOut    int64                 `json:"open_out"`
	OpenByKind map[domain.Kind]int64 `json:"open_by_kind"`
	BudgetRest int64                 `json:"budget_rest"` // variable spending still expected
	BudgetDays int                   `json:"budget_days"` // days left in the period
	IncomeIst  int64                 `json:"income_ist"`
	OutIst     int64                 `json:"out_ist"`
	Past       bool                  `json:"past"`    // the period is over
	Current    bool                  `json:"current"` // the period runs today
	// Projected is the month balance: income - spending, actual and still expected.
	Projected int64 `json:"projected"`

	// Cash view, only for the running month.
	Balance       int64      `json:"balance"`        // sum of the household account balances
	BalanceAt     *time.Time `json:"balance_at"`     // oldest of these balances
	NoBalance     int        `json:"no_balance"`     // active household accounts without a balance
	CashProjected int64      `json:"cash_projected"` // balance at the end of the month (before the salary)
	NextSalary    int64      `json:"next_salary"`    // salary that starts the next month (0 = unknown)
	NextSalaryOn  string     `json:"next_salary_on"` // its expected date
}

// LoadOverview computes the overview of a budget month ("" or invalid = current month).
func LoadOverview(ctx context.Context, st *store.Store, month string) (*Overview, error) {
	p, err := LoadPeriods(ctx, st)
	if err != nil {
		return nil, err
	}
	month = p.MonthOrCurrent(month)
	ov := &Overview{Period: p.Describe(month), CurrentMonth: p.Current(time.Now())}
	if ov.Report, err = Report(ctx, st, p, month); err != nil {
		return nil, err
	}
	if ov.Trend, err = Trend(ctx, st, p, month, TrendMonths); err != nil {
		return nil, err
	}
	var rec []store.Recurring
	if ov.Accounts, rec, err = HouseholdRecurring(ctx, st); err != nil {
		return nil, err
	}

	recCats := map[int64]bool{} // categories whose spending is forecast as recurring payments
	for _, x := range rec {
		if x.Ended || x.Status == domain.StatusIgnored || x.Direction != domain.DirectionOut {
			continue
		}
		if x.CategoryID != nil {
			recCats[*x.CategoryID] = true
		}
		switch x.Kind {
		case domain.KindSubscription:
			ov.AboMonthly += x.MonthlyCents
		case domain.KindFixedCost, domain.KindLoan:
			ov.FixedMonthly += x.MonthlyCents
		}
	}

	start, end, _ := p.Range(month)
	today := domain.Today()
	fc := Compute(rec, start, end, today)
	f := &ov.Forecast
	if p.Mode == domain.PeriodSalary {
		fc.DropNextSalary(p.SeriesIDs, month)
		f.NextSalary, f.NextSalaryOn = nextSalary(rec, p.SeriesIDs, month, start, today)
	}
	var varBudget, varIst int64
	for _, l := range ov.Report.Lines {
		switch l.Group {
		case domain.GroupIncome:
			f.IncomeIst += l.IstCents
		case domain.GroupTransfer:
		default:
			f.OutIst += l.IstCents
			if l.Group == domain.GroupExpenses && !recCats[l.ID] {
				varBudget += l.BudgetCents
				varIst += l.IstCents
			}
		}
	}
	f.BudgetRest, f.BudgetDays = VariableRest(varBudget, varIst, start, end, today)
	ov.Upcoming = Upcoming(rec, today, today.AddDate(0, 0, upcomingDays))

	f.Past = !end.After(today)
	if f.Past {
		fc = Forecast{Items: fc.Items, OpenByKind: map[domain.Kind]int64{}}
		f.BudgetRest = 0
	}
	f.Items, f.OpenIn, f.OpenOut, f.OpenByKind = fc.Items, fc.OpenIn, fc.OpenOut, fc.OpenByKind
	f.Projected = f.IncomeIst + fc.OpenIn - f.OutIst - fc.OpenOut - f.BudgetRest

	f.Current = !start.After(today) && end.After(today)
	if f.Current {
		f.Balance, f.BalanceAt, f.NoBalance = SumBalances(ov.Accounts)
		f.CashProjected = f.Balance + fc.OpenIn - fc.OpenOut - f.BudgetRest
	}
	return ov, nil
}

// SumBalances adds up the balances of the active accounts and returns the time of
// the oldest balance and how many active accounts have none yet.
func SumBalances(accts []store.Account) (sum int64, oldest *time.Time, missing int) {
	for _, a := range accts {
		if !a.Active {
			continue
		}
		if a.BalanceCents == nil {
			missing++
			continue
		}
		sum += *a.BalanceCents
		if a.BalanceAt != nil && (oldest == nil || a.BalanceAt.Before(*oldest)) {
			oldest = a.BalanceAt
		}
	}
	return sum, oldest, missing
}

// nextSalary returns the salary payments that start the month after this one:
// their sum and the earliest expected date ("" if no salary series is known).
func nextSalary(rec []store.Recurring, seriesIDs []int64, month string, start, today time.Time) (int64, string) {
	ids := map[int64]bool{}
	for _, id := range seriesIDs {
		ids[id] = true
	}
	var salaries []store.Recurring
	for _, r := range rec {
		if ids[r.ID] {
			salaries = append(salaries, r)
		}
	}
	// look a bit beyond the month: the next salary is due around its end
	var sum int64
	label, date := "", ""
	for _, it := range Compute(salaries, start, start.AddDate(0, 2, 0), today).Items {
		if it.Status == ItemPaid || it.Amount <= 0 {
			continue
		}
		d, err := time.Parse(domain.DateLayout, it.Date)
		if err != nil || SalaryLabel(d) <= month {
			continue
		}
		l := SalaryLabel(d)
		if label != "" && l != label {
			break // items are sorted by date: the month after next
		}
		label = l
		sum += it.Amount
		if date == "" {
			date = it.Date
		}
	}
	return sum, date
}

// HouseholdRecurring returns the household accounts and the recurring payments that
// don't belong to the property management, with cycle label and monthly amount.
func HouseholdRecurring(ctx context.Context, st *store.Store) ([]store.Account, []store.Recurring, error) {
	accts, err := st.Accounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	rec, err := st.RecurringList(ctx)
	if err != nil {
		return nil, nil, err
	}
	book := map[int64]domain.Book{}
	household := []store.Account{}
	for _, a := range accts {
		book[a.ID] = a.Book
		if a.Book != domain.BookProperty {
			household = append(household, a)
		}
	}
	out := []store.Recurring{}
	for _, x := range rec {
		if x.AccountID != nil && book[*x.AccountID] == domain.BookProperty {
			continue
		}
		x.CycleLabel = x.CycleDays.Label()
		x.MonthlyCents = x.CycleDays.MonthlyCents(x.AvgCents)
		out = append(out, x)
	}
	return household, out, nil
}
