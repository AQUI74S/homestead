package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

// bankCallRetention is how long logged bank requests are kept (the limits look at 24 h).
const bankCallRetention = "7 days"

type Account struct {
	ID           int64        `json:"id"`
	ConnectionID *int64       `json:"connection_id"`
	ProviderUID  string       `json:"-"`
	IBAN         string       `json:"iban"`
	BankName     string       `json:"bank"`
	Name         string       `json:"name"`
	DisplayName  string       `json:"display_name"`
	Currency     string       `json:"currency"`
	Owner        domain.Owner `json:"owner"`
	BalanceCents *int64       `json:"balance"`
	BalanceAt    *time.Time   `json:"balance_at"`
	LastSynced   *time.Time   `json:"last_synced_at"`
	SyncError    string       `json:"sync_error"`
	Active       bool         `json:"active"`
	Book         domain.Book  `json:"book"`
	DailyLimit   *int         `json:"daily_limit"`   // learned bank limit (nil = default)
	LimitedUntil *time.Time   `json:"limited_until"` // paused after the bank's limit was hit
	// from the connection
	ConnStatus domain.ConnStatus `json:"connection_status"`
	ValidUntil *time.Time        `json:"valid_until"`
}

// UpsertAccount creates an account or attaches a known account to a new connection.
func (s *Store) UpsertAccount(ctx context.Context, a Account) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO accounts(connection_id, provider_uid, iban, bank_name, name, currency, book)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (provider_uid) DO UPDATE SET connection_id=EXCLUDED.connection_id, iban=EXCLUDED.iban, bank_name=EXCLUDED.bank_name, name=EXCLUDED.name, sync_error=''
		RETURNING id`, a.ConnectionID, a.ProviderUID, a.IBAN, a.BankName, a.Name, a.Currency, domain.ParseBook(string(a.Book))).Scan(&id)
	return id, err
}

const accountCols = `a.id, a.connection_id, a.provider_uid, a.iban, a.bank_name, a.name, a.display_name, a.currency, a.owner,
	(a.balance*100)::bigint, a.balance_at, a.last_synced_at, a.sync_error, a.active, COALESCE(c.status,''), c.valid_until, a.book,
	a.daily_limit, a.limited_until`

func scanAccount(sc scanner) (Account, error) {
	var a Account
	var conn, bal, limit sql.NullInt64
	var balAt, synced, vu, limitedUntil sql.NullTime
	err := sc.Scan(&a.ID, &conn, &a.ProviderUID, &a.IBAN, &a.BankName, &a.Name, &a.DisplayName, &a.Currency, &a.Owner,
		&bal, &balAt, &synced, &a.SyncError, &a.Active, &a.ConnStatus, &vu, &a.Book, &limit, &limitedUntil)
	if limit.Valid {
		v := int(limit.Int64)
		a.DailyLimit = &v
	}
	a.ConnectionID, a.BalanceCents = ptrInt64(conn), ptrInt64(bal)
	a.BalanceAt, a.LastSynced, a.ValidUntil, a.LimitedUntil = ptrTime(balAt), ptrTime(synced), ptrTime(vu), ptrTime(limitedUntil)
	return a, err
}

func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	return queryAll(ctx, s.DB, scanAccount, `SELECT `+accountCols+` FROM accounts a LEFT JOIN bank_connections c ON c.id=a.connection_id ORDER BY a.id`)
}

// AccountsIn returns the accounts of one book.
func (s *Store) AccountsIn(ctx context.Context, book domain.Book) ([]Account, error) {
	all, err := s.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	out := []Account{}
	for _, a := range all {
		if a.Book == book {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, owner domain.Owner, displayName string, active bool, book domain.Book) error {
	return mustAffect(s.DB.ExecContext(ctx, `UPDATE accounts SET owner=$2, display_name=$3, active=$4, book=$5 WHERE id=$1`,
		id, owner, displayName, active, domain.ParseBook(string(book))))
}

func (s *Store) SetAccountBalance(ctx context.Context, id int64, cents int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET balance=($2::bigint)::numeric/100, balance_at=now() WHERE id=$1`, id, cents)
	return err
}

// SetAccountSynced records the result of a sync: an error text, or success ("").
func (s *Store) SetAccountSynced(ctx context.Context, id int64, syncErr string) error {
	if syncErr != "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET sync_error=$2 WHERE id=$1`, id, syncErr)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET sync_error='', last_synced_at=now() WHERE id=$1`, id)
	return err
}

// RecordBankCall logs a request to the bank for an account and drops old entries.
func (s *Store) RecordBankCall(ctx context.Context, accountID int64, attended bool) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO bank_calls(account_id, attended) VALUES ($1,$2)`, accountID, attended)
	if err == nil {
		_, err = s.DB.ExecContext(ctx, `DELETE FROM bank_calls WHERE at < now() - $1::interval`, bankCallRetention)
	}
	return err
}

// UnattendedCalls returns, per account, the times of unattended bank requests since the given time.
func (s *Store) UnattendedCalls(ctx context.Context, since time.Time) (map[int64][]time.Time, error) {
	type call struct {
		id int64
		at time.Time
	}
	calls, err := queryAll(ctx, s.DB, func(sc scanner) (call, error) {
		var c call
		return c, sc.Scan(&c.id, &c.at)
	}, `SELECT account_id, at FROM bank_calls WHERE NOT attended AND at >= $1 ORDER BY at`, since)
	if err != nil {
		return nil, err
	}
	out := map[int64][]time.Time{}
	for _, c := range calls {
		out[c.id] = append(out[c.id], c.at)
	}
	return out, nil
}

// SetAccountLimited pauses automatic syncs of an account until the given time and
// optionally stores a (lower) learned daily limit.
func (s *Store) SetAccountLimited(ctx context.Context, id int64, until time.Time, learned *int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET limited_until=$2,
		daily_limit = CASE WHEN $3::int IS NULL THEN daily_limit ELSE LEAST(COALESCE(daily_limit, $3::int), $3::int) END
		WHERE id=$1`, id, until, learned)
	return err
}

// LastBookingDate returns the latest booking date of an account (zero if there are no transactions yet).
func (s *Store) LastBookingDate(ctx context.Context, accountID int64) (time.Time, error) {
	var t sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT max(booking_date) FROM transactions WHERE account_id=$1`, accountID).Scan(&t)
	return t.Time, err
}

// OwnIBANs returns the normalized IBANs of all own accounts.
func (s *Store) OwnIBANs(ctx context.Context) (map[string]bool, error) {
	ibans, err := queryAll(ctx, s.DB, func(sc scanner) (string, error) {
		var i string
		return i, sc.Scan(&i)
	}, `SELECT iban FROM accounts WHERE iban <> ''`)
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, i := range ibans {
		m[domain.NormIBAN(i)] = true
	}
	return m, nil
}
