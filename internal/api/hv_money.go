package api

import (
	"net/http"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/hv"
	"github.com/AQUI74S/homestead/internal/store"
)

// Property management: rent account transactions and the utility settlement.

// hvTransactions lists rent account transactions, filtered by month or year,
// property, lease, and open=1 for those that still need to be assigned.
func (s *Server) hvTransactions(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	var from, to time.Time
	if m, err := time.Parse(domain.MonthLayout, q.Get("month")); err == nil {
		from, to = m, m.AddDate(0, 1, 0)
	} else if y := queryYear(r, 0); y > 0 {
		from = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		to = from.AddDate(1, 0, 0)
	}
	txns, err := s.st.HVTransactions(r.Context(), from, to)
	if err != nil {
		return err
	}
	pid, lid := queryInt64(r, "property_id"), queryInt64(r, "lease_id")
	open := q.Get("open") == "1"
	out := []store.HVTxn{}
	for _, t := range txns {
		if pid > 0 && (t.PropertyID == nil || *t.PropertyID != pid) {
			continue
		}
		if lid > 0 && (t.LeaseID == nil || *t.LeaseID != lid) {
			continue
		}
		if open && !hv.IsUnassigned(t) {
			continue
		}
		out = append(out, t)
	}
	return reply(w, out)
}

func (s *Server) hvPatchTxn(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var in struct {
		PropertyID int64  `json:"property_id"`
		LeaseID    int64  `json:"lease_id"`
		CostType   string `json:"cost_type"`
		Remember   bool   `json:"remember"` // remember for this payee
		Reset      bool   `json:"reset"`
	}
	if !ok || decode(r, &in) != nil {
		return badRequest("Ungültige Anfrage")
	}
	ctx := r.Context()
	if in.Reset {
		if err := s.st.ApplyHVAssignments(ctx, []store.HVAssign{{ID: id, Source: domain.SourceAuto}}); err != nil {
			return err
		}
		return s.reassignAndConfirm(w, r)
	}
	if _, ok := hv.TypeOf(in.CostType); !ok {
		return badRequest("Unbekannte Kostenart")
	}
	a := store.HVAssign{ID: id, CostType: in.CostType, Source: domain.SourceManual}
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
		return err
	}
	if in.Remember {
		txns, err := s.st.HVTransactions(ctx, time.Time{}, time.Time{})
		if err != nil {
			return err
		}
		for _, t := range txns {
			if t.ID == id && t.MerchantKey != "" {
				if err := s.st.SaveHVRule(ctx, store.HVRule{MerchantKey: t.MerchantKey, PropertyID: a.PropertyID, CostType: a.CostType}); err != nil {
					return err
				}
			}
		}
	}
	return s.reassignAndConfirm(w, r)
}

func (s *Server) reassignAndConfirm(w http.ResponseWriter, r *http.Request) error {
	if err := hv.Reassign(r.Context(), s.st); err != nil {
		return err
	}
	return okReply(w)
}

// hvNK returns the utility settlement (Nebenkostenabrechnung) of a property and year
// (default: the previous year, the one to be settled).
func (s *Server) hvNK(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	d, err := hv.Load(ctx, s.st)
	if err != nil {
		return err
	}
	year := queryYear(r, d.Today.Year()-1)
	prop, ok := d.Property(queryInt64(r, "property_id"))
	if !ok {
		return reply(w, map[string]any{"settlement": nil})
	}
	manual, err := s.st.ManualCosts(ctx, prop.ID, year)
	if err != nil {
		return err
	}
	keys, err := s.st.NKKeys(ctx, prop.ID)
	if err != nil {
		return err
	}
	st := hv.ComputeSettlement(*prop, d.Leases, d.Txns, manual, keys, year)
	return reply(w, map[string]any{"settlement": st, "property": prop, "manual_costs": manual, "keys": keys})
}

func (s *Server) hvAddManualCost(w http.ResponseWriter, r *http.Request) error {
	var c store.ManualCost
	if err := decode(r, &c); err != nil || c.PropertyID == 0 || c.Year < minYear || c.Amount == 0 {
		return badRequest("Objekt, Jahr, Kostenart und Betrag angeben")
	}
	if ct, ok := hv.TypeOf(c.CostType); !ok || !ct.Umlage {
		return badRequest("Nur umlagefähige Kostenarten")
	}
	id, err := s.st.AddManualCost(r.Context(), c)
	if err != nil {
		return err
	}
	return withID(w, http.StatusOK, id)
}

func (s *Server) hvSetKey(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		PropertyID int64  `json:"property_id"`
		CostType   string `json:"cost_type"`
		Key        string `json:"key"`
	}
	if err := decode(r, &in); err != nil || in.PropertyID == 0 || !hv.ValidKey(in.Key) {
		return badRequest("Schlüssel: flaeche, personen oder einheiten")
	}
	if err := s.st.SetNKKey(r.Context(), in.PropertyID, in.CostType, in.Key); err != nil {
		return err
	}
	return okReply(w)
}
