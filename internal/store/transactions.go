package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

// Limits for transaction lists.
const (
	DefaultTxnLimit = 500
	MaxTxnLimit     = 2000
)

// reasonManual is shown for categories chosen by hand.
const reasonManual = "Von Hand zugeordnet"

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
	n := 0
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO transactions
			(account_id, ext_id, booking_date, value_date, amount, currency, counterparty, counterparty_iban, remittance, bank_code)
			VALUES ($1,$2,$3,$4,($5::bigint)::numeric/100,$6,$7,$8,$9,$10)
			ON CONFLICT (account_id, ext_id) DO NOTHING`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, t := range txs {
			res, err := stmt.ExecContext(ctx, accountID, t.ExtID, t.BookingDate, t.ValueDate, t.AmountCents, t.Currency,
				t.Counterparty, t.CounterpartyIBAN, t.Remittance, t.BankCode)
			if err != nil {
				return fmt.Errorf("Umsatz %s: %w", t.ExtID, err)
			}
			if c, _ := res.RowsAffected(); c > 0 {
				n++
			}
		}
		return nil
	})
	return n, err
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
	Source           domain.Source
	Reason           string
	RecurringID      *int64
}

func (s *Store) ClassRows(ctx context.Context) ([]ClassRow, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (ClassRow, error) {
		var r ClassRow
		var rid sql.NullInt64
		err := sc.Scan(&r.ID, &r.AccountID, &r.BookingDate, &r.AmountCents, &r.Counterparty, &r.CounterpartyIBAN,
			&r.Remittance, &r.BankCode, &r.Merchant, &r.MerchantKey, &r.Slug, &r.Source, &r.Reason, &rid)
		r.RecurringID = ptrInt64(rid)
		return r, err
	}, `SELECT t.id, t.account_id, t.booking_date, (t.amount*100)::bigint, t.counterparty, t.counterparty_iban,
		t.remittance, t.bank_code, t.merchant, t.merchant_key, COALESCE(c.slug,''), t.category_source, t.reason, t.recurring_id
		FROM transactions t LEFT JOIN categories c ON c.id=t.category_id ORDER BY t.booking_date, t.id`)
}

// ClassUpdate is the classification result for a transaction.
type ClassUpdate struct {
	ID          int64
	Merchant    string
	MerchantKey string
	Slug        string
	Source      domain.Source
	Reason      string
	RecurringID *int64
}

// ApplyClassification writes changed classifications in a single DB transaction.
func (s *Store) ApplyClassification(ctx context.Context, ups []ClassUpdate) error {
	if len(ups) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
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
		return nil
	})
}

// SetTransactionCategory sets a category by hand (never overwritten by reclassification).
func (s *Store) SetTransactionCategory(ctx context.Context, id, categoryID int64, note *string) error {
	q := `UPDATE transactions SET category_id=$2, category_source=$3, reason=$4 WHERE id=$1`
	args := []any{id, categoryID, domain.SourceManual, reasonManual}
	if note != nil {
		q = `UPDATE transactions SET category_id=$2, category_source=$3, reason=$4, note=$5 WHERE id=$1`
		args = append(args, *note)
	}
	return mustAffect(s.DB.ExecContext(ctx, q, args...))
}

// ResetTransactionCategory hands a transaction back to automatic classification.
func (s *Store) ResetTransactionCategory(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE transactions SET category_source=$2 WHERE id=$1`, id, domain.SourceAuto)
	return err
}

type Transaction struct {
	ID               int64         `json:"id"`
	AccountID        int64         `json:"account_id"`
	AccountName      string        `json:"account"`
	Owner            domain.Owner  `json:"owner"`
	BookingDate      string        `json:"date"`
	AmountCents      int64         `json:"amount"`
	Counterparty     string        `json:"counterparty"`
	CounterpartyIBAN string        `json:"counterparty_iban"`
	Remittance       string        `json:"remittance"`
	Merchant         string        `json:"merchant"`
	MerchantKey      string        `json:"merchant_key"`
	CategoryID       *int64        `json:"category_id"`
	CategorySlug     string        `json:"category_slug"`
	Group            domain.Group  `json:"group"`
	Source           domain.Source `json:"source"`
	Reason           string        `json:"reason"`
	RecurringID      *int64        `json:"recurring_id"`
	Note             string        `json:"note"`
}

type TxnFilter struct {
	From, To    time.Time // booking date range [From, To); zero = open
	CategoryID  int64
	Group       domain.Group
	AccountID   int64
	Search      string
	RecurringID int64
	Book        domain.Book // "" = all books
	LeaseID     int64
	PropertyID  int64
	Limit       int
	// Uncategorized keeps only what the classifier could not place (domain.CatchAllSlugs)
	// and that neither a hand choice nor one of the user's rules has confirmed.
	Uncategorized bool
}

func (s *Store) Transactions(ctx context.Context, f TxnFilter) ([]Transaction, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	add := func(cond string) { where = append(where, cond) }
	if !f.From.IsZero() {
		add("t.booking_date >= " + arg(f.From))
	}
	if !f.To.IsZero() {
		add("t.booking_date < " + arg(f.To))
	}
	if f.CategoryID > 0 {
		add("t.category_id = " + arg(f.CategoryID))
	}
	if f.Uncategorized {
		add(sqlCatchAllAuto)
	}
	if f.Group != "" {
		add("c.grp = " + arg(f.Group))
	}
	if f.AccountID > 0 {
		add("t.account_id = " + arg(f.AccountID))
	}
	if f.Book != "" {
		add("a.book = " + arg(f.Book))
	}
	if f.LeaseID > 0 {
		add("t.lease_id = " + arg(f.LeaseID))
	}
	if f.PropertyID > 0 {
		add("t.property_id = " + arg(f.PropertyID))
	}
	if f.RecurringID > 0 {
		add("t.recurring_id = " + arg(f.RecurringID))
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		p := arg("%" + q + "%")
		add("(t.counterparty ILIKE " + p + " OR t.remittance ILIKE " + p + " OR t.merchant ILIKE " + p + " OR t.note ILIKE " + p + ")")
	}
	if f.Limit <= 0 || f.Limit > MaxTxnLimit {
		f.Limit = DefaultTxnLimit
	}
	q := `SELECT t.id, t.account_id, COALESCE(NULLIF(a.display_name,''), a.name), a.owner, to_char(t.booking_date,'YYYY-MM-DD'),
		(t.amount*100)::bigint, t.counterparty, t.counterparty_iban, t.remittance, t.merchant, t.merchant_key, t.category_id,
		COALESCE(c.slug,''), COALESCE(c.grp,''), t.category_source, t.reason, t.recurring_id, t.note
		FROM transactions t JOIN accounts a ON a.id=t.account_id LEFT JOIN categories c ON c.id=t.category_id`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY t.booking_date DESC, t.id DESC LIMIT " + arg(f.Limit)
	return queryAll(ctx, s.DB, func(sc scanner) (Transaction, error) {
		var t Transaction
		var cat, rid sql.NullInt64
		err := sc.Scan(&t.ID, &t.AccountID, &t.AccountName, &t.Owner, &t.BookingDate, &t.AmountCents, &t.Counterparty,
			&t.CounterpartyIBAN, &t.Remittance, &t.Merchant, &t.MerchantKey, &cat, &t.CategorySlug, &t.Group, &t.Source,
			&t.Reason, &rid, &t.Note)
		t.CategoryID, t.RecurringID = ptrInt64(cat), ptrInt64(rid)
		return t, err
	}, q, args...)
}

func (s *Store) TransactionByID(ctx context.Context, id int64) (*Transaction, error) {
	var t Transaction
	var cat sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT t.id, t.merchant, t.merchant_key, t.counterparty, t.counterparty_iban, t.category_id
		FROM transactions t WHERE t.id=$1`, id).Scan(&t.ID, &t.Merchant, &t.MerchantKey, &t.Counterparty, &t.CounterpartyIBAN, &cat)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.CategoryID = ptrInt64(cat)
	return &t, err
}
