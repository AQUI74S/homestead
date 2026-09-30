package syncer

import (
	"context"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

const (
	// DefaultDailyLimit is the number of unattended requests per account and day
	// that PSD2 allows (banks may allow fewer; the syncer learns that from 429s).
	DefaultDailyLimit = 4
	// DefaultMinInterval is the shortest interval between automatic syncs of an account.
	DefaultMinInterval = 6 * time.Hour
	// limitWindow is the rolling window the banks count requests in.
	limitWindow = 24 * time.Hour
	// callsPerSync is the number of bank requests of one sync: transactions + balances.
	callsPerSync = 2
	// rateLimitPause is how long an account rests after the bank rejected a request
	// because of its limit (as recommended by Enable Banking).
	rateLimitPause = 6 * time.Hour
)

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
	iv := limitWindow / time.Duration(per)
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
	// wait until enough of the last window's requests have expired
	if over := len(calls) + callsPerSync - limit; over > 0 && over <= len(calls) {
		if free := calls[over-1].Add(limitWindow); free.After(next) {
			next = free
		}
	}
	sc.Next = &next
	return sc
}

// Schedules returns the automatic sync plan per account ID.
func (s *Syncer) Schedules(ctx context.Context) (map[int64]Schedule, error) {
	now := s.now()
	calls, err := s.st.UnattendedCalls(ctx, now.Add(-limitWindow))
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
		if !a.Active || a.ConnStatus != domain.ConnActive {
			sc.Next = nil
		}
		out[a.ID] = sc
	}
	return out, nil
}

// handleRateLimit pauses automatic syncs of the account and learns the bank's limit
// from the unattended requests that succeeded in the last window.
func (s *Syncer) handleRateLimit(ctx context.Context, a store.Account) {
	now := s.now()
	var learned *int
	if calls, err := s.st.UnattendedCalls(ctx, now.Add(-limitWindow)); err == nil {
		// the rejected request is already recorded; the ones before it went through
		if n := len(calls[a.ID]) - 1; n >= callsPerSync && n < s.dailyLimit {
			learned = &n
		}
	}
	_ = s.st.SetAccountLimited(ctx, a.ID, now.Add(rateLimitPause), learned)
}
