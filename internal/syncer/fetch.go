package syncer

import (
	"context"
	"errors"
	"time"

	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
)

const (
	// overlapDays: each sync fetches again from this many days before the last
	// booking, so that late-booked transactions are not missed.
	overlapDays = 10
	// historyYears is how far back the first sync of an account asks.
	historyYears = 2
	// shortHistoryDays is the fallback for banks that only return 90 days without a new TAN.
	shortHistoryDays = 89
	// maxPages bounds the pagination of one transaction request.
	maxPages = 200
)

// balanceRank orders balance types when several have the same date: interim booked,
// expected, closing booked, interim available, closing available, opening booked.
var balanceRank = map[string]int{"ITBD": 0, "XPCD": 1, "CLBD": 2, "ITAV": 3, "CLAV": 4, "OPBD": 5}

func (s *Syncer) syncAccount(ctx context.Context, a store.Account) (int, error) {
	last, err := s.st.LastBookingDate(ctx, a.ID)
	if err != nil {
		return 0, err
	}
	today := s.now()
	first := last.IsZero()
	from := last.AddDate(0, 0, -overlapDays)
	if first {
		from = today.AddDate(-historyYears, 0, 0) // as far back as the bank allows
	}
	var txs []store.NewTxn
	err = s.call(ctx, a, func(ctx context.Context) (err error) {
		txs, err = s.fetch(ctx, a.ProviderUID, from, today)
		return err
	})
	if err != nil && first && clientError(err) {
		err = s.call(ctx, a, func(ctx context.Context) (err error) {
			txs, err = s.fetch(ctx, a.ProviderUID, today.AddDate(0, 0, -shortHistoryDays), today)
			return err
		})
	}
	if err != nil {
		return 0, err
	}
	n, err := s.st.InsertTransactions(ctx, a.ID, txs)
	if err != nil {
		return n, err
	}
	var bals []eb.Balance
	err = s.call(ctx, a, func(ctx context.Context) (err error) {
		bals, err = s.p.Balances(ctx, a.ProviderUID)
		return err
	})
	if eb.RateLimited(err) {
		return n, err
	}
	if err == nil {
		if c, ok := pickBalance(bals); ok {
			_ = s.st.SetAccountBalance(ctx, a.ID, c)
		}
	} else if eb.SessionInvalid(err) {
		return n, err
	}
	return n, nil
}

// clientError reports a 4xx answer that is neither an invalid session nor the rate limit,
// i.e. the bank did not like the request itself.
func clientError(err error) bool {
	var ae *eb.APIError
	return errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 && !eb.SessionInvalid(err) && !eb.RateLimited(err)
}

// call runs one bank request for an account and records it. A user-initiated request
// (PSU headers) that the bank rejects is retried once as a normal request, because
// some banks require headers we cannot provide.
func (s *Syncer) call(ctx context.Context, a store.Account, fn func(context.Context) error) error {
	_, attended := eb.PSUFrom(ctx)
	err := fn(ctx)
	_ = s.st.RecordBankCall(ctx, a.ID, attended)
	if attended && err != nil && clientError(err) {
		s.log.Warn("bank rejected user-initiated request, retrying as background request", "err", err)
		ctx = eb.WithoutPSU(ctx)
		err = fn(ctx)
		_ = s.st.RecordBankCall(ctx, a.ID, false)
	}
	return err
}

func (s *Syncer) fetch(ctx context.Context, uid string, from, to time.Time) ([]store.NewTxn, error) {
	var out []store.NewTxn
	seen := map[string]int{}
	cont := ""
	for page := 0; page < maxPages; page++ {
		p, err := s.p.Transactions(ctx, uid, from, to, cont)
		if err != nil {
			return nil, err
		}
		for _, t := range p.Transactions {
			n, err := eb.Normalize(t, seen)
			if err != nil {
				s.log.Warn("transaction skipped", "err", err)
				continue
			}
			if n.Pending { // take pending transactions only once they are booked
				continue
			}
			out = append(out, store.NewTxn{
				ExtID: n.ExtID, BookingDate: n.BookingDate, ValueDate: n.ValueDate, AmountCents: n.AmountCents,
				Currency: n.Currency, Counterparty: n.Counterparty, CounterpartyIBAN: n.CounterpartyIBAN,
				Remittance: n.Remittance, BankCode: n.BankCode,
			})
		}
		if p.ContinuationKey == "" {
			return out, nil
		}
		cont = p.ContinuationKey
	}
	return out, nil
}

// pickBalance returns the most current balance. Banks often report the closing
// booked balance (CLBD) of the previous day next to an interim balance that already
// includes today's bookings; the newest date wins, then the balance type (balanceRank).
func pickBalance(bals []eb.Balance) (int64, bool) {
	const dateLen = len("2006-01-02")
	date := func(b eb.Balance) string {
		if len(b.LastChange) >= dateLen && b.LastChange[:dateLen] > b.ReferenceDate {
			return b.LastChange[:dateLen]
		}
		return b.ReferenceDate
	}
	rank := func(b eb.Balance) int {
		if r, ok := balanceRank[b.BalanceType]; ok {
			return r
		}
		return len(balanceRank)
	}
	best := -1
	var bestCents int64
	for i, b := range bals {
		c, err := eb.ParseCents(b.BalanceAmount.Amount)
		if err != nil {
			continue
		}
		if best < 0 || date(b) > date(bals[best]) || (date(b) == date(bals[best]) && rank(b) < rank(bals[best])) {
			best, bestCents = i, c
		}
	}
	return bestCents, best >= 0
}
