package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/hv"
	"github.com/AQUI74S/homestead/internal/store"
)

func (s *Server) hvRoutes(a func(string, http.HandlerFunc)) {
	a("GET /api/hv/meta", s.hvMeta)
	a("GET /api/hv/overview", s.hvOverview)
	a("POST /api/hv/properties", s.hvSaveProperty)
	a("PUT /api/hv/properties/{id}", s.hvSaveProperty)
	a("DELETE /api/hv/properties/{id}", s.hvDelete(func(ctx context.Context, id int64) error { return s.st.DeleteProperty(ctx, id) }))
	a("POST /api/hv/units", s.hvSaveUnit)
	a("PUT /api/hv/units/{id}", s.hvSaveUnit)
	a("DELETE /api/hv/units/{id}", s.hvDelete(func(ctx context.Context, id int64) error { return s.st.DeleteUnit(ctx, id) }))
	a("POST /api/hv/leases", s.hvSaveLease)
	a("PUT /api/hv/leases/{id}", s.hvSaveLease)
	a("GET /api/hv/leases/{id}", s.hvLease)
	a("DELETE /api/hv/leases/{id}", s.hvDelete(func(ctx context.Context, id int64) error { return s.st.DeleteLease(ctx, id) }))
	a("POST /api/hv/leases/{id}/steps", s.hvAddStep)
	a("DELETE /api/hv/steps/{id}", s.hvDelete(func(ctx context.Context, id int64) error { return s.st.DeleteRentStep(ctx, id) }))
	a("GET /api/hv/transactions", s.hvTransactions)
	a("PATCH /api/hv/transactions/{id}", s.hvPatchTxn)
	a("GET /api/hv/nk", s.hvNK)
	a("POST /api/hv/manual-costs", s.hvAddManualCost)
	a("DELETE /api/hv/manual-costs/{id}", s.hvDelete(func(ctx context.Context, id int64) error { return s.st.DeleteManualCost(ctx, id) }))
	a("PUT /api/hv/nk-keys", s.hvSetKey)
	a("GET /api/hv/report", s.hvReport)
	a("POST /api/hv/reminders", s.hvAddReminder)
	a("PATCH /api/hv/reminders/{id}", s.hvReminderDone)
	a("DELETE /api/hv/reminders/{id}", s.hvDelete(func(ctx context.Context, id int64) error { return s.st.DeleteReminder(ctx, id) }))
}

func todayUTC() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// hvData loads everything the reports need.
type hvData struct {
	props     []store.Property
	leases    []store.Lease
	txns      []store.HVTxn
	firstData time.Time
	ledgers   map[int64]hv.Ledger
}

func (s *Server) loadHV(ctx context.Context) (*hvData, error) {
	d := &hvData{ledgers: map[int64]hv.Ledger{}}
	var err error
	if d.props, err = s.st.Properties(ctx); err != nil {
		return nil, err
	}
	if d.leases, err = s.st.Leases(ctx); err != nil {
		return nil, err
	}
	if d.txns, err = s.st.HVTransactions(ctx, time.Time{}, time.Time{}); err != nil {
		return nil, err
	}
	if d.firstData, err = s.st.FirstHVDate(ctx); err != nil {
		return nil, err
	}
	today := todayUTC()
	for _, l := range d.leases {
		d.ledgers[l.ID] = hv.ComputeLedger(l, d.txns, d.firstData, today)
	}
	return d, nil
}

func (s *Server) hvDelete(f func(context.Context, int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			bad(w, "Ungültige ID")
			return
		}
		if err := f(r.Context(), id); err != nil {
			s.fail(w, r, err)
			return
		}
		_ = hv.Reassign(r.Context(), s.st)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func (s *Server) hvMeta(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadHV(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"cost_types": hv.CostTypes, "properties": d.props, "leases": d.leases})
}

func (s *Server) hvOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.loadHV(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	today := todayUTC()
	month := r.URL.Query().Get("month")
	if _, err := time.Parse("2006-01", month); err != nil {
		month = today.Format("2006-01")
	}
	type leaseRow struct {
		store.Lease
		Row     *hv.MonthRow `json:"row"`
		Balance int64        `json:"balance"`
		Active  bool         `json:"active"`
	}
	rows := []leaseRow{}
	var soll, paid, open, arrears int64
	for _, l := range d.leases {
		lg := d.ledgers[l.ID]
		lr := leaseRow{Lease: l, Balance: lg.Balance}
		for i := range lg.Months {
			if lg.Months[i].Month == month {
				m := lg.Months[i]
				lr.Row = &m
				soll += m.Soll
				paid += m.Paid
				open += m.Open
			}
		}
		lr.Active = l.End == "" || !day(l.End).Before(today)
		if lg.Balance > 0 {
			arrears += lg.Balance
		}
		if lr.Active || lr.Row != nil || lg.Balance != 0 {
			rows = append(rows, lr)
		}
	}
	reminders, err := s.st.Reminders(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	deadlines := hv.Deadlines(d.props, d.leases, d.ledgers, reminders, today)
	upcoming := []hv.Deadline{}
	limit := today.AddDate(0, 2, 0).Format("2006-01-02")
	for _, dl := range deadlines {
		if dl.Urgent || dl.Date <= limit {
			upcoming = append(upcoming, dl)
		}
	}
	unassigned := 0
	for _, t := range d.txns {
		if t.CostType == "einnahme_sonst" || t.CostType == "sonstiges" || (t.PropertyID == nil && t.CostType != "entnahme" && t.CostType != "") {
			unassigned++
		}
	}
	accts, err := s.st.Accounts(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	hvAccts := []store.Account{}
	for _, a := range accts {
		if a.Book == "verwaltung" {
			hvAccts = append(hvAccts, a)
		}
	}
	writeJSON(w, 200, map[string]any{
		"month": month, "properties": d.props, "leases": rows, "deadlines": upcoming, "unassigned": unassigned,
		"totals": map[string]int64{"soll": soll, "paid": paid, "open": open, "arrears": arrears}, "accounts": hvAccts,
	})
}

func day(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func (s *Server) hvSaveProperty(w http.ResponseWriter, r *http.Request) {
	var p store.Property
	if err := decode(r, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		bad(w, "Name des Objekts angeben")
		return
	}
	if id, ok := pathID(r); ok {
		p.ID = id
	}
	id, err := s.st.SaveProperty(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (s *Server) hvSaveUnit(w http.ResponseWriter, r *http.Request) {
	var u store.Unit
	if err := decode(r, &u); err != nil || strings.TrimSpace(u.Name) == "" {
		bad(w, "Name der Einheit angeben")
		return
	}
	if id, ok := pathID(r); ok {
		u.ID = id
	} else if u.PropertyID == 0 {
		bad(w, "Objekt fehlt")
		return
	}
	id, err := s.st.SaveUnit(r.Context(), u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (s *Server) hvSaveLease(w http.ResponseWriter, r *http.Request) {
	var l store.Lease
	if err := decode(r, &l); err != nil || strings.TrimSpace(l.Tenant.Name) == "" || l.UnitID == 0 {
		bad(w, "Mieter und Einheit angeben")
		return
	}
	if _, err := time.Parse("2006-01-02", l.Start); err != nil {
		bad(w, "Mietbeginn angeben")
		return
	}
	if l.RentType == "" {
		l.RentType = "fest"
	}
	if l.DueDay < 1 || l.DueDay > 28 {
		l.DueDay = 3
	}
	if l.Persons < 0 {
		l.Persons = 0
	}
	if id, ok := pathID(r); ok {
		l.ID = id
	}
	id, err := s.st.SaveLease(r.Context(), l)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := hv.Reassign(r.Context(), s.st); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (s *Server) hvLease(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		bad(w, "Ungültige ID")
		return
	}
	d, err := s.loadHV(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, l := range d.leases {
		if l.ID == id {
			writeJSON(w, 200, map[string]any{"lease": l, "ledger": d.ledgers[id]})
			return
		}
	}
	writeJSON(w, 404, map[string]string{"error": "Mietvertrag nicht gefunden"})
}

func (s *Server) hvAddStep(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var st store.RentStep
	if !ok || decode(r, &st) != nil {
		bad(w, "Ungültige Anfrage")
		return
	}
	if _, err := time.Parse("2006-01-02", st.ValidFrom); err != nil || st.RentCold <= 0 {
		bad(w, "Datum und neue Kaltmiete angeben")
		return
	}
	st.LeaseID = id
	sid, err := s.st.AddRentStep(r.Context(), st)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": sid})
}

func (s *Server) hvTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var from, to time.Time
	if m, err := time.Parse("2006-01", q.Get("month")); err == nil {
		from, to = m, m.AddDate(0, 1, 0)
	} else if y, err := strconv.Atoi(q.Get("year")); err == nil && y > 1900 {
		from = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		to = from.AddDate(1, 0, 0)
	}
	txns, err := s.st.HVTransactions(r.Context(), from, to)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pid, _ := strconv.ParseInt(q.Get("property_id"), 10, 64)
	lid, _ := strconv.ParseInt(q.Get("lease_id"), 10, 64)
	open := q.Get("open") == "1"
	out := []store.HVTxn{}
	for _, t := range txns {
		if pid > 0 && (t.PropertyID == nil || *t.PropertyID != pid) {
			continue
		}
		if lid > 0 && (t.LeaseID == nil || *t.LeaseID != lid) {
			continue
		}
		if open && !(t.CostType == "einnahme_sonst" || t.CostType == "sonstiges" || (t.PropertyID == nil && t.CostType != "entnahme")) {
			continue
		}
		out = append(out, t)
	}
	writeJSON(w, 200, out)
}

func (s *Server) hvPatchTxn(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		PropertyID int64  `json:"property_id"`
		LeaseID    int64  `json:"lease_id"`
		CostType   string `json:"cost_type"`
		Remember   bool   `json:"remember"` // remember for this payee
		Reset      bool   `json:"reset"`
	}
	if !ok || decode(r, &in) != nil {
		bad(w, "Ungültige Anfrage")
		return
	}
	ctx := r.Context()
	if in.Reset {
		if err := s.st.ApplyHVAssignments(ctx, []store.HVAssign{{ID: id, Source: "auto"}}); err != nil {
			s.fail(w, r, err)
			return
		}
	} else {
		if _, ok := hv.TypeOf(in.CostType); !ok {
			bad(w, "Unbekannte Kostenart")
			return
		}
		a := store.HVAssign{ID: id, CostType: in.CostType, Source: "manual"}
		if in.PropertyID > 0 {
			a.PropertyID = &in.PropertyID
		}
		if in.LeaseID > 0 {
			a.LeaseID = &in.LeaseID
			if a.PropertyID == nil { // property from the lease
				if leases, err := s.st.Leases(ctx); err == nil {
					for _, l := range leases {
						if l.ID == in.LeaseID {
							pid := l.PropertyID
							a.PropertyID = &pid
						}
					}
				}
			}
		}
		if err := s.st.ApplyHVAssignments(ctx, []store.HVAssign{a}); err != nil {
			s.fail(w, r, err)
			return
		}
		if in.Remember {
			txns, err := s.st.HVTransactions(ctx, time.Time{}, time.Time{})
			if err != nil {
				s.fail(w, r, err)
				return
			}
			for _, t := range txns {
				if t.ID == id && t.MerchantKey != "" {
					if err := s.st.SaveHVRule(ctx, store.HVRule{MerchantKey: t.MerchantKey, PropertyID: a.PropertyID, CostType: a.CostType}); err != nil {
						s.fail(w, r, err)
						return
					}
				}
			}
		}
	}
	if err := hv.Reassign(ctx, s.st); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) hvNK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	pid, _ := strconv.ParseInt(q.Get("property_id"), 10, 64)
	year, _ := strconv.Atoi(q.Get("year"))
	if year < 1900 {
		year = todayUTC().Year() - 1
	}
	d, err := s.loadHV(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var prop *store.Property
	for i := range d.props {
		if d.props[i].ID == pid || (pid == 0 && i == 0) {
			prop = &d.props[i]
			break
		}
	}
	if prop == nil {
		writeJSON(w, 200, map[string]any{"settlement": nil})
		return
	}
	manual, err := s.st.ManualCosts(ctx, prop.ID, year)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	keys, err := s.st.NKKeys(ctx, prop.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st := hv.ComputeSettlement(*prop, d.leases, d.txns, manual, keys, year)
	writeJSON(w, 200, map[string]any{"settlement": st, "property": prop, "manual_costs": manual, "keys": keys})
}

func (s *Server) hvAddManualCost(w http.ResponseWriter, r *http.Request) {
	var c store.ManualCost
	if err := decode(r, &c); err != nil || c.PropertyID == 0 || c.Year < 1900 || c.Amount == 0 {
		bad(w, "Objekt, Jahr, Kostenart und Betrag angeben")
		return
	}
	if ct, ok := hv.TypeOf(c.CostType); !ok || !ct.Umlage {
		bad(w, "Nur umlagefähige Kostenarten")
		return
	}
	id, err := s.st.AddManualCost(r.Context(), c)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (s *Server) hvSetKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PropertyID int64  `json:"property_id"`
		CostType   string `json:"cost_type"`
		Key        string `json:"key"`
	}
	if err := decode(r, &in); err != nil || in.PropertyID == 0 || (in.Key != "flaeche" && in.Key != "personen" && in.Key != "einheiten") {
		bad(w, "Schlüssel: flaeche, personen oder einheiten")
		return
	}
	if err := s.st.SetNKKey(r.Context(), in.PropertyID, in.CostType, in.Key); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) hvReport(w http.ResponseWriter, r *http.Request) {
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if year < 1900 {
		year = todayUTC().Year()
	}
	d, err := s.loadHV(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	reminders, err := s.st.Reminders(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"year": year, "properties": hv.PropertyReport(d.props, d.leases, d.txns, year, todayUTC()),
		"deadlines": hv.Deadlines(d.props, d.leases, d.ledgers, reminders, todayUTC()), "reminders": reminders,
	})
}

func (s *Server) hvAddReminder(w http.ResponseWriter, r *http.Request) {
	var rm store.Reminder
	if err := decode(r, &rm); err != nil || strings.TrimSpace(rm.Title) == "" {
		bad(w, "Titel und Datum angeben")
		return
	}
	if _, err := time.Parse("2006-01-02", rm.DueDate); err != nil {
		bad(w, "Datum angeben")
		return
	}
	id, err := s.st.AddReminder(r.Context(), rm)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (s *Server) hvReminderDone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct{ Done bool }
	if !ok || decode(r, &in) != nil {
		bad(w, "Ungültige Anfrage")
		return
	}
	if err := s.st.SetReminderDone(r.Context(), id, in.Done); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
