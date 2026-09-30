package api

import (
	"context"
	"net/http"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/hv"
)

// Property management (Hausverwaltung): overview, reports and the lease detail.
// Editing is in hv_edit.go, the rent account and utility costs in hv_money.go.

func (s *Server) hvMeta(w http.ResponseWriter, r *http.Request) error {
	d, err := hv.Load(r.Context(), s.st)
	if err != nil {
		return err
	}
	return reply(w, map[string]any{"cost_types": hv.CostTypes, "properties": d.Properties, "leases": d.Leases})
}

func (s *Server) hvOverview(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	d, err := hv.Load(ctx, s.st)
	if err != nil {
		return err
	}
	reminders, err := s.st.Reminders(ctx)
	if err != nil {
		return err
	}
	accts, err := s.st.AccountsIn(ctx, domain.BookProperty)
	if err != nil {
		return err
	}
	return reply(w, hv.BuildOverview(d, r.URL.Query().Get("month"), reminders, accts))
}

func (s *Server) hvReport(w http.ResponseWriter, r *http.Request) error {
	d, err := hv.Load(r.Context(), s.st)
	if err != nil {
		return err
	}
	year := queryYear(r, d.Today.Year())
	reminders, err := s.st.Reminders(r.Context())
	if err != nil {
		return err
	}
	return reply(w, map[string]any{
		"year": year, "properties": hv.PropertyReport(d.Properties, d.Leases, d.Txns, year, d.Today),
		"deadlines": hv.Deadlines(d.Properties, d.Leases, d.Ledgers, reminders, d.Today), "reminders": reminders,
	})
}

func (s *Server) hvLease(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	d, err := hv.Load(r.Context(), s.st)
	if err != nil {
		return err
	}
	l, ok := d.Lease(id)
	if !ok {
		return notFound("Mietvertrag nicht gefunden")
	}
	return reply(w, map[string]any{"lease": l, "ledger": d.Ledgers[id]})
}

// hvDelete returns a handler that deletes a record by its {id} and re-assigns the
// rent account transactions.
func (s *Server) hvDelete(del func(context.Context, int64) error) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := requireID(r)
		if err != nil {
			return err
		}
		if err := del(r.Context(), id); err != nil {
			return err
		}
		_ = hv.Reassign(r.Context(), s.st)
		return okReply(w)
	}
}
