// Package store encapsulates all Postgres access.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct{ DB *sql.DB }

var ErrNotFound = errors.New("nicht gefunden")

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxIdleTime(5 * time.Minute)
	// When docker compose starts, Postgres may not be ready yet.
	var pingErr error
	for i := 0; i < 30; i++ {
		if pingErr = db.PingContext(ctx); pingErr == nil {
			return &Store{DB: db}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("Datenbank nicht erreichbar: %w", pingErr)
}

// Migrate applies all not-yet-applied SQL files in order.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") { // ignore macOS resource-fork files (._xyz.sql)
			continue
		}
		v, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("Migration %s: Name muss mit einer Nummer beginnen", e.Name())
		}
		var exists bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, v).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, _ := migrationFS.ReadFile("migrations/" + e.Name())
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("Migration %s: %w", e.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---------- Settings ----------

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

func (s *Store) SetSetting(ctx context.Context, k, v string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES ($1,$2) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, k, v)
	return err
}

// ---------- Categories & budgets ----------

type Category struct {
	ID          int64  `json:"id"`
	Group       string `json:"group"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	BudgetCents int64  `json:"budget"` // default budget per month
	Sort        int    `json:"sort"`
}

func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, grp, slug, name, (budget*100)::bigint, sort FROM categories ORDER BY grp, sort, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Group, &c.Slug, &c.Name, &c.BudgetCents, &c.Sort); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateCategory(ctx context.Context, group, name string) (int64, error) {
	slug := slugify(name)
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO categories(grp, slug, name, sort) VALUES ($1, $2, $3, 500) RETURNING id`, group, slug, name).Scan(&id)
	return id, err
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-") + "-" + strconv.FormatInt(time.Now().UnixNano()%100000, 36)
}

// SetBudget sets the budget. With month="" the default budget is changed,
// otherwise only the value for that month.
func (s *Store) SetBudget(ctx context.Context, categoryID int64, month string, cents int64) error {
	if month == "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE categories SET budget=($2::bigint)::numeric/100 WHERE id=$1`, categoryID, cents)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO budgets(category_id, month, amount) VALUES ($1,$2,($3::bigint)::numeric/100)
		ON CONFLICT (category_id, month) DO UPDATE SET amount=EXCLUDED.amount`, categoryID, month, cents)
	return err
}

// ---------- Bank connections & accounts ----------

type Connection struct {
	ID         int64      `json:"id"`
	ASPSPName  string     `json:"bank"`
	Country    string     `json:"country"`
	SessionID  string     `json:"-"`
	ValidUntil *time.Time `json:"valid_until"`
	Status     string     `json:"status"`
	LastError  string     `json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	Book       string     `json:"book"`
}

func (s *Store) CreatePendingConnection(ctx context.Context, bank, country, state, book string) (int64, error) {
	if book != "verwaltung" {
		book = "haushalt"
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO bank_connections(aspsp_name, aspsp_country, auth_state, book) VALUES ($1,$2,$3,$4) RETURNING id`, bank, country, state, book).Scan(&id)
	return id, err
}

func (s *Store) ConnectionByState(ctx context.Context, state string) (*Connection, error) {
	return s.oneConnection(ctx, `WHERE auth_state=$1 AND status='pending'`, state)
}

func (s *Store) Connection(ctx context.Context, id int64) (*Connection, error) {
	return s.oneConnection(ctx, `WHERE id=$1`, id)
}

func (s *Store) oneConnection(ctx context.Context, where string, arg any) (*Connection, error) {
	var c Connection
	var sess sql.NullString
	var vu sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT id, aspsp_name, aspsp_country, session_id, valid_until, status, last_error, created_at, book FROM bank_connections `+where, arg).
		Scan(&c.ID, &c.ASPSPName, &c.Country, &sess, &vu, &c.Status, &c.LastError, &c.CreatedAt, &c.Book)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.SessionID = sess.String
	if vu.Valid {
		c.ValidUntil = &vu.Time
	}
	return &c, nil
}

func (s *Store) Connections(ctx context.Context) ([]Connection, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, aspsp_name, aspsp_country, COALESCE(session_id,''), valid_until, status, last_error, created_at, book FROM bank_connections WHERE status <> 'pending' OR created_at > now() - interval '1 hour' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		var vu sql.NullTime
		if err := rows.Scan(&c.ID, &c.ASPSPName, &c.Country, &c.SessionID, &vu, &c.Status, &c.LastError, &c.CreatedAt, &c.Book); err != nil {
			return nil, err
		}
		if vu.Valid {
			c.ValidUntil = &vu.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ActivateConnection(ctx context.Context, id int64, sessionID string, validUntil time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE bank_connections SET session_id=$2, valid_until=$3, status='active', last_error='', auth_state=NULL WHERE id=$1`, id, sessionID, validUntil)
	return err
}

func (s *Store) SetConnectionStatus(ctx context.Context, id int64, status, msg string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE bank_connections SET status=$2, last_error=$3 WHERE id=$1`, id, status, msg)
	return err
}

type Account struct {
	ID           int64      `json:"id"`
	ConnectionID *int64     `json:"connection_id"`
	ProviderUID  string     `json:"-"`
	IBAN         string     `json:"iban"`
	BankName     string     `json:"bank"`
	Name         string     `json:"name"`
	DisplayName  string     `json:"display_name"`
	Currency     string     `json:"currency"`
	Owner        string     `json:"owner"`
	BalanceCents *int64     `json:"balance"`
	BalanceAt    *time.Time `json:"balance_at"`
	LastSynced   *time.Time `json:"last_synced_at"`
	SyncError    string     `json:"sync_error"`
	Active       bool       `json:"active"`
	Book         string     `json:"book"`
	// from the connection
	ConnStatus string     `json:"connection_status"`
	ValidUntil *time.Time `json:"valid_until"`
}

// UpsertAccount creates an account or attaches a known account to a new connection.
func (s *Store) UpsertAccount(ctx context.Context, a Account) (int64, error) {
	var id int64
	book := a.Book
	if book != "verwaltung" {
		book = "haushalt"
	}
	err := s.DB.QueryRowContext(ctx, `INSERT INTO accounts(connection_id, provider_uid, iban, bank_name, name, currency, book)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (provider_uid) DO UPDATE SET connection_id=EXCLUDED.connection_id, iban=EXCLUDED.iban, bank_name=EXCLUDED.bank_name, name=EXCLUDED.name, sync_error=''
		RETURNING id`, a.ConnectionID, a.ProviderUID, a.IBAN, a.BankName, a.Name, a.Currency, book).Scan(&id)
	return id, err
}

const accountCols = `a.id, a.connection_id, a.provider_uid, a.iban, a.bank_name, a.name, a.display_name, a.currency, a.owner,
	(a.balance*100)::bigint, a.balance_at, a.last_synced_at, a.sync_error, a.active, COALESCE(c.status,''), c.valid_until, a.book`

func scanAccount(sc interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var conn sql.NullInt64
	var bal sql.NullInt64
	var balAt, synced, vu sql.NullTime
	err := sc.Scan(&a.ID, &conn, &a.ProviderUID, &a.IBAN, &a.BankName, &a.Name, &a.DisplayName, &a.Currency, &a.Owner,
		&bal, &balAt, &synced, &a.SyncError, &a.Active, &a.ConnStatus, &vu, &a.Book)
	if conn.Valid {
		a.ConnectionID = &conn.Int64
	}
	if bal.Valid {
		a.BalanceCents = &bal.Int64
	}
	if balAt.Valid {
		a.BalanceAt = &balAt.Time
	}
	if synced.Valid {
		a.LastSynced = &synced.Time
	}
	if vu.Valid {
		a.ValidUntil = &vu.Time
	}
	return a, err
}

func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+accountCols+` FROM accounts a LEFT JOIN bank_connections c ON c.id=a.connection_id ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, owner, displayName string, active bool, book string) error {
	if book != "verwaltung" {
		book = "haushalt"
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE accounts SET owner=$2, display_name=$3, active=$4, book=$5 WHERE id=$1`, id, owner, displayName, active, book)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetAccountBalance(ctx context.Context, id int64, cents int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET balance=($2::bigint)::numeric/100, balance_at=now() WHERE id=$1`, id, cents)
	return err
}

func (s *Store) SetAccountSynced(ctx context.Context, id int64, syncErr string) error {
	if syncErr != "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET sync_error=$2 WHERE id=$1`, id, syncErr)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET sync_error='', last_synced_at=now() WHERE id=$1`, id)
	return err
}

// LastBookingDate returns the latest booking date of an account (zero if there are no transactions yet).
func (s *Store) LastBookingDate(ctx context.Context, accountID int64) (time.Time, error) {
	var t sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT max(booking_date) FROM transactions WHERE account_id=$1`, accountID).Scan(&t)
	return t.Time, err
}

func (s *Store) OwnIBANs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT iban FROM accounts WHERE iban <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var i string
		if err := rows.Scan(&i); err != nil {
			return nil, err
		}
		m[strings.ToUpper(strings.ReplaceAll(i, " ", ""))] = true
	}
	return m, rows.Err()
}
