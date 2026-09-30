package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/budget"
	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

func (s *Server) recurring(w http.ResponseWriter, r *http.Request) error {
	_, rec, err := budget.HouseholdRecurring(r.Context(), s.st)
	if err != nil {
		return err
	}
	return reply(w, rec)
}

func (s *Server) patchRecurring(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var in struct {
		Status domain.RecurringStatus
		Kind   domain.Kind
	}
	if !ok || decode(r, &in) != nil || (in.Kind != "" && !in.Kind.Valid()) || (in.Status != "" && !in.Status.Valid()) {
		return badRequest("Ungültige Art oder Status")
	}
	if err := s.st.UpdateRecurring(r.Context(), id, in.Status, in.Kind); err != nil {
		return err
	}
	return okReply(w)
}

// createRecurring adds a contract by hand, e.g. an annual insurance that has not
// been paid via a connected account yet.
func (s *Server) createRecurring(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Label      string       `json:"label"`
		Amount     int64        `json:"amount"` // cents, positive
		CycleDays  domain.Cycle `json:"cycle_days"`
		NextDate   string       `json:"next_date"`
		Kind       domain.Kind  `json:"kind"`
		CategoryID int64        `json:"category_id"`
		AccountID  int64        `json:"account_id"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Label) == "" || in.Amount <= 0 {
		return badRequest("Name und Betrag angeben")
	}
	if !in.CycleDays.Valid() {
		return badRequest("Rhythmus wählen")
	}
	next, err := time.Parse(domain.DateLayout, in.NextDate)
	if err != nil {
		return badRequest("Nächste Fälligkeit als Datum angeben")
	}
	if !in.Kind.Valid() {
		in.Kind = domain.KindFixedCost
	}
	dir := domain.DirectionOut
	if in.Kind == domain.KindIncome {
		dir = domain.DirectionIn
	}
	m := store.ManualRecurring{Label: strings.TrimSpace(in.Label), Direction: dir, Kind: in.Kind,
		CycleDays: in.CycleDays, AmountCents: in.Amount, NextDate: next}
	if in.CategoryID > 0 {
		m.CategoryID = &in.CategoryID
	}
	if in.AccountID > 0 {
		m.AccountID = &in.AccountID
	}
	id, err := s.st.CreateManualRecurring(r.Context(), m)
	if err != nil {
		return err
	}
	return withID(w, http.StatusCreated, id)
}

func (s *Server) deleteRecurring(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	if err := s.st.DeleteManualRecurring(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return badRequest("Nur von Hand angelegte Verträge lassen sich löschen. Erkannte Zahlungen bitte ignorieren.")
		}
		return err
	}
	return okReply(w)
}
