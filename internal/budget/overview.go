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
type MonthForecast struct {
	Items      []Item                `json:"items"`
	OpenIn     int64                 `json:"open_in"`
	OpenOut    int64                 `json:"open_out"`
	OpenByKind map[domain.Kind]int64 `json:"open_by_kind"`
	BudgetRest int64                 `json:"budget_rest"` // variable spending still expected
	BudgetDays int                   `json:"budget_days"` // days left in the period
	IncomeIst  int64                 `json:"income_ist"`
	OutIst     int64                 `json:"out_ist"`
	Past       bool                  `json:"past"` // the period is over
	Projected  int64                 `json:"projected"`
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
	if p.Mode == domain.PeriodSalary {
		fc.DropNextSalary(p.SeriesIDs, month)
	}
	f := &ov.Forecast
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
	return ov, nil
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
