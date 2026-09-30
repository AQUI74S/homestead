// Package syncer fetches transactions from the bank, stores them and triggers
// classification. Automatic syncs respect the banks' daily request limits.
package syncer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
)

const (
	// startDelay: the first automatic check runs shortly after start-up ...
	startDelay = 15 * time.Second
	// ... and then every tickInterval.
	tickInterval = 15 * time.Minute
	// syncTimeout bounds a sync started in the background.
	syncTimeout = 15 * time.Minute
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

// Problem is a sync error assigned to a book; "" = both.
type Problem struct {
	Book domain.Book `json:"book"`
	Text string      `json:"text"`
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
	return &Syncer{st: st, p: p, log: log, now: time.Now, dailyLimit: DefaultDailyLimit, minInterval: DefaultMinInterval}
}

// Configure sets the default daily request limit per account and the minimum interval
// between automatic syncs.
func (s *Syncer) Configure(dailyLimit int, minInterval time.Duration) {
	if dailyLimit >= callsPerSync {
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

// Run checks regularly which accounts are due for an automatic sync and syncs
// those, within the bank's daily request limit.
func (s *Syncer) Run(ctx context.Context) {
	t := time.NewTimer(startDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.SyncDue(ctx); err != nil {
				s.log.Error("sync failed", "err", err)
			}
			t.Reset(tickInterval)
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
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
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
	problem := func(book domain.Book, text string) {
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
		if c.Status != domain.ConnActive {
			continue
		}
		if c.ValidUntil != nil && s.now().After(*c.ValidUntil) {
			_ = s.st.SetConnectionStatus(ctx, c.ID, domain.ConnExpired, "Freigabe abgelaufen – bitte Bank neu verbinden")
			problem(c.Book, c.ASPSPName+": Freigabe abgelaufen")
			continue
		}
		for _, a := range accounts {
			if a.ConnectionID == nil || *a.ConnectionID != c.ID || !a.Active || !want(a) {
				continue
			}
			label := c.ASPSPName + " " + a.Name
			n, err := s.syncAccount(ctx, a)
			newTotal += n
			if eb.RateLimited(err) {
				// Not an error for the user: the bank's daily limit is used up. Pause the
				// account and remember the limit for the schedule.
				s.handleRateLimit(ctx, a)
				s.log.Info("bank request limit reached, pausing account", "account", label,
					"until", s.now().Add(rateLimitPause).Format(time.RFC3339))
				continue
			}
			if err != nil {
				problem(a.Book, label+": "+err.Error())
				s.log.Warn("account sync failed", "account", label, "err", err)
				_ = s.st.SetAccountSynced(ctx, a.ID, err.Error())
				if eb.SessionInvalid(err) {
					_ = s.st.SetConnectionStatus(ctx, c.ID, domain.ConnExpired, "Bank meldet: Freigabe ungültig – bitte neu verbinden")
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
