package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/lib/pq"
)

// maxCatchUpSteps bounds the loop that moves a past due date of a new contract
// forward (520 weekly steps = 10 years).
const maxCatchUpSteps = 520

type Recurring struct {
	ID           int64                  `json:"id"`
	Key          string                 `json:"merchant_key"`
	Direction    domain.Direction       `json:"direction"`
	Label        string                 `json:"label"`
	CycleDays    domain.Cycle           `json:"cycle_days"`
	CycleLabel   string                 `json:"cycle"`
	AvgCents     int64                  `json:"amount"`
	LastCents    int64                  `json:"last_amount"`
	MonthlyCents int64                  `json:"monthly"`
	FirstDate    string                 `json:"first_date"`
	LastDate     string                 `json:"last_date"`
	NextDate     string                 `json:"next_date"`
	Count        int                    `json:"occurrences"`
	Kind         domain.Kind            `json:"kind"`
	KindLocked   bool                   `json:"kind_locked"`
	CategoryID   *int64                 `json:"category_id"`
	Status       domain.RecurringStatus `json:"status"`
	Ended        bool                   `json:"ended"`
	Manual       bool                   `json:"manual"`
	AccountID    *int64                 `json:"account_id"`
}

// Active reports whether the series is still expected to be paid.
func (r Recurring) Active() bool {
	return !r.Ended && r.Status != domain.StatusIgnored && r.CycleDays > 0
}

// RecurringUpsert is a series detected by classification.
type RecurringUpsert struct {
	Key, Label, Slug    string
	Direction           domain.Direction
	Kind                domain.Kind
	CycleDays           domain.Cycle
	Count               int
	AvgCents, LastCents int64
	First, Last, Next   time.Time
	Ended               bool
	AccountID           int64
}

// SeriesKey identifies a series across detection runs.
func SeriesKey(dir domain.Direction, merchantKey string) string {
	return string(dir) + "|" + merchantKey
}

// SyncRecurring stores detected series. Kind/status set by the user are preserved;
// unconfirmed series that are no longer detected are removed. Returns SeriesKey -> ID.
func (s *Store) SyncRecurring(ctx context.Context, series []RecurringUpsert) (map[string]int64, error) {
	ids := map[string]int64{}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var keys []string
		for _, r := range series {
			var id int64
			err := tx.QueryRowContext(ctx, `INSERT INTO recurring (merchant_key, direction, label, cycle_days, avg_amount, last_amount,
					first_date, last_date, next_date, occurrences, kind, category_id, ended, account_id, updated_at)
				VALUES ($1,$2,$3,$4,($5::bigint)::numeric/100,($6::bigint)::numeric/100,$7,$8,$9,$10,$11,(SELECT id FROM categories WHERE slug=$12),$13,NULLIF($14,0),now())
				ON CONFLICT (merchant_key, direction) DO UPDATE SET label=EXCLUDED.label, cycle_days=EXCLUDED.cycle_days,
					avg_amount=EXCLUDED.avg_amount, last_amount=EXCLUDED.last_amount, first_date=EXCLUDED.first_date,
					last_date=EXCLUDED.last_date, next_date=EXCLUDED.next_date, occurrences=EXCLUDED.occurrences,
					kind=CASE WHEN recurring.kind_locked THEN recurring.kind ELSE EXCLUDED.kind END,
					category_id=EXCLUDED.category_id, ended=EXCLUDED.ended, account_id=EXCLUDED.account_id, updated_at=now()
				RETURNING id`,
				r.Key, r.Direction, r.Label, r.CycleDays, r.AvgCents, r.LastCents, r.First, r.Last, r.Next, r.Count, r.Kind, r.Slug, r.Ended, r.AccountID).Scan(&id)
			if err != nil {
				return fmt.Errorf("Serie %s: %w", r.Key, err)
			}
			k := SeriesKey(r.Direction, r.Key)
			ids[k] = id
			keys = append(keys, k)
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM recurring WHERE status=$2 AND NOT manual AND NOT (direction || '|' || merchant_key = ANY($1))`,
			pq.Array(keys), domain.StatusDetected)
		return err
	})
	return ids, err
}

func (s *Store) RecurringList(ctx context.Context) ([]Recurring, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (Recurring, error) {
		var r Recurring
		var cat, acc sql.NullInt64
		err := sc.Scan(&r.ID, &r.Key, &r.Direction, &r.Label, &r.CycleDays, &r.AvgCents, &r.LastCents, &r.FirstDate, &r.LastDate,
			&r.NextDate, &r.Count, &r.Kind, &r.KindLocked, &cat, &r.Status, &r.Ended, &r.Manual, &acc)
		r.CategoryID, r.AccountID = ptrInt64(cat), ptrInt64(acc)
		return r, err
	}, `SELECT id, merchant_key, direction, label, cycle_days, (avg_amount*100)::bigint, (last_amount*100)::bigint,
		to_char(first_date,'YYYY-MM-DD'), to_char(last_date,'YYYY-MM-DD'), to_char(next_date,'YYYY-MM-DD'), occurrences, kind, kind_locked,
		category_id, status, ended, manual, account_id FROM recurring ORDER BY ended, direction, avg_amount DESC`)
}

// UpdateRecurring sets status and/or kind; empty values keep the current one.
// A kind chosen by the user is locked against re-detection.
func (s *Store) UpdateRecurring(ctx context.Context, id int64, status domain.RecurringStatus, kind domain.Kind) error {
	return mustAffect(s.DB.ExecContext(ctx, `UPDATE recurring SET status=COALESCE(NULLIF($2,''), status),
		kind=COALESCE(NULLIF($3,''), kind), kind_locked = kind_locked OR $3 <> '' WHERE id=$1`, id, status, kind))
}

// ManualRecurring is a contract created by hand.
type ManualRecurring struct {
	Label       string
	Direction   domain.Direction
	Kind        domain.Kind
	CycleDays   domain.Cycle
	AmountCents int64
	NextDate    time.Time
	CategoryID  *int64
	AccountID   *int64
}

func (s *Store) CreateManualRecurring(ctx context.Context, m ManualRecurring) (int64, error) {
	// A date in the past is the last known payment: move on to the next due date.
	today := domain.Today()
	for i := 0; m.NextDate.Before(today) && i < maxCatchUpSteps; i++ {
		m.NextDate = m.CycleDays.Step(m.NextDate, 1)
	}
	last := m.CycleDays.Step(m.NextDate, -1)
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO recurring (merchant_key, direction, label, cycle_days, avg_amount, last_amount,
			first_date, last_date, next_date, occurrences, kind, kind_locked, category_id, status, manual, account_id)
		VALUES ('manual:' || md5(random()::text), $1, $2, $3, ($4::bigint)::numeric/100, ($4::bigint)::numeric/100,
			$5, $6, $5, 0, $7, TRUE, $8, $10, TRUE, $9) RETURNING id`,
		m.Direction, m.Label, m.CycleDays, m.AmountCents, m.NextDate, last, m.Kind, m.CategoryID, m.AccountID, domain.StatusConfirmed).Scan(&id)
	return id, err
}

// DeleteManualRecurring deletes only contracts created by hand.
func (s *Store) DeleteManualRecurring(ctx context.Context, id int64) error {
	return mustAffect(s.DB.ExecContext(ctx, `DELETE FROM recurring WHERE id=$1 AND manual`, id))
}
