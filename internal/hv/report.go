package hv

import (
	"sort"
	"time"

	"github.com/AQUI74S/homestead/internal/store"
)

// PropertyYear is the annual overview of a property.
type PropertyYear struct {
	PropertyID   int64            `json:"property_id"`
	Name         string           `json:"name"`
	Year         int              `json:"year"`
	Income       int64            `json:"income"`       // rent incl. utility prepayments, back payments, other
	Costs        int64            `json:"costs"`        // all costs (positive)
	CostsUmlage  int64            `json:"costs_umlage"` // of which allocable to tenants
	CostsByType  map[string]int64 `json:"costs_by_type"`
	Surplus      int64            `json:"surplus"`        // income - costs (cash flow)
	ColdRentYear int64            `json:"cold_rent_year"` // current due cold rent × 12
	GrossYield   float64          `json:"gross_yield"`    // gross rental yield in %
	Units        int              `json:"units"`
	Occupied     int              `json:"occupied"`
}

// PropertyReport summarizes income and costs per property for one year.
func PropertyReport(props []store.Property, leases []store.Lease, txns []store.HVTxn, year int, today time.Time) []PropertyYear {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	out := []PropertyYear{}
	for _, p := range props {
		py := PropertyYear{PropertyID: p.ID, Name: p.Name, Year: year, CostsByType: map[string]int64{}, Units: len(p.Units)}
		for _, t := range txns {
			if t.PropertyID == nil || *t.PropertyID != p.ID {
				continue
			}
			d := day(t.Date)
			if d.Before(from) || !d.Before(to) {
				continue
			}
			ct, _ := TypeOf(t.CostType)
			switch ct.Kind {
			case "einnahme":
				py.Income += t.AmountCents
			case "kosten":
				py.Costs -= t.AmountCents
				py.CostsByType[t.CostType] -= t.AmountCents
				if ct.Umlage {
					py.CostsUmlage -= t.AmountCents
				}
			}
		}
		py.Surplus = py.Income - py.Costs
		occupied := map[int64]bool{}
		for _, l := range leases {
			if l.PropertyID != p.ID {
				continue
			}
			if (l.Start == "" || !day(l.Start).After(today)) && (l.End == "" || !day(l.End).Before(today)) {
				cold, _ := RentAt(l, today)
				py.ColdRentYear += cold * 12
				occupied[l.UnitID] = true
			}
		}
		py.Occupied = len(occupied)
		if p.PurchasePrice != nil && *p.PurchasePrice > 0 {
			py.GrossYield = float64(py.ColdRentYear) / float64(*p.PurchasePrice) * 100
		}
		out = append(out, py)
	}
	return out
}

// Deadline is a deadline or a notice.
type Deadline struct {
	Date       string `json:"date"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Kind       string `json:"kind"` // nk | staffel | erhoehung | index | ende | kaution | rueckstand | manuell
	PropertyID int64  `json:"property_id,omitempty"`
	LeaseID    int64  `json:"lease_id,omitempty"`
	ReminderID int64  `json:"reminder_id,omitempty"`
	Urgent     bool   `json:"urgent"`
}

// Deadlines derives deadlines from leases, arrears and reminders.
func Deadlines(props []store.Property, leases []store.Lease, ledgers map[int64]Ledger, reminders []store.Reminder, today time.Time) []Deadline {
	out := []Deadline{}
	ds := func(t time.Time) string { return t.Format("2006-01-02") }
	soon := today.AddDate(0, 0, 30)

	// Previous year's Nebenkostenabrechnung (utility settlement): due at most 12 months after the end of the billing period
	prev := today.Year() - 1
	for _, p := range props {
		has := false
		for _, l := range leases {
			if l.PropertyID == p.ID && occupiedDays(l, time.Date(prev, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(prev+1, 1, 1, 0, 0, 0, 0, time.UTC)) > 0 {
				has = true
			}
		}
		if has {
			d := time.Date(prev+1, 12, 31, 0, 0, 0, 0, time.UTC)
			out = append(out, Deadline{Date: ds(d), Title: "Nebenkostenabrechnung " + itoa(prev), Detail: p.Name + ": muss den Mietern bis zum 31.12. zugehen",
				Kind: "nk", PropertyID: p.ID, Urgent: d.Before(today.AddDate(0, 2, 0))})
		}
	}
	for _, l := range leases {
		if l.End != "" && day(l.End).Before(today) {
			continue // ended
		}
		who := l.Tenant.Name + " (" + l.PropertyName + ", " + l.UnitName + ")"
		for _, s := range l.Steps {
			if d := day(s.ValidFrom); d.After(today) {
				out = append(out, Deadline{Date: s.ValidFrom, Title: "Mietänderung " + who, Detail: "neue Kaltmiete " + money(s.RentCold) + ", NK " + money(s.NKPrepay),
					Kind: "staffel", LeaseID: l.ID, Urgent: d.Before(soon)})
				break
			}
		}
		base := day(l.Start)
		if l.LastIncrease != "" {
			base = day(l.LastIncrease)
		}
		switch l.RentType {
		case "fest":
			// Rent increase up to the local comparative rent (§558 BGB): effective at the earliest 15 months after the last increase
			d := base.AddDate(0, 15, 0)
			dl := Deadline{Date: ds(d), Title: "Mieterhöhung möglich: " + who,
				Detail: "Erhöhung auf die Vergleichsmiete frühestens ab diesem Datum wirksam (Verlangen 2 Monate vorher)", Kind: "erhoehung", LeaseID: l.ID}
			if d.Before(today) {
				dl.Date, dl.Detail = ds(today), "Erhöhung auf die Vergleichsmiete ist seit "+d.Format("02.01.2006")+" möglich (letzte Erhöhung bzw. Mietbeginn "+base.Format("02.01.2006")+")"
			}
			out = append(out, dl)
		case "index":
			d := base.AddDate(1, 0, 0)
			dl := Deadline{Date: ds(d), Title: "Indexanpassung möglich: " + who, Detail: "frühestens 1 Jahr nach der letzten Anpassung", Kind: "index", LeaseID: l.ID}
			if d.Before(today) {
				dl.Date, dl.Detail = ds(today), "Anpassung an den Verbraucherpreisindex ist seit "+d.Format("02.01.2006")+" möglich"
			}
			out = append(out, dl)
		}
		if l.End != "" {
			if d := day(l.End); d.Before(today.AddDate(0, 3, 0)) {
				out = append(out, Deadline{Date: l.End, Title: "Mietende " + who, Detail: "Übergabe, Kaution abrechnen", Kind: "ende", LeaseID: l.ID, Urgent: true})
			}
		}
		if l.Deposit > 0 && !l.DepositPaid {
			out = append(out, Deadline{Date: l.Start, Title: "Kaution offen: " + who, Detail: money(l.Deposit) + " noch nicht als bezahlt markiert",
				Kind: "kaution", LeaseID: l.ID, Urgent: true})
		}
		if lg, ok := ledgers[l.ID]; ok {
			cold, nk := RentAt(l, today)
			if lg.Balance > 0 {
				d := Deadline{Date: ds(today), Title: "Mietrückstand " + who, Detail: money(lg.Balance) + " offen", Kind: "rueckstand", LeaseID: l.ID}
				if cold+nk > 0 && lg.Balance >= 2*(cold+nk) {
					d.Detail += " (mind. zwei Monatsmieten)"
					d.Urgent = true
				}
				out = append(out, d)
			}
		}
	}
	for _, r := range reminders {
		if r.Done {
			continue
		}
		d := Deadline{Date: r.DueDate, Title: r.Title, Kind: "manuell", ReminderID: r.ID, Urgent: day(r.DueDate).Before(soon)}
		if r.LeaseID != nil {
			d.LeaseID = *r.LeaseID
		}
		if r.PropertyID != nil {
			d.PropertyID = *r.PropertyID
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func money(c int64) string {
	neg := c < 0
	if neg {
		c = -c
	}
	euros := c / 100
	s := itoa(int(euros))
	// thousands separators
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	ct := itoa(int(c % 100))
	if c%100 < 10 {
		ct = "0" + ct
	}
	if neg {
		s = "-" + s
	}
	return s + "," + ct + " €"
}
