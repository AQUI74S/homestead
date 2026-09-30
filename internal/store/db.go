// Package store encapsulates all Postgres access. It only reads and writes data;
// the calculations built on top of it live in the budget, hv and classify packages.
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

// Connection pool and start-up settings.
const (
	maxOpenConns    = 10
	connMaxIdleTime = 5 * time.Minute
	// When docker compose starts, Postgres may not accept connections yet.
	pingAttempts = 30
	pingDelay    = time.Second
)

type Store struct{ DB *sql.DB }

var ErrNotFound = errors.New("nicht gefunden")

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetConnMaxIdleTime(connMaxIdleTime)
	var pingErr error
	for i := 0; i < pingAttempts; i++ {
		if pingErr = db.PingContext(ctx); pingErr == nil {
			return &Store{DB: db}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pingDelay):
		}
	}
	return nil, fmt.Errorf("Datenbank nicht erreichbar: %w", pingErr)
}

// Migrate applies all not-yet-applied SQL files in order. Each file name starts
// with its version number, e.g. 007_kapitalertraege.sql.
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
		if strings.HasPrefix(e.Name(), ".") { // macOS resource-fork files (._xyz.sql)
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
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("Migration %s: %w", e.Name(), err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, v)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// inTx runs fn in a transaction and commits if it returns nil.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// mustAffect turns "no row changed" into ErrNotFound.
func mustAffect(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanner is implemented by *sql.Row and *sql.Rows.
type scanner interface{ Scan(...any) error }

// queryAll runs a query and collects one value per row.
func queryAll[T any](ctx context.Context, db *sql.DB, scan func(scanner) (T, error), q string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func ptrInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func ptrTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}
