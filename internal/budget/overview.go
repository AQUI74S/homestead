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
	// Until is the end of the forecast (exclusive): the end of the period, or for
	// the running month the expected next salary if that comes later.
	Until string `json:"until"`
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
	f := &ov.Forecast
	f.Current = !start.After(today) && end.After(today)
	if p.Mode == domain.PeriodSalary {
		f.NextSalary, f.NextSalaryOn = nextSalary(rec, p.SeriesIDs, month, start, today)
	}
	until := forecastEnd(end, f.NextSalaryOn, f.Current)
	f.Until = until.Format(domain.DateLayout)
	fc := Compute(rec, start, until, today)
	if p.Mode == domain.PeriodSalary {
		fc.DropNextSalary(p.SeriesIDs, month)
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
	f.BudgetRest, f.BudgetDays = VariableRest(varBudget, varIst, start, until, today)
	ov.Upcoming = Upcoming(rec, today, today.AddDate(0, 0, upcomingDays))

	f.Past = !end.After(today)
	if f.Past {
		fc = Forecast{Items: fc.Items, OpenByKind: map[domain.Kind]int64{}}
		f.BudgetRest = 0
	}
	f.Items, f.OpenIn, f.OpenOut, f.OpenByKind = fc.Items, fc.OpenIn, fc.OpenOut, fc.OpenByKind
	ov.Report.Lines = withExpected(ov.Report.Lines, fc.Items, f.Past)
	f.Projected = f.IncomeIst + fc.OpenIn - f.OutIst - fc.OpenOut - f.BudgetRest

	if f.Current {
		f.Balance, f.BalanceAt, f.NoBalance = SumBalances(ov.Accounts)
		f.CashProjected = f.Balance + fc.OpenIn - fc.OpenOut - f.BudgetRest
	}
	return ov, nil
}

// PlannedByContracts reports whether a group is planned from the recognized
// contracts (expected amounts) rather than with a budget: everything except the
// variable spending.
func PlannedByContracts(g domain.Group) bool {
	switch g {
	case domain.GroupIncome, domain.GroupBills, domain.GroupSavings, domain.GroupDebts:
		return true
	}
	return false
}

// unassignedLabel names the line for open contract payments without a category.
const unassignedLabel = "Verträge ohne Kategorie"

// withExpected sets the expected amount of each category line: what was booked
// so far plus the contract payments still open in the forecast (none for a
// past month). Open payments of contracts without a category are collected in
// an extra line per group, so the group totals match the forecast.
func withExpected(lines []store.CategoryLine, items []Item, past bool) []store.CategoryLine {
	index := map[int64]int{}
	for i := range lines {
		lines[i].ExpectedCents = lines[i].IstCents
		index[lines[i].ID] = i
	}
	if past {
		return lines
	}
	unassigned := map[domain.Group]*store.CategoryLine{}
	for _, it := range items {
		if it.Status == ItemPaid {
			continue
		}
		var l *store.CategoryLine
		if it.CategoryID != nil {
			if i, ok := index[*it.CategoryID]; ok {
				l = &lines[i]
			}
		}
		if l == nil {
			g := kindGroup(it.Kind)
			if unassigned[g] == nil {
				unassigned[g] = &store.CategoryLine{Group: g, Name: unassignedLabel}
			}
			l = unassigned[g]
		}
		amount := it.Amount // signed, income positive
		if l.Group != domain.GroupIncome {
			amount = -amount
		}
		l.ExpectedCents += amount
		l.OpenDates = append(l.OpenDates, it.Date)
	}
	for _, g := range []domain.Group{domain.GroupIncome, domain.GroupBills, domain.GroupSavings, domain.GroupDebts, domain.GroupExpenses} {
		if u := unassigned[g]; u != nil {
			lines = append(lines, *u)
		}
	}
	return lines
}

// kindGroup is the group a contract without category counts in.
func kindGroup(k domain.Kind) domain.Group {
	switch k {
	case domain.KindIncome:
		return domain.GroupIncome
	case domain.KindLoan:
		return domain.GroupDebts
	case domain.KindSavings:
		return domain.GroupSavings
	}
	return domain.GroupBills
}

// forecastEnd returns where the forecast of a period stops (exclusive). The
// period ends on the usual salary day; if the next salary is expected later (it
// came late last time), everything due until then still has to be paid from the
// accounts, so the forecast of the running month runs until the salary.
func forecastEnd(end time.Time, nextSalaryOn string, current bool) time.Time {
	if d := domain.ParseDate(nextSalaryOn); current && d.After(end) {
		return d
	}
	return end
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
