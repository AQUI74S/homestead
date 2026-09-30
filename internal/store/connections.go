package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

// pendingConnectionTTL is how long an unfinished bank authorization stays visible.
const pendingConnectionTTL = "1 hour"

type Connection struct {
	ID         int64             `json:"id"`
	ASPSPName  string            `json:"bank"`
	Country    string            `json:"country"`
	SessionID  string            `json:"-"`
	ValidUntil *time.Time        `json:"valid_until"`
	Status     domain.ConnStatus `json:"status"`
	LastError  string            `json:"last_error"`
	CreatedAt  time.Time         `json:"created_at"`
	Book       domain.Book       `json:"book"`
}

const connectionCols = `id, aspsp_name, aspsp_country, COALESCE(session_id,''), valid_until, status, last_error, created_at, book`

func scanConnection(sc scanner) (Connection, error) {
	var c Connection
	var vu sql.NullTime
	err := sc.Scan(&c.ID, &c.ASPSPName, &c.Country, &c.SessionID, &vu, &c.Status, &c.LastError, &c.CreatedAt, &c.Book)
	c.ValidUntil = ptrTime(vu)
	return c, err
}

func (s *Store) CreatePendingConnection(ctx context.Context, bank, country, state string, book domain.Book) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO bank_connections(aspsp_name, aspsp_country, auth_state, book) VALUES ($1,$2,$3,$4) RETURNING id`,
		bank, country, state, domain.ParseBook(string(book))).Scan(&id)
	return id, err
}

// ConnectionByState finds the pending connection of an authorization in progress.
func (s *Store) ConnectionByState(ctx context.Context, state string) (*Connection, error) {
	return s.oneConnection(ctx, `WHERE auth_state=$1 AND status=$2`, state, domain.ConnPending)
}

func (s *Store) Connection(ctx context.Context, id int64) (*Connection, error) {
	return s.oneConnection(ctx, `WHERE id=$1`, id)
}

func (s *Store) oneConnection(ctx context.Context, where string, args ...any) (*Connection, error) {
	c, err := scanConnection(s.DB.QueryRowContext(ctx, `SELECT `+connectionCols+` FROM bank_connections `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Connections lists all connections except abandoned authorizations.
func (s *Store) Connections(ctx context.Context) ([]Connection, error) {
	return queryAll(ctx, s.DB, scanConnection, `SELECT `+connectionCols+` FROM bank_connections
		WHERE status <> $1 OR created_at > now() - $2::interval ORDER BY id`, domain.ConnPending, pendingConnectionTTL)
}

func (s *Store) ActivateConnection(ctx context.Context, id int64, sessionID string, validUntil time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE bank_connections SET session_id=$2, valid_until=$3, status=$4, last_error='', auth_state=NULL WHERE id=$1`,
		id, sessionID, validUntil, domain.ConnActive)
	return err
}

func (s *Store) SetConnectionStatus(ctx context.Context, id int64, status domain.ConnStatus, msg string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE bank_connections SET status=$2, last_error=$3 WHERE id=$1`, id, status, msg)
	return err
}
