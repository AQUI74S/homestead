package hv

import (
	"sort"
	"time"

	"github.com/AQUI74S/homestead/internal/store"
)

// CostLine is an allocable cost type of a property in the settlement year.
type CostLine struct {
	CostType string `json:"cost_type"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Total    int64  `json:"total"`
	FromBank int64  `json:"from_bank"`
	Manual   int64  `json:"manual"`
}

// TenantShare is a tenant's share of a cost type.
type TenantShare struct {
	CostType string  `json:"cost_type"`
	Name     string  `json:"name"`
	Key      string  `json:"key"`
	Basis    string  `json:"basis"` // human-readable calculation basis
	Factor   float64 `json:"factor"`
	Amount   int64   `json:"amount"`
}

type TenantStatement struct {
	LeaseID   int64         `json:"lease_id"`
	Tenant    string        `json:"tenant"`
	Unit      string        `json:"unit"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	Days      int           `json:"days"`
	Shares    []TenantShare `json:"shares"`
	CostSum   int64         `json:"cost_sum"`
	Prepaid   int64         `json:"prepaid"`
	Result    int64         `json:"result"` // > 0 back payment, < 0 credit
	NewPrepay int64         `json:"suggested_prepay"`
}

type Settlement struct {
	PropertyID int64             `json:"property_id"`
	Year       int               `json:"year"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	Costs      []CostLine        `json:"costs"`
	CostTotal  int64             `json:"cost_total"`
	Statements []TenantStatement `json:"statements"`
	OwnerShare int64             `json:"owner_share"` // vacancy and rounding
	Deadline   string            `json:"deadline"`
	TotalArea  int64             `json:"total_area"`
}

// ComputeSettlement builds the Nebenkostenabrechnung (utility cost settlement) of a property for a calendar year.
// Allocation: area (m² × days), persons (person-days) or units (unit × days).
func ComputeSettlement(p store.Property, leases []store.Lease, txns []store.HVTxn, manual []store.ManualCost, keys map[string]string, year int) Settlement {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	daysYear := int(to.Sub(from).Hours() / 24)
	st := Settlement{PropertyID: p.ID, Year: year, From: from.Format("2006-01-02"), To: to.AddDate(0, 0, -1).Format("2006-01-02"),
		Costs: []CostLine{}, Statements: []TenantStatement{}, Deadline: time.Date(year+1, 12, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02")}

	area := map[int64]int64{}
	for _, u := range p.Units {
		area[u.ID] = u.AreaCents
		st.TotalArea += u.AreaCents
	}
	nUnits := len(p.Units)

	// collect costs
	byType := map[string]*CostLine{}
	get := func(t string) *CostLine {
		if c, ok := byType[t]; ok {
			return c
		}
		ct, _ := TypeOf(t)
		key := ct.DefaultKey
		if k, ok := keys[t]; ok {
			key = k
		}
		c := &CostLine{CostType: t, Name: ct.Name, Key: key}
		byType[t] = c
		return c
	}
	for _, t := range txns {
		ct, ok := TypeOf(t.CostType)
		if !ok || !ct.Umlage || t.PropertyID == nil || *t.PropertyID != p.ID {
			continue
		}
		d := day(t.Date)
		if d.Before(from) || !d.Before(to) {
			continue
		}
		c := get(t.CostType)
		c.FromBank -= t.AmountCents // expenses are negative, refunds reduce the total
	}
	for _, m := range manual {
		ct, ok := TypeOf(m.CostType)
		if !ok || !ct.Umlage || m.PropertyID != p.ID || m.Year != year {
			continue
		}
		get(m.CostType).Manual += m.Amount
	}
	for _, c := range byType {
		c.Total = c.FromBank + c.Manual
		if c.Total != 0 {
			st.Costs = append(st.Costs, *c)
			st.CostTotal += c.Total
		}
	}
	sort.Slice(st.Costs, func(i, j int) bool { return st.Costs[i].Name < st.Costs[j].Name })

	// leases in the year
	type occ struct {
		l    store.Lease
		days int
	}
	var occs []occ
	var personDays int64
	for _, l := range leases {
		if l.PropertyID != p.ID {
			continue
		}
		d := occupiedDays(l, from, to)
		if d == 0 {
			continue
		}
		occs = append(occs, occ{l, d})
		personDays += int64(l.Persons) * int64(d)
	}
	sort.Slice(occs, func(i, j int) bool { return occs[i].l.UnitName+occs[i].l.Start < occs[j].l.UnitName+occs[j].l.Start })

	var allocated int64
	for _, o := range occs {
		ts := TenantStatement{LeaseID: o.l.ID, Tenant: o.l.Tenant.Name, Unit: o.l.UnitName, Days: o.days, Shares: []TenantShare{}}
		s, e := from, to.AddDate(0, 0, -1)
		if o.l.Start != "" && day(o.l.Start).After(s) {
			s = day(o.l.Start)
		}
		if o.l.End != "" && day(o.l.End).Before(e) {
			e = day(o.l.End)
		}
		ts.From, ts.To = s.Format("2006-01-02"), e.Format("2006-01-02")
		for _, c := range st.Costs {
			var f float64
			var basis string
			switch c.Key {
			case "personen":
				if personDays > 0 {
					f = float64(int64(o.l.Persons)*int64(o.days)) / float64(personDays)
				}
				basis = itoa(o.l.Persons) + " Pers. × " + itoa(o.days) + " Tage"
			case "einheiten":
				if nUnits > 0 {
					f = 1 / float64(nUnits) * float64(o.days) / float64(daysYear)
				}
				basis = "1/" + itoa(nUnits) + " Einheiten × " + itoa(o.days) + "/" + itoa(daysYear) + " Tage"
			default: // area (flaeche)
				if st.TotalArea > 0 {
					f = float64(area[o.l.UnitID]) / float64(st.TotalArea) * float64(o.days) / float64(daysYear)
				}
				basis = m2(area[o.l.UnitID]) + " / " + m2(st.TotalArea) + " m² × " + itoa(o.days) + "/" + itoa(daysYear) + " Tage"
			}
			amt := int64(float64(c.Total)*f + 0.5)
			ts.Shares = append(ts.Shares, TenantShare{CostType: c.CostType, Name: c.Name, Key: c.Key, Basis: basis, Factor: f, Amount: amt})
			ts.CostSum += amt
		}
		// prepayments per lease for the occupied days
		for m := monthStart(from); m.Before(to); m = m.AddDate(0, 1, 0) {
			next := m.AddDate(0, 1, 0)
			d := occupiedDays(o.l, m, next)
			if d == 0 {
				continue
			}
			_, nk := RentAt(o.l, m)
			total := int(next.Sub(m).Hours() / 24)
			ts.Prepaid += nk * int64(d) / int64(total)
		}
		ts.Result = ts.CostSum - ts.Prepaid
		if o.days > 0 {
			// suggested new prepayment: annual costs / 12, rounded to whole 5 €
			perYear := float64(ts.CostSum) * float64(daysYear) / float64(o.days)
			ts.NewPrepay = int64((perYear/12+249)/500) * 500
		}
		allocated += ts.CostSum
		st.Statements = append(st.Statements, ts)
	}
	st.OwnerShare = st.CostTotal - allocated
	return st
}

func itoa(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	b := []byte{}
	for {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
		if n == 0 {
			break
		}
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func m2(c int64) string {
	whole, frac := c/100, c%100
	if frac == 0 {
		return itoa(int(whole))
	}
	f := itoa(int(frac))
	if frac < 10 {
		f = "0" + f
	}
	return itoa(int(whole)) + "," + f
}
