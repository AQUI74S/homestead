package store

import (
	"context"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

type Rule struct {
	ID         int64            `json:"id"`
	Field      domain.RuleField `json:"field"`
	Pattern    string           `json:"pattern"`
	CategoryID int64            `json:"category_id"`
	Slug       string           `json:"category_slug"`
	CreatedAt  time.Time        `json:"created_at"`
	Hits       int              `json:"hits"` // transactions the rule categorizes (set by the API)
}

// RuleHits counts the rule-categorized transactions per reason text.
func (s *Store) RuleHits(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT reason, count(*) FROM transactions WHERE category_source=$1 GROUP BY reason`,
		domain.SourceRule)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := map[string]int{}
	for rows.Next() {
		var reason string
		var n int
		if err := rows.Scan(&reason, &n); err != nil {
			return nil, err
		}
		hits[reason] = n
	}
	return hits, rows.Err()
}

func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	return queryAll(ctx, s.DB, func(sc scanner) (Rule, error) {
		var r Rule
		return r, sc.Scan(&r.ID, &r.Field, &r.Pattern, &r.CategoryID, &r.Slug, &r.CreatedAt)
	}, `SELECT r.id, r.field, r.pattern, r.category_id, c.slug, r.created_at FROM rules r JOIN categories c ON c.id=r.category_id ORDER BY r.id DESC`)
}

func (s *Store) UpsertRule(ctx context.Context, field domain.RuleField, pattern string, categoryID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO rules(field, pattern, category_id) VALUES ($1, lower($2), $3)
		ON CONFLICT (field, pattern) DO UPDATE SET category_id=EXCLUDED.category_id`, field, strings.TrimSpace(pattern), categoryID)
	return err
}

// ruleColumns maps the rule fields that ReleaseToRule supports to their column.
var ruleColumns = map[domain.RuleField]string{domain.RuleMerchant: "merchant_key", domain.RuleIBAN: "counterparty_iban"}

// ReleaseToRule hands transactions that were set by hand to the same category
// over to a new rule, so they count as rule-based from now on. Hand choices
// that differ from the rule stay untouched.
func (s *Store) ReleaseToRule(ctx context.Context, field domain.RuleField, pattern string, categoryID int64) error {
	col, ok := ruleColumns[field]
	if !ok {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE transactions SET category_source=$3
		WHERE category_source=$4 AND category_id=$2 AND lower(replace(`+col+`,' ',''))=lower(replace($1,' ',''))`,
		strings.TrimSpace(pattern), categoryID, domain.SourceAuto, domain.SourceManual)
	return err
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM rules WHERE id=$1`, id)
	return err
}
