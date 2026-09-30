package store

import (
	"context"
	"database/sql"
	"github.com/AQUI74S/homestead/internal/domain"
	"strings"
	"time"
)

// ---------- Hausverwaltung (property management): master data ----------

type Property struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	PurchasePrice *int64 `json:"purchase_price"`
	Notes         string `json:"notes"`
	Units         []Unit `json:"units"`
}

type Unit struct {
	ID         int64  `json:"id"`
	PropertyID int64  `json:"property_id"`
	Name       string `json:"name"`
	AreaCents  int64  `json:"area"` // m² × 100
	Notes      string `json:"notes"`
}

type Tenant struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	IBAN  string `json:"iban"`
	Notes string `json:"notes"`
}

type RentStep struct {
	ID        int64  `json:"id"`
	LeaseID   int64  `json:"lease_id"`
	ValidFrom string `json:"valid_from"`
	RentCold  int64  `json:"rent_cold"`
	NKPrepay  int64  `json:"nk_prepay"`
}

type Lease struct {
	ID           int64      `json:"id"`
	UnitID       int64      `json:"unit_id"`
	UnitName     string     `json:"unit_name"`
	PropertyID   int64      `json:"property_id"`
	PropertyName string     `json:"property_name"`
	Tenant       Tenant     `json:"tenant"`
	Start        string     `json:"start_date"`
	End          string     `json:"end_date"`
	RentCold     int64      `json:"rent_cold"`
	NKPrepay     int64      `json:"nk_prepay"`
	DueDay       int        `json:"due_day"`
	Deposit      int64      `json:"deposit"`
	DepositPaid  bool       `json:"deposit_paid"`
	Persons      int        `json:"persons"`
	RentType     string     `json:"rent_type"`
	LastIncrease string     `json:"last_increase"`
	TrackFrom    string     `json:"track_from"`
	MatchText    string     `json:"match_text"`
	Notes        string     `json:"notes"`
	Steps        []RentStep `json:"steps"`
}

func (s *Store) Properties(ctx context.Context) ([]Property, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, address, (purchase_price*100)::bigint, notes FROM properties ORDER BY name`)
	if err != nil {
		return nil, err
	}
	out := []Property{}
	idx := map[int64]int{}
	for rows.Next() {
		var p Property
		var pp sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Name, &p.Address, &pp, &p.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		if pp.Valid {
			p.PurchasePrice = &pp.Int64
		}
		p.Units = []Unit{}
		idx[p.ID] = len(out)
		out = append(out, p)
	}
	rows.Close()
	urows, err := s.DB.QueryContext(ctx, `SELECT id, property_id, name, (area_m2*100)::bigint, notes FROM units ORDER BY property_id, name`)
	if err != nil {
		return nil, err
	}
	defer urows.Close()
	for urows.Next() {
		var u Unit
		if err := urows.Scan(&u.ID, &u.PropertyID, &u.Name, &u.AreaCents, &u.Notes); err != nil {
			return nil, err
		}
		if i, ok := idx[u.PropertyID]; ok {
			out[i].Units = append(out[i].Units, u)
		}
	}
	return out, urows.Err()
}

func (s *Store) SaveProperty(ctx context.Context, p Property) (int64, error) {
	if p.ID == 0 {
		err := s.DB.QueryRowContext(ctx, `INSERT INTO properties(name, address, purchase_price, notes) VALUES ($1,$2,($3::bigint)::numeric/100,$4) RETURNING id`,
			p.Name, p.Address, p.PurchasePrice, p.Notes).Scan(&p.ID)
		return p.ID, err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE properties SET name=$2, address=$3, purchase_price=($4::bigint)::numeric/100, notes=$5 WHERE id=$1`,
		p.ID, p.Name, p.Address, p.PurchasePrice, p.Notes)
	return p.ID, mustAffect(res, err)
}

func (s *Store) DeleteProperty(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM properties WHERE id=$1`, id)
	return mustAffect(res, err)
}

func (s *Store) SaveUnit(ctx context.Context, u Unit) (int64, error) {
	if u.ID == 0 {
		err := s.DB.QueryRowContext(ctx, `INSERT INTO units(property_id, name, area_m2, notes) VALUES ($1,$2,($3::bigint)::numeric/100,$4) RETURNING id`,
			u.PropertyID, u.Name, u.AreaCents, u.Notes).Scan(&u.ID)
		return u.ID, err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE units SET name=$2, area_m2=($3::bigint)::numeric/100, notes=$4 WHERE id=$1`, u.ID, u.Name, u.AreaCents, u.Notes)
	return u.ID, mustAffect(res, err)
}

func (s *Store) DeleteUnit(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM units WHERE id=$1`, id)
	return mustAffect(res, err)
}

func dateStr(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(domain.DateLayout)
}

func nullDate(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if t, err := time.Parse(domain.DateLayout, s); err == nil {
		return t
	}
	return nil
}

func (s *Store) Leases(ctx context.Context) ([]Lease, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT l.id, l.unit_id, u.name, p.id, p.name, t.id, t.name, t.email, t.phone, t.iban, t.notes,
		l.start_date, l.end_date, (l.rent_cold*100)::bigint, (l.nk_prepay*100)::bigint, l.due_day, (l.deposit*100)::bigint, l.deposit_paid,
		l.persons, l.rent_type, l.last_increase, l.track_from, l.match_text, l.notes
		FROM leases l JOIN units u ON u.id=l.unit_id JOIN properties p ON p.id=u.property_id JOIN tenants t ON t.id=l.tenant_id
		ORDER BY p.name, u.name, l.start_date DESC`)
	if err != nil {
		return nil, err
	}
	out := []Lease{}
	idx := map[int64]int{}
	for rows.Next() {
		var l Lease
		var start sql.NullTime
		var end, inc, track sql.NullTime
		if err := rows.Scan(&l.ID, &l.UnitID, &l.UnitName, &l.PropertyID, &l.PropertyName, &l.Tenant.ID, &l.Tenant.Name, &l.Tenant.Email,
			&l.Tenant.Phone, &l.Tenant.IBAN, &l.Tenant.Notes, &start, &end, &l.RentCold, &l.NKPrepay, &l.DueDay, &l.Deposit, &l.DepositPaid,
			&l.Persons, &l.RentType, &inc, &track, &l.MatchText, &l.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		l.Start, l.End, l.LastIncrease, l.TrackFrom = dateStr(start), dateStr(end), dateStr(inc), dateStr(track)
		l.Steps = []RentStep{}
		idx[l.ID] = len(out)
		out = append(out, l)
	}
	rows.Close()
	srows, err := s.DB.QueryContext(ctx, `SELECT id, lease_id, to_char(valid_from,'YYYY-MM-DD'), (rent_cold*100)::bigint, (nk_prepay*100)::bigint FROM rent_steps ORDER BY valid_from`)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var st RentStep
		if err := srows.Scan(&st.ID, &st.LeaseID, &st.ValidFrom, &st.RentCold, &st.NKPrepay); err != nil {
			return nil, err
		}
		if i, ok := idx[st.LeaseID]; ok {
			out[i].Steps = append(out[i].Steps, st)
		}
	}
	return out, srows.Err()
}

// SaveLease creates or updates both the tenant and the lease.
func (s *Store) SaveLease(ctx context.Context, l Lease) (int64, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		iban := domain.NormIBAN(l.Tenant.IBAN)
		if l.Tenant.ID == 0 {
			if err := tx.QueryRowContext(ctx, `INSERT INTO tenants(name, email, phone, iban, notes) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
				l.Tenant.Name, l.Tenant.Email, l.Tenant.Phone, iban, l.Tenant.Notes).Scan(&l.Tenant.ID); err != nil {
				return err
			}
		} else if _, err := tx.ExecContext(ctx, `UPDATE tenants SET name=$2, email=$3, phone=$4, iban=$5, notes=$6 WHERE id=$1`,
			l.Tenant.ID, l.Tenant.Name, l.Tenant.Email, l.Tenant.Phone, iban, l.Tenant.Notes); err != nil {
			return err
		}
		args := []any{l.UnitID, l.Tenant.ID, nullDate(l.Start), nullDate(l.End), l.RentCold, l.NKPrepay, l.DueDay, l.Deposit, l.DepositPaid,
			l.Persons, l.RentType, nullDate(l.LastIncrease), nullDate(l.TrackFrom), strings.TrimSpace(l.MatchText), l.Notes}
		if l.ID == 0 {
			return tx.QueryRowContext(ctx, `INSERT INTO leases(unit_id, tenant_id, start_date, end_date, rent_cold, nk_prepay, due_day, deposit,
				deposit_paid, persons, rent_type, last_increase, track_from, match_text, notes)
				VALUES ($1,$2,$3,$4,($5::bigint)::numeric/100,($6::bigint)::numeric/100,$7,($8::bigint)::numeric/100,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
				args...).Scan(&l.ID)
		}
		return mustAffect(tx.ExecContext(ctx, `UPDATE leases SET unit_id=$1, tenant_id=$2, start_date=$3, end_date=$4, rent_cold=($5::bigint)::numeric/100,
			nk_prepay=($6::bigint)::numeric/100, due_day=$7, deposit=($8::bigint)::numeric/100, deposit_paid=$9, persons=$10, rent_type=$11,
			last_increase=$12, track_from=$13, match_text=$14, notes=$15 WHERE id=$16`, append(args, l.ID)...))
	})
	return l.ID, err
}

func (s *Store) DeleteLease(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM leases WHERE id=$1`, id)
	return mustAffect(res, err)
}

func (s *Store) AddRentStep(ctx context.Context, st RentStep) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO rent_steps(lease_id, valid_from, rent_cold, nk_prepay) VALUES ($1,$2,($3::bigint)::numeric/100,($4::bigint)::numeric/100)
		ON CONFLICT (lease_id, valid_from) DO UPDATE SET rent_cold=EXCLUDED.rent_cold, nk_prepay=EXCLUDED.nk_prepay RETURNING id`,
		st.LeaseID, nullDate(st.ValidFrom), st.RentCold, st.NKPrepay).Scan(&id)
	return id, err
}

func (s *Store) DeleteRentStep(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM rent_steps WHERE id=$1`, id)
	return mustAffect(res, err)
}

// ---------- Deadlines ----------

type Reminder struct {
	ID         int64  `json:"id"`
	PropertyID *int64 `json:"property_id"`
	LeaseID    *int64 `json:"lease_id"`
	DueDate    string `json:"due_date"`
	Title      string `json:"title"`
	Done       bool   `json:"done"`
}

func (s *Store) Reminders(ctx context.Context) ([]Reminder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, property_id, lease_id, to_char(due_date,'YYYY-MM-DD'), title, done FROM reminders ORDER BY done, due_date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reminder{}
	for rows.Next() {
		var r Reminder
		var p, l sql.NullInt64
		if err := rows.Scan(&r.ID, &p, &l, &r.DueDate, &r.Title, &r.Done); err != nil {
			return nil, err
		}
		if p.Valid {
			r.PropertyID = &p.Int64
		}
		if l.Valid {
			r.LeaseID = &l.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddReminder(ctx context.Context, r Reminder) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO reminders(property_id, lease_id, due_date, title) VALUES ($1,$2,$3,$4) RETURNING id`,
		r.PropertyID, r.LeaseID, nullDate(r.DueDate), r.Title).Scan(&id)
	return id, err
}

func (s *Store) SetReminderDone(ctx context.Context, id int64, done bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE reminders SET done=$2 WHERE id=$1`, id, done)
	return mustAffect(res, err)
}

func (s *Store) DeleteReminder(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM reminders WHERE id=$1`, id)
	return mustAffect(res, err)
}

// ---------- Transactions on Mietkonten (rent accounts) ----------

type HVTxn struct {
	ID               int64         `json:"id"`
	AccountID        int64         `json:"account_id"`
	Date             string        `json:"date"`
	AmountCents      int64         `json:"amount"`
	Counterparty     string        `json:"counterparty"`
	CounterpartyIBAN string        `json:"counterparty_iban"`
	Remittance       string        `json:"remittance"`
	Merchant         string        `json:"merchant"`
	MerchantKey      string        `json:"merchant_key"`
	PropertyID       *int64        `json:"property_id"`
	LeaseID          *int64        `json:"lease_id"`
	CostType         string        `json:"cost_type"`
	Source           domain.Source `json:"hv_source"` // auto | manual
}

// HVTransactions returns all transactions of the property-management accounts, optionally within [from, to).
func (s *Store) HVTransactions(ctx context.Context, from, to time.Time) ([]HVTxn, error) {
	q := `SELECT t.id, t.account_id, to_char(t.booking_date,'YYYY-MM-DD'), (t.amount*100)::bigint, t.counterparty, t.counterparty_iban,
		t.remittance, t.merchant, t.merchant_key, t.property_id, t.lease_id, t.cost_type, t.hv_source
		FROM transactions t JOIN accounts a ON a.id=t.account_id WHERE ` + sqlPropertyBook + ` AND a.active`
	var args []any
	if !from.IsZero() {
		q += ` AND t.booking_date >= $1 AND t.booking_date < $2`
		args = append(args, from, to)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY t.booking_date DESC, t.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HVTxn{}
	for rows.Next() {
		var t HVTxn
		var p, l sql.NullInt64
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Date, &t.AmountCents, &t.Counterparty, &t.CounterpartyIBAN, &t.Remittance,
			&t.Merchant, &t.MerchantKey, &p, &l, &t.CostType, &t.Source); err != nil {
			return nil, err
		}
		if p.Valid {
			t.PropertyID = &p.Int64
		}
		if l.Valid {
			t.LeaseID = &l.Int64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FirstHVDate returns the earliest booking date on property-management accounts.
func (s *Store) FirstHVDate(ctx context.Context) (time.Time, error) {
	var t sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT min(t.booking_date) FROM transactions t JOIN accounts a ON a.id=t.account_id WHERE `+sqlPropertyBook).Scan(&t)
	return t.Time, err
}

type HVAssign struct {
	ID         int64
	PropertyID *int64
	LeaseID    *int64
	CostType   string
	Source     domain.Source
}

func (s *Store) ApplyHVAssignments(ctx context.Context, ups []HVAssign) error {
	if len(ups) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `UPDATE transactions SET property_id=$2, lease_id=$3, cost_type=$4, hv_source=$5 WHERE id=$1`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, u := range ups {
			if _, err := stmt.ExecContext(ctx, u.ID, u.PropertyID, u.LeaseID, u.CostType, u.Source); err != nil {
				return err
			}
		}
		return nil
	})
}

type HVRule struct {
	MerchantKey string `json:"merchant_key"`
	PropertyID  *int64 `json:"property_id"`
	CostType    string `json:"cost_type"`
}

func (s *Store) HVRules(ctx context.Context) (map[string]HVRule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT merchant_key, property_id, cost_type FROM hv_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HVRule{}
	for rows.Next() {
		var r HVRule
		var p sql.NullInt64
		if err := rows.Scan(&r.MerchantKey, &p, &r.CostType); err != nil {
			return nil, err
		}
		if p.Valid {
			r.PropertyID = &p.Int64
		}
		out[r.MerchantKey] = r
	}
	return out, rows.Err()
}

func (s *Store) SaveHVRule(ctx context.Context, r HVRule) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO hv_rules(merchant_key, property_id, cost_type) VALUES ($1,$2,$3)
		ON CONFLICT (merchant_key) DO UPDATE SET property_id=EXCLUDED.property_id, cost_type=EXCLUDED.cost_type`, r.MerchantKey, r.PropertyID, r.CostType)
	return err
}

// ---------- Nebenkosten (utility/service charges) ----------

type ManualCost struct {
	ID         int64  `json:"id"`
	PropertyID int64  `json:"property_id"`
	Year       int    `json:"year"`
	CostType   string `json:"cost_type"`
	Amount     int64  `json:"amount"`
	Note       string `json:"note"`
}

func (s *Store) ManualCosts(ctx context.Context, propertyID int64, year int) ([]ManualCost, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, property_id, year, cost_type, (amount*100)::bigint, note FROM manual_costs
		WHERE ($1 = 0 OR property_id=$1) AND ($2 = 0 OR year=$2) ORDER BY cost_type`, propertyID, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManualCost{}
	for rows.Next() {
		var c ManualCost
		if err := rows.Scan(&c.ID, &c.PropertyID, &c.Year, &c.CostType, &c.Amount, &c.Note); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddManualCost(ctx context.Context, c ManualCost) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO manual_costs(property_id, year, cost_type, amount, note) VALUES ($1,$2,$3,($4::bigint)::numeric/100,$5) RETURNING id`,
		c.PropertyID, c.Year, c.CostType, c.Amount, c.Note).Scan(&id)
	return id, err
}

func (s *Store) DeleteManualCost(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM manual_costs WHERE id=$1`, id)
	return mustAffect(res, err)
}

func (s *Store) NKKeys(ctx context.Context, propertyID int64) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT cost_type, key FROM nk_keys WHERE property_id=$1`, propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var t, k string
		if err := rows.Scan(&t, &k); err != nil {
			return nil, err
		}
		out[t] = k
	}
	return out, rows.Err()
}

func (s *Store) SetNKKey(ctx context.Context, propertyID int64, costType, key string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO nk_keys(property_id, cost_type, key) VALUES ($1,$2,$3)
		ON CONFLICT (property_id, cost_type) DO UPDATE SET key=EXCLUDED.key`, propertyID, costType, key)
	return err
}
