package store

import "github.com/AQUI74S/homestead/internal/domain"

// SQL fragments shared by several queries. They are built from the domain
// constants so that the values in SQL and Go cannot drift apart.
const (
	// sqlHousehold limits a query joined with accounts "a" to active household accounts.
	sqlHousehold = `a.active AND a.book='` + string(domain.BookHousehold) + `'`

	// sqlPropertyBook limits a query joined with accounts "a" to property management accounts.
	sqlPropertyBook = `a.book='` + string(domain.BookProperty) + `'`

	// sqlCatchAllAuto matches transactions (t, joined with categories c) that the
	// classifier could not place and that nobody has confirmed.
	sqlCatchAllAuto = `c.slug IN ('` + domain.SlugOther + `','` + domain.SlugOtherIncome + `')` +
		` AND t.category_source='` + string(domain.SourceAuto) + `'`

	// sqlNotTransfer excludes transfers between own accounts (categories c).
	sqlNotTransfer = `c.grp <> '` + string(domain.GroupTransfer) + `'`

	// sqlIsIncome matches income categories (categories c).
	sqlIsIncome = `c.grp='` + string(domain.GroupIncome) + `'`
)

// CSVExtIDPrefix marks transactions imported from a CSV file (their ext_id).
const CSVExtIDPrefix = "csv:"
