// Package syncer fetches transactions from the bank, stores them and triggers classification.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/AQUI74S/homestead/internal/classify"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/hv"
	"github.com/AQUI74S/homestead/internal/store"
)

type Syncer struct {
	st  *store.Store
	p   eb.Provider
	log *slog.Logger
	now func() time.Time

	dailyLimit  int           // unattended requests per account and 24 h (default, banks may allow fewer)
	minInterval time.Duration // shortest interval between automatic syncs of an account

	mu     sync.Mutex // prevents concurrent runs
	stMu   sync.Mutex
	status Status
}

// Problem is a sync error assigned to a book (haushalt | verwaltung); "" = both.
type Problem struct {
	Book string `json:"book"`
	Text string `json:"text"`
}

type Status struct {
	Running    bool      `json:"running"`
	LastStart  time.Time `json:"last_start"`
	LastFinish time.Time `json:"last_finish"`
	NewTxns    int       `json:"new_transactions"`
	Errors     []string  `json:"errors"`
	Problems   []Problem `json:"problems"`
}

func New(st *store.Store, p eb.Provider, log *slog.Logger) *Syncer {
	return &Syncer{st: st, p: p, log: log, now: time.Now, dailyLimit: 4, minInterval: 6 * time.Hour}
}

// Configure sets the default daily request limit per account and the minimum interval
// between automatic syncs.
func (s *Syncer) Configure(dailyLimit int, minInterval time.Duration) {
	if dailyLimit >= 2 {
		s.dailyLimit = dailyLimit
	}
	if minInterval > 0 {
		s.minInterval = minInterval
	}
}

func (s *Syncer) Status() Status {
	s.stMu.Lock()
	defer s.stMu.Unlock()
	st := s.status
	st.Errors = append([]string(nil), st.Errors...)
	st.Problems = append([]Problem{}, st.Problems...)
	return st
}

func (s *Syncer) setStatus(f func(*Status)) {
	s.stMu.Lock()
	f(&s.status)
	s.stMu.Unlock()
}

// Run checks every 15 minutes which accounts are due for an automatic sync and
// syncs those, within the bank's daily request limit.
func (s *Syncer) Run(ctx context.Context) {
	t := time.NewTimer(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.SyncDue(ctx); err != nil {
				s.log.Error("sync failed", "err", err)
			}
			t.Reset(15 * time.Minute)
		}
	}
}

// TriggerAsync starts a sync of all accounts in the background unless one is already
// running. With psu set (user clicked "sync now" or just connected a bank) the
// requests are user-initiated and do not count towards the daily limit.
func (s *Syncer) TriggerAsync(psu *eb.PSU) bool {
	if s.Status().Running {
		return false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if psu != nil {
			ctx = eb.WithPSU(ctx, *psu)
		}
		if err := s.SyncAll(ctx); err != nil {
			s.log.Error("sync failed", "err", err)
		}
	}()
	return true
}

// callsPerSync is the number of bank requests of one sync: transactions + balances.
const callsPerSync = 2

// rateLimitPause is how long an account rests after the bank rejected a request
// because of its limit (as recommended by Enable Banking).
const rateLimitPause = 6 * time.Hour

// Schedule describes the automatic sync plan of an account.
type Schedule struct {
	Limit        int        `json:"limit"`         // unattended requests per 24 h
	Learned      bool       `json:"learned"`       // limit learned from the bank
	Used         int        `json:"used"`          // unattended requests in the last 24 h
	Interval     string     `json:"interval"`      // e.g. "12h0m0s"
	Next         *time.Time `json:"next"`          // next automatic sync (nil = never, e.g. inactive)
	LimitedUntil *time.Time `json:"limited_until"` // paused after the bank's limit was hit
}

// interval spreads the allowed syncs over the day, but not more often than minInterval.
func (s *Syncer) interval(limit int) time.Duration {
	per := limit / callsPerSync
	if per < 1 {
		per = 1
	}
	iv := 24 * time.Hour / time.Duration(per)
	if iv < s.minInterval {
		iv = s.minInterval
	}
	return iv
}

// plan computes when the account may be synced automatically next.
func (s *Syncer) plan(a store.Account, calls []time.Time, now time.Time) Schedule {
	limit, learned := s.dailyLimit, false
	if a.DailyLimit != nil && *a.DailyLimit < limit {
		limit, learned = *a.DailyLimit, true
	}
	if limit < callsPerSync {
		limit = callsPerSync
	}
	iv := s.interval(limit)
	sc := Schedule{Limit: limit, Learned: learned, Used: len(calls), Interval: iv.String(), LimitedUntil: a.LimitedUntil}
	next := now
	if a.LastSynced != nil && a.LastSynced.Add(iv).After(next) {
		next = a.LastSynced.Add(iv)
	}
	if a.LimitedUntil != nil && a.LimitedUntil.After(next) {
		next = *a.LimitedUntil
	}
	// wait until enough of the last 24 h's requests have expired
	if over := len(calls) + callsPerSync - limit; over > 0 && over <= len(calls) {
		if free := calls[over-1].Add(24 * time.Hour); free.After(next) {
			next = free
		}
	}
	sc.Next = &next
	return sc
}

// Schedules returns the automatic sync plan per account ID.
func (s *Syncer) Schedules(ctx context.Context) (map[int64]Schedule, error) {
	now := s.now()
	calls, err := s.st.UnattendedCalls(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	accounts, err := s.st.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]Schedule{}
	for _, a := range accounts {
		sc := s.plan(a, calls[a.ID], now)
		if !a.Active || a.ConnStatus != "active" {
			sc.Next = nil
		}
		out[a.ID] = sc
	}
	return out, nil
}

// SyncDue syncs the accounts whose next automatic sync is due.
func (s *Syncer) SyncDue(ctx context.Context) error {
	sched, err := s.Schedules(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	due := map[int64]bool{}
	for id, sc := range sched {
		if sc.Next != nil && !sc.Next.After(now) {
			due[id] = true
		}
	}
	if len(due) == 0 {
		return nil
	}
	return s.sync(ctx, func(a store.Account) bool { return due[a.ID] })
}

// SyncAll syncs all active accounts now and then reclassifies. Requests count towards
// the daily limit unless the context carries the user (eb.WithPSU).
func (s *Syncer) SyncAll(ctx context.Context) error {
	return s.sync(ctx, func(store.Account) bool { return true })
}

func (s *Syncer) sync(ctx context.Context, want func(store.Account) bool) error {
	if !s.mu.TryLock() {
		return nil
	}
	defer s.mu.Unlock()
	s.setStatus(func(st *Status) { *st = Status{Running: true, LastStart: s.now(), LastFinish: st.LastFinish} })
	var errs []string
	problems := []Problem{}
	problem := func(book, text string) {
		errs = append(errs, text)
		problems = append(problems, Problem{Book: book, Text: text})
	}
	newTotal := 0
	defer func() {
		s.setStatus(func(st *Status) {
			st.Running, st.LastFinish, st.NewTxns, st.Errors, st.Problems = false, s.now(), newTotal, errs, problems
		})
	}()

	conns, err := s.st.Connections(ctx)
	if err != nil {
		return err
	}
	accounts, err := s.st.Accounts(ctx)
	if err != nil {
		return err
	}
	for _, c := range conns {
		if c.Status != "active" {
			continue
		}
		if c.ValidUntil != nil && s.now().After(*c.ValidUntil) {
			_ = s.st.SetConnectionStatus(ctx, c.ID, "expired", "Freigabe abgelaufen – bitte Bank neu verbinden")
			problem(c.Book, c.ASPSPName+": Freigabe abgelaufen")
			continue
		}
		for _, a := range accounts {
			if a.ConnectionID == nil || *a.ConnectionID != c.ID || !a.Active || !want(a) {
				continue
			}
			n, err := s.syncAccount(ctx, a)
			newTotal += n
			if eb.RateLimited(err) {
				// Not an error for the user: the bank's daily limit is used up. Pause the
				// account and remember the limit for the schedule.
				s.handleRateLimit(ctx, a)
				s.log.Info("bank request limit reached, pausing account", "account", c.ASPSPName+" "+a.Name,
					"until", s.now().Add(rateLimitPause).Format(time.RFC3339))
				continue
			}
			if err != nil {
				label := fmt.Sprintf("%s %s", c.ASPSPName, a.Name)
				problem(a.Book, label+": "+err.Error())
				s.log.Warn("account sync failed", "account", label, "err", err)
				_ = s.st.SetAccountSynced(ctx, a.ID, err.Error())
				if eb.SessionInvalid(err) {
					_ = s.st.SetConnectionStatus(ctx, c.ID, "expired", "Bank meldet: Freigabe ungültig – bitte neu verbinden")
					break
				}
				continue
			}
			_ = s.st.SetAccountSynced(ctx, a.ID, "")
		}
	}
	if n, err := s.st.RemoveCSVDuplicates(ctx); err != nil {
		problem("", "Doppelte Importe: "+err.Error())
	} else if n > 0 {
		s.log.Info("imported transactions replaced by bank sync", "count", n)
	}
	if err := s.Reclassify(ctx); err != nil {
		problem("", "Klassifizierung: "+err.Error())
		return err
	}
	s.log.Info("sync finished", "new_transactions", newTotal, "errors", len(errs))
	return nil
}

func (s *Syncer) syncAccount(ctx context.Context, a store.Account) (int, error) {
	last, err := s.st.LastBookingDate(ctx, a.ID)
	if err != nil {
		return 0, err
	}
	today := s.now()
	first := last.IsZero()
	from := last.AddDate(0, 0, -10) // overlap: don't miss late-booked transactions
	if first {
		from = today.AddDate(-2, 0, 0) // as far back as the bank allows
	}
	var txs []store.NewTxn
	err = s.call(ctx, a, func(ctx context.Context) (err error) {
		txs, err = s.fetch(ctx, a.ProviderUID, from, today)
		return err
	})
	var ae *eb.APIError
	if err != nil && first && errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 && !eb.SessionInvalid(err) && !eb.RateLimited(err) {
		// Many banks only return 90 days without a new TAN.
		err = s.call(ctx, a, func(ctx context.Context) (err error) {
			txs, err = s.fetch(ctx, a.ProviderUID, today.AddDate(0, 0, -89), today)
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

// call runs one bank request for an account and records it. A user-initiated request
// (PSU headers) that the bank rejects is retried once as a normal request, because
// some banks require headers we cannot provide.
func (s *Syncer) call(ctx context.Context, a store.Account, fn func(context.Context) error) error {
	_, attended := eb.PSUFrom(ctx)
	err := fn(ctx)
	_ = s.st.RecordBankCall(ctx, a.ID, attended)
	var ae *eb.APIError
	if attended && err != nil && errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 &&
		!eb.SessionInvalid(err) && !eb.RateLimited(err) {
		s.log.Warn("bank rejected user-initiated request, retrying as background request", "err", err)
		ctx = eb.WithoutPSU(ctx)
		err = fn(ctx)
		_ = s.st.RecordBankCall(ctx, a.ID, false)
	}
	return err
}

// handleRateLimit pauses automatic syncs of the account and learns the bank's limit
// from the unattended requests that succeeded in the last 24 hours.
func (s *Syncer) handleRateLimit(ctx context.Context, a store.Account) {
	now := s.now()
	var learned *int
	if calls, err := s.st.UnattendedCalls(ctx, now.Add(-24*time.Hour)); err == nil {
		// the rejected request is already recorded; the ones before it went through
		if n := len(calls[a.ID]) - 1; n >= callsPerSync && n < s.dailyLimit {
			learned = &n
		}
	}
	_ = s.st.SetAccountLimited(ctx, a.ID, now.Add(rateLimitPause), learned)
}

func (s *Syncer) fetch(ctx context.Context, uid string, from, to time.Time) ([]store.NewTxn, error) {
	var out []store.NewTxn
	seen := map[string]int{}
	cont := ""
	for page := 0; page < 200; page++ {
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
// includes today's bookings; the newest date wins, then the balance type in the
// order below.
func pickBalance(bals []eb.Balance) (int64, bool) {
	rank := map[string]int{"ITBD": 0, "XPCD": 1, "CLBD": 2, "ITAV": 3, "CLAV": 4, "OPBD": 5}
	date := func(b eb.Balance) string {
		if b.LastChange != "" && len(b.LastChange) >= 10 && b.LastChange[:10] > b.ReferenceDate {
			return b.LastChange[:10]
		}
		return b.ReferenceDate
	}
	rk := func(b eb.Balance) int {
		if r, ok := rank[b.BalanceType]; ok {
			return r
		}
		return len(rank)
	}
	best := -1
	var bestCents int64
	for i, b := range bals {
		c, err := eb.ParseCents(b.BalanceAmount.Amount)
		if err != nil {
			continue
		}
		if best < 0 || date(b) > date(bals[best]) || (date(b) == date(bals[best]) && rk(b) < rk(bals[best])) {
			best, bestCents = i, c
		}
	}
	return bestCents, best >= 0
}

// Reclassify reclassifies all transactions (except manually set ones) and updates the
// detected recurring payments.
func (s *Syncer) Reclassify(ctx context.Context) error {
	rows, err := s.st.ClassRows(ctx)
	if err != nil {
		return err
	}
	rules, err := s.st.Rules(ctx)
	if err != nil {
		return err
	}
	own, err := s.st.OwnIBANs(ctx)
	if err != nil {
		return err
	}
	cats, err := s.st.Categories(ctx)
	if err != nil {
		return err
	}
	groupOf := map[string]string{}
	for _, c := range cats {
		groupOf[c.Slug] = c.Group
	}
	cctx := classify.Context{OwnIBANs: own}
	bookOf := map[int64]string{}
	cctx.IBANBook = map[string]string{}
	if accs, err := s.st.Accounts(ctx); err == nil {
		var names []string
		for _, a := range accs {
			names = append(names, a.Name, a.DisplayName)
			bookOf[a.ID] = a.Book
			if a.IBAN != "" {
				cctx.IBANBook[strings.ToUpper(strings.ReplaceAll(a.IBAN, " ", ""))] = a.Book
			}
		}
		cctx.OwnNames = classify.OwnNamesFrom(names)
	}
	if st, err := s.st.Settings(ctx); err == nil && st["own_names"] != "" {
		for _, n := range strings.Split(st["own_names"], ",") {
			if n = strings.TrimSpace(classify.NormName(n)); n != "" {
				cctx.OwnNames = append(cctx.OwnNames, n)
			}
		}
	}
	for _, r := range rules {
		cctx.Rules = append(cctx.Rules, classify.Rule{Field: r.Field, Pattern: r.Pattern, Slug: r.Slug})
	}

	txns := make([]classify.Txn, len(rows))
	for i, r := range rows {
		t := classify.Txn{ID: r.ID, AmountCents: r.AmountCents, Counterparty: r.Counterparty, CounterpartyIBAN: r.CounterpartyIBAN,
			Remittance: r.Remittance, BankCode: r.BankCode, Slug: r.Slug, Source: r.Source, Reason: r.Reason, Book: bookOf[r.AccountID]}
		classify.Classify(&t, cctx)
		if _, ok := groupOf[t.Slug]; !ok && t.Source != "manual" {
			t.Slug = "sonstiges"
		}
		txns[i] = t
	}

	// recurring payments of the last two-plus years (annual subscriptions need two occurrences)
	cutoff := s.now().AddDate(0, -26, 0)
	var pts []classify.Point
	slugOf, srcOf := map[int64]string{}, map[int64]string{}
	for i, r := range rows {
		t := txns[i]
		slugOf[t.ID], srcOf[t.ID] = t.Slug, t.Source
		if r.BookingDate.Before(cutoff) {
			continue
		}
		pts = append(pts, classify.Point{TxnID: t.ID, Date: r.BookingDate, AmountCents: t.AmountCents, MerchantKey: t.MerchantKey,
			Merchant: t.Merchant, Slug: t.Slug, Group: groupOf[t.Slug], AccountID: r.AccountID})
	}
	series := classify.DetectRecurring(pts, s.now())
	for _, rf := range classify.Refine(series, slugOf, srcOf) {
		for i := range txns {
			if txns[i].ID == rf.TxnID {
				txns[i].Slug, txns[i].Reason = rf.Slug, rf.Reason
				break
			}
		}
	}
	// store series after refinement (adopt the category of the latest payment)
	idx := map[int64]int{}
	for i, t := range txns {
		idx[t.ID] = i
	}
	var ups []store.RecurringUpsert
	for _, sr := range series {
		if len(sr.TxnIDs) > 0 {
			sr.Slug = txns[idx[sr.TxnIDs[len(sr.TxnIDs)-1]]].Slug
		}
		ups = append(ups, store.RecurringUpsert{Key: sr.Key, Direction: sr.Direction, Label: sr.Label, Kind: sr.Kind, Slug: sr.Slug,
			CycleDays: sr.CycleDays, Count: sr.Count, AvgCents: sr.MedianCents, LastCents: sr.LastCents,
			First: sr.First, Last: sr.Last, Next: sr.Next, Ended: sr.Ended, AccountID: sr.AccountID})
	}
	ids, err := s.st.SyncRecurring(ctx, ups)
	if err != nil {
		return err
	}
	recOf := map[int64]int64{}
	for _, sr := range series {
		id := ids[sr.Direction+"|"+sr.Key]
		for _, tid := range sr.TxnIDs {
			recOf[tid] = id
		}
	}

	var changed []store.ClassUpdate
	for i, r := range rows {
		t := txns[i]
		var rid *int64
		if id, ok := recOf[t.ID]; ok {
			rid = &id
		}
		if t.Merchant == r.Merchant && t.MerchantKey == r.MerchantKey && t.Slug == r.Slug && t.Source == r.Source &&
			t.Reason == r.Reason && eqPtr(rid, r.RecurringID) {
			continue
		}
		changed = append(changed, store.ClassUpdate{ID: t.ID, Merchant: t.Merchant, MerchantKey: t.MerchantKey,
			Slug: t.Slug, Source: t.Source, Reason: t.Reason, RecurringID: rid})
	}
	s.log.Info("classification", "transactions", len(rows), "changed", len(changed), "series", len(series))
	if err := s.st.ApplyClassification(ctx, changed); err != nil {
		return err
	}
	return hv.Reassign(ctx, s.st)
}

func eqPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// CompleteAuth completes the bank authorization: create the session, store accounts, start the first sync.
// psu (the user who just returned from the bank) makes the first sync user-initiated.
func (s *Syncer) CompleteAuth(ctx context.Context, state, code string, psu *eb.PSU) (*store.Connection, error) {
	conn, err := s.st.ConnectionByState(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("unbekannte oder bereits verwendete Freigabe: %w", err)
	}
	sess, err := s.p.CreateSession(ctx, code)
	if err != nil {
		_ = s.st.SetConnectionStatus(ctx, conn.ID, "error", err.Error())
		return nil, err
	}
	valid := sess.Access.ValidUntil
	if valid.IsZero() {
		valid = s.now().Add(90 * 24 * time.Hour)
	}
	if err := s.st.ActivateConnection(ctx, conn.ID, sess.SessionID, valid); err != nil {
		return nil, err
	}
	for _, a := range sess.Accounts {
		name := a.Name
		if name == "" {
			name = a.Product
		}
		if name == "" {
			name = "Konto"
		}
		id := conn.ID
		if _, err := s.st.UpsertAccount(ctx, store.Account{ConnectionID: &id, ProviderUID: a.UID, IBAN: a.AccountID.IBAN,
			BankName: conn.ASPSPName, Name: name, Currency: a.Currency, Book: conn.Book}); err != nil {
			return nil, err
		}
	}
	s.TriggerAsync(psu)
	return conn, nil
}
