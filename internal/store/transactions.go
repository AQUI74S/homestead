package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// NewTxn is a newly fetched transaction.
type NewTxn struct {
	ExtID            string
	BookingDate      time.Time
	ValueDate        *time.Time
	AmountCents      int64
	Currency         string
	Counterparty     string
	CounterpartyIBAN string
	Remittance       string
	BankCode         string
}

// InsertTransactions stores new transactions; already known ones (same ext_id) are skipped.
// Returns the number of newly inserted transactions.
func (s *Store) InsertTransactions(ctx context.Context, accountID int64, txs []NewTxn) (int, error) {
	if len(txs) == 0 {
		return 0, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO transactions
		(account_id, ext_id, booking_date, value_date, amount, currency, counterparty, counterparty_iban, remittance, bank_code)
		VALUES ($1,$2,$3,$4,($5::bigint)::numeric/100,$6,$7,$8,$9,$10)
		ON CONFLICT (account_id, ext_id) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, t := range txs {
		res, err := stmt.ExecContext(ctx, accountID, t.ExtID, t.BookingDate, t.ValueDate, t.AmountCents, t.Currency,
			t.Counterparty, t.CounterpartyIBAN, t.Remittance, t.BankCode)
		if err != nil {
			return n, fmt.Errorf("Umsatz %s: %w", t.ExtID, err)
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

// ClassRow is a transaction with everything classification needs.
type ClassRow struct {
	ID               int64
	AccountID        int64
	BookingDate      time.Time
	AmountCents      int64
	Counterparty     string
	CounterpartyIBAN string
	Remittance       string
	BankCode         string
	Merchant         string
	MerchantKey      string
	Slug             string
	Source           string
	Reason           string
	RecurringID      *int64
}

func (s *Store) ClassRows(ctx context.Context) ([]ClassRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.account_id, t.booking_date, (t.amount*100)::bigint, t.counterparty, t.counterparty_iban,
		t.remittance, t.bank_code, t.merchant, t.merchant_key, COALESCE(c.slug,''), t.category_source, t.reason, t.recurring_id
		FROM transactions t LEFT JOIN categories c ON c.id=t.category_id ORDER BY t.booking_date, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClassRow
	for rows.Next() {
		var r ClassRow
		var rid sql.NullInt64
		if err := rows.Scan(&r.ID, &r.AccountID, &r.BookingDate, &r.AmountCents, &r.Counterparty, &r.CounterpartyIBAN,
			&r.Remittance, &r.BankCode, &r.Merchant, &r.MerchantKey, &r.Slug, &r.Source, &r.Reason, &rid); err != nil {
			return nil, err
		}
		if rid.Valid {
			r.RecurringID = &rid.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClassUpdate is the classification result for a transaction.
type ClassUpdate struct {
	ID          int64
	Merchant    string
	MerchantKey string
	Slug        string
	Source      string
	Reason      string
	RecurringID *int64
}

// ApplyClassification writes changed classifications in a single DB transaction.
func (s *Store) ApplyClassification(ctx context.Context, ups []ClassUpdate) error {
	if len(ups) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `UPDATE transactions SET merchant=$2, merchant_key=$3,
		category_id=(SELECT id FROM categories WHERE slug=$4), category_source=$5, reason=$6, recurring_id=$7 WHERE id=$1`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, u := range ups {
		if _, err := stmt.ExecContext(ctx, u.ID, u.Merchant, u.MerchantKey, u.Slug, u.Source, u.Reason, u.RecurringID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTransactionCategory sets a category manually (not overwritten by reclassification).
func (s *Store) SetTransactionCategory(ctx context.Context, id, categoryID int64, note *string) error {
	q := `UPDATE transactions SET category_id=$2, category_source='manual', reason='Von Hand zugeordnet' WHERE id=$1`
	args := []any{id, categoryID}
	if note != nil {
		q = `UPDATE transactions SET category_id=$2, category_source='manual', reason='Von Hand zugeordnet', note=$3 WHERE id=$1`
		args = append(args, *note)
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetTransactionCategory hands a transaction back to automatic classification.
func (s *Store) ResetTransactionCategory(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE transactions SET category_source='auto' WHERE id=$1`, id)
	return err
}

type Transaction struct {
	ID               int64  `json:"id"`
	AccountID        int64  `json:"account_id"`
	AccountName      string `json:"account"`
	Owner            string `json:"owner"`
	BookingDate      string `json:"date"`
	AmountCents      int64  `json:"amount"`
	Counterparty     string `json:"counterparty"`
	CounterpartyIBAN string `json:"counterparty_iban"`
	Remittance       string `json:"remittance"`
	Merchant         string `json:"merchant"`
	MerchantKey      string `json:"merchant_key"`
	CategoryID       *int64 `json:"category_id"`
	CategorySlug     string `json:"category_slug"`
	Group            string `json:"group"`
	Source           string `json:"source"`
	Reason           string `json:"reason"`
	RecurringID      *int64 `json:"recurring_id"`
	Note             string `json:"note"`
}

type TxnFilter struct {
	Month       string // YYYY-MM
	CategoryID  int64
	Group       string
	AccountID   int64
	Search      string
	RecurringID int64
	Book        string // haushalt | verwaltung | "" (all)
	LeaseID     int64
	PropertyID  int64
	Limit       int
}

func (s *Store) Transactions(ctx context.Context, f TxnFilter) ([]Transaction, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if f.Month != "" {
		pc, err := s.PeriodCalc(ctx)
		if err != nil {
			return nil, err
		}
		start, end, err := pc.Range(f.Month)
		if err != nil {
			return nil, err
		}
		where = append(where, "t.booking_date >= "+arg(start)+" AND t.booking_date < "+arg(end))
	}
	if f.CategoryID > 0 {
		where = append(where, "t.category_id = "+arg(f.CategoryID))
	}
	if f.Group != "" {
		where = append(where, "c.grp = "+arg(f.Group))
	}
	if f.AccountID > 0 {
		where = append(where, "t.account_id = "+arg(f.AccountID))
	}
	if f.Book != "" {
		where = append(where, "a.book = "+arg(f.Book))
	}
	if f.LeaseID > 0 {
		where = append(where, "t.lease_id = "+arg(f.LeaseID))
	}
	if f.PropertyID > 0 {
		where = append(where, "t.property_id = "+arg(f.PropertyID))
	}
	if f.RecurringID > 0 {
		where = append(where, "t.recurring_id = "+arg(f.RecurringID))
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		p := arg("%" + q + "%")
		where = append(where, "(t.counterparty ILIKE "+p+" OR t.remittance ILIKE "+p+" OR t.merchant ILIKE "+p+" OR t.note ILIKE "+p+")")
	}
	if f.Limit <= 0 || f.Limit > 2000 {
		f.Limit = 500
	}
	q := `SELECT t.id, t.account_id, COALESCE(NULLIF(a.display_name,''), a.name), a.owner, to_char(t.booking_date,'YYYY-MM-DD'),
		(t.amount*100)::bigint, t.counterparty, t.counterparty_iban, t.remittance, t.merchant, t.merchant_key, t.category_id,
		COALESCE(c.slug,''), COALESCE(c.grp,''), t.category_source, t.reason, t.recurring_id, t.note
		FROM transactions t JOIN accounts a ON a.id=t.account_id LEFT JOIN categories c ON c.id=t.category_id`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY t.booking_date DESC, t.id DESC LIMIT " + arg(f.Limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Transaction{}
	for rows.Next() {
		var t Transaction
		var cat, rid sql.NullInt64
		if err := rows.Scan(&t.ID, &t.AccountID, &t.AccountName, &t.Owner, &t.BookingDate, &t.AmountCents, &t.Counterparty,
			&t.CounterpartyIBAN, &t.Remittance, &t.Merchant, &t.MerchantKey, &cat, &t.CategorySlug, &t.Group, &t.Source,
			&t.Reason, &rid, &t.Note); err != nil {
			return nil, err
		}
		if cat.Valid {
			t.CategoryID = &cat.Int64
		}
		if rid.Valid {
			t.RecurringID = &rid.Int64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) TransactionByID(ctx context.Context, id int64) (*Transaction, error) {
	var t Transaction
	var cat sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT t.id, t.merchant, t.merchant_key, t.counterparty, t.counterparty_iban, t.category_id
		FROM transactions t WHERE t.id=$1`, id).Scan(&t.ID, &t.Merchant, &t.MerchantKey, &t.Counterparty, &t.CounterpartyIBAN, &cat)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if cat.Valid {
		t.CategoryID = &cat.Int64
	}
	return &t, err
}

// ---------- Rules ----------

type Rule struct {
	ID         int64     `json:"id"`
	Field      string    `json:"field"`
	Pattern    string    `json:"pattern"`
	CategoryID int64     `json:"category_id"`
	Slug       string    `json:"category_slug"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id, r.field, r.pattern, r.category_id, c.slug, r.created_at FROM rules r JOIN categories c ON c.id=r.category_id ORDER BY r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Field, &r.Pattern, &r.CategoryID, &r.Slug, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRule(ctx context.Context, field, pattern string, categoryID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO rules(field, pattern, category_id) VALUES ($1, lower($2), $3)
		ON CONFLICT (field, pattern) DO UPDATE SET category_id=EXCLUDED.category_id`, field, strings.TrimSpace(pattern), categoryID)
	return err
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM rules WHERE id=$1`, id)
	return err
}

// ---------- Recurring payments ----------

type Recurring struct {
	ID           int64  `json:"id"`
	Key          string `json:"merchant_key"`
	Direction    string `json:"direction"`
	Label        string `json:"label"`
	CycleDays    int    `json:"cycle_days"`
	CycleLabel   string `json:"cycle"`
	AvgCents     int64  `json:"amount"`
	LastCents    int64  `json:"last_amount"`
	MonthlyCents int64  `json:"monthly"`
	FirstDate    string `json:"first_date"`
	LastDate     string `json:"last_date"`
	NextDate     string `json:"next_date"`
	Count        int    `json:"occurrences"`
	Kind         string `json:"kind"`
	KindLocked   bool   `json:"kind_locked"`
	CategoryID   *int64 `json:"category_id"`
	Status       string `json:"status"`
	Ended        bool   `json:"ended"`
	Manual       bool   `json:"manual"`
	AccountID    *int64 `json:"account_id"`
}

// RecurringUpsert is a series detected by classification.
type RecurringUpsert struct {
	Key, Direction, Label, Kind, Slug string
	CycleDays, Count                  int
	AvgCents, LastCents               int64
	First, Last, Next                 time.Time
	Ended                             bool
	AccountID                         int64
}

// SyncRecurring stores detected series. Kind/status set by the user are preserved;
// unconfirmed series that are no longer detected are removed. Returns key -> ID.
func (s *Store) SyncRecurring(ctx context.Context, series []RecurringUpsert) (map[string]int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids := map[string]int64{}
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
			return nil, fmt.Errorf("Serie %s: %w", r.Key, err)
		}
		ids[r.Direction+"|"+r.Key] = id
		keys = append(keys, r.Direction+"|"+r.Key)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recurring WHERE status='detected' AND NOT manual AND NOT (direction || '|' || merchant_key = ANY($1))`, pq.Array(keys)); err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

func (s *Store) RecurringList(ctx context.Context) ([]Recurring, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, merchant_key, direction, label, cycle_days, (avg_amount*100)::bigint, (last_amount*100)::bigint,
		to_char(first_date,'YYYY-MM-DD'), to_char(last_date,'YYYY-MM-DD'), to_char(next_date,'YYYY-MM-DD'), occurrences, kind, kind_locked,
		category_id, status, ended, manual, account_id FROM recurring ORDER BY ended, direction, avg_amount DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recurring{}
	for rows.Next() {
		var r Recurring
		var cat, acc sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Key, &r.Direction, &r.Label, &r.CycleDays, &r.AvgCents, &r.LastCents, &r.FirstDate, &r.LastDate,
			&r.NextDate, &r.Count, &r.Kind, &r.KindLocked, &cat, &r.Status, &r.Ended, &r.Manual, &acc); err != nil {
			return nil, err
		}
		if cat.Valid {
			r.CategoryID = &cat.Int64
		}
		if acc.Valid {
			r.AccountID = &acc.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateRecurring(ctx context.Context, id int64, status, kind string) error {
	q := `UPDATE recurring SET status=COALESCE(NULLIF($2,''), status),
		kind=COALESCE(NULLIF($3,''), kind), kind_locked = kind_locked OR $3 <> '' WHERE id=$1`
	res, err := s.DB.ExecContext(ctx, q, id, status, kind)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ManualRecurring is a manually created contract.
type ManualRecurring struct {
	Label       string
	Direction   string // in | out
	Kind        string
	CycleDays   int
	AmountCents int64
	NextDate    time.Time
	CategoryID  *int64
	AccountID   *int64
}

func (s *Store) CreateManualRecurring(ctx context.Context, m ManualRecurring) (int64, error) {
	// If the date is in the past (last known payment), compute the next due date.
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for i := 0; m.NextDate.Before(today) && i < 520; i++ {
		m.NextDate = stepFwd(m.NextDate, m.CycleDays)
	}
	last := stepBack(m.NextDate, m.CycleDays)
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO recurring (merchant_key, direction, label, cycle_days, avg_amount, last_amount,
			first_date, last_date, next_date, occurrences, kind, kind_locked, category_id, status, manual, account_id)
		VALUES ('manual:' || md5(random()::text), $1, $2, $3, ($4::bigint)::numeric/100, ($4::bigint)::numeric/100,
			$5, $6, $5, 0, $7, TRUE, $8, 'confirmed', TRUE, $9) RETURNING id`,
		m.Direction, m.Label, m.CycleDays, m.AmountCents, m.NextDate, last, m.Kind, m.CategoryID, m.AccountID).Scan(&id)
	return id, err
}

// DeleteManualRecurring deletes only manually created contracts.
func (s *Store) DeleteManualRecurring(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM recurring WHERE id=$1 AND manual`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// stepBack goes back one cycle (monthly and yearly cycles calendar-exact).
func stepBack(d time.Time, cycle int) time.Time {
	switch cycle {
	case 30:
		return d.AddDate(0, -1, 0)
	case 61:
		return d.AddDate(0, -2, 0)
	case 91:
		return d.AddDate(0, -3, 0)
	case 182:
		return d.AddDate(0, -6, 0)
	case 365:
		return d.AddDate(-1, 0, 0)
	}
	return d.AddDate(0, 0, -cycle)
}

// InsertDeduped imports transactions from a file. Transactions that already exist for this
// account (same date and amount, e.g. from the bank fetch) are skipped.
// Returns: inserted, skipped.
func (s *Store) InsertDeduped(ctx context.Context, accountID int64, txs []NewTxn) (int, int, error) {
	if len(txs) == 0 {
		return 0, 0, nil
	}
	minD, maxD := txs[0].BookingDate, txs[0].BookingDate
	for _, t := range txs {
		if t.BookingDate.Before(minD) {
			minD = t.BookingDate
		}
		if t.BookingDate.After(maxD) {
			maxD = t.BookingDate
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT to_char(booking_date,'YYYY-MM-DD'), (amount*100)::bigint, count(*)
		FROM transactions WHERE account_id=$1 AND booking_date BETWEEN $2 AND $3 GROUP BY 1, 2`, accountID, minD, maxD)
	if err != nil {
		return 0, 0, err
	}
	have := map[string]int{}
	for rows.Next() {
		var d string
		var a int64
		var n int
		if err := rows.Scan(&d, &a, &n); err != nil {
			rows.Close()
			return 0, 0, err
		}
		have[fmt.Sprintf("%s|%d", d, a)] = n
	}
	rows.Close()
	var fresh []NewTxn
	skipped := 0
	for _, t := range txs {
		k := fmt.Sprintf("%s|%d", t.BookingDate.Format("2006-01-02"), t.AmountCents)
		if have[k] > 0 {
			have[k]--
			skipped++
			continue
		}
		fresh = append(fresh, t)
	}
	n, err := s.InsertTransactions(ctx, accountID, fresh)
	return n, skipped + (len(fresh) - n), err
}

// RemoveCSVDuplicates removes imported transactions that have since also arrived via bank fetch
// (same account, date and amount; counted pairwise).
func (s *Store) RemoveCSVDuplicates(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM transactions WHERE id IN (
		SELECT c.id FROM (
			SELECT id, account_id, booking_date, amount,
				row_number() OVER (PARTITION BY account_id, booking_date, amount ORDER BY id) AS rn
			FROM transactions WHERE ext_id LIKE 'csv:%') c
		JOIN (
			SELECT account_id, booking_date, amount, count(*) AS n
			FROM transactions WHERE ext_id NOT LIKE 'csv:%' GROUP BY 1, 2, 3) a
		USING (account_id, booking_date, amount)
		WHERE c.rn <= a.n)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func stepFwd(d time.Time, cycle int) time.Time {
	switch cycle {
	case 30:
		return d.AddDate(0, 1, 0)
	case 61:
		return d.AddDate(0, 2, 0)
	case 91:
		return d.AddDate(0, 3, 0)
	case 182:
		return d.AddDate(0, 6, 0)
	case 365:
		return d.AddDate(1, 0, 0)
	}
	return d.AddDate(0, 0, cycle)
}
