package syncer

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
)

func TestPlan(t *testing.T) {
	s := New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Configure(4, 6*time.Hour)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	last := now.Add(-3 * time.Hour)
	acc := store.Account{LastSynced: &last}

	// 4 requests per day, 2 per sync -> every 12 h
	sc := s.plan(acc, []time.Time{last, last}, now)
	if sc.Interval != "12h0m0s" || !sc.Next.Equal(last.Add(12*time.Hour)) {
		t.Errorf("default: interval %s next %s", sc.Interval, sc.Next)
	}
	// 3 requests used in the last 24 h -> wait until the oldest one expires
	old := now.Add(-20 * time.Hour)
	sc = s.plan(store.Account{LastSynced: &old}, []time.Time{old, old.Add(time.Minute), last}, now)
	if want := old.Add(24 * time.Hour); !sc.Next.Equal(want) {
		t.Errorf("budget: next %s, want %s", sc.Next, want)
	}
	// learned limit of 3 -> one sync per day
	three := 3
	sc = s.plan(store.Account{LastSynced: &last, DailyLimit: &three}, nil, now)
	if sc.Limit != 3 || !sc.Learned || sc.Interval != "24h0m0s" {
		t.Errorf("learned: %+v", sc)
	}
	// paused after the bank's limit was hit
	until := now.Add(5 * time.Hour)
	sc = s.plan(store.Account{LimitedUntil: &until}, nil, now)
	if !sc.Next.Equal(until) {
		t.Errorf("paused: next %s, want %s", sc.Next, until)
	}
}

// limitedBank is the demo bank, but rejects every request after the first `allow`.
type limitedBank struct {
	*eb.Demo
	allow, n int
}

func (b *limitedBank) hit() error {
	b.n++
	if b.n > b.allow {
		return &eb.APIError{Status: 429, Body: `{"error":"ASPSP_RATE_LIMIT_EXCEEDED"}`}
	}
	return nil
}

func (b *limitedBank) Balances(ctx context.Context, uid string) ([]eb.Balance, error) {
	if err := b.hit(); err != nil {
		return nil, err
	}
	return b.Demo.Balances(ctx, uid)
}

func (b *limitedBank) Transactions(ctx context.Context, uid string, from, to time.Time, cont string) (*eb.TransactionPage, error) {
	if cont == "" {
		if err := b.hit(); err != nil {
			return nil, err
		}
	}
	return b.Demo.Transactions(ctx, uid, from, to, cont)
}

func TestRateLimitIsLearned(t *testing.T) {
	dsn := os.Getenv("HS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	st.DB.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	bank := &limitedBank{Demo: eb.NewDemo(), allow: 3} // like a bank with 3 requests per day
	s := New(st, bank, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := st.CreatePendingConnection(ctx, "Demo-Direktbank", "DE", "st1", "haushalt"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteAuth(ctx, "st1", "demo:Demo-Direktbank", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100 && s.Status().Running; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 100 && s.Status().Running; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	// first sync used 2 requests; the second hits the limit at request 4
	if err := s.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if p := s.Status().Problems; len(p) != 0 {
		t.Errorf("rate limit must not be reported as a problem: %v", p)
	}
	sched, err := s.Schedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range sched {
		if sc.Limit != 3 || !sc.Learned || sc.LimitedUntil == nil || sc.Next.Before(*sc.LimitedUntil) {
			t.Errorf("schedule after rate limit: %+v", sc)
		}
	}
	// user-initiated requests don't count towards the limit
	bank.allow = 100
	if err := s.SyncAll(eb.WithPSU(ctx, eb.PSU{IPAddress: "192.0.2.1", UserAgent: "test"})); err != nil {
		t.Fatal(err)
	}
	sched2, _ := s.Schedules(ctx)
	for id, sc := range sched2 {
		if sc.Used != sched[id].Used {
			t.Errorf("attended requests counted: %d -> %d", sched[id].Used, sc.Used)
		}
	}
}
