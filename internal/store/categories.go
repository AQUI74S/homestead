package store

import (
	"context"
	"database/sql"
	"errors"
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
	Custom      bool         `json:"custom"` // created by the user: can be moved and deleted
}

const sqlCategory = `SELECT id, grp, slug, name, (budget*100)::bigint, sort, custom FROM categories`

func scanCategory(sc scanner) (Category, error) {
	var c Category
	return c, sc.Scan(&c.ID, &c.Group, &c.Slug, &c.Name, &c.BudgetCents, &c.Sort, &c.Custom)
}

func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	return queryAll(ctx, s.DB, scanCategory, sqlCategory+` ORDER BY grp, sort, name`)
}

// CategoryByID returns a category or ErrNotFound.
func (s *Store) CategoryByID(ctx context.Context, id int64) (Category, error) {
	c, err := scanCategory(s.DB.QueryRowContext(ctx, sqlCategory+` WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// CategoryNameTaken reports whether another category (not except) has this name.
func (s *Store) CategoryNameTaken(ctx context.Context, name string, except int64) (bool, error) {
	var taken bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE lower(name)=lower($1) AND id<>$2)`,
		strings.TrimSpace(name), except).Scan(&taken)
	return taken, err
}

// CreateCategory adds a category of the user.
func (s *Store) CreateCategory(ctx context.Context, group domain.Group, name string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO categories(grp, slug, name, sort, custom) VALUES ($1, $2, $3, $4, true) RETURNING id`,
		group, slugify(name), name, customCategorySort).Scan(&id)
	return id, err
}

// UpdateCategory renames a category and moves it to another group.
func (s *Store) UpdateCategory(ctx context.Context, id int64, name string, group domain.Group) error {
	return mustAffect(s.DB.ExecContext(ctx, `UPDATE categories SET name=$2, grp=$3 WHERE id=$1`, id, name, group))
}

// DeleteCategory removes a category of the user. Its transactions are handed
// back to the automatic classification (also those set by hand), its rules and
// budgets go with it. Returns the number of released transactions.
func (s *Store) DeleteCategory(ctx context.Context, id int64) (int64, error) {
	var released int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE transactions SET category_source=$2 WHERE category_id=$1`, id, domain.SourceAuto)
		if err != nil {
			return err
		}
		released, _ = res.RowsAffected()
		return mustAffect(tx.ExecContext(ctx, `DELETE FROM categories WHERE id=$1 AND custom`, id))
	})
	return released, err
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

// ApplyDefaultBudgets sets the default budget of the variable spending categories
// (the only ones planned with a budget) in a single transaction.
// want maps category ID to the new budget; keep decides per category (with its
// current budget) whether to leave it alone. With clearFrom set, monthly overrides
// from that month onward are removed. Returns the number of changed categories.
func (s *Store) ApplyDefaultBudgets(ctx context.Context, want map[int64]int64, keep func(current, next int64) bool, clearFrom string) (int, error) {
	changed := 0
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, (budget*100)::bigint FROM categories WHERE grp=$1`, domain.GroupExpenses)
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
