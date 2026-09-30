package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

// customCategorySort puts categories the user creates after the built-in ones.
const customCategorySort = 500

type Category struct {
	ID          int64        `json:"id"`
	Group       domain.Group `json:"group"`
	Slug        string       `json:"slug"`
	Name        string       `json:"name"`
	BudgetCents int64        `json:"budget"` // default budget per month
	Sort        int          `json:"sort"`
}

func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (Category, error) {
		var c Category
		return c, sc.Scan(&c.ID, &c.Group, &c.Slug, &c.Name, &c.BudgetCents, &c.Sort)
	}, `SELECT id, grp, slug, name, (budget*100)::bigint, sort FROM categories ORDER BY grp, sort, name`)
}

func (s *Store) CreateCategory(ctx context.Context, group domain.Group, name string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO categories(grp, slug, name, sort) VALUES ($1, $2, $3, $4) RETURNING id`,
		group, slugify(name), name, customCategorySort).Scan(&id)
	return id, err
}

// slugify derives a unique slug from a category name.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range domain.Fold(name) {
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

// ApplyDefaultBudgets sets the default budget of categories in a single transaction.
// want maps category ID to the new budget; keep decides per category (with its
// current budget) whether to leave it alone. With clearFrom set, monthly overrides
// from that month onward are removed. Returns the number of changed categories.
func (s *Store) ApplyDefaultBudgets(ctx context.Context, want map[int64]int64, keep func(current, next int64) bool, clearFrom string) (int, error) {
	changed := 0
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, (budget*100)::bigint FROM categories c WHERE `+sqlNotTransfer)
		if err != nil {
			return err
		}
		current := map[int64]int64{}
		for rows.Next() {
			var id, budget int64
			if err := rows.Scan(&id, &budget); err != nil {
				rows.Close()
				return err
			}
			current[id] = budget
		}
		rows.Close()
		for id, cur := range current {
			next := want[id]
			if next == cur || keep(cur, next) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE categories SET budget=($2::bigint)::numeric/100 WHERE id=$1`, id, next); err != nil {
				return err
			}
			changed++
		}
		if clearFrom != "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM budgets WHERE month >= $1`, clearFrom); err != nil {
				return err
			}
		}
		return nil
	})
	return changed, err
}
