package store

import (
	"context"
	"fmt"

	"github.com/AQUI74S/homestead/internal/domain"
)

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
	key := func(date string, cents int64) string { return fmt.Sprintf("%s|%d", date, cents) }
	type bucket struct {
		date  string
		cents int64
		n     int
	}
	buckets, err := queryAll(ctx, s.DB, func(sc scanner) (bucket, error) {
		var b bucket
		return b, sc.Scan(&b.date, &b.cents, &b.n)
	}, `SELECT to_char(booking_date,'YYYY-MM-DD'), (amount*100)::bigint, count(*)
		FROM transactions WHERE account_id=$1 AND booking_date BETWEEN $2 AND $3 GROUP BY 1, 2`, accountID, minD, maxD)
	if err != nil {
		return 0, 0, err
	}
	have := map[string]int{}
	for _, b := range buckets {
		have[key(b.date, b.cents)] = b.n
	}
	var fresh []NewTxn
	skipped := 0
	for _, t := range txs {
		k := key(t.BookingDate.Format(domain.DateLayout), t.AmountCents)
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
			FROM transactions WHERE ext_id LIKE $1) c
		JOIN (
			SELECT account_id, booking_date, amount, count(*) AS n
			FROM transactions WHERE ext_id NOT LIKE $1 GROUP BY 1, 2, 3) a
		USING (account_id, booking_date, amount)
		WHERE c.rn <= a.n)`, CSVExtIDPrefix+"%")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
